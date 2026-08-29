package api

import (
	"fmt"
	"net/http"
	"time"
)

type exportProfile struct {
	Username    string  `json:"username"`
	DisplayName string  `json:"display_name"`
	Email       *string `json:"email"`
	DateOfBirth *string `json:"date_of_birth"`
	Age         *int    `json:"age"`
	XP          int     `json:"xp"`
	Level       int     `json:"level"`
	HasPassword bool    `json:"has_password"`
	CreatedAt   string  `json:"created_at"`
}

// handleExportData assembles everything the app stores about the caller
// into one JSON document. Every piece is built from helpers that already
// exist for the corresponding "get my X" endpoints (loadSplitDetail,
// loadSessionDetail, scanProgressEntry) rather than new query logic.
func (s *Server) handleExportData(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	userID, _ := userIDFromContext(ctx)

	var profile exportProfile
	var email *string
	var dob *time.Time
	var createdAt time.Time
	if err := s.pool.QueryRow(ctx,
		`SELECT username, display_name, email, date_of_birth, xp, has_password, created_at
		 FROM users WHERE id = $1`, userID).
		Scan(&profile.Username, &profile.DisplayName, &email, &dob, &profile.XP, &profile.HasPassword, &createdAt); err != nil {
		writeError(w, http.StatusInternalServerError, "failed to export data")
		return
	}
	profile.Email = email
	profile.DateOfBirth, profile.Age = formatDOBAndAge(dob)
	profile.Level, _, _ = levelFromXP(profile.XP)
	profile.CreatedAt = createdAt.Format(time.RFC3339)

	habitRows, err := s.pool.Query(ctx,
		`SELECT id, name, color, icon, created_at FROM habits WHERE user_id = $1 ORDER BY created_at`, userID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to export data")
		return
	}
	habits := []habitResponse{}
	for habitRows.Next() {
		var h habitResponse
		var hCreatedAt time.Time
		if err := habitRows.Scan(&h.ID, &h.Name, &h.Color, &h.Icon, &hCreatedAt); err != nil {
			habitRows.Close()
			writeError(w, http.StatusInternalServerError, "failed to export data")
			return
		}
		h.CreatedAt = hCreatedAt.Format(time.RFC3339)
		habits = append(habits, h)
	}
	habitErr := habitRows.Err()
	habitRows.Close()
	if habitErr != nil {
		writeError(w, http.StatusInternalServerError, "failed to export data")
		return
	}

	progressRows, err := s.pool.Query(ctx,
		`SELECT `+progressColumns+` FROM daily_progress WHERE user_id = $1 ORDER BY date`, userID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to export data")
		return
	}
	dailyProgress := []progressEntryResponse{}
	for progressRows.Next() {
		entry, err := scanProgressEntry(progressRows)
		if err != nil {
			progressRows.Close()
			writeError(w, http.StatusInternalServerError, "failed to export data")
			return
		}
		dailyProgress = append(dailyProgress, entry)
	}
	progressErr := progressRows.Err()
	progressRows.Close()
	if progressErr != nil {
		writeError(w, http.StatusInternalServerError, "failed to export data")
		return
	}

	splitIDRows, err := s.pool.Query(ctx,
		`SELECT id FROM workout_splits WHERE user_id = $1 ORDER BY created_at`, userID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to export data")
		return
	}
	var splitIDs []string
	for splitIDRows.Next() {
		var id string
		if err := splitIDRows.Scan(&id); err != nil {
			splitIDRows.Close()
			writeError(w, http.StatusInternalServerError, "failed to export data")
			return
		}
		splitIDs = append(splitIDs, id)
	}
	splitIDErr := splitIDRows.Err()
	splitIDRows.Close()
	if splitIDErr != nil {
		writeError(w, http.StatusInternalServerError, "failed to export data")
		return
	}
	splits := []splitResponse{}
	for _, id := range splitIDs {
		detail, err := loadSplitDetail(ctx, s.pool, id)
		if err != nil {
			writeError(w, http.StatusInternalServerError, "failed to export data")
			return
		}
		splits = append(splits, detail)
	}

	sessionIDRows, err := s.pool.Query(ctx,
		`SELECT id FROM workout_sessions WHERE user_id = $1 ORDER BY session_date DESC, created_at DESC`, userID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to export data")
		return
	}
	var sessionIDs []string
	for sessionIDRows.Next() {
		var id string
		if err := sessionIDRows.Scan(&id); err != nil {
			sessionIDRows.Close()
			writeError(w, http.StatusInternalServerError, "failed to export data")
			return
		}
		sessionIDs = append(sessionIDs, id)
	}
	sessionIDErr := sessionIDRows.Err()
	sessionIDRows.Close()
	if sessionIDErr != nil {
		writeError(w, http.StatusInternalServerError, "failed to export data")
		return
	}
	sessions := []sessionResponse{}
	for _, id := range sessionIDs {
		detail, err := loadSessionDetail(ctx, s.pool, id)
		if err != nil {
			writeError(w, http.StatusInternalServerError, "failed to export data")
			return
		}
		sessions = append(sessions, detail)
	}

	var shieldCount int
	if err := s.pool.QueryRow(ctx,
		`SELECT shield_count FROM streak_shields WHERE user_id = $1`, userID).Scan(&shieldCount); err != nil {
		shieldCount = 0
	}

	var kudosReceived int
	if err := s.pool.QueryRow(ctx,
		`SELECT COUNT(*) FROM kudos WHERE to_user_id = $1`, userID).Scan(&kudosReceived); err != nil {
		writeError(w, http.StatusInternalServerError, "failed to export data")
		return
	}

	filename := fmt.Sprintf("aura-export-%s.json", time.Now().Format("2006-01-02"))
	w.Header().Set("Content-Disposition", fmt.Sprintf(`attachment; filename="%s"`, filename))
	writeJSON(w, http.StatusOK, map[string]any{
		"exported_at":          time.Now().Format(time.RFC3339),
		"profile":              profile,
		"habits":               habits,
		"daily_progress":       dailyProgress,
		"workout_splits":       splits,
		"workout_sessions":     sessions,
		"streak_shields":       shieldCount,
		"kudos_received_count": kudosReceived,
	})
}
