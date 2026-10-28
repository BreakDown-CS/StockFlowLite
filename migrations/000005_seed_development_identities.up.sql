-- Phase 5: deterministic development attribution identities.
--
-- stock_requests.requester_id is NOT NULL and stock_requests.reviewer_id /
-- stock_movements.actor_id are FK-bound to users(id), but authentication and
-- user provisioning are out of scope for Phase 5. Without at least one user row
-- no stock request can ever be persisted.
--
-- These two rows are the ONLY identities the API uses for audit columns. They are
-- resolved server-side (never accepted from a client payload) and their password
-- hash is intentionally invalid, so both accounts are unusable for login once
-- authentication is implemented.
INSERT INTO users (id, username, password_hash, role)
VALUES
    ('00000000-0000-4000-8000-000000000001', 'dev_requester', '!disabled-development-identity', 'REQUESTER'),
    ('00000000-0000-4000-8000-000000000002', 'dev_storekeeper', '!disabled-development-identity', 'STOREKEEPER')
ON CONFLICT (id) DO NOTHING;
