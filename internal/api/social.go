package api

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"sort"
	"strings"

	"github.com/jackc/pgx/v5"
)

// socialUserRow is the shared shape for leaderboard rows and directory
// entries. Rank is only populated for the leaderboard. Username is never
// included -- public views only ever expose display_name.
type socialUserRow struct {
	Rank            int     `json:"rank,omitempty"`
	UserID          string  `json:"user_id"`
	DisplayName     string  `json:"display_name"`
	Level           int     `json:"level"`
	XP              int     `json:"xp"`
	CurrentStreak   int     `json:"current_streak"`
	ActiveSplitName *string `json:"active_split_name"`
	KudosCount      int     `json:"kudos_count"`
}

// fetchAllUsersWithStats loads every registered user with their level and a
// read-only streak peek (consumeShields=false -- viewing the leaderboard or
// community directory must never spend anyone's shields). At the small
// scale this app targets (tens of users, not thousands), computing streak
// per user in a Go loop is simpler and fast enough; sorting/pagination then
// happens in Go too since "streak" isn't a plain sortable column.
func (s *Server) fetchAllUsersWithStats(ctx context.Context, callerID string) ([]socialUserRow, error) {
	rows, err := s.pool.Query(ctx,
		`SELECT u.id, u.display_name, u.xp, ws.name,
		        (SELECT COUNT(*) FROM kudos k WHERE k.to_user_id = u.id)
		 FROM users u
		 LEFT JOIN workout_splits ws ON ws.user_id = u.id AND ws.is_active
		 WHERE u.profile_private = false OR u.id = $1`, callerID)
	if err != nil {
		return nil, fmt.Errorf("load users: %w", err)
	}
	defer rows.Close()

	var result []socialUserRow
	for rows.Next() {
		var row socialUserRow
		if err := rows.Scan(&row.UserID, &row.DisplayName, &row.XP, &row.ActiveSplitName, &row.KudosCount); err != nil {
			return nil, fmt.Errorf("scan user: %w", err)
		}
		result = append(result, row)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}

	for i := range result {
		level, _, _ := levelFromXP(result[i].XP)
		result[i].Level = level

		streak, err := computeStreak(ctx, s.pool, result[i].UserID, false)
		if err != nil {
			return nil, fmt.Errorf("compute streak for %s: %w", result[i].UserID, err)
		}
		result[i].CurrentStreak = streak.CurrentStreak
	}

	return result, nil
}

func (s *Server) handleLeaderboard(w http.ResponseWriter, r *http.Request) {
	sortBy := r.URL.Query().Get("sort")
	limit := parseIntOrDefault(r.URL.Query().Get("limit"), 25, 1, 200)
	offset := parseIntOrDefault(r.URL.Query().Get("offset"), 0, 0, 1_000_000)

	callerID, _ := userIDFromContext(r.Context())
	users, err := s.fetchAllUsersWithStats(r.Context(), callerID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to load leaderboard")
		return
	}

	if sortBy == "streak" {
		sort.SliceStable(users, func(i, j int) bool { return users[i].CurrentStreak > users[j].CurrentStreak })
	} else {
		sort.SliceStable(users, func(i, j int) bool { return users[i].XP > users[j].XP })
	}
	for i := range users {
		users[i].Rank = i + 1
	}

	writeJSON(w, http.StatusOK, map[string]any{
		"entries": paginate(users, limit, offset),
		"total":   len(users),
	})
}

func (s *Server) handleListUsers(w http.ResponseWriter, r *http.Request) {
	query := strings.ToLower(strings.TrimSpace(r.URL.Query().Get("query")))
	limit := parseIntOrDefault(r.URL.Query().Get("limit"), 20, 1, 200)
	offset := parseIntOrDefault(r.URL.Query().Get("offset"), 0, 0, 1_000_000)

	callerID, _ := userIDFromContext(r.Context())
	users, err := s.fetchAllUsersWithStats(r.Context(), callerID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to load users")
		return
	}

	if query != "" {
		var filtered []socialUserRow
		for _, u := range users {
			if strings.Contains(strings.ToLower(u.DisplayName), query) {
				filtered = append(filtered, u)
			}
		}
		users = filtered
	}

	sort.SliceStable(users, func(i, j int) bool {
		return strings.ToLower(users[i].DisplayName) < strings.ToLower(users[j].DisplayName)
	})

	writeJSON(w, http.StatusOK, map[string]any{
		"entries": paginate(users, limit, offset),
		"total":   len(users),
	})
}

func (s *Server) handleUserHabitsSummary(w http.ResponseWriter, r *http.Request) {
	targetID := r.PathValue("id")
	callerID, _ := userIDFromContext(r.Context())

	var displayName string
	var xp int
	var profilePrivate, hideHabits bool
	var activeSplitName *string
	err := s.pool.QueryRow(r.Context(),
		`SELECT u.display_name, u.xp, u.profile_private, u.hide_habits, ws.name
		 FROM users u
		 LEFT JOIN workout_splits ws ON ws.user_id = u.id AND ws.is_active
		 WHERE u.id = $1`, targetID).Scan(&displayName, &xp, &profilePrivate, &hideHabits, &activeSplitName)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			writeError(w, http.StatusNotFound, "user not found")
			return
		}
		writeError(w, http.StatusInternalServerError, "failed to load user")
		return
	}
	if profilePrivate && targetID != callerID {
		writeError(w, http.StatusNotFound, "user not found")
		return
	}

	streak, err := computeStreak(r.Context(), s.pool, targetID, false)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to load user")
		return
	}

	var kudosCount int
	if err := s.pool.QueryRow(r.Context(),
		`SELECT COUNT(*) FROM kudos WHERE to_user_id = $1`, targetID).Scan(&kudosCount); err != nil {
		writeError(w, http.StatusInternalServerError, "failed to load user")
		return
	}

	type habitSummary struct {
		ID    string `json:"id"`
		Name  string `json:"name"`
		Color string `json:"color"`
		Icon  string `json:"icon"`
	}
	habits := []habitSummary{}
	habitsHidden := hideHabits && targetID != callerID
	if !habitsHidden {
		rows, err := s.pool.Query(r.Context(),
			`SELECT id, name, color, icon FROM habits WHERE user_id = $1 ORDER BY created_at`, targetID)
		if err != nil {
			writeError(w, http.StatusInternalServerError, "failed to load habits")
			return
		}
		defer rows.Close()

		for rows.Next() {
			var h habitSummary
			if err := rows.Scan(&h.ID, &h.Name, &h.Color, &h.Icon); err != nil {
				writeError(w, http.StatusInternalServerError, "failed to load habits")
				return
			}
			habits = append(habits, h)
		}
		if err := rows.Err(); err != nil {
			writeError(w, http.StatusInternalServerError, "failed to load habits")
			return
		}
	}

	level, _, _ := levelFromXP(xp)
	writeJSON(w, http.StatusOK, map[string]any{
		"display_name":      displayName,
		"level":             level,
		"xp":                xp,
		"current_streak":    streak.CurrentStreak,
		"active_split_name": activeSplitName,
		"habits":            habits,
		"habits_hidden":     habitsHidden,
		"kudos_count":       kudosCount,
	})
}

// handleGiveKudos records the caller sending kudos to another hunter, once
// per pair per day (enforced by the kudos_daily_unique_idx unique index --
// a violation there becomes a friendly cooldown message rather than a raw
// DB error).
func (s *Server) handleGiveKudos(w http.ResponseWriter, r *http.Request) {
	targetID := r.PathValue("id")
	callerID, _ := userIDFromContext(r.Context())

	if targetID == callerID {
		writeError(w, http.StatusBadRequest, "you can't send kudos to yourself")
		return
	}

	var profilePrivate bool
	err := s.pool.QueryRow(r.Context(),
		`SELECT profile_private FROM users WHERE id = $1`, targetID).Scan(&profilePrivate)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			writeError(w, http.StatusNotFound, "user not found")
			return
		}
		writeError(w, http.StatusInternalServerError, "failed to send kudos")
		return
	}
	if profilePrivate {
		writeError(w, http.StatusNotFound, "user not found")
		return
	}

	if _, err := s.pool.Exec(r.Context(),
		`INSERT INTO kudos (from_user_id, to_user_id) VALUES ($1, $2)`, callerID, targetID); err != nil {
		if isUniqueViolation(err) {
			writeError(w, http.StatusConflict, "you already sent kudos to this hunter today")
			return
		}
		writeError(w, http.StatusInternalServerError, "failed to send kudos")
		return
	}

	var kudosCount int
	if err := s.pool.QueryRow(r.Context(),
		`SELECT COUNT(*) FROM kudos WHERE to_user_id = $1`, targetID).Scan(&kudosCount); err != nil {
		writeError(w, http.StatusInternalServerError, "failed to send kudos")
		return
	}

	writeJSON(w, http.StatusCreated, map[string]any{"kudos_count": kudosCount})
}
