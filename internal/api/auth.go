package api

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"errors"
	"fmt"
	"net/http"
	"time"

	"github.com/golang-jwt/jwt/v5"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"golang.org/x/crypto/bcrypt"
)

const (
	accessTokenTTL  = 15 * time.Minute
	refreshTokenTTL = 30 * 24 * time.Hour
	bcryptCost      = 12
)

// errInvalidRefreshToken is returned by rotateSession for any presented
// refresh token that isn't currently valid (unknown, expired, or already
// rotated). It maps to a generic 401 at the handler level.
var errInvalidRefreshToken = errors.New("invalid refresh token")

// hashPassword bcrypt-hashes password at cost 12.
func hashPassword(password string) (string, error) {
	hash, err := bcrypt.GenerateFromPassword([]byte(password), bcryptCost)
	if err != nil {
		return "", fmt.Errorf("hash password: %w", err)
	}
	return string(hash), nil
}

// verifyPassword reports whether password matches the given bcrypt hash.
func verifyPassword(hash, password string) bool {
	return bcrypt.CompareHashAndPassword([]byte(hash), []byte(password)) == nil
}

// accessClaims is the JWT claim set for access tokens: {sub, exp, iat}.
type accessClaims struct {
	jwt.RegisteredClaims
}

// generateAccessToken signs a 15-minute HS256 JWT with userID as its subject.
func generateAccessToken(secret, userID string) (string, error) {
	now := time.Now().UTC()
	claims := accessClaims{
		RegisteredClaims: jwt.RegisteredClaims{
			Subject:   userID,
			ExpiresAt: jwt.NewNumericDate(now.Add(accessTokenTTL)),
			IssuedAt:  jwt.NewNumericDate(now),
		},
	}

	signed, err := jwt.NewWithClaims(jwt.SigningMethodHS256, claims).SignedString([]byte(secret))
	if err != nil {
		return "", fmt.Errorf("sign access token: %w", err)
	}
	return signed, nil
}

// parseAccessToken verifies an access token's signature and expiry and
// returns its subject (the user id).
func parseAccessToken(secret, tokenStr string) (string, error) {
	claims := &accessClaims{}
	token, err := jwt.ParseWithClaims(tokenStr, claims, func(*jwt.Token) (any, error) {
		return []byte(secret), nil
	}, jwt.WithValidMethods([]string{jwt.SigningMethodHS256.Name}))
	if err != nil || !token.Valid {
		return "", fmt.Errorf("invalid access token")
	}
	return claims.Subject, nil
}

// generateRefreshToken returns a new opaque refresh token (32 random bytes,
// base64url-encoded for the browser) along with the SHA-256 hash that gets
// stored server-side in the sessions table.
func generateRefreshToken() (token string, hash []byte, err error) {
	buf := make([]byte, 32)
	if _, err := rand.Read(buf); err != nil {
		return "", nil, fmt.Errorf("generate refresh token: %w", err)
	}
	token = base64.RawURLEncoding.EncodeToString(buf)
	sum := sha256.Sum256([]byte(token))
	return token, sum[:], nil
}

// hashRefreshToken hashes a presented refresh token for lookup against the
// stored token_hash column.
func hashRefreshToken(token string) []byte {
	sum := sha256.Sum256([]byte(token))
	return sum[:]
}

// setAuthCookies sets the access_token and refresh_token httpOnly cookies on
// a successful signup/login/refresh.
func setAuthCookies(w http.ResponseWriter, accessToken, refreshToken string) {
	http.SetCookie(w, &http.Cookie{
		Name:     "access_token",
		Value:    accessToken,
		Path:     "/",
		MaxAge:   int(accessTokenTTL.Seconds()),
		HttpOnly: true,
		Secure:   true,
		SameSite: http.SameSiteStrictMode,
	})
	http.SetCookie(w, &http.Cookie{
		Name:     "refresh_token",
		Value:    refreshToken,
		Path:     "/api/auth",
		MaxAge:   int(refreshTokenTTL.Seconds()),
		HttpOnly: true,
		Secure:   true,
		SameSite: http.SameSiteStrictMode,
	})
}

// clearAuthCookies expires both auth cookies immediately, used on logout.
func clearAuthCookies(w http.ResponseWriter) {
	http.SetCookie(w, &http.Cookie{
		Name:     "access_token",
		Value:    "",
		Path:     "/",
		MaxAge:   -1,
		HttpOnly: true,
		Secure:   true,
		SameSite: http.SameSiteStrictMode,
	})
	http.SetCookie(w, &http.Cookie{
		Name:     "refresh_token",
		Value:    "",
		Path:     "/api/auth",
		MaxAge:   -1,
		HttpOnly: true,
		Secure:   true,
		SameSite: http.SameSiteStrictMode,
	})
}

// createSession inserts a new session row for userID (30-day expiry from
// now) and returns the opaque refresh token to hand to the browser. db may
// be a pool or a tx, so this composes with signup/login (plain pool) and
// refresh rotation (inside a transaction).
func createSession(ctx context.Context, db dbExecutor, userID string) (string, error) {
	token, hash, err := generateRefreshToken()
	if err != nil {
		return "", err
	}

	_, err = db.Exec(ctx,
		`INSERT INTO sessions (user_id, token_hash, expires_at) VALUES ($1, $2, $3)`,
		userID, hash, time.Now().UTC().Add(refreshTokenTTL))
	if err != nil {
		return "", fmt.Errorf("create session: %w", err)
	}
	return token, nil
}

// rotateSession validates a presented refresh token and, if it is currently
// valid, revokes it and creates its replacement in the same transaction
// (rotate-on-every-use with a 30-day sliding expiry). A presented token whose
// session was already revoked indicates reuse of a rotated token (theft):
// every session for that user is revoked and errInvalidRefreshToken is
// returned.
func rotateSession(ctx context.Context, pool *pgxpool.Pool, presentedToken string) (userID, newRefreshToken string, err error) {
	hash := hashRefreshToken(presentedToken)

	err = withTx(ctx, pool, func(tx pgx.Tx) error {
		var sessionID, sessUserID string
		var revokedAt *time.Time
		var expiresAt time.Time

		scanErr := tx.QueryRow(ctx,
			`SELECT id, user_id, revoked_at, expires_at FROM sessions WHERE token_hash = $1`,
			hash).Scan(&sessionID, &sessUserID, &revokedAt, &expiresAt)
		if errors.Is(scanErr, pgx.ErrNoRows) {
			return errInvalidRefreshToken
		}
		if scanErr != nil {
			return fmt.Errorf("load session: %w", scanErr)
		}

		if revokedAt != nil {
			if _, revokeErr := tx.Exec(ctx,
				`UPDATE sessions SET revoked_at = now() WHERE user_id = $1 AND revoked_at IS NULL`,
				sessUserID); revokeErr != nil {
				return fmt.Errorf("revoke sessions after reuse: %w", revokeErr)
			}
			return errInvalidRefreshToken
		}

		if time.Now().UTC().After(expiresAt) {
			return errInvalidRefreshToken
		}

		if _, revokeErr := tx.Exec(ctx,
			`UPDATE sessions SET revoked_at = now() WHERE id = $1`, sessionID); revokeErr != nil {
			return fmt.Errorf("revoke rotated session: %w", revokeErr)
		}

		newToken, createErr := createSession(ctx, tx, sessUserID)
		if createErr != nil {
			return createErr
		}

		userID, newRefreshToken = sessUserID, newToken
		return nil
	})
	if err != nil {
		return "", "", err
	}

	return userID, newRefreshToken, nil
}

// revokeSessionByToken revokes the session matching presentedToken, if any.
// Used by logout; it is not an error for the token to already be
// unknown/revoked, since logout should succeed idempotently.
func revokeSessionByToken(ctx context.Context, pool *pgxpool.Pool, presentedToken string) error {
	hash := hashRefreshToken(presentedToken)
	_, err := pool.Exec(ctx,
		`UPDATE sessions SET revoked_at = now() WHERE token_hash = $1 AND revoked_at IS NULL`, hash)
	if err != nil {
		return fmt.Errorf("revoke session: %w", err)
	}
	return nil
}

// refreshTokenRequest is the JSON body shape accepted by handleRefresh and
// handleLogout for clients that don't use cookies (e.g. a native mobile app).
type refreshTokenRequest struct {
	RefreshToken string `json:"refresh_token"`
}

// refreshTokenFromRequest reads the refresh token from the refresh_token
// cookie (the web flow), falling back to a {"refresh_token": "..."} JSON
// body for clients that don't use cookies.
func refreshTokenFromRequest(r *http.Request) (string, bool) {
	if cookie, err := r.Cookie("refresh_token"); err == nil && cookie.Value != "" {
		return cookie.Value, true
	}
	var body refreshTokenRequest
	if err := decodeJSON(r, &body); err == nil && body.RefreshToken != "" {
		return body.RefreshToken, true
	}
	return "", false
}

// purgeExpiredSessions deletes session rows that expired more than 30 days
// ago. Called from a daily ticker in main.go.
func purgeExpiredSessions(ctx context.Context, pool *pgxpool.Pool) error {
	_, err := pool.Exec(ctx,
		`DELETE FROM sessions WHERE expires_at < now() - interval '30 days'`)
	if err != nil {
		return fmt.Errorf("purge expired sessions: %w", err)
	}
	return nil
}
