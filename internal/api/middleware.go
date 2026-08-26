package api

import (
	"context"
	"log"
	"net/http"
	"net/url"
	"runtime/debug"
	"strings"
	"time"
)

// ctxKey is a private type for context keys so this package never collides
// with keys set by other packages.
type ctxKey int

const userIDContextKey ctxKey = iota

// contextWithUserID attaches the authenticated user's id to ctx.
func contextWithUserID(ctx context.Context, userID string) context.Context {
	return context.WithValue(ctx, userIDContextKey, userID)
}

// userIDFromContext reads the user id set by requireAuth.
func userIDFromContext(ctx context.Context) (string, bool) {
	userID, ok := ctx.Value(userIDContextKey).(string)
	return userID, ok
}

// accessTokenFromRequest reads the access token from the access_token cookie
// (the web flow), falling back to an "Authorization: Bearer <token>" header
// for clients that don't use cookies (e.g. a native mobile app).
func accessTokenFromRequest(r *http.Request) (string, bool) {
	if cookie, err := r.Cookie("access_token"); err == nil && cookie.Value != "" {
		return cookie.Value, true
	}
	if auth := r.Header.Get("Authorization"); strings.HasPrefix(auth, "Bearer ") {
		return strings.TrimPrefix(auth, "Bearer "), true
	}
	return "", false
}

// requireAuth verifies the caller's access token (cookie or bearer header)
// and puts the user id in the request context, or responds 401 if it's
// missing/invalid/expired.
func (s *Server) requireAuth(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		token, ok := accessTokenFromRequest(r)
		if !ok {
			writeError(w, http.StatusUnauthorized, "unauthorized")
			return
		}

		userID, err := parseAccessToken(s.cfg.JWTSecret, token)
		if err != nil {
			writeError(w, http.StatusUnauthorized, "unauthorized")
			return
		}

		next.ServeHTTP(w, r.WithContext(contextWithUserID(r.Context(), userID)))
	})
}

// requireOrigin rejects state-changing /api/* requests (POST/PUT/PATCH/
// DELETE) whose Origin header doesn't match PUBLIC_HOSTNAME. This is CSRF
// protection: SameSite=Strict cookies already stop most cross-site requests,
// this is a second layer.
func (s *Server) requireOrigin(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if isStateChangingMethod(r.Method) && strings.HasPrefix(r.URL.Path, "/api/") {
			origin := r.Header.Get("Origin")
			if origin == "" || !originMatchesHost(origin, s.cfg.PublicHostname) {
				writeError(w, http.StatusForbidden, "origin not allowed")
				return
			}
		}

		next.ServeHTTP(w, r)
	})
}

func isStateChangingMethod(method string) bool {
	switch method {
	case http.MethodPost, http.MethodPut, http.MethodPatch, http.MethodDelete:
		return true
	default:
		return false
	}
}

func originMatchesHost(origin, hostname string) bool {
	u, err := url.Parse(origin)
	if err != nil {
		return false
	}
	return strings.EqualFold(u.Hostname(), hostname)
}

// statusRecorder wraps a ResponseWriter to capture the status code written,
// for logging.
type statusRecorder struct {
	http.ResponseWriter
	status int
}

func (r *statusRecorder) WriteHeader(status int) {
	r.status = status
	r.ResponseWriter.WriteHeader(status)
}

// loggingMiddleware logs method, path, status and duration for every
// request.
func loggingMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		start := time.Now()
		rec := &statusRecorder{ResponseWriter: w, status: http.StatusOK}

		next.ServeHTTP(rec, r)

		log.Printf("%s %s %d %s", r.Method, r.URL.Path, rec.status, time.Since(start))
	})
}

// recoverMiddleware turns a panic anywhere downstream into a logged 500
// instead of crashing the process.
func recoverMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		defer func() {
			if err := recover(); err != nil {
				log.Printf("panic recovered: %v\n%s", err, debug.Stack())
				writeError(w, http.StatusInternalServerError, "internal server error")
			}
		}()

		next.ServeHTTP(w, r)
	})
}
