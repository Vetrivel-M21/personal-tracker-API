package api

import (
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
)

// defaultHabits mirrors the four starter habits the old Supabase trigger
// (handle_new_user / ensureUserProfile) seeded for every new account.
var defaultHabits = []struct {
	Name  string
	Color string
	Icon  string
}{
	{"Workout Routine", "#6366f1", "fa-solid fa-dumbbell"},
	{"Daily Walk", "#10b981", "fa-solid fa-person-walking"},
	{"Education / Skills", "#a855f7", "fa-solid fa-book-open"},
	{"Focus & Discipline", "#f97316", "fa-solid fa-shield-halved"},
}

// authResponse extends meResponse with the access/refresh tokens, returned
// by signup/login/refresh so that clients without a cookie jar (e.g. a
// native mobile app) can capture them and authenticate via a Bearer header.
// GET /api/me returns bare meResponse and never includes tokens.
type authResponse struct {
	meResponse
	AccessToken  string `json:"access_token"`
	RefreshToken string `json:"refresh_token"`
}

type signupRequest struct {
	Username    string `json:"username"`
	Password    string `json:"password"`
	DisplayName string `json:"display_name"`
}

func (s *Server) handleSignup(w http.ResponseWriter, r *http.Request) {
	var req signupRequest
	if err := decodeJSON(r, &req); err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	if err := validateUsername(req.Username); err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	if err := validatePassword(req.Password); err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	if err := validateDisplayName(req.DisplayName); err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	displayName := strings.TrimSpace(req.DisplayName)

	hash, err := hashPassword(req.Password)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to create account")
		return
	}

	var userID, refreshToken string
	var createdAt time.Time
	err = withTx(r.Context(), s.pool, func(tx pgx.Tx) error {
		if insertErr := tx.QueryRow(r.Context(),
			`INSERT INTO users (username, password_hash, display_name) VALUES ($1,$2,$3)
			 RETURNING id, created_at`,
			req.Username, hash, displayName).Scan(&userID, &createdAt); insertErr != nil {
			return insertErr
		}

		for _, h := range defaultHabits {
			if _, habitErr := tx.Exec(r.Context(),
				`INSERT INTO habits (user_id, name, color, icon) VALUES ($1,$2,$3,$4)`,
				userID, h.Name, h.Color, h.Icon); habitErr != nil {
				return fmt.Errorf("seed default habits: %w", habitErr)
			}
		}

		if _, shieldErr := tx.Exec(r.Context(),
			`INSERT INTO streak_shields (user_id, shield_count) VALUES ($1, 0)`, userID); shieldErr != nil {
			return fmt.Errorf("seed streak shields: %w", shieldErr)
		}

		token, sessErr := createSession(r.Context(), tx, userID)
		if sessErr != nil {
			return sessErr
		}
		refreshToken = token
		return nil
	})
	if err != nil {
		if isUniqueViolation(err) {
			writeError(w, http.StatusConflict, "username is already taken")
			return
		}
		writeError(w, http.StatusInternalServerError, "failed to create account")
		return
	}

	accessToken, err := generateAccessToken(s.cfg.JWTSecret, userID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to create account")
		return
	}
	setAuthCookies(w, accessToken, refreshToken)

	level, xpIntoLevel, xpForNextLevel := levelFromXP(0)
	writeJSON(w, http.StatusCreated, authResponse{
		meResponse: meResponse{
			ID: userID, DisplayName: displayName, XP: 0,
			Level: level, XPIntoLevel: xpIntoLevel, XPForNextLevel: xpForNextLevel,
			CreatedAt: createdAt.Format(time.RFC3339),
		},
		AccessToken:  accessToken,
		RefreshToken: refreshToken,
	})
}

type loginRequest struct {
	Username string `json:"username"`
	Password string `json:"password"`
}

func (s *Server) handleLogin(w http.ResponseWriter, r *http.Request) {
	var req loginRequest
	if err := decodeJSON(r, &req); err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}

	var userID, hash, displayName string
	var xp int
	var createdAt time.Time
	err := s.pool.QueryRow(r.Context(),
		`SELECT id, password_hash, display_name, xp, created_at FROM users WHERE lower(username) = lower($1)`,
		req.Username).Scan(&userID, &hash, &displayName, &xp, &createdAt)
	// Identical error for "no such user" and "wrong password" -- avoids
	// username enumeration.
	if err != nil || !verifyPassword(hash, req.Password) {
		writeError(w, http.StatusUnauthorized, "invalid username or password")
		return
	}

	refreshToken, err := createSession(r.Context(), s.pool, userID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to log in")
		return
	}
	accessToken, err := generateAccessToken(s.cfg.JWTSecret, userID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to log in")
		return
	}
	setAuthCookies(w, accessToken, refreshToken)

	level, xpIntoLevel, xpForNextLevel := levelFromXP(xp)
	writeJSON(w, http.StatusOK, authResponse{
		meResponse: meResponse{
			ID: userID, DisplayName: displayName, XP: xp,
			Level: level, XPIntoLevel: xpIntoLevel, XPForNextLevel: xpForNextLevel,
			CreatedAt: createdAt.Format(time.RFC3339),
		},
		AccessToken:  accessToken,
		RefreshToken: refreshToken,
	})
}

func (s *Server) handleLogout(w http.ResponseWriter, r *http.Request) {
	if token, ok := refreshTokenFromRequest(r); ok {
		_ = revokeSessionByToken(r.Context(), s.pool, token)
	}
	clearAuthCookies(w)
	writeJSON(w, http.StatusNoContent, nil)
}

func (s *Server) handleRefresh(w http.ResponseWriter, r *http.Request) {
	token, ok := refreshTokenFromRequest(r)
	if !ok {
		writeError(w, http.StatusUnauthorized, "unauthorized")
		return
	}

	userID, newRefreshToken, err := rotateSession(r.Context(), s.pool, token)
	if err != nil {
		clearAuthCookies(w)
		writeError(w, http.StatusUnauthorized, "unauthorized")
		return
	}

	accessToken, err := generateAccessToken(s.cfg.JWTSecret, userID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to refresh session")
		return
	}
	setAuthCookies(w, accessToken, newRefreshToken)
	writeJSON(w, http.StatusOK, map[string]string{
		"access_token":  accessToken,
		"refresh_token": newRefreshToken,
	})
}

func (s *Server) handleHealthz(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "text/plain; charset=utf-8")
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write([]byte("ok"))
}
