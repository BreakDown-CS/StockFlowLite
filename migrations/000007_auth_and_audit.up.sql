-- Phase 7: authentication, authorization and audit trail.
-- Additive only: existing tables keep their data; new columns have safe defaults.

-- 1) Account state on users ---------------------------------------------------
ALTER TABLE users ADD COLUMN IF NOT EXISTS is_active BOOLEAN NOT NULL DEFAULT true;
ALTER TABLE users ADD COLUMN IF NOT EXISTS last_login_at TIMESTAMPTZ;
ALTER TABLE users ADD COLUMN IF NOT EXISTS password_changed_at TIMESTAMPTZ;

-- The Phase 5 development identities can never authenticate; make that explicit.
UPDATE users
SET is_active = false
WHERE password_hash = '!disabled-development-identity'
  AND id IN (
      '00000000-0000-4000-8000-000000000001',
      '00000000-0000-4000-8000-000000000002'
  );

-- 2) Opaque bearer sessions ---------------------------------------------------
-- Only a keyed hash of the token is stored, so a database leak cannot be replayed.
CREATE TABLE IF NOT EXISTS auth_sessions (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    user_id UUID NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    token_hash CHAR(64) NOT NULL UNIQUE,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    expires_at TIMESTAMPTZ NOT NULL,
    revoked_at TIMESTAMPTZ,
    last_used_at TIMESTAMPTZ,
    ip_address VARCHAR(45),
    user_agent VARCHAR(255),
    CONSTRAINT chk_auth_sessions_expiry CHECK (expires_at > created_at)
);

CREATE INDEX IF NOT EXISTS idx_auth_sessions_user_id ON auth_sessions(user_id);
CREATE INDEX IF NOT EXISTS idx_auth_sessions_expires_at ON auth_sessions(expires_at);

-- 3) Audit trail --------------------------------------------------------------
CREATE TABLE IF NOT EXISTS audit_events (
    id BIGSERIAL PRIMARY KEY,
    event_type VARCHAR(50) NOT NULL,
    actor_id UUID REFERENCES users(id) ON DELETE RESTRICT,
    actor_username VARCHAR(50) NOT NULL,
    actor_role VARCHAR(20) NOT NULL,
    entity_type VARCHAR(30) NOT NULL,
    entity_id UUID,
    reference VARCHAR(100),
    from_status VARCHAR(20),
    to_status VARCHAR(20),
    reason TEXT,
    metadata JSONB,
    request_id VARCHAR(64),
    ip_address VARCHAR(45),
    user_agent VARCHAR(255),
    created_at TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE INDEX IF NOT EXISTS idx_audit_events_entity ON audit_events(entity_type, entity_id, created_at DESC);
CREATE INDEX IF NOT EXISTS idx_audit_events_actor ON audit_events(actor_id, created_at DESC);
CREATE INDEX IF NOT EXISTS idx_audit_events_type ON audit_events(event_type, created_at DESC);
CREATE INDEX IF NOT EXISTS idx_audit_events_created_at ON audit_events(created_at DESC);

-- 4) Cancellation reason (Phase 6 gap) ---------------------------------------
ALTER TABLE stock_requests ADD COLUMN IF NOT EXISTS cancellation_reason TEXT;
