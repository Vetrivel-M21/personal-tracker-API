CREATE TABLE focus_sessions (
    id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    user_id uuid NOT NULL REFERENCES users (id) ON DELETE CASCADE,
    session_type text NOT NULL DEFAULT 'Focus' CHECK (session_type IN ('Focus', 'Meditation')),
    duration_minutes integer NOT NULL CHECK (duration_minutes >= 1 AND duration_minutes <= 180),
    completed_at timestamptz NOT NULL DEFAULT now()
);

CREATE INDEX focus_sessions_user_completed_idx ON focus_sessions (user_id, completed_at DESC);
