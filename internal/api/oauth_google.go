package api

import (
	"context"
	"crypto/rand"
	"encoding/base64"
	"encoding/hex"
	"errors"
	"fmt"
	"regexp"
	"strings"

	"google.golang.org/api/idtoken"
)

// errGoogleSignInUnavailable is returned when GOOGLE_CLIENT_ID isn't
// configured - lets handleGoogleLogin respond with a clear message instead
// of a confusing token-verification failure.
var errGoogleSignInUnavailable = errors.New("google sign-in is not configured on this server")

// googleIdentity is the subset of Google ID token claims this app needs.
type googleIdentity struct {
	Sub   string // stable Google account id
	Email string
	Name  string
}

// verifyGoogleIDToken validates the token's signature, issuer, audience and
// expiry against Google's public keys (idtoken.Validate fetches/caches
// Google's JWKS internally) and extracts the claims we care about.
func verifyGoogleIDToken(ctx context.Context, clientID, rawToken string) (googleIdentity, error) {
	if clientID == "" {
		return googleIdentity{}, errGoogleSignInUnavailable
	}

	payload, err := idtoken.Validate(ctx, rawToken, clientID)
	if err != nil {
		return googleIdentity{}, fmt.Errorf("verify google id token: %w", err)
	}

	email, _ := payload.Claims["email"].(string)
	name, _ := payload.Claims["name"].(string)
	if email == "" {
		return googleIdentity{}, errors.New("google account has no email")
	}

	return googleIdentity{Sub: payload.Subject, Email: email, Name: name}, nil
}

// randomUnusablePasswordHash bcrypt-hashes a random 32-byte value that is
// never returned to the caller, so a Google-only account has a syntactically
// valid password_hash (the column stays NOT NULL) that no one can ever know
// or log in with - has_password=false is what actually gates the UI/API.
func randomUnusablePasswordHash() (string, error) {
	buf := make([]byte, 32)
	if _, err := rand.Read(buf); err != nil {
		return "", err
	}
	return hashPassword(base64.RawURLEncoding.EncodeToString(buf))
}

var usernameSanitizeRE = regexp.MustCompile(`[^a-zA-Z0-9_]`)

// generateUniqueUsername derives a username candidate from an email's local
// part (matching the app's existing 3-32 char, letters/digits/underscore
// rule), then appends a short random suffix until it finds one that isn't
// already taken.
func generateUniqueUsername(ctx context.Context, db dbExecutor, emailLocalPart string) (string, error) {
	base := usernameSanitizeRE.ReplaceAllString(emailLocalPart, "_")
	if len(base) > 24 {
		base = base[:24]
	}
	if len(base) < 3 {
		base = base + strings.Repeat("_", 3-len(base))
	}

	for attempt := 0; attempt < 20; attempt++ {
		candidate := base
		if attempt > 0 {
			suffixBytes := make([]byte, 3)
			if _, err := rand.Read(suffixBytes); err != nil {
				return "", err
			}
			candidate = fmt.Sprintf("%s_%s", base, hex.EncodeToString(suffixBytes))
			if len(candidate) > 32 {
				candidate = candidate[len(candidate)-32:]
			}
		}

		var exists bool
		if err := db.QueryRow(ctx,
			`SELECT EXISTS(SELECT 1 FROM users WHERE lower(username) = lower($1))`, candidate).
			Scan(&exists); err != nil {
			return "", err
		}
		if !exists {
			return candidate, nil
		}
	}

	return "", errors.New("could not generate a unique username")
}
