package api

import (
	"net/http"
	"strings"
	"time"
)

type habitResponse struct {
	ID         string  `json:"id"`
	Name       string  `json:"name"`
	Color      string  `json:"color"`
	Icon       string  `json:"icon"`
	Schedule   int16   `json:"schedule"`
	ArchivedAt *string `json:"archived_at,omitempty"`
	CreatedAt  string  `json:"created_at"`
}

func (s *Server) handleListHabits(w http.ResponseWriter, r *http.Request) {
	userID, _ := userIDFromContext(r.Context())

	archivedFilter := "archived_at IS NULL"
	if r.URL.Query().Get("archived") == "true" {
		archivedFilter = "archived_at IS NOT NULL"
	}

	rows, err := s.pool.Query(r.Context(),
		`SELECT id, name, color, icon, schedule, archived_at, created_at FROM habits
		 WHERE user_id = $1 AND `+archivedFilter+` ORDER BY created_at`, userID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to load habits")
		return
	}
	defer rows.Close()

	habits := []habitResponse{}
	for rows.Next() {
		var h habitResponse
		var createdAt time.Time
		var archivedAt *time.Time
		if err := rows.Scan(&h.ID, &h.Name, &h.Color, &h.Icon, &h.Schedule, &archivedAt, &createdAt); err != nil {
			writeError(w, http.StatusInternalServerError, "failed to load habits")
			return
		}
		h.CreatedAt = createdAt.Format(time.RFC3339)
		if archivedAt != nil {
			formatted := archivedAt.Format(time.RFC3339)
			h.ArchivedAt = &formatted
		}
		habits = append(habits, h)
	}
	if err := rows.Err(); err != nil {
		writeError(w, http.StatusInternalServerError, "failed to load habits")
		return
	}

	writeJSON(w, http.StatusOK, habits)
}

type createHabitRequest struct {
	Name     string `json:"name"`
	Color    string `json:"color"`
	Icon     string `json:"icon"`
	Schedule *int16 `json:"schedule,omitempty"`
}

func (s *Server) handleCreateHabit(w http.ResponseWriter, r *http.Request) {
	userID, _ := userIDFromContext(r.Context())

	var req createHabitRequest
	if err := decodeJSON(r, &req); err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	name := strings.TrimSpace(req.Name)
	if name == "" || len(name) > 50 {
		writeError(w, http.StatusBadRequest, "name must be 1-50 characters")
		return
	}
	color := req.Color
	if color == "" {
		color = "#6366f1"
	}
	icon := req.Icon
	if icon == "" {
		icon = "fa-solid fa-star"
	}
	var schedule int16 = 127
	if req.Schedule != nil {
		schedule = *req.Schedule
	}

	var h habitResponse
	var createdAt time.Time
	err := s.pool.QueryRow(r.Context(),
		`INSERT INTO habits (user_id, name, color, icon, schedule) VALUES ($1,$2,$3,$4,$5)
		 RETURNING id, name, color, icon, schedule, created_at`,
		userID, name, color, icon, schedule).Scan(&h.ID, &h.Name, &h.Color, &h.Icon, &h.Schedule, &createdAt)
	if err != nil {
		if isUniqueViolation(err) {
			writeError(w, http.StatusConflict, "a habit with this name already exists")
			return
		}
		writeError(w, http.StatusInternalServerError, "failed to create habit")
		return
	}
	h.CreatedAt = createdAt.Format(time.RFC3339)

	writeJSON(w, http.StatusCreated, h)
}

type updateHabitRequest struct {
	Name     *string `json:"name"`
	Color    *string `json:"color"`
	Icon     *string `json:"icon"`
	Schedule *int16  `json:"schedule"`
}

func (s *Server) handleUpdateHabit(w http.ResponseWriter, r *http.Request) {
	userID, _ := userIDFromContext(r.Context())
	id := r.PathValue("id")

	var req updateHabitRequest
	if err := decodeJSON(r, &req); err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	if req.Name != nil {
		trimmed := strings.TrimSpace(*req.Name)
		if trimmed == "" || len(trimmed) > 50 {
			writeError(w, http.StatusBadRequest, "name must be 1-50 characters")
			return
		}
		req.Name = &trimmed
	}

	var h habitResponse
	var createdAt time.Time
	var archivedAt *time.Time
	err := s.pool.QueryRow(r.Context(),
		`UPDATE habits SET
		   name = COALESCE($3, name),
		   color = COALESCE($4, color),
		   icon = COALESCE($5, icon),
		   schedule = COALESCE($6, schedule)
		 WHERE id = $1 AND user_id = $2
		 RETURNING id, name, color, icon, schedule, archived_at, created_at`,
		id, userID, req.Name, req.Color, req.Icon, req.Schedule).
		Scan(&h.ID, &h.Name, &h.Color, &h.Icon, &h.Schedule, &archivedAt, &createdAt)
	if err != nil {
		if isUniqueViolation(err) {
			writeError(w, http.StatusConflict, "a habit with this name already exists")
			return
		}
		writeError(w, http.StatusNotFound, "habit not found")
		return
	}
	h.CreatedAt = createdAt.Format(time.RFC3339)
	if archivedAt != nil {
		formatted := archivedAt.Format(time.RFC3339)
		h.ArchivedAt = &formatted
	}

	writeJSON(w, http.StatusOK, h)
}

// handleDeleteHabit archives rather than hard-deletes, so a habit's name/icon
// stay resolvable for anyone looking back at old logged progress that still
// references its id.
func (s *Server) handleDeleteHabit(w http.ResponseWriter, r *http.Request) {
	userID, _ := userIDFromContext(r.Context())
	id := r.PathValue("id")

	tag, err := s.pool.Exec(r.Context(),
		`UPDATE habits SET archived_at = now() WHERE id = $1 AND user_id = $2 AND archived_at IS NULL`,
		id, userID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to delete habit")
		return
	}
	if tag.RowsAffected() == 0 {
		writeError(w, http.StatusNotFound, "habit not found")
		return
	}

	writeJSON(w, http.StatusNoContent, nil)
}

func (s *Server) handleRestoreHabit(w http.ResponseWriter, r *http.Request) {
	userID, _ := userIDFromContext(r.Context())
	id := r.PathValue("id")

	var h habitResponse
	var createdAt time.Time
	err := s.pool.QueryRow(r.Context(),
		`UPDATE habits SET archived_at = NULL WHERE id = $1 AND user_id = $2 AND archived_at IS NOT NULL
		 RETURNING id, name, color, icon, schedule, created_at`,
		id, userID).Scan(&h.ID, &h.Name, &h.Color, &h.Icon, &h.Schedule, &createdAt)
	if err != nil {
		writeError(w, http.StatusNotFound, "archived habit not found")
		return
	}
	h.CreatedAt = createdAt.Format(time.RFC3339)

	writeJSON(w, http.StatusOK, h)
}
