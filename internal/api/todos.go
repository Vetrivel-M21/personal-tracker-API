package api

import (
	"net/http"
	"strings"
	"time"
)

var validTodoStatuses = map[string]bool{"todo": true, "in_progress": true, "done": true}
var validTodoPriorities = map[string]bool{"low": true, "medium": true, "high": true}

type todoResponse struct {
	ID        string `json:"id"`
	Title     string `json:"title"`
	Status    string `json:"status"`
	Priority  string `json:"priority"`
	CreatedAt string `json:"created_at"`
}

func (s *Server) handleListTodos(w http.ResponseWriter, r *http.Request) {
	userID, _ := userIDFromContext(r.Context())

	rows, err := s.pool.Query(r.Context(),
		`SELECT id, title, status, priority, created_at FROM todos WHERE user_id = $1 ORDER BY created_at`, userID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to load todos")
		return
	}
	defer rows.Close()

	todos := []todoResponse{}
	for rows.Next() {
		var t todoResponse
		var createdAt time.Time
		if err := rows.Scan(&t.ID, &t.Title, &t.Status, &t.Priority, &createdAt); err != nil {
			writeError(w, http.StatusInternalServerError, "failed to load todos")
			return
		}
		t.CreatedAt = createdAt.Format(time.RFC3339)
		todos = append(todos, t)
	}
	if err := rows.Err(); err != nil {
		writeError(w, http.StatusInternalServerError, "failed to load todos")
		return
	}

	writeJSON(w, http.StatusOK, todos)
}

type createTodoRequest struct {
	Title    string `json:"title"`
	Priority string `json:"priority"`
}

func (s *Server) handleCreateTodo(w http.ResponseWriter, r *http.Request) {
	userID, _ := userIDFromContext(r.Context())

	var req createTodoRequest
	if err := decodeJSON(r, &req); err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	title := strings.TrimSpace(req.Title)
	if title == "" || len(title) > 200 {
		writeError(w, http.StatusBadRequest, "title must be 1-200 characters")
		return
	}
	priority := req.Priority
	if priority == "" {
		priority = "medium"
	}
	if !validTodoPriorities[priority] {
		writeError(w, http.StatusBadRequest, "priority must be low, medium, or high")
		return
	}

	// New to-dos always start in the "todo" column, regardless of input.
	var t todoResponse
	var createdAt time.Time
	err := s.pool.QueryRow(r.Context(),
		`INSERT INTO todos (user_id, title, priority) VALUES ($1, $2, $3)
		 RETURNING id, title, status, priority, created_at`,
		userID, title, priority).Scan(&t.ID, &t.Title, &t.Status, &t.Priority, &createdAt)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to create todo")
		return
	}
	t.CreatedAt = createdAt.Format(time.RFC3339)

	writeJSON(w, http.StatusCreated, t)
}

type updateTodoRequest struct {
	Status   *string `json:"status"`
	Priority *string `json:"priority"`
}

func (s *Server) handleUpdateTodo(w http.ResponseWriter, r *http.Request) {
	userID, _ := userIDFromContext(r.Context())
	id := r.PathValue("id")

	var req updateTodoRequest
	if err := decodeJSON(r, &req); err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	if req.Status != nil && !validTodoStatuses[*req.Status] {
		writeError(w, http.StatusBadRequest, "status must be todo, in_progress, or done")
		return
	}
	if req.Priority != nil && !validTodoPriorities[*req.Priority] {
		writeError(w, http.StatusBadRequest, "priority must be low, medium, or high")
		return
	}

	var t todoResponse
	var createdAt time.Time
	err := s.pool.QueryRow(r.Context(),
		`UPDATE todos SET status = COALESCE($3, status), priority = COALESCE($4, priority)
		 WHERE id = $1 AND user_id = $2
		 RETURNING id, title, status, priority, created_at`,
		id, userID, req.Status, req.Priority).Scan(&t.ID, &t.Title, &t.Status, &t.Priority, &createdAt)
	if err != nil {
		writeError(w, http.StatusNotFound, "todo not found")
		return
	}
	t.CreatedAt = createdAt.Format(time.RFC3339)

	writeJSON(w, http.StatusOK, t)
}

func (s *Server) handleDeleteTodo(w http.ResponseWriter, r *http.Request) {
	userID, _ := userIDFromContext(r.Context())
	id := r.PathValue("id")

	tag, err := s.pool.Exec(r.Context(), `DELETE FROM todos WHERE id = $1 AND user_id = $2`, id, userID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to delete todo")
		return
	}
	if tag.RowsAffected() == 0 {
		writeError(w, http.StatusNotFound, "todo not found")
		return
	}

	writeJSON(w, http.StatusNoContent, nil)
}
