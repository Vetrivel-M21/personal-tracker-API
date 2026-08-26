package api

import (
	"context"
	"net/http"
	"strings"

	"github.com/jackc/pgx/v5/pgxpool"
)

// Server holds the shared dependencies every handler needs.
type Server struct {
	cfg  *Config
	pool *pgxpool.Pool
}

// NewServer constructs a Server ready to build routes from.
func NewServer(cfg *Config, pool *pgxpool.Pool) *Server {
	return &Server{cfg: cfg, pool: pool}
}

// PurgeExpiredSessions deletes session rows expired more than 30 days ago.
// Exported so cmd/api's daily cleanup ticker can call it.
func (s *Server) PurgeExpiredSessions(ctx context.Context) error {
	return purgeExpiredSessions(ctx, s.pool)
}

// withAuthExceptPublic wraps next with requireAuth, except for /healthz and
// /api/auth/* which must remain reachable without a session.
func (s *Server) withAuthExceptPublic(next http.Handler) http.Handler {
	protected := s.requireAuth(next)
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/healthz" || strings.HasPrefix(r.URL.Path, "/api/auth/") {
			next.ServeHTTP(w, r)
			return
		}
		protected.ServeHTTP(w, r)
	})
}

// Routes builds the full handler chain: recovery -> logging -> origin check
// -> auth (except public routes) -> mux.
func (s *Server) Routes() http.Handler {
	mux := http.NewServeMux()

	mux.HandleFunc("GET /healthz", s.handleHealthz)

	mux.HandleFunc("POST /api/auth/signup", s.handleSignup)
	mux.HandleFunc("POST /api/auth/login", s.handleLogin)
	mux.HandleFunc("POST /api/auth/logout", s.handleLogout)
	mux.HandleFunc("POST /api/auth/refresh", s.handleRefresh)

	mux.HandleFunc("GET /api/me", s.handleGetMe)
	mux.HandleFunc("PATCH /api/me", s.handleUpdateMe)
	mux.HandleFunc("PATCH /api/me/password", s.handleChangePassword)
	mux.HandleFunc("GET /api/me/stats", s.handleGetMeStats)

	mux.HandleFunc("GET /api/habits", s.handleListHabits)
	mux.HandleFunc("POST /api/habits", s.handleCreateHabit)
	mux.HandleFunc("DELETE /api/habits/{id}", s.handleDeleteHabit)

	mux.HandleFunc("GET /api/progress/{date}", s.handleGetProgress)
	mux.HandleFunc("GET /api/progress", s.handleListProgress)
	mux.HandleFunc("PUT /api/progress/{date}", s.handleUpsertProgress)
	mux.HandleFunc("DELETE /api/progress/{date}", s.handleDeleteProgress)

	mux.HandleFunc("GET /api/leaderboard", s.handleLeaderboard)
	mux.HandleFunc("GET /api/users", s.handleListUsers)
	mux.HandleFunc("GET /api/users/{id}/habits-summary", s.handleUserHabitsSummary)

	mux.HandleFunc("GET /api/workouts/templates", s.handleListWorkoutTemplates)
	mux.HandleFunc("GET /api/workouts/templates/{id}", s.handleGetWorkoutTemplate)
	mux.HandleFunc("POST /api/workouts/templates/{id}/clone", s.handleCloneWorkoutTemplate)
	mux.HandleFunc("GET /api/workouts/splits", s.handleListWorkoutSplits)
	mux.HandleFunc("POST /api/workouts/splits", s.handleCreateWorkoutSplit)
	mux.HandleFunc("GET /api/workouts/splits/active", s.handleGetActiveWorkoutSplit)
	mux.HandleFunc("POST /api/workouts/splits/deactivate", s.handleDeactivateWorkoutSplit)
	mux.HandleFunc("GET /api/workouts/splits/{id}", s.handleGetWorkoutSplit)
	mux.HandleFunc("PATCH /api/workouts/splits/{id}", s.handleUpdateWorkoutSplit)
	mux.HandleFunc("DELETE /api/workouts/splits/{id}", s.handleDeleteWorkoutSplit)
	mux.HandleFunc("POST /api/workouts/splits/{id}/activate", s.handleActivateWorkoutSplit)
	mux.HandleFunc("POST /api/workouts/splits/{id}/days", s.handleAddSplitDay)
	mux.HandleFunc("PUT /api/workouts/splits/{id}/days/reorder", s.handleReorderSplitDays)
	mux.HandleFunc("PATCH /api/workouts/splits/{id}/days/{dayId}", s.handleUpdateSplitDay)
	mux.HandleFunc("DELETE /api/workouts/splits/{id}/days/{dayId}", s.handleDeleteSplitDay)
	mux.HandleFunc("POST /api/workouts/splits/{id}/days/{dayId}/exercises", s.handleAddSplitExercise)
	mux.HandleFunc("PUT /api/workouts/splits/{id}/days/{dayId}/exercises/reorder", s.handleReorderSplitExercises)
	mux.HandleFunc("PATCH /api/workouts/splits/{id}/days/{dayId}/exercises/{exId}", s.handleUpdateSplitExercise)
	mux.HandleFunc("DELETE /api/workouts/splits/{id}/days/{dayId}/exercises/{exId}", s.handleDeleteSplitExercise)

	mux.HandleFunc("POST /api/workouts/sessions", s.handleLogWorkoutSession)
	mux.HandleFunc("GET /api/workouts/sessions", s.handleListWorkoutSessions)
	mux.HandleFunc("GET /api/workouts/sessions/{id}", s.handleGetWorkoutSession)
	mux.HandleFunc("PUT /api/workouts/sessions/{id}", s.handleUpdateWorkoutSession)
	mux.HandleFunc("DELETE /api/workouts/sessions/{id}", s.handleDeleteWorkoutSession)
	mux.HandleFunc("GET /api/workouts/exercise-names", s.handleListExerciseNames)
	mux.HandleFunc("GET /api/workouts/exercise-history/{name}", s.handleGetExerciseHistory)

	var handler http.Handler = mux
	handler = s.withAuthExceptPublic(handler)
	handler = s.requireOrigin(handler)
	handler = loggingMiddleware(handler)
	handler = recoverMiddleware(handler)
	return handler
}
