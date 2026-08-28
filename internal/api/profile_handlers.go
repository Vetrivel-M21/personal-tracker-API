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
	// HasPassword is false for a Google-only account (its password_hash is a
	// random value it can never know) - lets the frontend hide/replace the
	// "Change Password" UI for that account instead of it just failing.
	HasPassword bool `json:"has_password"`
	// DateOfBirth (ISO YYYY-MM-DD) and Age are both null until the user sets
	// a birth date - Age is computed from DateOfBirth on every read rather
	// than stored, so it never goes stale.
	DateOfBirth *string `json:"date_of_birth"`
	Age         *int    `json:"age"`
	// ProfilePrivate hides this account from Community/Leaderboard for
	// everyone but itself; HideHabits keeps the profile visible but omits
	// the habit list from handleUserHabitsSummary for everyone else.
	ProfilePrivate bool   `json:"profile_private"`
	HideHabits     bool   `json:"hide_habits"`
	CreatedAt      string `json:"created_at"`
}

// formatDOBAndAge converts a nullable birth date into the (date_of_birth,
// age) pair every meResponse-building handler needs.
func formatDOBAndAge(dob *time.Time) (*string, *int) {
	if dob == nil {
		return nil, nil
	}
	formatted := dob.Format("2006-01-02")
	age := ageFromDOB(*dob, time.Now())
	return &formatted, &age
}

func (s *Server) handleGetMe(w http.ResponseWriter, r *http.Request) {
	userID, _ := userIDFromContext(r.Context())

	var displayName string
	var xp int
	var hasPassword, profilePrivate, hideHabits bool
	var dob *time.Time
	var createdAt time.Time
	err := s.pool.QueryRow(r.Context(),
		`SELECT display_name, xp, has_password, date_of_birth, profile_private, hide_habits, created_at
		 FROM users WHERE id = $1`, userID).
		Scan(&displayName, &xp, &hasPassword, &dob, &profilePrivate, &hideHabits, &createdAt)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to load profile")
		return
	}

	dateOfBirth, age := formatDOBAndAge(dob)
	level, xpIntoLevel, xpForNextLevel := levelFromXP(xp)
	writeJSON(w, http.StatusOK, meResponse{
		ID: userID, DisplayName: displayName, XP: xp,
		Level: level, XPIntoLevel: xpIntoLevel, XPForNextLevel: xpForNextLevel,
		HasPassword: hasPassword,
		DateOfBirth: dateOfBirth, Age: age,
		ProfilePrivate: profilePrivate, HideHabits: hideHabits,
		CreatedAt: createdAt.Format(time.RFC3339),
	})
}

type updateMeRequest struct {
	DisplayName string `json:"display_name"`
	// DateOfBirth (ISO YYYY-MM-DD), ProfilePrivate, and HideHabits are all
	// optional - nil means "don't change".
	DateOfBirth    *string `json:"date_of_birth"`
	ProfilePrivate *bool   `json:"profile_private"`
	HideHabits     *bool   `json:"hide_habits"`
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

	var dobParam *time.Time
	if req.DateOfBirth != nil {
		parsed, err := time.Parse("2006-01-02", *req.DateOfBirth)
		if err != nil {
			writeError(w, http.StatusBadRequest, "please enter a valid date of birth")
			return
		}
		if err := validateDateOfBirth(parsed); err != nil {
			writeError(w, http.StatusBadRequest, err.Error())
			return
		}
		dobParam = &parsed
	}

	var xp int
	var hasPassword, profilePrivate, hideHabits bool
	var dob *time.Time
	var createdAt time.Time
	err := s.pool.QueryRow(r.Context(),
		`UPDATE users SET display_name = $2, date_of_birth = COALESCE($3, date_of_birth),
		   profile_private = COALESCE($4, profile_private), hide_habits = COALESCE($5, hide_habits),
		   updated_at = now()
		 WHERE id = $1
		 RETURNING xp, has_password, date_of_birth, profile_private, hide_habits, created_at`,
		userID, displayName, dobParam, req.ProfilePrivate, req.HideHabits).
		Scan(&xp, &hasPassword, &dob, &profilePrivate, &hideHabits, &createdAt)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to update profile")
		return
	}

	dateOfBirth, age := formatDOBAndAge(dob)
	level, xpIntoLevel, xpForNextLevel := levelFromXP(xp)
	writeJSON(w, http.StatusOK, meResponse{
		ID: userID, DisplayName: displayName, XP: xp,
		Level: level, XPIntoLevel: xpIntoLevel, XPForNextLevel: xpForNextLevel,
		HasPassword: hasPassword,
		DateOfBirth: dateOfBirth, Age: age,
		ProfilePrivate: profilePrivate, HideHabits: hideHabits,
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
