package api

import (
	"context"
	"net/http"
)

// calisthenicsSkill is static, shared content -- mirrors the workoutTemplates
// pattern in workouts.go (hardcoded catalog, not stored in the database).
// Only per-user unlock state lives in the DB (unlocked_calisthenics_skills).
type calisthenicsSkill struct {
	ID          string `json:"id"`
	Branch      string `json:"branch"`
	Order       int    `json:"order"`
	Tier        int    `json:"tier"`
	Name        string `json:"name"`
	Description string `json:"description"`
}

var calisthenicsSkills = []calisthenicsSkill{
	{ID: "push-1", Branch: "Push", Order: 0, Tier: 1, Name: "Wall Push-up", Description: "Perform 15 clean push-ups against a wall."},
	{ID: "push-2", Branch: "Push", Order: 1, Tier: 1, Name: "Incline Push-up", Description: "Perform 12 push-ups with hands elevated on a bench or step."},
	{ID: "push-3", Branch: "Push", Order: 2, Tier: 2, Name: "Standard Push-up", Description: "Perform 15 full push-ups on the floor with good form."},
	{ID: "push-4", Branch: "Push", Order: 3, Tier: 2, Name: "Diamond Push-up", Description: "Perform 10 push-ups with hands together under your chest."},
	{ID: "push-5", Branch: "Push", Order: 4, Tier: 3, Name: "Archer Push-up", Description: "Perform 6 archer push-ups per side."},
	{ID: "push-6", Branch: "Push", Order: 5, Tier: 4, Name: "One-Arm Push-up", Description: "Perform 1 strict one-arm push-up per side."},

	{ID: "pull-1", Branch: "Pull", Order: 0, Tier: 1, Name: "Dead Hang", Description: "Hold a dead hang from a bar for 30 seconds."},
	{ID: "pull-2", Branch: "Pull", Order: 1, Tier: 1, Name: "Negative Pull-up", Description: "Perform 5 slow (5+ second) negative pull-ups."},
	{ID: "pull-3", Branch: "Pull", Order: 2, Tier: 2, Name: "Assisted Pull-up", Description: "Perform 8 assisted (band or machine) pull-ups."},
	{ID: "pull-4", Branch: "Pull", Order: 3, Tier: 2, Name: "Pull-up", Description: "Perform 8 strict bodyweight pull-ups."},
	{ID: "pull-5", Branch: "Pull", Order: 4, Tier: 3, Name: "Weighted Pull-up", Description: "Perform 5 pull-ups with added weight."},
	{ID: "pull-6", Branch: "Pull", Order: 5, Tier: 4, Name: "Muscle-Up", Description: "Perform 1 strict bar muscle-up."},

	{ID: "core-1", Branch: "Core", Order: 0, Tier: 1, Name: "Plank", Description: "Hold a full plank for 60 seconds."},
	{ID: "core-2", Branch: "Core", Order: 1, Tier: 1, Name: "Hollow Body Hold", Description: "Hold a hollow body position for 30 seconds."},
	{ID: "core-3", Branch: "Core", Order: 2, Tier: 2, Name: "Tuck L-sit", Description: "Hold a tuck L-sit for 15 seconds."},
	{ID: "core-4", Branch: "Core", Order: 3, Tier: 2, Name: "L-sit", Description: "Hold a full L-sit for 10 seconds."},
	{ID: "core-5", Branch: "Core", Order: 4, Tier: 3, Name: "Front Lever Tuck", Description: "Hold a tucked front lever for 10 seconds."},
	{ID: "core-6", Branch: "Core", Order: 5, Tier: 4, Name: "Front Lever", Description: "Hold a full front lever for 5 seconds."},

	{ID: "legs-1", Branch: "Legs", Order: 0, Tier: 1, Name: "Bodyweight Squat", Description: "Perform 25 bodyweight squats with full depth."},
	{ID: "legs-2", Branch: "Legs", Order: 1, Tier: 1, Name: "Split Squat", Description: "Perform 12 split squats per leg."},
	{ID: "legs-3", Branch: "Legs", Order: 2, Tier: 2, Name: "Bulgarian Split Squat", Description: "Perform 10 Bulgarian split squats per leg."},
	{ID: "legs-4", Branch: "Legs", Order: 3, Tier: 2, Name: "Assisted Pistol Squat", Description: "Perform 8 assisted pistol squats per leg."},
	{ID: "legs-5", Branch: "Legs", Order: 4, Tier: 3, Name: "Pistol Squat", Description: "Perform 5 strict pistol squats per leg."},
	{ID: "legs-6", Branch: "Legs", Order: 5, Tier: 4, Name: "Shrimp Squat", Description: "Perform 3 shrimp squats per leg."},
}

type calisthenicsSkillView struct {
	calisthenicsSkill
	Unlocked  bool `json:"unlocked"`
	Available bool `json:"available"`
}

// loadUnlockedCalisthenicsSkillIDs returns the set of skill IDs the given
// user has already unlocked.
func loadUnlockedCalisthenicsSkillIDs(ctx context.Context, db dbExecutor, userID string) (map[string]bool, error) {
	unlocked := make(map[string]bool)
	rows, err := db.Query(ctx, `SELECT skill_id FROM unlocked_calisthenics_skills WHERE user_id = $1`, userID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	for rows.Next() {
		var id string
		if err := rows.Scan(&id); err != nil {
			return nil, err
		}
		unlocked[id] = true
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	return unlocked, nil
}

func (s *Server) handleListCalisthenicsSkills(w http.ResponseWriter, r *http.Request) {
	userID, _ := userIDFromContext(r.Context())

	unlocked, err := loadUnlockedCalisthenicsSkillIDs(r.Context(), s.pool, userID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to load skills")
		return
	}

	writeJSON(w, http.StatusOK, map[string]any{"branches": groupCalisthenicsSkills(unlocked)})
}

// groupCalisthenicsSkills annotates the static catalog with the given
// unlocked set and groups it by branch, preserving catalog order within
// each branch.
func groupCalisthenicsSkills(unlocked map[string]bool) map[string][]calisthenicsSkillView {
	branches := map[string][]calisthenicsSkillView{}
	for _, skill := range calisthenicsSkills {
		branchSkills := branches[skill.Branch]
		isUnlocked := unlocked[skill.ID]
		available := isUnlocked || skill.Order == 0
		if !available && len(branchSkills) > 0 {
			available = branchSkills[len(branchSkills)-1].Unlocked
		}
		branches[skill.Branch] = append(branchSkills, calisthenicsSkillView{
			calisthenicsSkill: skill,
			Unlocked:          isUnlocked,
			Available:         available,
		})
	}
	return branches
}

func (s *Server) handleUnlockCalisthenicsSkill(w http.ResponseWriter, r *http.Request) {
	userID, _ := userIDFromContext(r.Context())
	skillID := r.PathValue("skillId")

	var target *calisthenicsSkill
	for i := range calisthenicsSkills {
		if calisthenicsSkills[i].ID == skillID {
			target = &calisthenicsSkills[i]
			break
		}
	}
	if target == nil {
		writeError(w, http.StatusNotFound, "skill not found")
		return
	}

	unlocked, err := loadUnlockedCalisthenicsSkillIDs(r.Context(), s.pool, userID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to unlock skill")
		return
	}

	grouped := groupCalisthenicsSkills(unlocked)
	var available bool
	for _, view := range grouped[target.Branch] {
		if view.ID == skillID {
			available = view.Available
			break
		}
	}
	if !available {
		writeError(w, http.StatusBadRequest, "unlock the previous skill in this branch first")
		return
	}

	if _, err := s.pool.Exec(r.Context(),
		`INSERT INTO unlocked_calisthenics_skills (user_id, skill_id) VALUES ($1, $2)
		 ON CONFLICT (user_id, skill_id) DO NOTHING`, userID, skillID); err != nil {
		writeError(w, http.StatusInternalServerError, "failed to unlock skill")
		return
	}
	unlocked[skillID] = true

	writeJSON(w, http.StatusOK, map[string]any{"branches": groupCalisthenicsSkills(unlocked)})
}
