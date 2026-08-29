ALTER TABLE users ADD COLUMN password_reset_code_hash text;
ALTER TABLE users ADD COLUMN password_reset_expires_at timestamptz;
ALTER TABLE users ADD COLUMN password_reset_attempts integer NOT NULL DEFAULT 0;
ALTER TABLE users ADD COLUMN password_reset_last_sent_at timestamptz;
