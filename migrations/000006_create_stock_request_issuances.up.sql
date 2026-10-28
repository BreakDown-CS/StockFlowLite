-- Phase 6: one immutable fulfillment (issuance) record per stock request.
--
-- Rationale:
--   * enforces at the database level that a request can be issued at most once
--     (UNIQUE request_id) even if application logic regresses;
--   * stores the optional client Idempotency-Key so a retried request can replay the
--     original result instead of failing with 409 (UNIQUE idempotency_key);
--   * provides an audit row: who fulfilled the request, when, how many items/units.
--
-- Purely additive: no existing table, column, or row is modified.
CREATE TABLE IF NOT EXISTS stock_request_issuances (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    request_id UUID NOT NULL REFERENCES stock_requests(id) ON DELETE RESTRICT,
    idempotency_key VARCHAR(100),
    actor_id UUID REFERENCES users(id) ON DELETE RESTRICT,
    item_count INT NOT NULL CHECK (item_count > 0),
    total_quantity INT NOT NULL CHECK (total_quantity > 0),
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    CONSTRAINT uq_stock_request_issuances_request UNIQUE (request_id),
    CONSTRAINT uq_stock_request_issuances_key UNIQUE (idempotency_key)
);

CREATE INDEX IF NOT EXISTS idx_stock_request_issuances_request_id ON stock_request_issuances(request_id);
CREATE INDEX IF NOT EXISTS idx_stock_request_issuances_actor_id ON stock_request_issuances(actor_id);
