package api

import (
	"encoding/json"
	"errors"
	"io"
	"log"
	"net/http"
	"regexp"
	"strings"
	"time"
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

// apiError is the JSON shape returned for all error responses. Both `error`
// and `message` carry the same text - the frontend's apiClient.js reads
// `message` (with `error` kept for any other consumer expecting the
// original field name); `code` is only set by writeErrorWithCode, for the
// handful of errors the frontend needs to distinguish programmatically
// (e.g. "EMAIL_NOT_VERIFIED") rather than just display.
type apiError struct {
	Error   string `json:"error"`
	Message string `json:"message"`
	Code    string `json:"code,omitempty"`
}

// writeError writes a JSON error body with the given status.
func writeError(w http.ResponseWriter, status int, message string) {
	writeJSON(w, status, apiError{Error: message, Message: message})
}

// writeErrorWithCode is writeError plus a machine-readable `code` field.
func writeErrorWithCode(w http.ResponseWriter, status int, code, message string) {
	writeJSON(w, status, apiError{Error: message, Message: message, Code: code})
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

var emailRE = regexp.MustCompile(`^[^\s@]+@[^\s@]+\.[^\s@]+$`)

// validateEmail enforces a basic, permissive email format check (not a full
// RFC 5322 parse) plus a sane length cap.
func validateEmail(email string) error {
	if len(email) > 254 || !emailRE.MatchString(email) {
		return errors.New("please enter a valid email address")
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

// ageFromDOB computes a whole-years-old age from a birth date, accounting
// for whether this year's birthday has happened yet.
func ageFromDOB(dob, now time.Time) int {
	age := now.Year() - dob.Year()
	if now.Month() < dob.Month() || (now.Month() == dob.Month() && now.Day() < dob.Day()) {
		age--
	}
	return age
}

// validateDateOfBirth rejects a birth date in the future or one implying an
// age outside the 13-120 range.
func validateDateOfBirth(dob time.Time) error {
	now := time.Now()
	if dob.After(now) {
		return errors.New("date of birth must be in the past")
	}
	if age := ageFromDOB(dob, now); age < 13 || age > 120 {
		return errors.New("you must be between 13 and 120 years old")
	}
	return nil
}
