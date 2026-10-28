-- Removes the development attribution identities seeded by the up migration.
-- Rows are only deleted when no business data references them, so existing
-- requests and movements are never orphaned or modified.
DELETE FROM users
WHERE id = '00000000-0000-4000-8000-000000000001'
  AND NOT EXISTS (
      SELECT 1 FROM stock_requests
      WHERE requester_id = '00000000-0000-4000-8000-000000000001'
         OR reviewer_id = '00000000-0000-4000-8000-000000000001'
  )
  AND NOT EXISTS (
      SELECT 1 FROM stock_movements
      WHERE actor_id = '00000000-0000-4000-8000-000000000001'
  );

DELETE FROM users
WHERE id = '00000000-0000-4000-8000-000000000002'
  AND NOT EXISTS (
      SELECT 1 FROM stock_requests
      WHERE requester_id = '00000000-0000-4000-8000-000000000002'
         OR reviewer_id = '00000000-0000-4000-8000-000000000002'
  )
  AND NOT EXISTS (
      SELECT 1 FROM stock_movements
      WHERE actor_id = '00000000-0000-4000-8000-000000000002'
  );
