package domain

import (
	"strings"
	"time"

	"github.com/google/uuid"
)

// RequestStatus represents the state of a stock request.
type RequestStatus string

const (
	StatusPending   RequestStatus = "PENDING"
	StatusApproved  RequestStatus = "APPROVED"
	StatusRejected  RequestStatus = "REJECTED"
	StatusIssued    RequestStatus = "ISSUED"
	StatusCancelled RequestStatus = "CANCELLED"
)

// ValidRequestStatuses lists all legal status values.
var ValidRequestStatuses = []RequestStatus{
	StatusPending,
	StatusApproved,
	StatusRejected,
	StatusIssued,
	StatusCancelled,
}

// StockRequestItem represents a single requested line item.
type StockRequestItem struct {
	ID                uuid.UUID `json:"id"`
	RequestID         uuid.UUID `json:"request_id"`
	ProductID         uuid.UUID `json:"product_id"`
	RequestedQuantity int       `json:"requested_quantity"`
}

// Validate ensures a StockRequestItem satisfies domain rules.
func (item *StockRequestItem) Validate() error {
	if item.ProductID == uuid.Nil {
		return ErrMissingProductID
	}
	if item.RequestedQuantity <= 0 {
		return ErrInvalidQuantity
	}
	return nil
}

// StockRequest represents a department requisition for inventory items.
type StockRequest struct {
	ID              uuid.UUID             `json:"id"`
	RequestNumber   string                `json:"request_number"`
	RequesterID     uuid.UUID             `json:"requester_id"`
	Status          RequestStatus         `json:"status"`
	Notes           string                `json:"notes,omitempty"`
	ReviewerID      *uuid.UUID            `json:"reviewer_id,omitempty"`
	ReviewedAt      *time.Time            `json:"reviewed_at,omitempty"`
	RejectionReason string                `json:"rejection_reason,omitempty"`
	CreatedAt       time.Time             `json:"created_at"`
	UpdatedAt       time.Time             `json:"updated_at"`
	Items           []StockRequestItem    `json:"items,omitempty"`
	Issuance        *StockRequestIssuance `json:"issuance,omitempty"`
}

// NewStockRequest creates and validates a new stock request.
func NewStockRequest(requestNumber string, requesterID uuid.UUID, notes string, items []StockRequestItem) (*StockRequest, error) {
	now := time.Now().UTC()
	reqID := uuid.New()

	for i := range items {
		if items[i].ID == uuid.Nil {
			items[i].ID = uuid.New()
		}
		items[i].RequestID = reqID
	}

	sr := &StockRequest{
		ID:            reqID,
		RequestNumber: strings.TrimSpace(requestNumber),
		RequesterID:   requesterID,
		Status:        StatusPending,
		Notes:         strings.TrimSpace(notes),
		CreatedAt:     now,
		UpdatedAt:     now,
		Items:         items,
	}

	if err := sr.Validate(); err != nil {
		return nil, err
	}
	return sr, nil
}

// Validate enforces domain integrity on the StockRequest and its line items.
func (r *StockRequest) Validate() error {
	if strings.TrimSpace(r.RequestNumber) == "" {
		return ErrEmptyRequestNumber
	}
	if r.RequesterID == uuid.Nil {
		return ErrMissingRequester
	}
	if !r.IsValidStatus() {
		return ErrInvalidStatus
	}
	if len(r.Items) == 0 {
		return ErrEmptyRequestItems
	}

	seenProducts := make(map[uuid.UUID]struct{}, len(r.Items))
	for _, item := range r.Items {
		if err := item.Validate(); err != nil {
			return err
		}
		if _, exists := seenProducts[item.ProductID]; exists {
			return ErrDuplicateItem
		}
		seenProducts[item.ProductID] = struct{}{}
	}

	return nil
}

// IsValidStatus checks if the request status belongs to the recognized enum.
func (r *StockRequest) IsValidStatus() bool {
	for _, s := range ValidRequestStatuses {
		if r.Status == s {
			return true
		}
	}
	return false
}

// CanBeReviewed checks if the request can be approved or rejected.
func (r *StockRequest) CanBeReviewed() bool {
	return r.Status == StatusPending
}

// CanBeCancelled checks if the requester can still abandon the request.
// A request may be cancelled while PENDING, or after approval as long as no stock
// has been deducted yet (an ISSUED request can no longer be cancelled).
func (r *StockRequest) CanBeCancelled() bool {
	return r.Status == StatusPending || r.Status == StatusApproved
}

// Cancel moves a PENDING or APPROVED request to CANCELLED without touching stock.
func (r *StockRequest) Cancel(cancelledAt time.Time) error {
	if !r.CanBeCancelled() {
		return ErrCannotCancel
	}

	r.Status = StatusCancelled
	r.UpdatedAt = cancelledAt
	return nil
}

// CanBeIssued checks if the warehouse can fulfill and deduct inventory.
func (r *StockRequest) CanBeIssued() bool {
	return r.Status == StatusApproved
}

// Approve transitions a PENDING request to APPROVED and records reviewer metadata.
// Approval never touches stock; physical issuance is a separate step (see MarkIssued).
func (r *StockRequest) Approve(reviewerID uuid.UUID, reviewedAt time.Time) error {
	if reviewerID == uuid.Nil {
		return ErrMissingReviewer
	}
	if !r.CanBeReviewed() {
		return ErrCannotReview
	}

	r.Status = StatusApproved
	r.ReviewerID = &reviewerID
	r.ReviewedAt = &reviewedAt
	r.RejectionReason = ""
	r.UpdatedAt = reviewedAt
	return nil
}

// Reject transitions a PENDING request to REJECTED and records reviewer metadata and the optional reason.
// Rejection never touches stock.
func (r *StockRequest) Reject(reviewerID uuid.UUID, reason string, reviewedAt time.Time) error {
	if reviewerID == uuid.Nil {
		return ErrMissingReviewer
	}
	if !r.CanBeReviewed() {
		return ErrCannotReview
	}

	r.Status = StatusRejected
	r.ReviewerID = &reviewerID
	r.ReviewedAt = &reviewedAt
	r.RejectionReason = strings.TrimSpace(reason)
	r.UpdatedAt = reviewedAt
	return nil
}

// MarkIssued transitions an APPROVED request to ISSUED once every item has been deducted from stock.
func (r *StockRequest) MarkIssued(issuedAt time.Time) error {
	if !r.CanBeIssued() {
		return ErrCannotIssue
	}

	r.Status = StatusIssued
	r.UpdatedAt = issuedAt
	return nil
}
