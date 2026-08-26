package api

import (
	"net/http"
	"strings"
	"time"
)

type habitResponse struct {
	ID        string `json:"id"`
	Name      string `json:"name"`
	Color     string `json:"color"`
	Icon      string `json:"icon"`
	CreatedAt string `json:"created_at"`
}

func (s *Server) handleListHabits(w http.ResponseWriter, r *http.Request) {
	userID, _ := userIDFromContext(r.Context())

	rows, err := s.pool.Query(r.Context(),
		`SELECT id, name, color, icon, created_at FROM habits WHERE user_id = $1 ORDER BY created_at`, userID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to load habits")
		return
	}
	defer rows.Close()

	habits := []habitResponse{}
	for rows.Next() {
		var h habitResponse
		var createdAt time.Time
		if err := rows.Scan(&h.ID, &h.Name, &h.Color, &h.Icon, &createdAt); err != nil {
			writeError(w, http.StatusInternalServerError, "failed to load habits")
			return
		}
		h.CreatedAt = createdAt.Format(time.RFC3339)
		habits = append(habits, h)
	}
	if err := rows.Err(); err != nil {
		writeError(w, http.StatusInternalServerError, "failed to load habits")
		return
	}

	writeJSON(w, http.StatusOK, habits)
}

type createHabitRequest struct {
	Name  string `json:"name"`
	Color string `json:"color"`
	Icon  string `json:"icon"`
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

	var h habitResponse
	var createdAt time.Time
	err := s.pool.QueryRow(r.Context(),
		`INSERT INTO habits (user_id, name, color, icon) VALUES ($1,$2,$3,$4)
		 RETURNING id, name, color, icon, created_at`,
		userID, name, color, icon).Scan(&h.ID, &h.Name, &h.Color, &h.Icon, &createdAt)
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

func (s *Server) handleDeleteHabit(w http.ResponseWriter, r *http.Request) {
	userID, _ := userIDFromContext(r.Context())
	id := r.PathValue("id")

	tag, err := s.pool.Exec(r.Context(), `DELETE FROM habits WHERE id = $1 AND user_id = $2`, id, userID)
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
