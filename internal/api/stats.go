package api

import (
	"context"
	"errors"
	"fmt"
	"math"
	"net/http"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

// levelFromXP computes level and the XP thresholds either side of it, using
// the canonical formula: level = floor(sqrt(xp/50)) + 1. This is the ONLY
// place this formula should be implemented -- every handler that returns a
// level derives it from here, so the frontend never has to guess the curve.
func levelFromXP(xp int) (level, xpIntoLevel, xpForNextLevel int) {
	level = int(math.Floor(math.Sqrt(float64(xp)/50))) + 1
	xpForCurrentLevel := (level - 1) * (level - 1) * 50
	xpForNextLevel = level * level * 50
	return level, xp - xpForCurrentLevel, xpForNextLevel
}

// maxStreakShields caps how many shields a user can bank at once, granted
// (see handleUpsertProgress) one per 7-day real-streak milestone.
const maxStreakShields = 3

// streakStats is the result of computeStreak/computeStreakWith.
type streakStats struct {
	CurrentStreak    int `json:"current_streak"`
	ShieldsRemaining int `json:"shields_remaining"`
}

// computeStreak is the public entry point for streak calculation.
//
// consumeShields must be true ONLY when computing the account owner's own
// stats (GET /api/me/stats, and as part of a progress write) -- that is the
// only path allowed to spend a shield. Any read of someone else's streak
// (leaderboard, another user's public habits-summary) MUST pass false, or
// merely viewing a profile would burn that person's shields.
//
// When consumeShields is true, this opens its own transaction with a locked
// read of the shield row, so callers who are NOT already inside a
// transaction should use this function. Callers already inside a
// transaction (e.g. the progress upsert handler, which needs the streak
// recomputed atomically with the XP update) must call computeStreakWith(tx,
// ..., true) directly instead -- calling computeStreak from within an
// existing transaction on the same pool would try to open a second,
// independent transaction and deadlock waiting for the row lock the outer
// transaction already holds.
func computeStreak(ctx context.Context, pool *pgxpool.Pool, userID string, consumeShields bool) (streakStats, error) {
	if !consumeShields {
		return computeStreakWith(ctx, pool, userID, false)
	}

	var result streakStats
	err := withTx(ctx, pool, func(tx pgx.Tx) error {
		r, err := computeStreakWith(ctx, tx, userID, true)
		if err != nil {
			return err
		}
		result = r
		return nil
	})
	return result, err
}

// computeStreakWith is the shared streak-walk implementation. db is either
// a pool (read-only peek) or a tx (owner's mutating call, row-locked via
// FOR UPDATE so concurrent requests can't double-spend the same shield).
func computeStreakWith(ctx context.Context, db dbExecutor, userID string, consumeShields bool) (streakStats, error) {
	shieldQuery := `SELECT shield_count FROM streak_shields WHERE user_id = $1`
	if consumeShields {
		shieldQuery += ` FOR UPDATE`
	}

	var shields int
	if err := db.QueryRow(ctx, shieldQuery, userID).Scan(&shields); err != nil {
		if !errors.Is(err, pgx.ErrNoRows) {
			return streakStats{}, fmt.Errorf("load shields: %w", err)
		}
		shields = 0
	}

	since := time.Now().UTC().AddDate(0, 0, -400)
	rows, err := db.Query(ctx,
		`SELECT date FROM daily_progress
		 WHERE user_id = $1 AND date >= $2 AND jsonb_array_length(completed_habits) > 0
		 ORDER BY date DESC`, userID, since)
	if err != nil {
		return streakStats{}, fmt.Errorf("load progress dates: %w", err)
	}
	defer rows.Close()

	completed := make(map[string]bool)
	for rows.Next() {
		var d time.Time
		if err := rows.Scan(&d); err != nil {
			return streakStats{}, fmt.Errorf("scan progress date: %w", err)
		}
		completed[d.Format("2006-01-02")] = true
	}
	if err := rows.Err(); err != nil {
		return streakStats{}, err
	}

	today := time.Now().UTC().Truncate(24 * time.Hour)
	cursor := today
	if !completed[cursor.Format("2006-01-02")] {
		cursor = cursor.AddDate(0, 0, -1)
	}

	streak, shieldsUsed := 0, 0
	for {
		key := cursor.Format("2006-01-02")
		if completed[key] {
			streak++
			cursor = cursor.AddDate(0, 0, -1)
			continue
		}
		if streak > 0 && shields-shieldsUsed > 0 {
			shieldsUsed++
			cursor = cursor.AddDate(0, 0, -1)
			continue
		}
		break
	}

	if consumeShields && shieldsUsed > 0 {
		if _, err := db.Exec(ctx,
			`UPDATE streak_shields SET shield_count = shield_count - $2, updated_at = now() WHERE user_id = $1`,
			userID, shieldsUsed); err != nil {
			return streakStats{}, fmt.Errorf("consume shields: %w", err)
		}
	}

	return streakStats{CurrentStreak: streak, ShieldsRemaining: shields - shieldsUsed}, nil
}

// handleGetMeStats serves GET /api/me/stats. Deliberately mutating (it
// consumes shields as a side effect of computing the streak) -- call it
// once on session bootstrap, not on every render.
func (s *Server) handleGetMeStats(w http.ResponseWriter, r *http.Request) {
	userID, _ := userIDFromContext(r.Context())

	stats, err := computeStreak(r.Context(), s.pool, userID, true)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to load stats")
		return
	}

	writeJSON(w, http.StatusOK, stats)
}
