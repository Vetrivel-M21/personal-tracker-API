package api

import (
	"context"
	"errors"
	"fmt"
	"log"
	"net/http"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
)

const (
	verificationCodeTTL        = 15 * time.Minute
	verificationMaxAttempts    = 5
	verificationResendCooldown = 60 * time.Second
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

// seedNewUserExtras inserts the starter habits and streak-shields row every
// new account gets, regardless of which signup path created it (password
// signup or Google sign-in auto-provisioning).
func seedNewUserExtras(ctx context.Context, tx pgx.Tx, userID string) error {
	for _, h := range defaultHabits {
		if _, err := tx.Exec(ctx,
			`INSERT INTO habits (user_id, name, color, icon) VALUES ($1,$2,$3,$4)`,
			userID, h.Name, h.Color, h.Icon); err != nil {
			return fmt.Errorf("seed default habits: %w", err)
		}
	}
	if _, err := tx.Exec(ctx,
		`INSERT INTO streak_shields (user_id, shield_count) VALUES ($1, 0)`, userID); err != nil {
		return fmt.Errorf("seed streak shields: %w", err)
	}
	return nil
}

type signupRequest struct {
	Username    string `json:"username"`
	Password    string `json:"password"`
	DisplayName string `json:"display_name"`
	Email       string `json:"email"`
	// DateOfBirth is ISO YYYY-MM-DD, matching an HTML <input type="date">.
	DateOfBirth string `json:"date_of_birth"`
}

// handleSignup creates the account but does NOT log the caller in - it
// generates and emails a verification code, and the account can't be used
// until handleVerifyEmail confirms it (that's the handler that actually
// creates a session). See errEmailNotConfigured for the "SMTP isn't set up"
// case, checked up front so we never create an account that can't possibly
// receive its code.
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
	if err := validateEmail(req.Email); err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	dob, err := time.Parse("2006-01-02", req.DateOfBirth)
	if err != nil {
		writeError(w, http.StatusBadRequest, "please enter a valid date of birth")
		return
	}
	if err := validateDateOfBirth(dob); err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	if s.cfg.SMTPHost == "" || s.cfg.SMTPFrom == "" {
		writeError(w, http.StatusInternalServerError, "email verification is not configured on this server")
		return
	}
	displayName := strings.TrimSpace(req.DisplayName)
	email := strings.TrimSpace(req.Email)

	hash, err := hashPassword(req.Password)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to create account")
		return
	}

	code, err := generateVerificationCode()
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to create account")
		return
	}
	codeHash := hashVerificationCode(code)
	expiresAt := time.Now().Add(verificationCodeTTL)

	var userID string
	err = withTx(r.Context(), s.pool, func(tx pgx.Tx) error {
		if insertErr := tx.QueryRow(r.Context(),
			`INSERT INTO users (username, password_hash, display_name, email, date_of_birth, email_verified,
			   email_verification_code_hash, email_verification_expires_at, email_verification_last_sent_at)
			 VALUES ($1,$2,$3,$4,$5,false,$6,$7,now())
			 RETURNING id`,
			req.Username, hash, displayName, email, dob, codeHash, expiresAt).Scan(&userID); insertErr != nil {
			return insertErr
		}

		return seedNewUserExtras(r.Context(), tx, userID)
	})
	if err != nil {
		if isUniqueViolation(err) {
			writeError(w, http.StatusConflict, "username or email is already taken")
			return
		}
		writeError(w, http.StatusInternalServerError, "failed to create account")
		return
	}

	if sendErr := sendVerificationEmail(s.cfg, email, code); sendErr != nil {
		// The account exists and the code is stored either way - "Resend
		// Code" on the frontend is the recovery path for a transient send
		// failure, so this doesn't need to fail the whole request.
		log.Printf("signup: failed to send verification email to user %s: %v", userID, sendErr)
	}

	writeJSON(w, http.StatusCreated, map[string]any{
		"pending_verification": true,
		"username":             req.Username,
		"email_hint":           maskEmail(email),
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
	var hasPassword, emailVerified bool
	var dob *time.Time
	var createdAt time.Time
	err := s.pool.QueryRow(r.Context(),
		`SELECT id, password_hash, display_name, xp, has_password, email_verified, date_of_birth, created_at FROM users WHERE lower(username) = lower($1)`,
		req.Username).Scan(&userID, &hash, &displayName, &xp, &hasPassword, &emailVerified, &dob, &createdAt)
	// Identical error for "no such user" and "wrong password" -- avoids
	// username enumeration.
	if err != nil || !verifyPassword(hash, req.Password) {
		writeError(w, http.StatusUnauthorized, "invalid username or password")
		return
	}
	if !emailVerified {
		writeErrorWithCode(w, http.StatusForbidden, "EMAIL_NOT_VERIFIED", "please verify your email before logging in")
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

	dateOfBirth, age := formatDOBAndAge(dob)
	level, xpIntoLevel, xpForNextLevel := levelFromXP(xp)
	writeJSON(w, http.StatusOK, authResponse{
		meResponse: meResponse{
			ID: userID, DisplayName: displayName, XP: xp,
			Level: level, XPIntoLevel: xpIntoLevel, XPForNextLevel: xpForNextLevel,
			HasPassword: hasPassword,
			DateOfBirth: dateOfBirth, Age: age,
			CreatedAt: createdAt.Format(time.RFC3339),
		},
		AccessToken:  accessToken,
		RefreshToken: refreshToken,
	})
}

type verifyEmailRequest struct {
	Username string `json:"username"`
	Code     string `json:"code"`
}

// handleVerifyEmail is where a newly-signed-up user actually gets logged in
// for the first time - success sets email_verified and immediately issues a
// session, exactly like handleLogin/handleGoogleLogin's tail.
func (s *Server) handleVerifyEmail(w http.ResponseWriter, r *http.Request) {
	var req verifyEmailRequest
	if err := decodeJSON(r, &req); err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}

	var userID, displayName, codeHash string
	var xp, attempts int
	var emailVerified bool
	var expiresAt *time.Time
	var dob *time.Time
	var createdAt time.Time
	err := s.pool.QueryRow(r.Context(),
		`SELECT id, display_name, xp, email_verified, email_verification_code_hash,
		        email_verification_expires_at, email_verification_attempts, date_of_birth, created_at
		 FROM users WHERE lower(username) = lower($1)`,
		req.Username).Scan(&userID, &displayName, &xp, &emailVerified, &codeHash, &expiresAt, &attempts, &dob, &createdAt)
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid or expired code")
		return
	}
	if emailVerified {
		writeError(w, http.StatusBadRequest, "this account is already verified")
		return
	}
	if attempts >= verificationMaxAttempts {
		writeError(w, http.StatusTooManyRequests, "too many attempts -- request a new code")
		return
	}
	if expiresAt == nil || time.Now().After(*expiresAt) || codeHash == "" || hashVerificationCode(req.Code) != codeHash {
		if _, updErr := s.pool.Exec(r.Context(),
			`UPDATE users SET email_verification_attempts = email_verification_attempts + 1 WHERE id = $1`, userID); updErr != nil {
			log.Printf("verify-email: failed to record attempt for user %s: %v", userID, updErr)
		}
		writeError(w, http.StatusBadRequest, "invalid or expired code")
		return
	}

	if _, err := s.pool.Exec(r.Context(),
		`UPDATE users SET email_verified = true, email_verification_code_hash = NULL,
		   email_verification_expires_at = NULL, email_verification_attempts = 0, updated_at = now()
		 WHERE id = $1`, userID); err != nil {
		writeError(w, http.StatusInternalServerError, "failed to verify email")
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

	dateOfBirth, age := formatDOBAndAge(dob)
	level, xpIntoLevel, xpForNextLevel := levelFromXP(xp)
	writeJSON(w, http.StatusOK, authResponse{
		meResponse: meResponse{
			ID: userID, DisplayName: displayName, XP: xp,
			Level: level, XPIntoLevel: xpIntoLevel, XPForNextLevel: xpForNextLevel,
			HasPassword: true,
			DateOfBirth: dateOfBirth, Age: age,
			CreatedAt: createdAt.Format(time.RFC3339),
		},
		AccessToken:  accessToken,
		RefreshToken: refreshToken,
	})
}

type resendVerificationRequest struct {
	Username string `json:"username"`
}

// handleResendVerification issues a fresh code, replacing any prior one.
// Responds 204 for "doesn't exist" / "already verified" the same way (avoids
// confirming account existence); a 60s cooldown (tracked via
// email_verification_last_sent_at) is the only distinguishable response,
// which only a real unverified account can trigger.
func (s *Server) handleResendVerification(w http.ResponseWriter, r *http.Request) {
	var req resendVerificationRequest
	if err := decodeJSON(r, &req); err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}

	var userID, email string
	var emailVerified bool
	var lastSentAt *time.Time
	err := s.pool.QueryRow(r.Context(),
		`SELECT id, email, email_verified, email_verification_last_sent_at FROM users WHERE lower(username) = lower($1)`,
		req.Username).Scan(&userID, &email, &emailVerified, &lastSentAt)
	if err != nil || emailVerified || email == "" {
		writeJSON(w, http.StatusNoContent, nil)
		return
	}
	if lastSentAt != nil && time.Since(*lastSentAt) < verificationResendCooldown {
		writeErrorWithCode(w, http.StatusTooManyRequests, "RESEND_COOLDOWN", "please wait before requesting another code")
		return
	}

	code, err := generateVerificationCode()
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to resend code")
		return
	}
	codeHash := hashVerificationCode(code)
	expiresAt := time.Now().Add(verificationCodeTTL)

	if _, err := s.pool.Exec(r.Context(),
		`UPDATE users SET email_verification_code_hash = $2, email_verification_expires_at = $3,
		   email_verification_attempts = 0, email_verification_last_sent_at = now(), updated_at = now()
		 WHERE id = $1`, userID, codeHash, expiresAt); err != nil {
		writeError(w, http.StatusInternalServerError, "failed to resend code")
		return
	}

	if sendErr := sendVerificationEmail(s.cfg, email, code); sendErr != nil {
		log.Printf("resend-verification: failed to send email to user %s: %v", userID, sendErr)
	}

	writeJSON(w, http.StatusNoContent, nil)
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

type googleLoginRequest struct {
	Credential string `json:"credential"`
}

// handleGoogleLogin verifies a Google Identity Services ID token, then
// finds-or-creates the local user by (in order) google_id, email, or a
// brand-new auto-provisioned account - then issues our own session exactly
// like handleLogin/handleSignup do.
func (s *Server) handleGoogleLogin(w http.ResponseWriter, r *http.Request) {
	var req googleLoginRequest
	if err := decodeJSON(r, &req); err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	if req.Credential == "" {
		writeError(w, http.StatusBadRequest, "credential is required")
		return
	}

	identity, err := verifyGoogleIDToken(r.Context(), s.cfg.GoogleClientID, req.Credential)
	if err != nil {
		if errors.Is(err, errGoogleSignInUnavailable) {
			writeError(w, http.StatusInternalServerError, "google sign-in is not configured on this server")
			return
		}
		writeError(w, http.StatusUnauthorized, "invalid google credential")
		return
	}

	var userID, displayName string
	var xp int
	var hasPassword bool
	var dob *time.Time
	var createdAt time.Time
	isNew := false

	err = withTx(r.Context(), s.pool, func(tx pgx.Tx) error {
		lookupErr := tx.QueryRow(r.Context(),
			`SELECT id, display_name, xp, has_password, date_of_birth, created_at FROM users WHERE google_id = $1`,
			identity.Sub).Scan(&userID, &displayName, &xp, &hasPassword, &dob, &createdAt)
		if lookupErr == nil {
			return nil
		}
		if !errors.Is(lookupErr, pgx.ErrNoRows) {
			return lookupErr
		}

		// No account linked to this Google id yet - if an existing account
		// already uses this email (e.g. it was created via username/
		// password signup), link it instead of creating a duplicate.
		linkErr := tx.QueryRow(r.Context(),
			`UPDATE users SET google_id = $1, updated_at = now()
			 WHERE lower(email) = lower($2) AND google_id IS NULL
			 RETURNING id, display_name, xp, has_password, date_of_birth, created_at`,
			identity.Sub, identity.Email).Scan(&userID, &displayName, &xp, &hasPassword, &dob, &createdAt)
		if linkErr == nil {
			return nil
		}
		if !errors.Is(linkErr, pgx.ErrNoRows) {
			return linkErr
		}

		// Genuinely new account.
		localPart, _, _ := strings.Cut(identity.Email, "@")
		username, usernameErr := generateUniqueUsername(r.Context(), tx, localPart)
		if usernameErr != nil {
			return usernameErr
		}
		name := strings.TrimSpace(identity.Name)
		if name == "" {
			name = localPart
		}
		hash, hashErr := randomUnusablePasswordHash()
		if hashErr != nil {
			return hashErr
		}

		if insertErr := tx.QueryRow(r.Context(),
			`INSERT INTO users (username, password_hash, display_name, google_id, email, has_password)
			 VALUES ($1,$2,$3,$4,$5,false)
			 RETURNING id, created_at`,
			username, hash, name, identity.Sub, identity.Email).Scan(&userID, &createdAt); insertErr != nil {
			return insertErr
		}
		displayName = name
		xp = 0
		hasPassword = false
		isNew = true

		return seedNewUserExtras(r.Context(), tx, userID)
	})
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to sign in with google")
		return
	}

	refreshToken, err := createSession(r.Context(), s.pool, userID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to sign in with google")
		return
	}
	accessToken, err := generateAccessToken(s.cfg.JWTSecret, userID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to sign in with google")
		return
	}
	setAuthCookies(w, accessToken, refreshToken)

	status := http.StatusOK
	if isNew {
		status = http.StatusCreated
	}
	dateOfBirth, age := formatDOBAndAge(dob)
	level, xpIntoLevel, xpForNextLevel := levelFromXP(xp)
	writeJSON(w, status, authResponse{
		meResponse: meResponse{
			ID: userID, DisplayName: displayName, XP: xp,
			Level: level, XPIntoLevel: xpIntoLevel, XPForNextLevel: xpForNextLevel,
			HasPassword: hasPassword,
			DateOfBirth: dateOfBirth, Age: age,
			CreatedAt: createdAt.Format(time.RFC3339),
		},
		AccessToken:  accessToken,
		RefreshToken: refreshToken,
	})
}

func (s *Server) handleHealthz(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "text/plain; charset=utf-8")
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write([]byte("ok"))
}
