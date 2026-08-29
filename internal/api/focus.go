package api

import (
	"net/http"
	"time"
)

var validFocusSessionTypes = map[string]bool{"Focus": true, "Meditation": true}

type focusSessionResponse struct {
	ID              string `json:"id"`
	SessionType     string `json:"session_type"`
	DurationMinutes int    `json:"duration_minutes"`
	CompletedAt     string `json:"completed_at"`
}

type logFocusSessionRequest struct {
	SessionType     string `json:"session_type"`
	DurationMinutes int    `json:"duration_minutes"`
}

func (s *Server) handleLogFocusSession(w http.ResponseWriter, r *http.Request) {
	userID, _ := userIDFromContext(r.Context())

	var req logFocusSessionRequest
	if err := decodeJSON(r, &req); err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	if !validFocusSessionTypes[req.SessionType] {
		writeError(w, http.StatusBadRequest, "session_type must be Focus or Meditation")
		return
	}
	if req.DurationMinutes < 1 || req.DurationMinutes > 180 {
		writeError(w, http.StatusBadRequest, "duration_minutes must be between 1 and 180")
		return
	}

	var session focusSessionResponse
	var completedAt time.Time
	if err := s.pool.QueryRow(r.Context(),
		`INSERT INTO focus_sessions (user_id, session_type, duration_minutes)
		 VALUES ($1, $2, $3) RETURNING id, session_type, duration_minutes, completed_at`,
		userID, req.SessionType, req.DurationMinutes).
		Scan(&session.ID, &session.SessionType, &session.DurationMinutes, &completedAt); err != nil {
		writeError(w, http.StatusInternalServerError, "failed to log session")
		return
	}
	session.CompletedAt = completedAt.Format(time.RFC3339)

	var totalThisWeek int
	if err := s.pool.QueryRow(r.Context(),
		`SELECT COALESCE(SUM(duration_minutes), 0) FROM focus_sessions
		 WHERE user_id = $1 AND completed_at >= date_trunc('week', now())`, userID).Scan(&totalThisWeek); err != nil {
		writeError(w, http.StatusInternalServerError, "failed to log session")
		return
	}

	writeJSON(w, http.StatusCreated, map[string]any{
		"session":                 session,
		"total_minutes_this_week": totalThisWeek,
	})
}

func (s *Server) handleListFocusSessions(w http.ResponseWriter, r *http.Request) {
	userID, _ := userIDFromContext(r.Context())
	limit := parseIntOrDefault(r.URL.Query().Get("limit"), 20, 1, 200)
	offset := parseIntOrDefault(r.URL.Query().Get("offset"), 0, 0, 1_000_000)

	rows, err := s.pool.Query(r.Context(),
		`SELECT id, session_type, duration_minutes, completed_at FROM focus_sessions
		 WHERE user_id = $1 ORDER BY completed_at DESC`, userID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to load sessions")
		return
	}
	all := []focusSessionResponse{}
	for rows.Next() {
		var session focusSessionResponse
		var completedAt time.Time
		if err := rows.Scan(&session.ID, &session.SessionType, &session.DurationMinutes, &completedAt); err != nil {
			rows.Close()
			writeError(w, http.StatusInternalServerError, "failed to load sessions")
			return
		}
		session.CompletedAt = completedAt.Format(time.RFC3339)
		all = append(all, session)
	}
	rowErr := rows.Err()
	rows.Close()
	if rowErr != nil {
		writeError(w, http.StatusInternalServerError, "failed to load sessions")
		return
	}

	var totalThisWeek int
	if err := s.pool.QueryRow(r.Context(),
		`SELECT COALESCE(SUM(duration_minutes), 0) FROM focus_sessions
		 WHERE user_id = $1 AND completed_at >= date_trunc('week', now())`, userID).Scan(&totalThisWeek); err != nil {
		writeError(w, http.StatusInternalServerError, "failed to load sessions")
		return
	}

	writeJSON(w, http.StatusOK, map[string]any{
		"entries":                 paginate(all, limit, offset),
		"total":                   len(all),
		"total_minutes_this_week": totalThisWeek,
	})
}
