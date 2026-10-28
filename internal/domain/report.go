package domain

import (
	"context"
	"time"

	"github.com/google/uuid"
)

// InventoryReportFilter filters the inventory report.
type InventoryReportFilter struct {
	Search   string
	IsActive *bool // nil means "all products"
	LowStock bool  // when true, only products at or below min_stock
	Limit    int
	Offset   int
}

// InventoryReportRow is a single product line of the inventory report.
type InventoryReportRow struct {
	ID           uuid.UUID `json:"id"`
	SKU          string    `json:"sku"`
	Name         string    `json:"name"`
	Unit         string    `json:"unit"`
	CurrentStock int       `json:"current_stock"`
	MinStock     int       `json:"min_stock"`
	IsActive     bool      `json:"is_active"`
	IsLowStock   bool      `json:"is_low_stock"`
	IsOutOfStock bool      `json:"is_out_of_stock"`
	UpdatedAt    time.Time `json:"updated_at"`
}

// InventorySummary aggregates the filtered inventory set (never the current page only).
type InventorySummary struct {
	TotalProducts      int64 `json:"total_products"`
	ActiveProducts     int64 `json:"active_products"`
	InactiveProducts   int64 `json:"inactive_products"`
	LowStockProducts   int64 `json:"low_stock_products"`
	OutOfStockProducts int64 `json:"out_of_stock_products"`
}

// MovementReportFilter filters the stock movement report.
type MovementReportFilter struct {
	ProductID    *uuid.UUID
	MovementType *MovementType
	From         *time.Time
	To           *time.Time
	Limit        int
	Offset       int
}

// MovementReportRow is a ledger row enriched with product information.
type MovementReportRow struct {
	ID             int64        `json:"id"`
	ProductID      uuid.UUID    `json:"product_id"`
	SKU            string       `json:"sku"`
	ProductName    string       `json:"product_name"`
	MovementType   MovementType `json:"movement_type"`
	Quantity       int          `json:"quantity"`
	SignedQuantity int          `json:"signed_quantity"`
	ReferenceType  string       `json:"reference_type"`
	ReferenceID    string       `json:"reference_id,omitempty"`
	ActorID        *uuid.UUID   `json:"actor_id,omitempty"`
	Notes          string       `json:"notes,omitempty"`
	CreatedAt      time.Time    `json:"created_at"`
}

// MovementSummary aggregates the filtered movement set.
type MovementSummary struct {
	MovementCount           int64 `json:"movement_count"`
	ReceiptTotalQuantity    int64 `json:"receipt_total_quantity"`
	IssueTotalQuantity      int64 `json:"issue_total_quantity"`
	AdjustmentTotalQuantity int64 `json:"adjustment_total_quantity"`
	NetChange               int64 `json:"net_change"`
}

// RequestReportFilter filters the stock request report.
type RequestReportFilter struct {
	Status      *RequestStatus
	RequesterID *uuid.UUID
	From        *time.Time
	To          *time.Time
	Limit       int
	Offset      int
}

// RequestReportRow is a request line with fulfillment and item aggregates.
type RequestReportRow struct {
	ID                 uuid.UUID     `json:"id"`
	RequestNumber      string        `json:"request_number"`
	Status             RequestStatus `json:"status"`
	RequesterID        uuid.UUID     `json:"requester_id"`
	RequesterUsername  string        `json:"requester_username"`
	ItemCount          int           `json:"item_count"`
	RequestedUnits     int           `json:"requested_units"`
	IssuedUnits        int           `json:"issued_units"`
	CreatedAt          time.Time     `json:"created_at"`
	ReviewedAt         *time.Time    `json:"reviewed_at,omitempty"`
	IssuedAt           *time.Time    `json:"issued_at,omitempty"`
	RejectionReason    string        `json:"rejection_reason,omitempty"`
	CancellationReason string        `json:"cancellation_reason,omitempty"`
}

// RequestSummary aggregates the filtered request set.
type RequestSummary struct {
	TotalRequests       int64 `json:"total_requests"`
	PendingRequests     int64 `json:"pending_requests"`
	ApprovedRequests    int64 `json:"approved_requests"`
	RejectedRequests    int64 `json:"rejected_requests"`
	IssuedRequests      int64 `json:"issued_requests"`
	CancelledRequests   int64 `json:"cancelled_requests"`
	RequestedUnitsTotal int64 `json:"requested_units_total"`
	IssuedUnitsTotal    int64 `json:"issued_units_total"`
}

// StockIntegrityFilter filters the stock integrity report.
type StockIntegrityFilter struct {
	OnlyDrifted bool
	Limit       int
	Offset      int
}

// StockIntegrityRow compares the stored balance with the movement ledger.
// LedgerStock is the signed sum of stock_movements (RECEIPT/ADJUSTMENT positive, ISSUE negative)
// and is only a valid expectation for stock that was changed through the API.
type StockIntegrityRow struct {
	ProductID    uuid.UUID `json:"product_id"`
	SKU          string    `json:"sku"`
	Name         string    `json:"name"`
	IsActive     bool      `json:"is_active"`
	CurrentStock int       `json:"current_stock"`
	LedgerStock  int64     `json:"ledger_stock"`
	Drift        int64     `json:"drift"`
}

// StockIntegritySummary aggregates the integrity check over the filtered set.
type StockIntegritySummary struct {
	TotalProducts   int64 `json:"total_products"`
	DriftedProducts int64 `json:"drifted_products"`
	AbsoluteDrift   int64 `json:"absolute_drift"`
}

// ReportRepository provides read-only reporting queries.
// Implementations must run each report in a single consistent snapshot and use
// only one-to-one joins (never fan out rows).
type ReportRepository interface {
	Inventory(ctx context.Context, filter InventoryReportFilter) ([]InventoryReportRow, int64, InventorySummary, error)
	Movements(ctx context.Context, filter MovementReportFilter) ([]MovementReportRow, int64, MovementSummary, error)
	Requests(ctx context.Context, filter RequestReportFilter) ([]RequestReportRow, int64, RequestSummary, error)
	StockIntegrity(ctx context.Context, filter StockIntegrityFilter) ([]StockIntegrityRow, int64, StockIntegritySummary, error)
}
