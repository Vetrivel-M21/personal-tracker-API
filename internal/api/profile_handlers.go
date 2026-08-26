package api

import (
	"net/http"
	"strings"
	"time"
)

// meResponse is the shape returned for the caller's own profile, from
// signup, login, and GET /api/me.
type meResponse struct {
	ID             string `json:"id"`
	DisplayName    string `json:"display_name"`
	XP             int    `json:"xp"`
	Level          int    `json:"level"`
	XPIntoLevel    int    `json:"xp_into_level"`
	XPForNextLevel int    `json:"xp_for_next_level"`
	CreatedAt      string `json:"created_at"`
}

func (s *Server) handleGetMe(w http.ResponseWriter, r *http.Request) {
	userID, _ := userIDFromContext(r.Context())

	var displayName string
	var xp int
	var createdAt time.Time
	err := s.pool.QueryRow(r.Context(),
		`SELECT display_name, xp, created_at FROM users WHERE id = $1`, userID).
		Scan(&displayName, &xp, &createdAt)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to load profile")
		return
	}

	level, xpIntoLevel, xpForNextLevel := levelFromXP(xp)
	writeJSON(w, http.StatusOK, meResponse{
		ID: userID, DisplayName: displayName, XP: xp,
		Level: level, XPIntoLevel: xpIntoLevel, XPForNextLevel: xpForNextLevel,
		CreatedAt: createdAt.Format(time.RFC3339),
	})
}

type updateMeRequest struct {
	DisplayName string `json:"display_name"`
}

func (s *Server) handleUpdateMe(w http.ResponseWriter, r *http.Request) {
	userID, _ := userIDFromContext(r.Context())

	var req updateMeRequest
	if err := decodeJSON(r, &req); err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	if err := validateDisplayName(req.DisplayName); err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	displayName := strings.TrimSpace(req.DisplayName)

	var xp int
	var createdAt time.Time
	err := s.pool.QueryRow(r.Context(),
		`UPDATE users SET display_name = $2, updated_at = now() WHERE id = $1 RETURNING xp, created_at`,
		userID, displayName).Scan(&xp, &createdAt)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to update profile")
		return
	}

	level, xpIntoLevel, xpForNextLevel := levelFromXP(xp)
	writeJSON(w, http.StatusOK, meResponse{
		ID: userID, DisplayName: displayName, XP: xp,
		Level: level, XPIntoLevel: xpIntoLevel, XPForNextLevel: xpForNextLevel,
		CreatedAt: createdAt.Format(time.RFC3339),
	})
}

type changePasswordRequest struct {
	CurrentPassword string `json:"current_password"`
	NewPassword     string `json:"new_password"`
}

func (s *Server) handleChangePassword(w http.ResponseWriter, r *http.Request) {
	userID, _ := userIDFromContext(r.Context())

	var req changePasswordRequest
	if err := decodeJSON(r, &req); err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	if err := validatePassword(req.NewPassword); err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}

	var hash string
	if err := s.pool.QueryRow(r.Context(), `SELECT password_hash FROM users WHERE id = $1`, userID).Scan(&hash); err != nil {
		writeError(w, http.StatusInternalServerError, "failed to change password")
		return
	}
	if !verifyPassword(hash, req.CurrentPassword) {
		writeError(w, http.StatusUnauthorized, "current password is incorrect")
		return
	}

	newHash, err := hashPassword(req.NewPassword)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to change password")
		return
	}
	if _, err := s.pool.Exec(r.Context(),
		`UPDATE users SET password_hash = $2, updated_at = now() WHERE id = $1`, userID, newHash); err != nil {
		writeError(w, http.StatusInternalServerError, "failed to change password")
		return
	}

	writeJSON(w, http.StatusNoContent, nil)
}
