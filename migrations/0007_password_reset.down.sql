ALTER TABLE users DROP COLUMN password_reset_code_hash;
ALTER TABLE users DROP COLUMN password_reset_expires_at;
ALTER TABLE users DROP COLUMN password_reset_attempts;
ALTER TABLE users DROP COLUMN password_reset_last_sent_at;
