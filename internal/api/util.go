package api

import (
	"errors"
	"strconv"

	"github.com/jackc/pgx/v5/pgconn"
)

// errNotFound is a sentinel used inside withTx closures to signal "the
// requested row doesn't exist or isn't owned by this user", which handlers
// map to a 404 response.
var errNotFound = errors.New("not found")

// isUniqueViolation reports whether err is a Postgres unique_violation
// (23505), e.g. a duplicate username or duplicate habit name.
func isUniqueViolation(err error) bool {
	var pgErr *pgconn.PgError
	return errors.As(err, &pgErr) && pgErr.Code == "23505"
}

// nullableString converts an empty string to a nil (SQL NULL) parameter so
// optional text columns (e.g. daily_progress.notes) store NULL rather than
// an empty string.
func nullableString(s string) any {
	if s == "" {
		return nil
	}
	return s
}

// parseIntOrDefault parses s as an int, clamped to [min, max], falling back
// to def if s is empty or not a valid integer.
func parseIntOrDefault(s string, def, min, max int) int {
	if s == "" {
		return def
	}
	v, err := strconv.Atoi(s)
	if err != nil {
		return def
	}
	if v < min {
		return min
	}
	if v > max {
		return max
	}
	return v
}

// paginate returns the [offset, offset+limit) slice of items, clamped to
// items' bounds. Used by handlers that sort/filter in Go (leaderboard,
// user directory) rather than in SQL, since the sort key (streak) isn't a
// plain column.
func paginate[T any](items []T, limit, offset int) []T {
	if offset >= len(items) {
		return []T{}
	}
	end := offset + limit
	if end > len(items) {
		end = len(items)
	}
	return items[offset:end]
}
