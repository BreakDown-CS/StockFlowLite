-- Reverts Phase 7.
-- NOTE: dropping the columns also discards the stored cancellation reasons and
-- the authentication state; only the Phase 1-6 tables themselves are preserved.
DROP TABLE IF EXISTS audit_events;
DROP TABLE IF EXISTS auth_sessions;

ALTER TABLE stock_requests DROP COLUMN IF EXISTS cancellation_reason;

ALTER TABLE users DROP COLUMN IF EXISTS password_changed_at;
ALTER TABLE users DROP COLUMN IF EXISTS last_login_at;
ALTER TABLE users DROP COLUMN IF EXISTS is_active;
