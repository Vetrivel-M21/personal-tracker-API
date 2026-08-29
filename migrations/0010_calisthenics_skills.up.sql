CREATE TABLE unlocked_calisthenics_skills (
    user_id uuid NOT NULL REFERENCES users (id) ON DELETE CASCADE,
    skill_id text NOT NULL,
    unlocked_at timestamptz NOT NULL DEFAULT now(),
    PRIMARY KEY (user_id, skill_id)
);
