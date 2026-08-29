CREATE TABLE kudos (
    id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    from_user_id uuid NOT NULL REFERENCES users (id) ON DELETE CASCADE,
    to_user_id uuid NOT NULL REFERENCES users (id) ON DELETE CASCADE,
    sent_date date NOT NULL DEFAULT CURRENT_DATE,
    created_at timestamptz NOT NULL DEFAULT now(),
    CHECK (from_user_id <> to_user_id)
);

CREATE UNIQUE INDEX kudos_daily_unique_idx ON kudos (from_user_id, to_user_id, sent_date);
CREATE INDEX kudos_to_user_idx ON kudos (to_user_id);
