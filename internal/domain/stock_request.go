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
	ID              uuid.UUID          `json:"id"`
	RequestNumber   string             `json:"request_number"`
	RequesterID     uuid.UUID          `json:"requester_id"`
	Status          RequestStatus      `json:"status"`
	Notes           string             `json:"notes,omitempty"`
	ReviewerID      *uuid.UUID         `json:"reviewer_id,omitempty"`
	ReviewedAt      *time.Time         `json:"reviewed_at,omitempty"`
	RejectionReason string             `json:"rejection_reason,omitempty"`
	CreatedAt       time.Time          `json:"created_at"`
	UpdatedAt       time.Time          `json:"updated_at"`
	Items           []StockRequestItem `json:"items,omitempty"`
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

// CanBeCancelled checks if the requester can cancel the request.
func (r *StockRequest) CanBeCancelled() bool {
	return r.Status == StatusPending
}

// CanBeIssued checks if the warehouse can fulfill and deduct inventory.
func (r *StockRequest) CanBeIssued() bool {
	return r.Status == StatusApproved
}
