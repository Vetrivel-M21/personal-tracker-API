DROP INDEX IF EXISTS users_email_lower_idx;
DROP INDEX IF EXISTS users_google_id_idx;

ALTER TABLE users DROP COLUMN has_password;
ALTER TABLE users DROP COLUMN email;
ALTER TABLE users DROP COLUMN google_id;
