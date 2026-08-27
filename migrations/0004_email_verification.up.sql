ALTER TABLE users ADD COLUMN email_verified boolean NOT NULL DEFAULT true;
ALTER TABLE users ADD COLUMN email_verification_code_hash text;
ALTER TABLE users ADD COLUMN email_verification_expires_at timestamptz;
ALTER TABLE users ADD COLUMN email_verification_attempts integer NOT NULL DEFAULT 0;
ALTER TABLE users ADD COLUMN email_verification_last_sent_at timestamptz;
