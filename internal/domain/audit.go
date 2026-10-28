package domain

import (
	"context"
	"time"

	"github.com/google/uuid"
)

// AuditEventType enumerates the auditable domain events.
type AuditEventType string

// Authentication and security events.
const (
	AuditAuthLoginSucceeded  AuditEventType = "AUTH_LOGIN_SUCCEEDED"
	AuditAuthLoginFailed     AuditEventType = "AUTH_LOGIN_FAILED"
	AuditAuthLogout          AuditEventType = "AUTH_LOGOUT"
	AuditAuthPasswordChanged AuditEventType = "AUTH_PASSWORD_CHANGED"
	AuditAccessDenied        AuditEventType = "ACCESS_DENIED"
	AuditSelfApprovalBlocked AuditEventType = "SELF_APPROVAL_BLOCKED"
)

// Catalog and inventory events.
const (
	AuditProductCreated     AuditEventType = "PRODUCT_CREATED"
	AuditProductUpdated     AuditEventType = "PRODUCT_UPDATED"
	AuditProductDeactivated AuditEventType = "PRODUCT_DEACTIVATED"
	AuditStockReceived      AuditEventType = "STOCK_RECEIVED"
)

// Request workflow events.
const (
	AuditRequestCreated   AuditEventType = "REQUEST_CREATED"
	AuditRequestApproved  AuditEventType = "REQUEST_APPROVED"
	AuditRequestRejected  AuditEventType = "REQUEST_REJECTED"
	AuditRequestCancelled AuditEventType = "REQUEST_CANCELLED"
	AuditRequestIssued    AuditEventType = "REQUEST_ISSUED"
)

// User administration events.
const (
	AuditUserCreated       AuditEventType = "USER_CREATED"
	AuditUserRoleChanged   AuditEventType = "USER_ROLE_CHANGED"
	AuditUserActivated     AuditEventType = "USER_ACTIVATED"
	AuditUserDeactivated   AuditEventType = "USER_DEACTIVATED"
	AuditUserPasswordReset AuditEventType = "USER_PASSWORD_RESET"
)

// ValidAuditEventTypes lists every accepted event type (used for filter validation).
var ValidAuditEventTypes = []AuditEventType{
	AuditAuthLoginSucceeded,
	AuditAuthLoginFailed,
	AuditAuthLogout,
	AuditAuthPasswordChanged,
	AuditAccessDenied,
	AuditSelfApprovalBlocked,
	AuditProductCreated,
	AuditProductUpdated,
	AuditProductDeactivated,
	AuditStockReceived,
	AuditRequestCreated,
	AuditRequestApproved,
	AuditRequestRejected,
	AuditRequestCancelled,
	AuditRequestIssued,
	AuditUserCreated,
	AuditUserRoleChanged,
	AuditUserActivated,
	AuditUserDeactivated,
	AuditUserPasswordReset,
}

// Audit entity type identifiers.
const (
	AuditEntityAuth         = "AUTH"
	AuditEntityProduct      = "PRODUCT"
	AuditEntityStockRequest = "STOCK_REQUEST"
	AuditEntityUser         = "USER"
)

// AuditEvent is an immutable record of a security or business state change.
type AuditEvent struct {
	ID            int64                  `json:"id"`
	EventType     AuditEventType         `json:"event_type"`
	ActorID       *uuid.UUID             `json:"actor_id,omitempty"`
	ActorUsername string                 `json:"actor_username"`
	ActorRole     UserRole               `json:"actor_role"`
	EntityType    string                 `json:"entity_type"`
	EntityID      *uuid.UUID             `json:"entity_id,omitempty"`
	Reference     string                 `json:"reference,omitempty"`
	FromStatus    string                 `json:"from_status,omitempty"`
	ToStatus      string                 `json:"to_status,omitempty"`
	Reason        string                 `json:"reason,omitempty"`
	Metadata      map[string]interface{} `json:"metadata,omitempty"`
	RequestID     string                 `json:"request_id,omitempty"`
	IPAddress     string                 `json:"ip_address,omitempty"`
	UserAgent     string                 `json:"user_agent,omitempty"`
	CreatedAt     time.Time              `json:"created_at"`
}

// ListAuditFilter holds filtering and pagination parameters for audit queries.
type ListAuditFilter struct {
	EventType  *AuditEventType
	EntityType string
	EntityID   *uuid.UUID
	ActorID    *uuid.UUID
	From       *time.Time
	To         *time.Time
	Limit      int
	Offset     int
}

// AuditRepository records and reads audit events.
// Record is best-effort for security events; state changes write their audit row
// inside the same transaction as the business change.
type AuditRepository interface {
	Record(ctx context.Context, event AuditEvent) error
	List(ctx context.Context, filter ListAuditFilter) ([]AuditEvent, int64, error)
}
