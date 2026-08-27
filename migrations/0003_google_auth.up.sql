ALTER TABLE users ADD COLUMN google_id text;
ALTER TABLE users ADD COLUMN email text;
ALTER TABLE users ADD COLUMN has_password boolean NOT NULL DEFAULT true;

CREATE UNIQUE INDEX users_google_id_idx ON users (google_id) WHERE google_id IS NOT NULL;
CREATE UNIQUE INDEX users_email_lower_idx ON users (lower(email)) WHERE email IS NOT NULL;
