package api

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"time"

	"github.com/jackc/pgx/v5"
)

var validMoods = map[string]bool{"Great": true, "Good": true, "Average": true, "Bad": true}

type progressEntryResponse struct {
	ID                string   `json:"id"`
	Date              string   `json:"date"`
	CompletedHabitIDs []string `json:"completed_habit_ids"`
	LearningHours     float64  `json:"learning_hours"`
	Mood              string   `json:"mood"`
	Notes             string   `json:"notes"`
}

// rowScanner is satisfied by both pgx.Row (QueryRow) and pgx.Rows (after
// Next()), letting scanProgressEntry serve both single-row and
// multi-row callers.
type rowScanner interface {
	Scan(dest ...any) error
}

// scanProgressEntry scans a (id, date, completed_habits, learning_hours,
// mood, notes) row shared by every handler that reads/writes daily_progress.
func scanProgressEntry(sc rowScanner) (progressEntryResponse, error) {
	var entry progressEntryResponse
	var date time.Time
	var completedRaw []byte
	var notes *string

	if err := sc.Scan(&entry.ID, &date, &completedRaw, &entry.LearningHours, &entry.Mood, &notes); err != nil {
		return progressEntryResponse{}, err
	}
	entry.Date = date.Format("2006-01-02")
	if notes != nil {
		entry.Notes = *notes
	}
	if err := json.Unmarshal(completedRaw, &entry.CompletedHabitIDs); err != nil {
		return progressEntryResponse{}, fmt.Errorf("unmarshal completed_habits: %w", err)
	}
	if entry.CompletedHabitIDs == nil {
		entry.CompletedHabitIDs = []string{}
	}
	return entry, nil
}

const progressColumns = `id, date, completed_habits, learning_hours, mood, notes`

func (s *Server) handleGetProgress(w http.ResponseWriter, r *http.Request) {
	userID, _ := userIDFromContext(r.Context())
	date := r.PathValue("date")

	row := s.pool.QueryRow(r.Context(),
		`SELECT `+progressColumns+` FROM daily_progress WHERE user_id = $1 AND date = $2`, userID, date)
	entry, err := scanProgressEntry(row)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			writeError(w, http.StatusNotFound, "no progress entry for that date")
			return
		}
		writeError(w, http.StatusInternalServerError, "failed to load progress")
		return
	}

	writeJSON(w, http.StatusOK, entry)
}

func (s *Server) handleListProgress(w http.ResponseWriter, r *http.Request) {
	userID, _ := userIDFromContext(r.Context())
	from := r.URL.Query().Get("from")
	to := r.URL.Query().Get("to")

	query := `SELECT ` + progressColumns + ` FROM daily_progress WHERE user_id = $1`
	args := []any{userID}
	if from != "" {
		args = append(args, from)
		query += fmt.Sprintf(" AND date >= $%d", len(args))
	}
	if to != "" {
		args = append(args, to)
		query += fmt.Sprintf(" AND date <= $%d", len(args))
	}
	query += " ORDER BY date"

	rows, err := s.pool.Query(r.Context(), query, args...)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to load progress")
		return
	}
	defer rows.Close()

	entries := []progressEntryResponse{}
	for rows.Next() {
		entry, err := scanProgressEntry(rows)
		if err != nil {
			writeError(w, http.StatusInternalServerError, "failed to load progress")
			return
		}
		entries = append(entries, entry)
	}
	if err := rows.Err(); err != nil {
		writeError(w, http.StatusInternalServerError, "failed to load progress")
		return
	}

	writeJSON(w, http.StatusOK, map[string]any{"entries": entries, "total": len(entries)})
}

type upsertProgressRequest struct {
	CompletedHabitIDs []string `json:"completed_habit_ids"`
	LearningHours     float64  `json:"learning_hours"`
	Mood              string   `json:"mood"`
	Notes             string   `json:"notes"`
}

type progressWriteResponse struct {
	Entry            progressEntryResponse `json:"entry"`
	XP               int                   `json:"xp"`
	Level            int                   `json:"level"`
	XPIntoLevel      int                   `json:"xp_into_level"`
	XPForNextLevel   int                   `json:"xp_for_next_level"`
	CurrentStreak    int                   `json:"current_streak"`
	ShieldsRemaining int                   `json:"shields_remaining"`
}

// handleUpsertProgress is the single write path for a day's habit
// check-in. XP is granted EXCLUSIVELY here, computed server-side from the
// delta between the old and new row in the same transaction as the write --
// the client never sends an XP amount (the old Supabase version trusted a
// client-computed XP delta, forgeable via devtools).
func (s *Server) handleUpsertProgress(w http.ResponseWriter, r *http.Request) {
	userID, _ := userIDFromContext(r.Context())
	date := r.PathValue("date")
	if _, err := time.Parse("2006-01-02", date); err != nil {
		writeError(w, http.StatusBadRequest, "date must be YYYY-MM-DD")
		return
	}

	var req upsertProgressRequest
	if err := decodeJSON(r, &req); err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	if req.CompletedHabitIDs == nil {
		req.CompletedHabitIDs = []string{}
	}
	if req.Mood == "" {
		req.Mood = "Average"
	}
	if !validMoods[req.Mood] {
		writeError(w, http.StatusBadRequest, "mood must be one of Great, Good, Average, Bad")
		return
	}
	if req.LearningHours < 0 {
		writeError(w, http.StatusBadRequest, "learning_hours must not be negative")
		return
	}

	completedJSON, err := json.Marshal(req.CompletedHabitIDs)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to save progress")
		return
	}

	var resp progressWriteResponse
	txErr := withTx(r.Context(), s.pool, func(tx pgx.Tx) error {
		var oldCompletedCount int
		var oldLearningHours float64
		existingErr := tx.QueryRow(r.Context(),
			`SELECT jsonb_array_length(completed_habits), learning_hours
			 FROM daily_progress WHERE user_id = $1 AND date = $2`, userID, date).
			Scan(&oldCompletedCount, &oldLearningHours)
		if existingErr != nil && !errors.Is(existingErr, pgx.ErrNoRows) {
			return fmt.Errorf("load existing progress: %w", existingErr)
		}

		row := tx.QueryRow(r.Context(),
			`INSERT INTO daily_progress (user_id, date, completed_habits, learning_hours, mood, notes)
			 VALUES ($1,$2,$3,$4,$5,$6)
			 ON CONFLICT (user_id, date) DO UPDATE SET
			   completed_habits = EXCLUDED.completed_habits,
			   learning_hours = EXCLUDED.learning_hours,
			   mood = EXCLUDED.mood,
			   notes = EXCLUDED.notes,
			   updated_at = now()
			 RETURNING `+progressColumns,
			userID, date, completedJSON, req.LearningHours, req.Mood, nullableString(req.Notes))

		entry, scanErr := scanProgressEntry(row)
		if scanErr != nil {
			return fmt.Errorf("upsert progress: %w", scanErr)
		}

		xpDelta := (len(req.CompletedHabitIDs)-oldCompletedCount)*10 + int((req.LearningHours-oldLearningHours)*20)

		var newXP int
		if xpErr := tx.QueryRow(r.Context(),
			`UPDATE users SET xp = GREATEST(0, xp + $2), updated_at = now() WHERE id = $1 RETURNING xp`,
			userID, xpDelta).Scan(&newXP); xpErr != nil {
			return fmt.Errorf("update xp: %w", xpErr)
		}

		streak, streakErr := computeStreakWith(r.Context(), tx, userID, true)
		if streakErr != nil {
			return streakErr
		}

		level, xpIntoLevel, xpForNextLevel := levelFromXP(newXP)
		resp = progressWriteResponse{
			Entry: entry, XP: newXP, Level: level,
			XPIntoLevel: xpIntoLevel, XPForNextLevel: xpForNextLevel,
			CurrentStreak: streak.CurrentStreak, ShieldsRemaining: streak.ShieldsRemaining,
		}
		return nil
	})
	if txErr != nil {
		writeError(w, http.StatusInternalServerError, "failed to save progress")
		return
	}

	writeJSON(w, http.StatusOK, resp)
}

func (s *Server) handleDeleteProgress(w http.ResponseWriter, r *http.Request) {
	userID, _ := userIDFromContext(r.Context())
	date := r.PathValue("date")

	var resp struct {
		XP               int `json:"xp"`
		Level            int `json:"level"`
		CurrentStreak    int `json:"current_streak"`
		ShieldsRemaining int `json:"shields_remaining"`
	}

	err := withTx(r.Context(), s.pool, func(tx pgx.Tx) error {
		var completedCount int
		var learningHours float64
		delErr := tx.QueryRow(r.Context(),
			`DELETE FROM daily_progress WHERE user_id = $1 AND date = $2
			 RETURNING jsonb_array_length(completed_habits), learning_hours`, userID, date).
			Scan(&completedCount, &learningHours)
		if delErr != nil {
			if errors.Is(delErr, pgx.ErrNoRows) {
				return errNotFound
			}
			return fmt.Errorf("delete progress: %w", delErr)
		}

		// Reverses exactly the XP that entry granted -- the old app didn't
		// do this, making create+delete a free-XP exploit.
		xpDelta := -(completedCount*10 + int(learningHours*20))

		var newXP int
		if xpErr := tx.QueryRow(r.Context(),
			`UPDATE users SET xp = GREATEST(0, xp + $2), updated_at = now() WHERE id = $1 RETURNING xp`,
			userID, xpDelta).Scan(&newXP); xpErr != nil {
			return fmt.Errorf("update xp: %w", xpErr)
		}

		streak, streakErr := computeStreakWith(r.Context(), tx, userID, true)
		if streakErr != nil {
			return streakErr
		}

		level, _, _ := levelFromXP(newXP)
		resp.XP, resp.Level = newXP, level
		resp.CurrentStreak, resp.ShieldsRemaining = streak.CurrentStreak, streak.ShieldsRemaining
		return nil
	})
	if errors.Is(err, errNotFound) {
		writeError(w, http.StatusNotFound, "progress entry not found")
		return
	}
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to delete progress")
		return
	}

	writeJSON(w, http.StatusOK, resp)
}
