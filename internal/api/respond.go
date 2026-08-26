package api

import (
	"encoding/json"
	"errors"
	"io"
	"log"
	"net/http"
	"regexp"
	"strings"
)

// writeJSON writes v as a JSON response body with the given status code.
func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(status)
	if v == nil {
		return
	}
	if err := json.NewEncoder(w).Encode(v); err != nil {
		log.Printf("writeJSON encode error: %v", err)
	}
}

// apiError is the JSON shape returned for all error responses.
type apiError struct {
	Error string `json:"error"`
}

// writeError writes a JSON {"error": message} body with the given status.
func writeError(w http.ResponseWriter, status int, message string) {
	writeJSON(w, status, apiError{Error: message})
}

// decodeJSON decodes the request body into v, rejecting unknown fields and
// bodies larger than 1MB.
func decodeJSON(r *http.Request, v any) error {
	dec := json.NewDecoder(io.LimitReader(r.Body, 1<<20))
	dec.DisallowUnknownFields()
	if err := dec.Decode(v); err != nil {
		if errors.Is(err, io.EOF) {
			return errors.New("request body is required")
		}
		return err
	}
	return nil
}

var usernameRE = regexp.MustCompile(`^[a-zA-Z0-9_]{3,32}$`)

// validateUsername enforces the 3-32 char [a-zA-Z0-9_] rule.
func validateUsername(username string) error {
	if !usernameRE.MatchString(username) {
		return errors.New("username must be 3-32 characters, letters/numbers/underscore only")
	}
	return nil
}

// validatePassword enforces the 8-200 char length rule. Rejecting >200
// chars here (before bcrypt) matters because bcrypt silently truncates
// input at 72 bytes.
func validatePassword(password string) error {
	if len(password) < 8 || len(password) > 200 {
		return errors.New("password must be 8-200 characters")
	}
	return nil
}

// validateDisplayName enforces a reasonable non-empty display name.
func validateDisplayName(displayName string) error {
	trimmed := strings.TrimSpace(displayName)
	if len(trimmed) == 0 || len(trimmed) > 100 {
		return errors.New("display name must be 1-100 characters")
	}
	return nil
}
