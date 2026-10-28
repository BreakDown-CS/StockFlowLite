-- Reverts Phase 6: removes the fulfillment audit/idempotency table.
-- The stock_requests, stock_request_items and stock_movements tables are untouched.
DROP TABLE IF EXISTS stock_request_issuances;
