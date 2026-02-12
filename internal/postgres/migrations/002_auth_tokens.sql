-- +goose Up
CREATE TABLE email_verifications (
    id          UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    user_id     UUID NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    token       TEXT UNIQUE NOT NULL,
    expires_at  TIMESTAMPTZ NOT NULL,
    consumed_at TIMESTAMPTZ,
    created_at  TIMESTAMPTZ NOT NULL DEFAULT NOW()
);
CREATE INDEX idx_email_verifications_token ON email_verifications (token) WHERE consumed_at IS NULL;
CREATE INDEX idx_email_verifications_user ON email_verifications (user_id);

CREATE TABLE login_attempts (
    id         UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    email      TEXT NOT NULL,
    ip_address INET NOT NULL,
    success    BOOLEAN NOT NULL,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);
CREATE INDEX idx_login_attempts_email_time ON login_attempts (email, created_at DESC);
CREATE INDEX idx_login_attempts_ip_time ON login_attempts (ip_address, created_at DESC);

-- +goose Down
DROP TABLE IF EXISTS login_attempts;
DROP TABLE IF EXISTS email_verifications;
