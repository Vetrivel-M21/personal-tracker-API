package api

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
)

var errLabelRequired = errors.New("label is required for an ad-hoc session (no splitDayId given)")

// ---------------------------------------------------------------------
// Premade templates -- mirrors the codebase's existing precedent of
// hardcoded content arrays (e.g. the old CALISTHENICS_SKILLS list).
// ---------------------------------------------------------------------

type templateExercise struct {
	Name       string `json:"name"`
	TargetSets int    `json:"target_sets"`
	TargetReps string `json:"target_reps"`
	Notes      string `json:"notes,omitempty"`
}

type templateDay struct {
	Name      string             `json:"name"`
	Exercises []templateExercise `json:"exercises"`
}

type workoutTemplate struct {
	ID          string        `json:"id"`
	Name        string        `json:"name"`
	Description string        `json:"description"`
	DaysPerWeek int           `json:"days_per_week"`
	Level       string        `json:"level"`
	Days        []templateDay `json:"days"`
}

func ex(name string, sets int, reps string) templateExercise {
	return templateExercise{Name: name, TargetSets: sets, TargetReps: reps}
}

var workoutTemplates = []workoutTemplate{
	{
		ID: "ppl-6day", Name: "Push/Pull/Legs", DaysPerWeek: 6, Level: "Intermediate",
		Description: "Push and pull movements separated from legs, each hit twice a week.",
		Days: []templateDay{
			{Name: "Push A", Exercises: []templateExercise{
				ex("Barbell Bench Press", 4, "6-8"), ex("Overhead Press", 3, "8-10"), ex("Incline Dumbbell Press", 3, "10-12"),
				ex("Lateral Raise", 3, "12-15"), ex("Triceps Pushdown", 3, "12-15"), ex("Skull Crushers", 3, "10-12"),
			}},
			{Name: "Pull A", Exercises: []templateExercise{
				ex("Deadlift", 3, "5"), ex("Pull-Ups", 4, "6-10"), ex("Barbell Row", 3, "8-10"),
				ex("Seated Cable Row", 3, "10-12"), ex("Face Pull", 3, "15"), ex("Barbell Curl", 3, "10-12"),
			}},
			{Name: "Legs A", Exercises: []templateExercise{
				ex("Back Squat", 4, "6-8"), ex("Romanian Deadlift", 3, "8-10"), ex("Leg Press", 3, "10-12"),
				ex("Leg Curl", 3, "12-15"), ex("Standing Calf Raise", 4, "12-15"), ex("Hanging Leg Raise", 3, "15"),
			}},
			{Name: "Push B", Exercises: []templateExercise{
				ex("Overhead Press", 4, "6-8"), ex("Incline Barbell Press", 3, "8-10"), ex("Flat Dumbbell Fly", 3, "12-15"),
				ex("Dips", 3, "10-12"), ex("Cable Lateral Raise", 3, "15"), ex("Overhead Triceps Extension", 3, "12"),
			}},
			{Name: "Pull B", Exercises: []templateExercise{
				ex("Weighted Pull-Ups", 4, "6-8"), ex("Pendlay Row", 3, "8-10"), ex("Lat Pulldown", 3, "10-12"),
				ex("Rear Delt Fly", 3, "15"), ex("Hammer Curl", 3, "10-12"), ex("EZ-Bar Curl", 3, "12"),
			}},
			{Name: "Legs B", Exercises: []templateExercise{
				ex("Front Squat", 4, "6-8"), ex("Walking Lunges", 3, "10/leg"), ex("Leg Extension", 3, "12-15"),
				ex("Seated Leg Curl", 3, "12-15"), ex("Seated Calf Raise", 4, "15"), ex("Cable Crunch", 3, "15"),
			}},
		},
	},
	{
		ID: "upper-lower-4day", Name: "Upper/Lower", DaysPerWeek: 4, Level: "Intermediate",
		Description: "Four days a week alternating upper- and lower-body sessions.",
		Days: []templateDay{
			{Name: "Upper A", Exercises: []templateExercise{
				ex("Bench Press", 4, "6-8"), ex("Barbell Row", 4, "6-8"), ex("Overhead Press", 3, "8-10"),
				ex("Lat Pulldown", 3, "10-12"), ex("Dumbbell Curl", 3, "10-12"), ex("Triceps Rope Pushdown", 3, "10-12"),
			}},
			{Name: "Lower A", Exercises: []templateExercise{
				ex("Back Squat", 4, "6-8"), ex("Romanian Deadlift", 3, "8-10"), ex("Leg Press", 3, "10-12"),
				ex("Standing Calf Raise", 4, "12-15"), ex("Hanging Knee Raise", 3, "15"),
			}},
			{Name: "Upper B", Exercises: []templateExercise{
				ex("Incline Dumbbell Press", 4, "8-10"), ex("Pull-Ups", 4, "6-10"), ex("Seated Dumbbell Shoulder Press", 3, "8-10"),
				ex("Seated Cable Row", 3, "10-12"), ex("Lateral Raise", 3, "15"), ex("EZ-Bar Curl", 3, "10-12"),
			}},
			{Name: "Lower B", Exercises: []templateExercise{
				ex("Deadlift", 4, "5"), ex("Front Squat", 3, "8"), ex("Walking Lunges", 3, "10/leg"),
				ex("Leg Curl", 3, "12-15"), ex("Seated Calf Raise", 4, "15"),
			}},
		},
	},
	{
		ID: "full-body-3day", Name: "Full Body", DaysPerWeek: 3, Level: "Beginner",
		Description: "Linear-progression style, the standard beginner recommendation.",
		Days: []templateDay{
			{Name: "Full Body A", Exercises: []templateExercise{
				ex("Back Squat", 3, "5"), ex("Bench Press", 3, "5"), ex("Barbell Row", 3, "8-10"), ex("Plank", 3, "30-45s"),
			}},
			{Name: "Full Body B", Exercises: []templateExercise{
				ex("Deadlift", 3, "5"), ex("Overhead Press", 3, "5"), ex("Lat Pulldown", 3, "10-12"), ex("Dumbbell Lunges", 3, "10/leg"),
			}},
			{Name: "Full Body C", Exercises: []templateExercise{
				ex("Front Squat", 3, "6-8"), ex("Incline Dumbbell Press", 3, "8-10"), ex("Pull-Ups", 3, "8-10"),
				ex("Leg Curl", 3, "12-15"), ex("Cable Crunch", 3, "15"),
			}},
		},
	},
	{
		ID: "bro-split-5day", Name: "Classic Bro Split", DaysPerWeek: 5, Level: "Intermediate/Advanced",
		Description: "One muscle group per day -- the classic bodybuilding template.",
		Days: []templateDay{
			{Name: "Chest", Exercises: []templateExercise{
				ex("Barbell Bench Press", 4, "8-10"), ex("Incline Dumbbell Press", 4, "10-12"), ex("Cable Fly", 3, "12-15"),
				ex("Dips", 3, "10-12"), ex("Push-Ups", 2, "AMRAP"),
			}},
			{Name: "Back", Exercises: []templateExercise{
				ex("Deadlift", 4, "6-8"), ex("Pull-Ups", 4, "8-10"), ex("Barbell Row", 4, "8-10"),
				ex("Lat Pulldown", 3, "10-12"), ex("Seated Cable Row", 3, "12"),
			}},
			{Name: "Shoulders", Exercises: []templateExercise{
				ex("Overhead Press", 4, "8-10"), ex("Lateral Raise", 4, "12-15"), ex("Rear Delt Fly", 3, "15"),
				ex("Front Raise", 3, "12-15"), ex("Barbell Shrug", 3, "12"),
			}},
			{Name: "Arms", Exercises: []templateExercise{
				ex("Barbell Curl", 4, "10-12"), ex("Close-Grip Bench Press", 4, "10-12"), ex("Hammer Curl", 3, "12"),
				ex("Triceps Pushdown", 3, "12-15"), ex("Preacher Curl", 3, "12"), ex("Overhead Triceps Extension", 3, "12"),
			}},
			{Name: "Legs", Exercises: []templateExercise{
				ex("Back Squat", 4, "8-10"), ex("Leg Press", 4, "10-12"), ex("Romanian Deadlift", 3, "10-12"),
				ex("Leg Extension", 3, "12-15"), ex("Leg Curl", 3, "12-15"), ex("Standing Calf Raise", 4, "15"),
			}},
		},
	},
	{
		ID: "arnold-6day", Name: "Arnold Split", DaysPerWeek: 6, Level: "Advanced",
		Description: "Antagonist-paired split popularized by Arnold Schwarzenegger's training.",
		Days: []templateDay{
			{Name: "Chest & Back (1)", Exercises: []templateExercise{
				ex("Flat Bench Press", 4, "8-10"), ex("Wide-Grip Pull-Ups", 4, "8-10"), ex("Incline Dumbbell Press", 3, "10-12"),
				ex("Barbell Row", 3, "10-12"), ex("Dumbbell Fly", 3, "12-15"), ex("Straight-Arm Pulldown", 3, "12-15"), ex("Dips", 3, "AMRAP"),
			}},
			{Name: "Shoulders & Arms (1)", Exercises: []templateExercise{
				ex("Seated Barbell Press", 4, "8-10"), ex("Lateral Raise", 3, "12-15"), ex("Rear Delt Fly", 3, "12-15"),
				ex("Barbell Curl", 4, "10-12"), ex("Close-Grip Bench Press", 4, "10-12"), ex("Cable Curl", 3, "12"), ex("Triceps Pushdown", 3, "12"),
			}},
			{Name: "Legs & Abs (1)", Exercises: []templateExercise{
				ex("Back Squat", 4, "8-10"), ex("Leg Press", 4, "10-12"), ex("Walking Lunges", 3, "10/leg"),
				ex("Leg Curl", 3, "12-15"), ex("Standing Calf Raise", 4, "15"), ex("Hanging Leg Raise", 3, "15"),
			}},
			{Name: "Chest & Back (2)", Exercises: []templateExercise{
				ex("Incline Barbell Press", 4, "8-10"), ex("Weighted Pull-Ups", 4, "8-10"), ex("Cable Fly", 3, "12-15"),
				ex("T-Bar Row", 3, "10-12"), ex("Pullover", 3, "12"), ex("Seated Cable Row", 3, "12"),
			}},
			{Name: "Shoulders & Arms (2)", Exercises: []templateExercise{
				ex("Arnold Press", 4, "8-10"), ex("Front Raise", 3, "12-15"), ex("Face Pull", 3, "15"),
				ex("Preacher Curl", 4, "10-12"), ex("Overhead Triceps Extension", 4, "10-12"), ex("Hammer Curl", 3, "12"),
			}},
			{Name: "Legs & Abs (2)", Exercises: []templateExercise{
				ex("Front Squat", 4, "8-10"), ex("Romanian Deadlift", 3, "10-12"), ex("Leg Extension", 3, "12-15"),
				ex("Seated Leg Curl", 3, "12-15"), ex("Seated Calf Raise", 4, "15"), ex("Cable Crunch", 3, "15"),
			}},
		},
	},
}

func findTemplate(id string) (workoutTemplate, bool) {
	for _, t := range workoutTemplates {
		if t.ID == id {
			return t, true
		}
	}
	return workoutTemplate{}, false
}

type templateSummary struct {
	ID          string `json:"id"`
	Name        string `json:"name"`
	Description string `json:"description"`
	DaysPerWeek int    `json:"days_per_week"`
	Level       string `json:"level"`
}

func (s *Server) handleListWorkoutTemplates(w http.ResponseWriter, r *http.Request) {
	summaries := make([]templateSummary, 0, len(workoutTemplates))
	for _, t := range workoutTemplates {
		summaries = append(summaries, templateSummary{t.ID, t.Name, t.Description, t.DaysPerWeek, t.Level})
	}
	writeJSON(w, http.StatusOK, summaries)
}

func (s *Server) handleGetWorkoutTemplate(w http.ResponseWriter, r *http.Request) {
	t, ok := findTemplate(r.PathValue("id"))
	if !ok {
		writeError(w, http.StatusNotFound, "template not found")
		return
	}
	writeJSON(w, http.StatusOK, t)
}

// ---------------------------------------------------------------------
// Splits / days / exercises -- shared response shapes and helpers
// ---------------------------------------------------------------------

type exerciseResponse struct {
	ID         string `json:"id"`
	Name       string `json:"name"`
	TargetSets int    `json:"target_sets"`
	TargetReps string `json:"target_reps"`
	Notes      string `json:"notes"`
	Order      int    `json:"order"`
}

type dayResponse struct {
	ID        string             `json:"id"`
	Name      string             `json:"name"`
	Order     int                `json:"order"`
	Exercises []exerciseResponse `json:"exercises"`
}

type splitResponse struct {
	ID       string        `json:"id"`
	Name     string        `json:"name"`
	Source   string        `json:"source"`
	IsActive bool          `json:"is_active"`
	Days     []dayResponse `json:"days"`
}

type splitSummaryResponse struct {
	ID       string `json:"id"`
	Name     string `json:"name"`
	Source   string `json:"source"`
	IsActive bool   `json:"is_active"`
	DayCount int    `json:"day_count"`
}

func ownsSplit(ctx context.Context, db dbExecutor, splitID, userID string) (bool, error) {
	var exists bool
	err := db.QueryRow(ctx,
		`SELECT EXISTS(SELECT 1 FROM workout_splits WHERE id = $1 AND user_id = $2)`, splitID, userID).Scan(&exists)
	return exists, err
}

func ownsDay(ctx context.Context, db dbExecutor, splitID, dayID, userID string) (bool, error) {
	var exists bool
	err := db.QueryRow(ctx,
		`SELECT EXISTS(
			SELECT 1 FROM workout_split_days d
			JOIN workout_splits s ON s.id = d.split_id
			WHERE d.id = $1 AND d.split_id = $2 AND s.user_id = $3
		)`, dayID, splitID, userID).Scan(&exists)
	return exists, err
}

// daySplitAndOwner resolves a bare day id to its split id and owning user,
// used when logging a session against a day without the split id in hand.
func daySplitAndOwner(ctx context.Context, db dbExecutor, dayID string) (splitID, userID string, err error) {
	err = db.QueryRow(ctx,
		`SELECT d.split_id, s.user_id FROM workout_split_days d
		 JOIN workout_splits s ON s.id = d.split_id WHERE d.id = $1`, dayID).Scan(&splitID, &userID)
	return splitID, userID, err
}

func loadSplitDetail(ctx context.Context, db dbExecutor, splitID string) (splitResponse, error) {
	var sr splitResponse
	if err := db.QueryRow(ctx,
		`SELECT id, name, source, is_active FROM workout_splits WHERE id = $1`, splitID).
		Scan(&sr.ID, &sr.Name, &sr.Source, &sr.IsActive); err != nil {
		return splitResponse{}, err
	}

	dayRows, err := db.Query(ctx,
		`SELECT id, name, day_order FROM workout_split_days WHERE split_id = $1 ORDER BY day_order`, splitID)
	if err != nil {
		return splitResponse{}, err
	}
	sr.Days = []dayResponse{}
	for dayRows.Next() {
		var d dayResponse
		if err := dayRows.Scan(&d.ID, &d.Name, &d.Order); err != nil {
			dayRows.Close()
			return splitResponse{}, err
		}
		d.Exercises = []exerciseResponse{}
		sr.Days = append(sr.Days, d)
	}
	dayErr := dayRows.Err()
	dayRows.Close()
	if dayErr != nil {
		return splitResponse{}, dayErr
	}

	for i := range sr.Days {
		exRows, err := db.Query(ctx,
			`SELECT id, exercise_name, target_sets, target_reps, notes, exercise_order
			 FROM workout_split_exercises WHERE split_day_id = $1 ORDER BY exercise_order`, sr.Days[i].ID)
		if err != nil {
			return splitResponse{}, err
		}
		for exRows.Next() {
			var e exerciseResponse
			var notes *string
			if err := exRows.Scan(&e.ID, &e.Name, &e.TargetSets, &e.TargetReps, &notes, &e.Order); err != nil {
				exRows.Close()
				return splitResponse{}, err
			}
			if notes != nil {
				e.Notes = *notes
			}
			sr.Days[i].Exercises = append(sr.Days[i].Exercises, e)
		}
		exErr := exRows.Err()
		exRows.Close()
		if exErr != nil {
			return splitResponse{}, exErr
		}
	}

	return sr, nil
}

// renumberDays/renumberExercises compact day_order/exercise_order back to a
// dense 1..N sequence after a delete. Two-phase (negative, then positive) so
// a single UPDATE never collides with the UNIQUE(split_id, day_order) /
// UNIQUE(split_day_id, exercise_order) constraint mid-statement.
func renumberDays(ctx context.Context, tx pgx.Tx, splitID string) error {
	if _, err := tx.Exec(ctx, `
		WITH ranked AS (
			SELECT id, ROW_NUMBER() OVER (ORDER BY day_order) AS rn
			FROM workout_split_days WHERE split_id = $1
		)
		UPDATE workout_split_days d SET day_order = -ranked.rn FROM ranked WHERE d.id = ranked.id`, splitID); err != nil {
		return err
	}
	_, err := tx.Exec(ctx,
		`UPDATE workout_split_days SET day_order = -day_order WHERE split_id = $1 AND day_order < 0`, splitID)
	return err
}

func renumberExercises(ctx context.Context, tx pgx.Tx, dayID string) error {
	if _, err := tx.Exec(ctx, `
		WITH ranked AS (
			SELECT id, ROW_NUMBER() OVER (ORDER BY exercise_order) AS rn
			FROM workout_split_exercises WHERE split_day_id = $1
		)
		UPDATE workout_split_exercises e SET exercise_order = -ranked.rn FROM ranked WHERE e.id = ranked.id`, dayID); err != nil {
		return err
	}
	_, err := tx.Exec(ctx,
		`UPDATE workout_split_exercises SET exercise_order = -exercise_order WHERE split_day_id = $1 AND exercise_order < 0`, dayID)
	return err
}

// ---------------------------------------------------------------------
// Split handlers
// ---------------------------------------------------------------------

func (s *Server) handleListWorkoutSplits(w http.ResponseWriter, r *http.Request) {
	userID, _ := userIDFromContext(r.Context())

	rows, err := s.pool.Query(r.Context(), `
		SELECT s.id, s.name, s.source, s.is_active, COUNT(d.id)
		FROM workout_splits s
		LEFT JOIN workout_split_days d ON d.split_id = s.id
		WHERE s.user_id = $1
		GROUP BY s.id, s.name, s.source, s.is_active
		ORDER BY s.created_at`, userID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to load splits")
		return
	}
	defer rows.Close()

	splits := []splitSummaryResponse{}
	for rows.Next() {
		var sm splitSummaryResponse
		if err := rows.Scan(&sm.ID, &sm.Name, &sm.Source, &sm.IsActive, &sm.DayCount); err != nil {
			writeError(w, http.StatusInternalServerError, "failed to load splits")
			return
		}
		splits = append(splits, sm)
	}
	writeJSON(w, http.StatusOK, splits)
}

func (s *Server) handleCreateWorkoutSplit(w http.ResponseWriter, r *http.Request) {
	userID, _ := userIDFromContext(r.Context())

	var req struct {
		Name string `json:"name"`
	}
	if err := decodeJSON(r, &req); err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	name := strings.TrimSpace(req.Name)
	if name == "" {
		writeError(w, http.StatusBadRequest, "name is required")
		return
	}

	var splitID string
	if err := s.pool.QueryRow(r.Context(),
		`INSERT INTO workout_splits (user_id, name, source) VALUES ($1,$2,'custom') RETURNING id`,
		userID, name).Scan(&splitID); err != nil {
		writeError(w, http.StatusInternalServerError, "failed to create split")
		return
	}

	detail, err := loadSplitDetail(r.Context(), s.pool, splitID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to create split")
		return
	}
	writeJSON(w, http.StatusCreated, detail)
}

func (s *Server) handleCloneWorkoutTemplate(w http.ResponseWriter, r *http.Request) {
	userID, _ := userIDFromContext(r.Context())
	t, ok := findTemplate(r.PathValue("id"))
	if !ok {
		writeError(w, http.StatusNotFound, "template not found")
		return
	}

	var splitID string
	err := withTx(r.Context(), s.pool, func(tx pgx.Tx) error {
		if err := tx.QueryRow(r.Context(),
			`INSERT INTO workout_splits (user_id, name, source, source_template_id) VALUES ($1,$2,'premade',$3) RETURNING id`,
			userID, t.Name, t.ID).Scan(&splitID); err != nil {
			return fmt.Errorf("create split: %w", err)
		}

		for dayIdx, day := range t.Days {
			var dayID string
			if err := tx.QueryRow(r.Context(),
				`INSERT INTO workout_split_days (split_id, day_order, name) VALUES ($1,$2,$3) RETURNING id`,
				splitID, dayIdx+1, day.Name).Scan(&dayID); err != nil {
				return fmt.Errorf("create day: %w", err)
			}
			for exIdx, exercise := range day.Exercises {
				if _, err := tx.Exec(r.Context(),
					`INSERT INTO workout_split_exercises (split_day_id, exercise_order, exercise_name, target_sets, target_reps, notes)
					 VALUES ($1,$2,$3,$4,$5,$6)`,
					dayID, exIdx+1, exercise.Name, exercise.TargetSets, exercise.TargetReps, nullableString(exercise.Notes)); err != nil {
					return fmt.Errorf("create exercise: %w", err)
				}
			}
		}
		return nil
	})
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to clone template")
		return
	}

	detail, err := loadSplitDetail(r.Context(), s.pool, splitID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to load cloned split")
		return
	}
	writeJSON(w, http.StatusCreated, detail)
}

func (s *Server) handleGetActiveWorkoutSplit(w http.ResponseWriter, r *http.Request) {
	userID, _ := userIDFromContext(r.Context())

	var splitID string
	err := s.pool.QueryRow(r.Context(),
		`SELECT id FROM workout_splits WHERE user_id = $1 AND is_active`, userID).Scan(&splitID)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			writeError(w, http.StatusNotFound, "no active split")
			return
		}
		writeError(w, http.StatusInternalServerError, "failed to load active split")
		return
	}

	detail, err := loadSplitDetail(r.Context(), s.pool, splitID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to load active split")
		return
	}
	writeJSON(w, http.StatusOK, detail)
}

func (s *Server) handleGetWorkoutSplit(w http.ResponseWriter, r *http.Request) {
	userID, _ := userIDFromContext(r.Context())
	splitID := r.PathValue("id")

	owns, err := ownsSplit(r.Context(), s.pool, splitID, userID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to load split")
		return
	}
	if !owns {
		writeError(w, http.StatusNotFound, "split not found")
		return
	}

	detail, err := loadSplitDetail(r.Context(), s.pool, splitID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to load split")
		return
	}
	writeJSON(w, http.StatusOK, detail)
}

func (s *Server) handleUpdateWorkoutSplit(w http.ResponseWriter, r *http.Request) {
	userID, _ := userIDFromContext(r.Context())
	splitID := r.PathValue("id")

	var req struct {
		Name string `json:"name"`
	}
	if err := decodeJSON(r, &req); err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	name := strings.TrimSpace(req.Name)
	if name == "" {
		writeError(w, http.StatusBadRequest, "name is required")
		return
	}

	tag, err := s.pool.Exec(r.Context(),
		`UPDATE workout_splits SET name = $3, updated_at = now() WHERE id = $1 AND user_id = $2`, splitID, userID, name)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to update split")
		return
	}
	if tag.RowsAffected() == 0 {
		writeError(w, http.StatusNotFound, "split not found")
		return
	}

	detail, err := loadSplitDetail(r.Context(), s.pool, splitID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to update split")
		return
	}
	writeJSON(w, http.StatusOK, detail)
}

func (s *Server) handleDeleteWorkoutSplit(w http.ResponseWriter, r *http.Request) {
	userID, _ := userIDFromContext(r.Context())
	splitID := r.PathValue("id")

	tag, err := s.pool.Exec(r.Context(), `DELETE FROM workout_splits WHERE id = $1 AND user_id = $2`, splitID, userID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to delete split")
		return
	}
	if tag.RowsAffected() == 0 {
		writeError(w, http.StatusNotFound, "split not found")
		return
	}
	writeJSON(w, http.StatusNoContent, nil)
}

func (s *Server) handleActivateWorkoutSplit(w http.ResponseWriter, r *http.Request) {
	userID, _ := userIDFromContext(r.Context())
	splitID := r.PathValue("id")

	err := withTx(r.Context(), s.pool, func(tx pgx.Tx) error {
		owns, err := ownsSplit(r.Context(), tx, splitID, userID)
		if err != nil {
			return err
		}
		if !owns {
			return errNotFound
		}
		if _, err := tx.Exec(r.Context(),
			`UPDATE workout_splits SET is_active = false WHERE user_id = $1 AND is_active`, userID); err != nil {
			return err
		}
		_, err = tx.Exec(r.Context(),
			`UPDATE workout_splits SET is_active = true, updated_at = now() WHERE id = $1`, splitID)
		return err
	})
	if errors.Is(err, errNotFound) {
		writeError(w, http.StatusNotFound, "split not found")
		return
	}
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to activate split")
		return
	}
	writeJSON(w, http.StatusNoContent, nil)
}

func (s *Server) handleDeactivateWorkoutSplit(w http.ResponseWriter, r *http.Request) {
	userID, _ := userIDFromContext(r.Context())
	if _, err := s.pool.Exec(r.Context(),
		`UPDATE workout_splits SET is_active = false WHERE user_id = $1 AND is_active`, userID); err != nil {
		writeError(w, http.StatusInternalServerError, "failed to deactivate split")
		return
	}
	writeJSON(w, http.StatusNoContent, nil)
}

// ---------------------------------------------------------------------
// Day handlers
// ---------------------------------------------------------------------

func (s *Server) handleAddSplitDay(w http.ResponseWriter, r *http.Request) {
	userID, _ := userIDFromContext(r.Context())
	splitID := r.PathValue("id")

	var req struct {
		Name string `json:"name"`
	}
	if err := decodeJSON(r, &req); err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	name := strings.TrimSpace(req.Name)
	if name == "" {
		writeError(w, http.StatusBadRequest, "name is required")
		return
	}

	var day dayResponse
	day.Exercises = []exerciseResponse{}
	err := withTx(r.Context(), s.pool, func(tx pgx.Tx) error {
		owns, err := ownsSplit(r.Context(), tx, splitID, userID)
		if err != nil {
			return err
		}
		if !owns {
			return errNotFound
		}

		var nextOrder int
		if err := tx.QueryRow(r.Context(),
			`SELECT COALESCE(MAX(day_order),0)+1 FROM workout_split_days WHERE split_id = $1`, splitID).Scan(&nextOrder); err != nil {
			return err
		}
		return tx.QueryRow(r.Context(),
			`INSERT INTO workout_split_days (split_id, day_order, name) VALUES ($1,$2,$3) RETURNING id, name, day_order`,
			splitID, nextOrder, name).Scan(&day.ID, &day.Name, &day.Order)
	})
	if errors.Is(err, errNotFound) {
		writeError(w, http.StatusNotFound, "split not found")
		return
	}
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to add day")
		return
	}
	writeJSON(w, http.StatusCreated, day)
}

func (s *Server) handleUpdateSplitDay(w http.ResponseWriter, r *http.Request) {
	userID, _ := userIDFromContext(r.Context())
	splitID := r.PathValue("id")
	dayID := r.PathValue("dayId")

	var req struct {
		Name string `json:"name"`
	}
	if err := decodeJSON(r, &req); err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	name := strings.TrimSpace(req.Name)
	if name == "" {
		writeError(w, http.StatusBadRequest, "name is required")
		return
	}

	var day dayResponse
	day.Exercises = []exerciseResponse{}
	err := withTx(r.Context(), s.pool, func(tx pgx.Tx) error {
		owns, err := ownsDay(r.Context(), tx, splitID, dayID, userID)
		if err != nil {
			return err
		}
		if !owns {
			return errNotFound
		}
		return tx.QueryRow(r.Context(),
			`UPDATE workout_split_days SET name = $2 WHERE id = $1 RETURNING id, name, day_order`,
			dayID, name).Scan(&day.ID, &day.Name, &day.Order)
	})
	if errors.Is(err, errNotFound) {
		writeError(w, http.StatusNotFound, "day not found")
		return
	}
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to update day")
		return
	}
	writeJSON(w, http.StatusOK, day)
}

func (s *Server) handleDeleteSplitDay(w http.ResponseWriter, r *http.Request) {
	userID, _ := userIDFromContext(r.Context())
	splitID := r.PathValue("id")
	dayID := r.PathValue("dayId")

	err := withTx(r.Context(), s.pool, func(tx pgx.Tx) error {
		owns, err := ownsDay(r.Context(), tx, splitID, dayID, userID)
		if err != nil {
			return err
		}
		if !owns {
			return errNotFound
		}
		if _, err := tx.Exec(r.Context(), `DELETE FROM workout_split_days WHERE id = $1`, dayID); err != nil {
			return err
		}
		return renumberDays(r.Context(), tx, splitID)
	})
	if errors.Is(err, errNotFound) {
		writeError(w, http.StatusNotFound, "day not found")
		return
	}
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to delete day")
		return
	}
	writeJSON(w, http.StatusNoContent, nil)
}

func (s *Server) handleReorderSplitDays(w http.ResponseWriter, r *http.Request) {
	userID, _ := userIDFromContext(r.Context())
	splitID := r.PathValue("id")

	var req struct {
		OrderedDayIDs []string `json:"orderedDayIds"`
	}
	if err := decodeJSON(r, &req); err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}

	err := withTx(r.Context(), s.pool, func(tx pgx.Tx) error {
		owns, err := ownsSplit(r.Context(), tx, splitID, userID)
		if err != nil {
			return err
		}
		if !owns {
			return errNotFound
		}
		for i, dayID := range req.OrderedDayIDs {
			if _, err := tx.Exec(r.Context(),
				`UPDATE workout_split_days SET day_order = $3 WHERE id = $1 AND split_id = $2`, dayID, splitID, -(i + 1)); err != nil {
				return err
			}
		}
		for i, dayID := range req.OrderedDayIDs {
			if _, err := tx.Exec(r.Context(),
				`UPDATE workout_split_days SET day_order = $3 WHERE id = $1 AND split_id = $2`, dayID, splitID, i+1); err != nil {
				return err
			}
		}
		return nil
	})
	if errors.Is(err, errNotFound) {
		writeError(w, http.StatusNotFound, "split not found")
		return
	}
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to reorder days")
		return
	}
	writeJSON(w, http.StatusNoContent, nil)
}

// ---------------------------------------------------------------------
// Exercise handlers
// ---------------------------------------------------------------------

func (s *Server) handleAddSplitExercise(w http.ResponseWriter, r *http.Request) {
	userID, _ := userIDFromContext(r.Context())
	splitID := r.PathValue("id")
	dayID := r.PathValue("dayId")

	var req struct {
		Name       string `json:"name"`
		TargetSets int    `json:"targetSets"`
		TargetReps string `json:"targetReps"`
		Notes      string `json:"notes"`
	}
	if err := decodeJSON(r, &req); err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	name := strings.TrimSpace(req.Name)
	if name == "" || req.TargetSets < 1 || strings.TrimSpace(req.TargetReps) == "" {
		writeError(w, http.StatusBadRequest, "name, targetSets (>=1) and targetReps are required")
		return
	}

	var e exerciseResponse
	var notes *string
	err := withTx(r.Context(), s.pool, func(tx pgx.Tx) error {
		owns, err := ownsDay(r.Context(), tx, splitID, dayID, userID)
		if err != nil {
			return err
		}
		if !owns {
			return errNotFound
		}

		var nextOrder int
		if err := tx.QueryRow(r.Context(),
			`SELECT COALESCE(MAX(exercise_order),0)+1 FROM workout_split_exercises WHERE split_day_id = $1`, dayID).
			Scan(&nextOrder); err != nil {
			return err
		}
		return tx.QueryRow(r.Context(),
			`INSERT INTO workout_split_exercises (split_day_id, exercise_order, exercise_name, target_sets, target_reps, notes)
			 VALUES ($1,$2,$3,$4,$5,$6)
			 RETURNING id, exercise_name, target_sets, target_reps, notes, exercise_order`,
			dayID, nextOrder, name, req.TargetSets, req.TargetReps, nullableString(req.Notes)).
			Scan(&e.ID, &e.Name, &e.TargetSets, &e.TargetReps, &notes, &e.Order)
	})
	if errors.Is(err, errNotFound) {
		writeError(w, http.StatusNotFound, "day not found")
		return
	}
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to add exercise")
		return
	}
	if notes != nil {
		e.Notes = *notes
	}
	writeJSON(w, http.StatusCreated, e)
}

func (s *Server) handleUpdateSplitExercise(w http.ResponseWriter, r *http.Request) {
	userID, _ := userIDFromContext(r.Context())
	splitID := r.PathValue("id")
	dayID := r.PathValue("dayId")
	exID := r.PathValue("exId")

	var req struct {
		Name       *string `json:"name"`
		TargetSets *int    `json:"targetSets"`
		TargetReps *string `json:"targetReps"`
		Notes      *string `json:"notes"`
	}
	if err := decodeJSON(r, &req); err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}

	var e exerciseResponse
	var notes *string
	err := withTx(r.Context(), s.pool, func(tx pgx.Tx) error {
		owns, err := ownsDay(r.Context(), tx, splitID, dayID, userID)
		if err != nil {
			return err
		}
		if !owns {
			return errNotFound
		}

		var curName, curReps string
		var curSets int
		var curNotes *string
		scanErr := tx.QueryRow(r.Context(),
			`SELECT exercise_name, target_sets, target_reps, notes FROM workout_split_exercises WHERE id = $1 AND split_day_id = $2`,
			exID, dayID).Scan(&curName, &curSets, &curReps, &curNotes)
		if errors.Is(scanErr, pgx.ErrNoRows) {
			return errNotFound
		}
		if scanErr != nil {
			return scanErr
		}

		if req.Name != nil {
			curName = strings.TrimSpace(*req.Name)
		}
		if req.TargetSets != nil {
			curSets = *req.TargetSets
		}
		if req.TargetReps != nil {
			curReps = *req.TargetReps
		}
		var notesParam any
		switch {
		case req.Notes != nil:
			notesParam = nullableString(*req.Notes)
		case curNotes != nil:
			notesParam = *curNotes
		}

		return tx.QueryRow(r.Context(),
			`UPDATE workout_split_exercises SET exercise_name=$3, target_sets=$4, target_reps=$5, notes=$6
			 WHERE id = $1 AND split_day_id = $2
			 RETURNING id, exercise_name, target_sets, target_reps, notes, exercise_order`,
			exID, dayID, curName, curSets, curReps, notesParam).
			Scan(&e.ID, &e.Name, &e.TargetSets, &e.TargetReps, &notes, &e.Order)
	})
	if errors.Is(err, errNotFound) {
		writeError(w, http.StatusNotFound, "exercise not found")
		return
	}
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to update exercise")
		return
	}
	if notes != nil {
		e.Notes = *notes
	}
	writeJSON(w, http.StatusOK, e)
}

func (s *Server) handleDeleteSplitExercise(w http.ResponseWriter, r *http.Request) {
	userID, _ := userIDFromContext(r.Context())
	splitID := r.PathValue("id")
	dayID := r.PathValue("dayId")
	exID := r.PathValue("exId")

	err := withTx(r.Context(), s.pool, func(tx pgx.Tx) error {
		owns, err := ownsDay(r.Context(), tx, splitID, dayID, userID)
		if err != nil {
			return err
		}
		if !owns {
			return errNotFound
		}
		tag, err := tx.Exec(r.Context(), `DELETE FROM workout_split_exercises WHERE id = $1 AND split_day_id = $2`, exID, dayID)
		if err != nil {
			return err
		}
		if tag.RowsAffected() == 0 {
			return errNotFound
		}
		return renumberExercises(r.Context(), tx, dayID)
	})
	if errors.Is(err, errNotFound) {
		writeError(w, http.StatusNotFound, "exercise not found")
		return
	}
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to delete exercise")
		return
	}
	writeJSON(w, http.StatusNoContent, nil)
}

func (s *Server) handleReorderSplitExercises(w http.ResponseWriter, r *http.Request) {
	userID, _ := userIDFromContext(r.Context())
	splitID := r.PathValue("id")
	dayID := r.PathValue("dayId")

	var req struct {
		OrderedExerciseIDs []string `json:"orderedExerciseIds"`
	}
	if err := decodeJSON(r, &req); err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}

	err := withTx(r.Context(), s.pool, func(tx pgx.Tx) error {
		owns, err := ownsDay(r.Context(), tx, splitID, dayID, userID)
		if err != nil {
			return err
		}
		if !owns {
			return errNotFound
		}
		for i, exID := range req.OrderedExerciseIDs {
			if _, err := tx.Exec(r.Context(),
				`UPDATE workout_split_exercises SET exercise_order = $3 WHERE id = $1 AND split_day_id = $2`,
				exID, dayID, -(i + 1)); err != nil {
				return err
			}
		}
		for i, exID := range req.OrderedExerciseIDs {
			if _, err := tx.Exec(r.Context(),
				`UPDATE workout_split_exercises SET exercise_order = $3 WHERE id = $1 AND split_day_id = $2`,
				exID, dayID, i+1); err != nil {
				return err
			}
		}
		return nil
	})
	if errors.Is(err, errNotFound) {
		writeError(w, http.StatusNotFound, "day not found")
		return
	}
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to reorder exercises")
		return
	}
	writeJSON(w, http.StatusNoContent, nil)
}

// ---------------------------------------------------------------------
// Sessions
// ---------------------------------------------------------------------

type sessionSetRequest struct {
	ExerciseName string   `json:"exerciseName"`
	SetNumber    int      `json:"setNumber"`
	Reps         int      `json:"reps"`
	WeightKg     *float64 `json:"weightKg"`
}

type logSessionRequest struct {
	SplitDayID  *string             `json:"splitDayId"`
	SplitID     *string             `json:"splitId"`
	Label       string              `json:"label"`
	SessionDate string              `json:"sessionDate"`
	Notes       string              `json:"notes"`
	Sets        []sessionSetRequest `json:"sets"`
}

type sessionSetResponse struct {
	ID           string   `json:"id"`
	ExerciseName string   `json:"exercise_name"`
	SetNumber    int      `json:"set_number"`
	Reps         int      `json:"reps"`
	WeightKg     *float64 `json:"weight_kg"`
}

type sessionResponse struct {
	ID          string               `json:"id"`
	SplitID     *string              `json:"split_id"`
	SplitDayID  *string              `json:"split_day_id"`
	DayLabel    string               `json:"day_label"`
	SessionDate string               `json:"session_date"`
	Notes       string               `json:"notes"`
	Sets        []sessionSetResponse `json:"sets"`
}

func (s *Server) handleLogWorkoutSession(w http.ResponseWriter, r *http.Request) {
	userID, _ := userIDFromContext(r.Context())

	var req logSessionRequest
	if err := decodeJSON(r, &req); err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	if _, err := time.Parse("2006-01-02", req.SessionDate); err != nil {
		writeError(w, http.StatusBadRequest, "sessionDate must be YYYY-MM-DD")
		return
	}
	dayLabel := strings.TrimSpace(req.Label)

	var resp sessionResponse
	err := withTx(r.Context(), s.pool, func(tx pgx.Tx) error {
		if req.SplitDayID != nil {
			resolvedSplitID, ownerID, err := daySplitAndOwner(r.Context(), tx, *req.SplitDayID)
			if errors.Is(err, pgx.ErrNoRows) {
				return errNotFound
			}
			if err != nil {
				return err
			}
			if ownerID != userID {
				return errNotFound
			}
			if req.SplitID == nil {
				req.SplitID = &resolvedSplitID
			}
			if dayLabel == "" {
				if err := tx.QueryRow(r.Context(),
					`SELECT name FROM workout_split_days WHERE id = $1`, *req.SplitDayID).Scan(&dayLabel); err != nil {
					return err
				}
			}
		} else if dayLabel == "" {
			return errLabelRequired
		}

		if req.SplitID != nil {
			owns, err := ownsSplit(r.Context(), tx, *req.SplitID, userID)
			if err != nil {
				return err
			}
			if !owns {
				return errNotFound
			}
		}

		var sessionID string
		if err := tx.QueryRow(r.Context(),
			`INSERT INTO workout_sessions (user_id, split_id, split_day_id, day_label, session_date, notes)
			 VALUES ($1,$2,$3,$4,$5,$6) RETURNING id`,
			userID, req.SplitID, req.SplitDayID, dayLabel, req.SessionDate, nullableString(req.Notes)).
			Scan(&sessionID); err != nil {
			return fmt.Errorf("create session: %w", err)
		}

		for _, set := range req.Sets {
			exName := strings.TrimSpace(set.ExerciseName)
			if exName == "" || set.SetNumber < 1 || set.Reps < 0 {
				return fmt.Errorf("invalid set: exerciseName, setNumber (>=1) and reps (>=0) are required")
			}
			if _, err := tx.Exec(r.Context(),
				`INSERT INTO workout_session_sets (session_id, user_id, exercise_name, set_number, reps, weight_kg)
				 VALUES ($1,$2,$3,$4,$5,$6)`,
				sessionID, userID, exName, set.SetNumber, set.Reps, set.WeightKg); err != nil {
				return fmt.Errorf("create set: %w", err)
			}
		}

		loaded, err := loadSessionDetail(r.Context(), tx, sessionID)
		if err != nil {
			return err
		}
		resp = loaded
		return nil
	})
	if errors.Is(err, errNotFound) {
		writeError(w, http.StatusNotFound, "split or day not found")
		return
	}
	if errors.Is(err, errLabelRequired) {
		writeError(w, http.StatusBadRequest, errLabelRequired.Error())
		return
	}
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to log session")
		return
	}
	writeJSON(w, http.StatusCreated, resp)
}

func loadSessionDetail(ctx context.Context, db dbExecutor, sessionID string) (sessionResponse, error) {
	var sr sessionResponse
	var sessionDate time.Time
	var notes *string
	if err := db.QueryRow(ctx,
		`SELECT id, split_id, split_day_id, day_label, session_date, notes FROM workout_sessions WHERE id = $1`, sessionID).
		Scan(&sr.ID, &sr.SplitID, &sr.SplitDayID, &sr.DayLabel, &sessionDate, &notes); err != nil {
		return sessionResponse{}, err
	}
	sr.SessionDate = sessionDate.Format("2006-01-02")
	if notes != nil {
		sr.Notes = *notes
	}

	rows, err := db.Query(ctx,
		`SELECT id, exercise_name, set_number, reps, weight_kg FROM workout_session_sets
		 WHERE session_id = $1 ORDER BY exercise_name, set_number`, sessionID)
	if err != nil {
		return sessionResponse{}, err
	}
	defer rows.Close()

	sr.Sets = []sessionSetResponse{}
	for rows.Next() {
		var set sessionSetResponse
		if err := rows.Scan(&set.ID, &set.ExerciseName, &set.SetNumber, &set.Reps, &set.WeightKg); err != nil {
			return sessionResponse{}, err
		}
		sr.Sets = append(sr.Sets, set)
	}
	if err := rows.Err(); err != nil {
		return sessionResponse{}, err
	}

	return sr, nil
}

func (s *Server) handleListWorkoutSessions(w http.ResponseWriter, r *http.Request) {
	userID, _ := userIDFromContext(r.Context())
	q := r.URL.Query()
	limit := parseIntOrDefault(q.Get("limit"), 25, 1, 200)
	offset := parseIntOrDefault(q.Get("offset"), 0, 0, 1_000_000)

	query := `SELECT DISTINCT s.id FROM workout_sessions s`
	args := []any{userID}
	where := `WHERE s.user_id = $1`
	if exercise := q.Get("exercise"); exercise != "" {
		query += ` JOIN workout_session_sets ss ON ss.session_id = s.id`
		args = append(args, exercise)
		where += fmt.Sprintf(` AND lower(ss.exercise_name) = lower($%d)`, len(args))
	}
	if splitID := q.Get("splitID"); splitID != "" {
		args = append(args, splitID)
		where += fmt.Sprintf(` AND s.split_id = $%d`, len(args))
	}
	if dayID := q.Get("dayID"); dayID != "" {
		args = append(args, dayID)
		where += fmt.Sprintf(` AND s.split_day_id = $%d`, len(args))
	}
	query += " " + where + " ORDER BY s.id"

	rows, err := s.pool.Query(r.Context(), query, args...)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to load sessions")
		return
	}
	var ids []string
	for rows.Next() {
		var id string
		if err := rows.Scan(&id); err != nil {
			rows.Close()
			writeError(w, http.StatusInternalServerError, "failed to load sessions")
			return
		}
		ids = append(ids, id)
	}
	rows.Close()

	// Re-fetch ordered by session_date desc (DISTINCT above doesn't
	// guarantee order); simplest correct approach at this scale is to sort
	// the small candidate id set with one more query.
	sessions := []sessionResponse{}
	if len(ids) > 0 {
		dateRows, err := s.pool.Query(r.Context(),
			`SELECT id FROM workout_sessions WHERE id = ANY($1) ORDER BY session_date DESC, created_at DESC`, ids)
		if err != nil {
			writeError(w, http.StatusInternalServerError, "failed to load sessions")
			return
		}
		var ordered []string
		for dateRows.Next() {
			var id string
			if err := dateRows.Scan(&id); err != nil {
				dateRows.Close()
				writeError(w, http.StatusInternalServerError, "failed to load sessions")
				return
			}
			ordered = append(ordered, id)
		}
		dateRows.Close()

		for _, id := range paginate(ordered, limit, offset) {
			detail, err := loadSessionDetail(r.Context(), s.pool, id)
			if err != nil {
				writeError(w, http.StatusInternalServerError, "failed to load sessions")
				return
			}
			sessions = append(sessions, detail)
		}
	}

	writeJSON(w, http.StatusOK, map[string]any{"entries": sessions, "total": len(ids)})
}

func (s *Server) handleGetWorkoutSession(w http.ResponseWriter, r *http.Request) {
	userID, _ := userIDFromContext(r.Context())
	sessionID := r.PathValue("id")

	var owner string
	if err := s.pool.QueryRow(r.Context(), `SELECT user_id FROM workout_sessions WHERE id = $1`, sessionID).Scan(&owner); err != nil {
		writeError(w, http.StatusNotFound, "session not found")
		return
	}
	if owner != userID {
		writeError(w, http.StatusNotFound, "session not found")
		return
	}

	detail, err := loadSessionDetail(r.Context(), s.pool, sessionID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to load session")
		return
	}
	writeJSON(w, http.StatusOK, detail)
}

func (s *Server) handleUpdateWorkoutSession(w http.ResponseWriter, r *http.Request) {
	userID, _ := userIDFromContext(r.Context())
	sessionID := r.PathValue("id")

	var req logSessionRequest
	if err := decodeJSON(r, &req); err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	if _, err := time.Parse("2006-01-02", req.SessionDate); err != nil {
		writeError(w, http.StatusBadRequest, "sessionDate must be YYYY-MM-DD")
		return
	}

	var resp sessionResponse
	err := withTx(r.Context(), s.pool, func(tx pgx.Tx) error {
		var owner string
		if err := tx.QueryRow(r.Context(), `SELECT user_id FROM workout_sessions WHERE id = $1`, sessionID).Scan(&owner); err != nil {
			if errors.Is(err, pgx.ErrNoRows) {
				return errNotFound
			}
			return err
		}
		if owner != userID {
			return errNotFound
		}

		dayLabel := strings.TrimSpace(req.Label)
		if dayLabel == "" {
			return errLabelRequired
		}

		if _, err := tx.Exec(r.Context(),
			`UPDATE workout_sessions SET split_id=$2, split_day_id=$3, day_label=$4, session_date=$5, notes=$6
			 WHERE id = $1`,
			sessionID, req.SplitID, req.SplitDayID, dayLabel, req.SessionDate, nullableString(req.Notes)); err != nil {
			return err
		}

		if _, err := tx.Exec(r.Context(), `DELETE FROM workout_session_sets WHERE session_id = $1`, sessionID); err != nil {
			return err
		}
		for _, set := range req.Sets {
			exName := strings.TrimSpace(set.ExerciseName)
			if exName == "" || set.SetNumber < 1 || set.Reps < 0 {
				return fmt.Errorf("invalid set: exerciseName, setNumber (>=1) and reps (>=0) are required")
			}
			if _, err := tx.Exec(r.Context(),
				`INSERT INTO workout_session_sets (session_id, user_id, exercise_name, set_number, reps, weight_kg)
				 VALUES ($1,$2,$3,$4,$5,$6)`,
				sessionID, userID, exName, set.SetNumber, set.Reps, set.WeightKg); err != nil {
				return err
			}
		}

		loaded, err := loadSessionDetail(r.Context(), tx, sessionID)
		if err != nil {
			return err
		}
		resp = loaded
		return nil
	})
	if errors.Is(err, errNotFound) {
		writeError(w, http.StatusNotFound, "session not found")
		return
	}
	if errors.Is(err, errLabelRequired) {
		writeError(w, http.StatusBadRequest, errLabelRequired.Error())
		return
	}
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to update session")
		return
	}
	writeJSON(w, http.StatusOK, resp)
}

func (s *Server) handleDeleteWorkoutSession(w http.ResponseWriter, r *http.Request) {
	userID, _ := userIDFromContext(r.Context())
	sessionID := r.PathValue("id")

	tag, err := s.pool.Exec(r.Context(), `DELETE FROM workout_sessions WHERE id = $1 AND user_id = $2`, sessionID, userID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to delete session")
		return
	}
	if tag.RowsAffected() == 0 {
		writeError(w, http.StatusNotFound, "session not found")
		return
	}
	writeJSON(w, http.StatusNoContent, nil)
}

func (s *Server) handleListExerciseNames(w http.ResponseWriter, r *http.Request) {
	userID, _ := userIDFromContext(r.Context())

	rows, err := s.pool.Query(r.Context(),
		`SELECT DISTINCT ON (lower(exercise_name)) exercise_name FROM workout_session_sets
		 WHERE user_id = $1 ORDER BY lower(exercise_name)`, userID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to load exercise names")
		return
	}
	defer rows.Close()

	names := []string{}
	for rows.Next() {
		var name string
		if err := rows.Scan(&name); err != nil {
			writeError(w, http.StatusInternalServerError, "failed to load exercise names")
			return
		}
		names = append(names, name)
	}
	writeJSON(w, http.StatusOK, names)
}

func (s *Server) handleGetExerciseHistory(w http.ResponseWriter, r *http.Request) {
	userID, _ := userIDFromContext(r.Context())
	name := r.PathValue("name")

	rows, err := s.pool.Query(r.Context(), `
		SELECT ss.reps, ss.weight_kg, ws.session_date
		FROM workout_session_sets ss
		JOIN workout_sessions ws ON ws.id = ss.session_id
		WHERE ss.user_id = $1 AND lower(ss.exercise_name) = lower($2)
		ORDER BY ws.session_date DESC, ss.created_at DESC`, userID, name)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to load exercise history")
		return
	}
	defer rows.Close()

	type historySet struct {
		Reps     int      `json:"reps"`
		WeightKg *float64 `json:"weight_kg"`
		Date     string   `json:"date"`
	}
	sets := []historySet{}
	var best *historySet
	for rows.Next() {
		var hs historySet
		var date time.Time
		if err := rows.Scan(&hs.Reps, &hs.WeightKg, &date); err != nil {
			writeError(w, http.StatusInternalServerError, "failed to load exercise history")
			return
		}
		hs.Date = date.Format("2006-01-02")
		sets = append(sets, hs)
		if hs.WeightKg != nil && (best == nil || best.WeightKg == nil || *hs.WeightKg > *best.WeightKg) {
			b := hs
			best = &b
		}
	}
	if err := rows.Err(); err != nil {
		writeError(w, http.StatusInternalServerError, "failed to load exercise history")
		return
	}

	writeJSON(w, http.StatusOK, map[string]any{"sets": sets, "best": best})
}
