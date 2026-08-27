ALTER TABLE users DROP COLUMN email_verification_last_sent_at;
ALTER TABLE users DROP COLUMN email_verification_attempts;
ALTER TABLE users DROP COLUMN email_verification_expires_at;
ALTER TABLE users DROP COLUMN email_verification_code_hash;
ALTER TABLE users DROP COLUMN email_verified;
