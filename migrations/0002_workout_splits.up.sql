-- Workout splits: premade/custom split templates, days, exercises, logged sessions/sets

CREATE TABLE workout_splits (
    id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    user_id uuid NOT NULL REFERENCES users (id) ON DELETE CASCADE,
    name text NOT NULL,
    source text NOT NULL DEFAULT 'custom' CHECK (source IN ('custom', 'premade')),
    source_template_id text,
    is_active boolean NOT NULL DEFAULT false,
    created_at timestamptz NOT NULL DEFAULT now(),
    updated_at timestamptz NOT NULL DEFAULT now()
);

-- at most one active split per user
CREATE UNIQUE INDEX workout_splits_user_active_idx ON workout_splits (user_id) WHERE is_active;

CREATE TABLE workout_split_days (
    id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    split_id uuid NOT NULL REFERENCES workout_splits (id) ON DELETE CASCADE,
    day_order integer NOT NULL,
    name text NOT NULL,
    created_at timestamptz NOT NULL DEFAULT now(),
    UNIQUE (split_id, day_order)
);

CREATE TABLE workout_split_exercises (
    id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    split_day_id uuid NOT NULL REFERENCES workout_split_days (id) ON DELETE CASCADE,
    exercise_order integer NOT NULL,
    exercise_name text NOT NULL,
    target_sets integer NOT NULL CHECK (target_sets >= 1),
    target_reps text NOT NULL,
    notes text,
    created_at timestamptz NOT NULL DEFAULT now(),
    UNIQUE (split_day_id, exercise_order)
);

CREATE TABLE workout_sessions (
    id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    user_id uuid NOT NULL REFERENCES users (id) ON DELETE CASCADE,
    split_id uuid REFERENCES workout_splits (id) ON DELETE SET NULL,
    split_day_id uuid REFERENCES workout_split_days (id) ON DELETE SET NULL,
    day_label text NOT NULL,
    session_date date NOT NULL DEFAULT current_date,
    notes text,
    created_at timestamptz NOT NULL DEFAULT now()
);

CREATE INDEX workout_sessions_user_date_idx ON workout_sessions (user_id, session_date DESC);
CREATE INDEX workout_sessions_split_id_idx ON workout_sessions (split_id) WHERE split_id IS NOT NULL;
CREATE INDEX workout_sessions_split_day_id_idx ON workout_sessions (split_day_id) WHERE split_day_id IS NOT NULL;

CREATE TABLE workout_session_sets (
    id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    session_id uuid NOT NULL REFERENCES workout_sessions (id) ON DELETE CASCADE,
    user_id uuid NOT NULL REFERENCES users (id) ON DELETE CASCADE,
    exercise_name text NOT NULL,
    set_number integer NOT NULL CHECK (set_number >= 1),
    reps integer NOT NULL CHECK (reps >= 0),
    weight_kg numeric(6, 2),
    created_at timestamptz NOT NULL DEFAULT now()
);

CREATE INDEX workout_session_sets_session_id_idx ON workout_session_sets (session_id);
CREATE INDEX workout_session_sets_user_exercise_idx ON workout_session_sets (user_id, lower(exercise_name));
