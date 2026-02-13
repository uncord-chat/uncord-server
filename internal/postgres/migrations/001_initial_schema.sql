-- +goose Up

-- Timestamp update trigger function
-- +goose StatementBegin
CREATE OR REPLACE FUNCTION trigger_set_updated_at()
RETURNS TRIGGER AS $$
BEGIN
    NEW.updated_at = NOW();
    RETURN NEW;
END;
$$ LANGUAGE plpgsql;
-- +goose StatementEnd

-- Users

CREATE TABLE users (
    id              UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    email           TEXT UNIQUE NOT NULL,
    username        TEXT UNIQUE NOT NULL,
    display_name    TEXT,
    password_hash   TEXT NOT NULL,
    avatar_key      TEXT,
    mfa_secret      TEXT,
    mfa_enabled     BOOLEAN NOT NULL DEFAULT false,
    email_verified  BOOLEAN NOT NULL DEFAULT false,
    phone           TEXT,
    phone_verified  BOOLEAN NOT NULL DEFAULT false,
    locale          TEXT NOT NULL DEFAULT 'en',
    created_at      TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at      TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE TRIGGER set_updated_at BEFORE UPDATE ON users
    FOR EACH ROW EXECUTE FUNCTION trigger_set_updated_at();

-- Server (single row)

CREATE TABLE server_config (
    id              UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    name            TEXT NOT NULL,
    description     TEXT NOT NULL DEFAULT '',
    icon_key        TEXT,
    banner_key      TEXT,
    owner_id        UUID NOT NULL REFERENCES users(id),
    created_at      TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at      TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE TRIGGER set_updated_at BEFORE UPDATE ON server_config
    FOR EACH ROW EXECUTE FUNCTION trigger_set_updated_at();

-- Members

CREATE TABLE members (
    user_id         UUID NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    nickname        TEXT,
    status          TEXT NOT NULL DEFAULT 'pending',  -- pending, active, timed_out
    timeout_until   TIMESTAMPTZ,
    joined_at       TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    onboarded_at    TIMESTAMPTZ,
    updated_at      TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    PRIMARY KEY (user_id),
    CONSTRAINT chk_members_status CHECK (status IN ('pending', 'active', 'timed_out'))
);

CREATE TRIGGER set_updated_at BEFORE UPDATE ON members
    FOR EACH ROW EXECUTE FUNCTION trigger_set_updated_at();

-- Categories

CREATE TABLE categories (
    id              UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    name            TEXT NOT NULL,
    position        INTEGER NOT NULL,
    created_at      TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at      TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE TRIGGER set_updated_at BEFORE UPDATE ON categories
    FOR EACH ROW EXECUTE FUNCTION trigger_set_updated_at();

CREATE UNIQUE INDEX idx_categories_position ON categories (position);

-- Channels

CREATE TABLE channels (
    id              UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    category_id     UUID REFERENCES categories(id) ON DELETE SET NULL,
    name            TEXT NOT NULL,
    type            TEXT NOT NULL DEFAULT 'text',  -- text, voice, announcement, forum, stage
    topic           TEXT NOT NULL DEFAULT '',
    position        INTEGER NOT NULL,
    slowmode_seconds INTEGER NOT NULL DEFAULT 0,
    nsfw            BOOLEAN NOT NULL DEFAULT false,
    created_at      TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at      TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    CONSTRAINT chk_channels_type CHECK (type IN ('text', 'voice', 'announcement', 'forum', 'stage'))
);

CREATE TRIGGER set_updated_at BEFORE UPDATE ON channels
    FOR EACH ROW EXECUTE FUNCTION trigger_set_updated_at();

CREATE INDEX idx_channels_category ON channels (category_id);

-- Messages

CREATE TABLE messages (
    id              UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    channel_id      UUID NOT NULL REFERENCES channels(id) ON DELETE CASCADE,
    author_id       UUID NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    content         TEXT NOT NULL DEFAULT '',
    edited_at       TIMESTAMPTZ,
    reply_to_id     UUID REFERENCES messages(id) ON DELETE SET NULL,
    thread_id       UUID,  -- FK added after threads table creation
    pinned          BOOLEAN NOT NULL DEFAULT false,
    deleted         BOOLEAN NOT NULL DEFAULT false,
    created_at      TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at      TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE TRIGGER set_updated_at BEFORE UPDATE ON messages
    FOR EACH ROW EXECUTE FUNCTION trigger_set_updated_at();

CREATE INDEX idx_messages_channel_time ON messages (channel_id, created_at DESC);
CREATE INDEX idx_messages_author ON messages (author_id, created_at DESC);

-- Message Attachments

CREATE TABLE message_attachments (
    id              UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    message_id      UUID NOT NULL REFERENCES messages(id) ON DELETE CASCADE,
    filename        TEXT NOT NULL,
    content_type    TEXT NOT NULL,
    size_bytes      BIGINT NOT NULL,
    storage_key     TEXT NOT NULL,
    width           INTEGER,
    height          INTEGER,
    thumbnail_key   TEXT,
    created_at      TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE INDEX idx_attachments_message ON message_attachments (message_id);

-- Threads

CREATE TABLE threads (
    id                UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    channel_id        UUID NOT NULL REFERENCES channels(id) ON DELETE CASCADE,
    parent_message_id UUID NOT NULL REFERENCES messages(id) ON DELETE CASCADE,
    name              TEXT NOT NULL,
    archived          BOOLEAN NOT NULL DEFAULT false,
    locked            BOOLEAN NOT NULL DEFAULT false,
    created_at        TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at        TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE TRIGGER set_updated_at BEFORE UPDATE ON threads
    FOR EACH ROW EXECUTE FUNCTION trigger_set_updated_at();

ALTER TABLE messages ADD CONSTRAINT fk_messages_thread
    FOREIGN KEY (thread_id) REFERENCES threads(id) ON DELETE SET NULL;

-- Reactions

CREATE TABLE reactions (
    id              UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    message_id      UUID NOT NULL REFERENCES messages(id) ON DELETE CASCADE,
    user_id         UUID NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    emoji_id        UUID,   -- FK to custom_emoji, null = unicode
    emoji_unicode   TEXT,   -- null = custom emoji
    created_at      TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE UNIQUE INDEX idx_reactions_unique
    ON reactions (message_id, user_id, COALESCE(emoji_id, '00000000-0000-0000-0000-000000000000'), COALESCE(emoji_unicode, ''));

-- Roles

CREATE TABLE roles (
    id              UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    name            TEXT NOT NULL UNIQUE,
    color           INTEGER NOT NULL DEFAULT 0,
    position        INTEGER NOT NULL,
    hoist           BOOLEAN NOT NULL DEFAULT false,
    permissions     BIGINT NOT NULL DEFAULT 0,  -- server-level bitfield
    is_everyone     BOOLEAN NOT NULL DEFAULT false,
    created_at      TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at      TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE TRIGGER set_updated_at BEFORE UPDATE ON roles
    FOR EACH ROW EXECUTE FUNCTION trigger_set_updated_at();

CREATE UNIQUE INDEX idx_roles_position ON roles (position);

-- Member Roles

CREATE TABLE member_roles (
    user_id         UUID NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    role_id         UUID NOT NULL REFERENCES roles(id) ON DELETE CASCADE,
    assigned_at     TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    PRIMARY KEY (user_id, role_id)
);

-- Permission Overrides

CREATE TABLE permission_overrides (
    id              UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    target_type     TEXT NOT NULL,      -- 'channel' or 'category'
    target_id       UUID NOT NULL,      -- references channels.id or categories.id
    principal_type  TEXT NOT NULL,       -- 'role' or 'user'
    principal_id    UUID NOT NULL,       -- references roles.id or users.id
    allow           BIGINT NOT NULL DEFAULT 0,
    deny            BIGINT NOT NULL DEFAULT 0,
    created_at      TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at      TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    CONSTRAINT chk_permission_overrides_target_type CHECK (target_type IN ('channel', 'category')),
    CONSTRAINT chk_permission_overrides_principal_type CHECK (principal_type IN ('role', 'user'))
);

CREATE TRIGGER set_updated_at BEFORE UPDATE ON permission_overrides
    FOR EACH ROW EXECUTE FUNCTION trigger_set_updated_at();

CREATE UNIQUE INDEX idx_overrides_unique
    ON permission_overrides (target_type, target_id, principal_type, principal_id);
CREATE INDEX idx_overrides_target ON permission_overrides (target_type, target_id);

-- Orphan cleanup triggers for permission_overrides polymorphic foreign keys
-- +goose StatementBegin
CREATE OR REPLACE FUNCTION clean_permission_overrides_target()
RETURNS TRIGGER AS $$
BEGIN
    DELETE FROM permission_overrides WHERE target_type = TG_ARGV[0] AND target_id = OLD.id;
    RETURN OLD;
END;
$$ LANGUAGE plpgsql;
-- +goose StatementEnd

-- +goose StatementBegin
CREATE OR REPLACE FUNCTION clean_permission_overrides_principal()
RETURNS TRIGGER AS $$
BEGIN
    DELETE FROM permission_overrides WHERE principal_type = TG_ARGV[0] AND principal_id = OLD.id;
    RETURN OLD;
END;
$$ LANGUAGE plpgsql;
-- +goose StatementEnd

CREATE TRIGGER trg_channels_clean_overrides AFTER DELETE ON channels
    FOR EACH ROW EXECUTE FUNCTION clean_permission_overrides_target('channel');

CREATE TRIGGER trg_categories_clean_overrides AFTER DELETE ON categories
    FOR EACH ROW EXECUTE FUNCTION clean_permission_overrides_target('category');

CREATE TRIGGER trg_roles_clean_overrides AFTER DELETE ON roles
    FOR EACH ROW EXECUTE FUNCTION clean_permission_overrides_principal('role');

CREATE TRIGGER trg_users_clean_overrides AFTER DELETE ON users
    FOR EACH ROW EXECUTE FUNCTION clean_permission_overrides_principal('user');

-- DM Channels

CREATE TABLE dm_channels (
    id              UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    type            TEXT NOT NULL DEFAULT 'dm',  -- dm, group_dm
    name            TEXT,
    owner_id        UUID REFERENCES users(id) ON DELETE SET NULL,
    created_at      TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at      TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    CONSTRAINT chk_dm_channels_type CHECK (type IN ('dm', 'group_dm'))
);

CREATE TRIGGER set_updated_at BEFORE UPDATE ON dm_channels
    FOR EACH ROW EXECUTE FUNCTION trigger_set_updated_at();

CREATE TABLE dm_participants (
    dm_channel_id   UUID NOT NULL REFERENCES dm_channels(id) ON DELETE CASCADE,
    user_id         UUID NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    joined_at       TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    PRIMARY KEY (dm_channel_id, user_id)
);

-- Custom Emoji

CREATE TABLE custom_emoji (
    id              UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    name            TEXT NOT NULL UNIQUE,
    animated        BOOLEAN NOT NULL DEFAULT false,
    storage_key     TEXT NOT NULL,
    uploader_id     UUID NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    created_at      TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at      TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE TRIGGER set_updated_at BEFORE UPDATE ON custom_emoji
    FOR EACH ROW EXECUTE FUNCTION trigger_set_updated_at();

-- Invites

CREATE TABLE invites (
    id              UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    code            TEXT UNIQUE NOT NULL,
    channel_id      UUID NOT NULL REFERENCES channels(id) ON DELETE CASCADE,
    creator_id      UUID NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    max_uses        INTEGER,
    use_count       INTEGER NOT NULL DEFAULT 0,
    max_age_seconds INTEGER,
    expires_at      TIMESTAMPTZ,
    created_at      TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE INDEX idx_invites_code ON invites (code);

-- Webhooks

CREATE TABLE webhooks (
    id              UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    channel_id      UUID NOT NULL REFERENCES channels(id) ON DELETE CASCADE,
    type            TEXT NOT NULL DEFAULT 'incoming',  -- incoming, outgoing
    name            TEXT NOT NULL,
    avatar_key      TEXT,
    token           TEXT UNIQUE NOT NULL,
    url             TEXT,  -- outgoing webhook target URL
    creator_id      UUID NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    created_at      TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at      TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    CONSTRAINT chk_webhooks_type CHECK (type IN ('incoming', 'outgoing'))
);

CREATE TRIGGER set_updated_at BEFORE UPDATE ON webhooks
    FOR EACH ROW EXECUTE FUNCTION trigger_set_updated_at();

-- Audit Log

CREATE TABLE audit_log (
    id              UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    actor_id        UUID NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    action          TEXT NOT NULL,
    target_type     TEXT,
    target_id       UUID,
    changes         JSONB,
    reason          TEXT,
    created_at      TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE INDEX idx_audit_log_time ON audit_log (created_at DESC);
CREATE INDEX idx_audit_log_actor ON audit_log (actor_id);

-- Bans

CREATE TABLE bans (
    user_id         UUID NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    reason          TEXT,
    banned_by       UUID NOT NULL REFERENCES users(id),
    expires_at      TIMESTAMPTZ,
    created_at      TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    PRIMARY KEY (user_id)
);

-- Reports

CREATE TABLE reports (
    id              UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    reporter_id     UUID NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    message_id      UUID NOT NULL REFERENCES messages(id) ON DELETE CASCADE,
    reason          TEXT NOT NULL,
    status          TEXT NOT NULL DEFAULT 'open',  -- open, resolved, dismissed
    resolved_by     UUID REFERENCES users(id),
    resolution_note TEXT,
    created_at      TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at      TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    CONSTRAINT chk_reports_status CHECK (status IN ('open', 'resolved', 'dismissed'))
);

CREATE TRIGGER set_updated_at BEFORE UPDATE ON reports
    FOR EACH ROW EXECUTE FUNCTION trigger_set_updated_at();

-- Auto-Moderation Rules

CREATE TABLE automod_rules (
    id              UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    name            TEXT NOT NULL,
    type            TEXT NOT NULL,           -- keyword, regex, spam, link
    trigger_config  JSONB NOT NULL,
    action_config   JSONB NOT NULL,
    enabled         BOOLEAN NOT NULL DEFAULT true,
    exempt_roles    UUID[] NOT NULL DEFAULT '{}',
    exempt_channels UUID[] NOT NULL DEFAULT '{}',
    creator_id      UUID NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    created_at      TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at      TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE TRIGGER set_updated_at BEFORE UPDATE ON automod_rules
    FOR EACH ROW EXECUTE FUNCTION trigger_set_updated_at();

-- Onboarding

CREATE TABLE onboarding_config (
    id                      UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    rules_markdown          TEXT NOT NULL DEFAULT '',
    welcome_channel_id      UUID REFERENCES channels(id) ON DELETE SET NULL,
    require_rules_acceptance BOOLEAN NOT NULL DEFAULT true,
    require_email_verification BOOLEAN NOT NULL DEFAULT true,
    min_account_age_seconds INTEGER NOT NULL DEFAULT 0,
    require_phone            BOOLEAN NOT NULL DEFAULT false,
    require_captcha          BOOLEAN NOT NULL DEFAULT false,
    auto_roles               UUID[] NOT NULL DEFAULT '{}',
    created_at               TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at               TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE TRIGGER set_updated_at BEFORE UPDATE ON onboarding_config
    FOR EACH ROW EXECUTE FUNCTION trigger_set_updated_at();

-- Anti-Abuse

CREATE TABLE user_ip_log (
    id              UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    user_id         UUID NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    ip_address      INET NOT NULL,
    action          TEXT NOT NULL,  -- register, login, join
    created_at      TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE INDEX idx_ip_log_ip ON user_ip_log (ip_address);
CREATE INDEX idx_ip_log_user ON user_ip_log (user_id, created_at DESC);

CREATE TABLE user_devices (
    id              UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    user_id         UUID NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    device_fingerprint TEXT NOT NULL,
    os              TEXT,
    client_name     TEXT,
    client_version  TEXT,
    first_seen      TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    last_seen       TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE INDEX idx_devices_fingerprint ON user_devices (device_fingerprint);
CREATE INDEX idx_devices_user ON user_devices (user_id);

CREATE TABLE abuse_flags (
    id              UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    flagged_user_id UUID NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    reason          TEXT NOT NULL,  -- shared_ip, shared_device, behavioral, manual
    related_user_id UUID REFERENCES users(id) ON DELETE SET NULL,
    evidence        JSONB,
    status          TEXT NOT NULL DEFAULT 'pending',  -- pending, confirmed, dismissed
    reviewed_by     UUID REFERENCES users(id),
    created_at      TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at      TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    CONSTRAINT chk_abuse_flags_reason CHECK (reason IN ('shared_ip', 'shared_device', 'behavioral', 'manual')),
    CONSTRAINT chk_abuse_flags_status CHECK (status IN ('pending', 'confirmed', 'dismissed'))
);

CREATE TRIGGER set_updated_at BEFORE UPDATE ON abuse_flags
    FOR EACH ROW EXECUTE FUNCTION trigger_set_updated_at();

CREATE INDEX idx_abuse_flags_user ON abuse_flags (flagged_user_id);
CREATE INDEX idx_abuse_flags_status ON abuse_flags (status);

-- Registered Plugins

CREATE TABLE registered_plugins (
    id                  UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    name                TEXT UNIQUE NOT NULL,
    version             TEXT NOT NULL,
    description         TEXT NOT NULL DEFAULT '',
    event_subscriptions TEXT[] NOT NULL DEFAULT '{}',
    api_permissions     TEXT[] NOT NULL DEFAULT '{}',
    slash_commands      JSONB,
    status              TEXT NOT NULL DEFAULT 'active',  -- active, disabled, error
    registered_at       TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at          TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    CONSTRAINT chk_registered_plugins_status CHECK (status IN ('active', 'disabled', 'error'))
);

CREATE TRIGGER set_updated_at BEFORE UPDATE ON registered_plugins
    FOR EACH ROW EXECUTE FUNCTION trigger_set_updated_at();

-- +goose Down

DROP FUNCTION IF EXISTS clean_permission_overrides_target();
DROP FUNCTION IF EXISTS clean_permission_overrides_principal();

DROP TABLE IF EXISTS registered_plugins CASCADE;
DROP TABLE IF EXISTS abuse_flags CASCADE;
DROP TABLE IF EXISTS user_devices CASCADE;
DROP TABLE IF EXISTS user_ip_log CASCADE;
DROP TABLE IF EXISTS onboarding_config CASCADE;
DROP TABLE IF EXISTS automod_rules CASCADE;
DROP TABLE IF EXISTS reports CASCADE;
DROP TABLE IF EXISTS bans CASCADE;
DROP TABLE IF EXISTS audit_log CASCADE;
DROP TABLE IF EXISTS webhooks CASCADE;
DROP TABLE IF EXISTS invites CASCADE;
DROP TABLE IF EXISTS custom_emoji CASCADE;
DROP TABLE IF EXISTS dm_participants CASCADE;
DROP TABLE IF EXISTS dm_channels CASCADE;
DROP TABLE IF EXISTS permission_overrides CASCADE;
DROP TABLE IF EXISTS member_roles CASCADE;
DROP TABLE IF EXISTS roles CASCADE;
DROP TABLE IF EXISTS reactions CASCADE;
DROP TABLE IF EXISTS threads CASCADE;
DROP TABLE IF EXISTS message_attachments CASCADE;
DROP TABLE IF EXISTS messages CASCADE;
DROP TABLE IF EXISTS channels CASCADE;
DROP TABLE IF EXISTS categories CASCADE;
DROP TABLE IF EXISTS members CASCADE;
DROP TABLE IF EXISTS server_config CASCADE;
DROP TABLE IF EXISTS users CASCADE;
DROP FUNCTION IF EXISTS trigger_set_updated_at();
