package api

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"sort"
	"strings"

	"github.com/jackc/pgx/v5"
)

// socialUserRow is the shared shape for leaderboard rows and directory
// entries. Rank is only populated for the leaderboard. Username is never
// included -- public views only ever expose display_name.
type socialUserRow struct {
	Rank            int     `json:"rank,omitempty"`
	UserID          string  `json:"user_id"`
	DisplayName     string  `json:"display_name"`
	Level           int     `json:"level"`
	XP              int     `json:"xp"`
	CurrentStreak   int     `json:"current_streak"`
	ActiveSplitName *string `json:"active_split_name"`
}

// fetchAllUsersWithStats loads every registered user with their level and a
// read-only streak peek (consumeShields=false -- viewing the leaderboard or
// community directory must never spend anyone's shields). At the small
// scale this app targets (tens of users, not thousands), computing streak
// per user in a Go loop is simpler and fast enough; sorting/pagination then
// happens in Go too since "streak" isn't a plain sortable column.
func (s *Server) fetchAllUsersWithStats(ctx context.Context) ([]socialUserRow, error) {
	rows, err := s.pool.Query(ctx,
		`SELECT u.id, u.display_name, u.xp, ws.name
		 FROM users u
		 LEFT JOIN workout_splits ws ON ws.user_id = u.id AND ws.is_active`)
	if err != nil {
		return nil, fmt.Errorf("load users: %w", err)
	}
	defer rows.Close()

	var result []socialUserRow
	for rows.Next() {
		var row socialUserRow
		if err := rows.Scan(&row.UserID, &row.DisplayName, &row.XP, &row.ActiveSplitName); err != nil {
			return nil, fmt.Errorf("scan user: %w", err)
		}
		result = append(result, row)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}

	for i := range result {
		level, _, _ := levelFromXP(result[i].XP)
		result[i].Level = level

		streak, err := computeStreak(ctx, s.pool, result[i].UserID, false)
		if err != nil {
			return nil, fmt.Errorf("compute streak for %s: %w", result[i].UserID, err)
		}
		result[i].CurrentStreak = streak.CurrentStreak
	}

	return result, nil
}

func (s *Server) handleLeaderboard(w http.ResponseWriter, r *http.Request) {
	sortBy := r.URL.Query().Get("sort")
	limit := parseIntOrDefault(r.URL.Query().Get("limit"), 25, 1, 200)
	offset := parseIntOrDefault(r.URL.Query().Get("offset"), 0, 0, 1_000_000)

	users, err := s.fetchAllUsersWithStats(r.Context())
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to load leaderboard")
		return
	}

	if sortBy == "streak" {
		sort.SliceStable(users, func(i, j int) bool { return users[i].CurrentStreak > users[j].CurrentStreak })
	} else {
		sort.SliceStable(users, func(i, j int) bool { return users[i].XP > users[j].XP })
	}
	for i := range users {
		users[i].Rank = i + 1
	}

	writeJSON(w, http.StatusOK, map[string]any{
		"entries": paginate(users, limit, offset),
		"total":   len(users),
	})
}

func (s *Server) handleListUsers(w http.ResponseWriter, r *http.Request) {
	query := strings.ToLower(strings.TrimSpace(r.URL.Query().Get("query")))
	limit := parseIntOrDefault(r.URL.Query().Get("limit"), 20, 1, 200)
	offset := parseIntOrDefault(r.URL.Query().Get("offset"), 0, 0, 1_000_000)

	users, err := s.fetchAllUsersWithStats(r.Context())
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to load users")
		return
	}

	if query != "" {
		var filtered []socialUserRow
		for _, u := range users {
			if strings.Contains(strings.ToLower(u.DisplayName), query) {
				filtered = append(filtered, u)
			}
		}
		users = filtered
	}

	sort.SliceStable(users, func(i, j int) bool {
		return strings.ToLower(users[i].DisplayName) < strings.ToLower(users[j].DisplayName)
	})

	writeJSON(w, http.StatusOK, map[string]any{
		"entries": paginate(users, limit, offset),
		"total":   len(users),
	})
}

func (s *Server) handleUserHabitsSummary(w http.ResponseWriter, r *http.Request) {
	targetID := r.PathValue("id")

	var displayName string
	var xp int
	var activeSplitName *string
	err := s.pool.QueryRow(r.Context(),
		`SELECT u.display_name, u.xp, ws.name
		 FROM users u
		 LEFT JOIN workout_splits ws ON ws.user_id = u.id AND ws.is_active
		 WHERE u.id = $1`, targetID).Scan(&displayName, &xp, &activeSplitName)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			writeError(w, http.StatusNotFound, "user not found")
			return
		}
		writeError(w, http.StatusInternalServerError, "failed to load user")
		return
	}

	streak, err := computeStreak(r.Context(), s.pool, targetID, false)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to load user")
		return
	}

	rows, err := s.pool.Query(r.Context(),
		`SELECT id, name, color, icon FROM habits WHERE user_id = $1 ORDER BY created_at`, targetID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to load habits")
		return
	}
	defer rows.Close()

	type habitSummary struct {
		ID    string `json:"id"`
		Name  string `json:"name"`
		Color string `json:"color"`
		Icon  string `json:"icon"`
	}
	habits := []habitSummary{}
	for rows.Next() {
		var h habitSummary
		if err := rows.Scan(&h.ID, &h.Name, &h.Color, &h.Icon); err != nil {
			writeError(w, http.StatusInternalServerError, "failed to load habits")
			return
		}
		habits = append(habits, h)
	}
	if err := rows.Err(); err != nil {
		writeError(w, http.StatusInternalServerError, "failed to load habits")
		return
	}

	level, _, _ := levelFromXP(xp)
	writeJSON(w, http.StatusOK, map[string]any{
		"display_name":      displayName,
		"level":             level,
		"xp":                xp,
		"current_streak":    streak.CurrentStreak,
		"active_split_name": activeSplitName,
		"habits":            habits,
	})
}
