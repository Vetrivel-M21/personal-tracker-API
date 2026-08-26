-- Core schema: users, sessions, habits, daily_progress, streak_shields

CREATE TABLE users (
    id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    username text NOT NULL,
    password_hash text NOT NULL,
    display_name text NOT NULL,
    xp integer NOT NULL DEFAULT 0,
    created_at timestamptz NOT NULL DEFAULT now(),
    updated_at timestamptz NOT NULL DEFAULT now()
);

CREATE UNIQUE INDEX users_username_lower_idx ON users (lower(username));
CREATE INDEX users_xp_desc_idx ON users (xp DESC);

CREATE TABLE sessions (
    id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    user_id uuid NOT NULL REFERENCES users (id) ON DELETE CASCADE,
    token_hash bytea NOT NULL,
    expires_at timestamptz NOT NULL,
    revoked_at timestamptz,
    created_at timestamptz NOT NULL DEFAULT now()
);

CREATE UNIQUE INDEX sessions_token_hash_idx ON sessions (token_hash);
CREATE INDEX sessions_user_id_idx ON sessions (user_id);

CREATE TABLE habits (
    id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    user_id uuid NOT NULL REFERENCES users (id) ON DELETE CASCADE,
    name varchar(50) NOT NULL,
    color varchar(20) NOT NULL DEFAULT '#6366f1',
    icon varchar(50) NOT NULL DEFAULT 'fa-solid fa-star',
    created_at timestamptz NOT NULL DEFAULT now(),
    updated_at timestamptz NOT NULL DEFAULT now(),
    UNIQUE (user_id, name)
);

CREATE TYPE mood_type AS ENUM ('Great', 'Good', 'Average', 'Bad');

CREATE TABLE daily_progress (
    id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    user_id uuid NOT NULL REFERENCES users (id) ON DELETE CASCADE,
    date date NOT NULL,
    completed_habits jsonb NOT NULL DEFAULT '[]',
    learning_hours numeric(4, 2) NOT NULL DEFAULT 0.00,
    mood mood_type NOT NULL DEFAULT 'Average',
    notes text,
    created_at timestamptz NOT NULL DEFAULT now(),
    updated_at timestamptz NOT NULL DEFAULT now(),
    UNIQUE (user_id, date)
);

CREATE INDEX daily_progress_user_date_idx ON daily_progress (user_id, date);

CREATE TABLE streak_shields (
    user_id uuid PRIMARY KEY REFERENCES users (id) ON DELETE CASCADE,
    shield_count integer NOT NULL DEFAULT 0,
    updated_at timestamptz NOT NULL DEFAULT now()
);
