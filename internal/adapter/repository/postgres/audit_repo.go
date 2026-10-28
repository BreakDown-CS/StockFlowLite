package postgres

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"strings"
	"time"

	"github.com/BreakDown-CS/StockFlowLite/internal/domain"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

// AuditRepository implements domain.AuditRepository using pgxpool.
type AuditRepository struct {
	pool *pgxpool.Pool
}

// NewAuditRepository constructs an AuditRepository.
func NewAuditRepository(pool *pgxpool.Pool) *AuditRepository {
	return &AuditRepository{pool: pool}
}

// Record persists one audit event in its own short transaction. It is used for
// best-effort security events (failed logins, access denials) where no business
// state changes. Callers must treat a failure as non-fatal.
func (r *AuditRepository) Record(ctx context.Context, event domain.AuditEvent) error {
	tx, err := r.pool.Begin(ctx)
	if err != nil {
		return fmt.Errorf("failed to begin audit transaction: %w", err)
	}
	defer func() {
		if rErr := tx.Rollback(ctx); rErr != nil && !errors.Is(rErr, pgx.ErrTxClosed) {
			slog.Warn("Failed to roll back audit transaction", "event_type", string(event.EventType), "error", rErr)
		}
	}()

	if err := insertAuditEvent(ctx, tx, event); err != nil {
		return err
	}

	if err := tx.Commit(ctx); err != nil {
		return fmt.Errorf("failed to commit audit transaction: %w", err)
	}
	return nil
}

// List returns filtered audit events (newest first) together with the total count.
func (r *AuditRepository) List(ctx context.Context, filter domain.ListAuditFilter) ([]domain.AuditEvent, int64, error) {
	limit := filter.Limit
	if limit <= 0 {
		limit = 20
	} else if limit > 200 {
		limit = 200
	}
	offset := filter.Offset
	if offset < 0 {
		offset = 0
	}

	where := []string{"1=1"}
	args := make([]any, 0, 6)
	argIdx := 1

	if filter.EventType != nil {
		where = append(where, fmt.Sprintf("event_type = $%d", argIdx))
		args = append(args, string(*filter.EventType))
		argIdx++
	}
	if strings.TrimSpace(filter.EntityType) != "" {
		where = append(where, fmt.Sprintf("entity_type = $%d", argIdx))
		args = append(args, strings.TrimSpace(filter.EntityType))
		argIdx++
	}
	if filter.EntityID != nil {
		where = append(where, fmt.Sprintf("entity_id = $%d", argIdx))
		args = append(args, *filter.EntityID)
		argIdx++
	}
	if filter.ActorID != nil {
		where = append(where, fmt.Sprintf("actor_id = $%d", argIdx))
		args = append(args, *filter.ActorID)
		argIdx++
	}
	if filter.From != nil {
		where = append(where, fmt.Sprintf("created_at >= $%d", argIdx))
		args = append(args, *filter.From)
		argIdx++
	}
	if filter.To != nil {
		where = append(where, fmt.Sprintf("created_at <= $%d", argIdx))
		args = append(args, *filter.To)
		argIdx++
	}

	whereClause := strings.Join(where, " AND ")

	var total int64
	countQuery := fmt.Sprintf("SELECT COUNT(*) FROM audit_events WHERE %s", whereClause)
	if err := r.pool.QueryRow(ctx, countQuery, args...).Scan(&total); err != nil {
		return nil, 0, fmt.Errorf("failed to count audit events: %w", err)
	}

	dataQuery := fmt.Sprintf(`
		SELECT id, event_type, actor_id, actor_username, actor_role, entity_type, entity_id,
		       COALESCE(reference, ''), COALESCE(from_status, ''), COALESCE(to_status, ''),
		       COALESCE(reason, ''), metadata, COALESCE(request_id, ''),
		       COALESCE(ip_address, ''), COALESCE(user_agent, ''), created_at
		FROM audit_events
		WHERE %s
		ORDER BY created_at DESC, id DESC
		LIMIT $%d OFFSET $%d;
	`, whereClause, argIdx, argIdx+1)

	rows, err := r.pool.Query(ctx, dataQuery, append(args, limit, offset)...)
	if err != nil {
		return nil, 0, fmt.Errorf("failed to query audit events: %w", err)
	}
	defer rows.Close()

	events := make([]domain.AuditEvent, 0, limit)
	for rows.Next() {
		var event domain.AuditEvent
		var eventType string
		var actorRole string
		var metadata []byte

		if err := rows.Scan(
			&event.ID,
			&eventType,
			&event.ActorID,
			&event.ActorUsername,
			&actorRole,
			&event.EntityType,
			&event.EntityID,
			&event.Reference,
			&event.FromStatus,
			&event.ToStatus,
			&event.Reason,
			&metadata,
			&event.RequestID,
			&event.IPAddress,
			&event.UserAgent,
			&event.CreatedAt,
		); err != nil {
			return nil, 0, fmt.Errorf("failed to scan audit event: %w", err)
		}

		event.EventType = domain.AuditEventType(eventType)
		event.ActorRole = domain.UserRole(actorRole)
		if len(metadata) > 0 {
			if err := json.Unmarshal(metadata, &event.Metadata); err != nil {
				return nil, 0, fmt.Errorf("failed to decode audit metadata: %w", err)
			}
		}
		events = append(events, event)
	}
	if err := rows.Err(); err != nil {
		return nil, 0, fmt.Errorf("error during audit iteration: %w", err)
	}

	return events, total, nil
}

// insertAuditEvent writes one immutable audit row inside the caller's transaction.
// Business state changes use this so the audit trail commits atomically with them.
func insertAuditEvent(ctx context.Context, tx pgx.Tx, event domain.AuditEvent) error {
	if event.EventType == "" || event.EntityType == "" {
		return fmt.Errorf("audit event requires an event type and an entity type")
	}

	actorUsername := event.ActorUsername
	if strings.TrimSpace(actorUsername) == "" {
		actorUsername = "system"
	}
	actorRole := event.ActorRole
	if actorRole == "" {
		actorRole = domain.RoleAdmin
	}
	createdAt := event.CreatedAt
	if createdAt.IsZero() {
		createdAt = time.Now().UTC()
	}

	var metadata []byte
	if len(event.Metadata) > 0 {
		encoded, err := json.Marshal(event.Metadata)
		if err != nil {
			return fmt.Errorf("failed to encode audit metadata: %w", err)
		}
		metadata = encoded
	}

	const query = `
		INSERT INTO audit_events (
			event_type, actor_id, actor_username, actor_role, entity_type, entity_id,
			reference, from_status, to_status, reason, metadata, request_id, ip_address, user_agent, created_at
		) VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12, $13, $14, $15);
	`

	_, err := tx.Exec(ctx, query,
		string(event.EventType),
		event.ActorID,
		actorUsername,
		string(actorRole),
		event.EntityType,
		event.EntityID,
		nullableText(event.Reference),
		nullableText(event.FromStatus),
		nullableText(event.ToStatus),
		nullableText(event.Reason),
		metadata,
		nullableText(event.RequestID),
		nullableText(event.IPAddress),
		nullableText(event.UserAgent),
		createdAt,
	)
	if err != nil {
		return fmt.Errorf("failed to insert audit event: %w", err)
	}

	return nil
}

// auditEventFromContext builds an audit event attributed to the authenticated
// principal carried in the context (falling back to the system actor for CLI/tests).
func auditEventFromContext(
	ctx context.Context,
	eventType domain.AuditEventType,
	entityType string,
	entityID *uuid.UUID,
) domain.AuditEvent {
	event := domain.AuditEvent{
		EventType:  eventType,
		EntityType: entityType,
		EntityID:   entityID,
	}

	if actor, ok := domain.ActorFromContext(ctx); ok {
		actorID := actor.ID
		event.ActorID = &actorID
		event.ActorUsername = actor.Username
		event.ActorRole = actor.Role
		event.RequestID = actor.RequestID
		event.IPAddress = actor.IPAddress
		event.UserAgent = actor.UserAgent
		return event
	}

	system := domain.SystemActor()
	event.ActorUsername = system.Username
	event.ActorRole = system.Role
	return event
}

// recordAuditBestEffort writes an audit row in its own transaction and never fails the
// caller. It is used for denied attempts where the business transaction was rolled back.
func recordAuditBestEffort(ctx context.Context, pool *pgxpool.Pool, event domain.AuditEvent) {
	tx, err := pool.Begin(ctx)
	if err != nil {
		slog.Warn("Failed to begin audit transaction", "event_type", string(event.EventType), "error", err)
		return
	}
	defer func() {
		_ = tx.Rollback(ctx)
	}()

	if err := insertAuditEvent(ctx, tx, event); err != nil {
		slog.Warn("Failed to record audit event", "event_type", string(event.EventType), "error", err)
		return
	}
	if err := tx.Commit(ctx); err != nil {
		slog.Warn("Failed to commit audit transaction", "event_type", string(event.EventType), "error", err)
	}
}

// selfApprovalEvent builds the audit event for a blocked four-eyes violation.
func selfApprovalEvent(ctx context.Context, req *domain.StockRequest, action string) domain.AuditEvent {
	requestID := req.ID
	event := auditEventFromContext(ctx, domain.AuditSelfApprovalBlocked, domain.AuditEntityStockRequest, &requestID)
	event.Reference = req.RequestNumber
	event.Reason = "requester cannot " + action + " their own stock request"
	event.Metadata = map[string]interface{}{"action": action, "requester_id": req.RequesterID.String()}
	return event
}
