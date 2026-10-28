CREATE TABLE IF NOT EXISTS stock_requests (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    request_number VARCHAR(50) NOT NULL UNIQUE,
    requester_id UUID NOT NULL REFERENCES users(id) ON DELETE RESTRICT,
    status VARCHAR(20) NOT NULL DEFAULT 'PENDING' 
        CHECK (status IN ('PENDING', 'APPROVED', 'REJECTED', 'ISSUED', 'CANCELLED')),
    notes TEXT,
    reviewer_id UUID REFERENCES users(id) ON DELETE RESTRICT,
    reviewed_at TIMESTAMPTZ,
    rejection_reason TEXT,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE INDEX IF NOT EXISTS idx_stock_requests_requester_id ON stock_requests(requester_id);
CREATE INDEX IF NOT EXISTS idx_stock_requests_status ON stock_requests(status);

CREATE TABLE IF NOT EXISTS stock_request_items (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    request_id UUID NOT NULL REFERENCES stock_requests(id) ON DELETE CASCADE,
    product_id UUID NOT NULL REFERENCES products(id) ON DELETE RESTRICT,
    requested_quantity INT NOT NULL CHECK (requested_quantity > 0),
    CONSTRAINT uq_stock_request_items_req_prod UNIQUE (request_id, product_id)
);

CREATE INDEX IF NOT EXISTS idx_stock_request_items_request_id ON stock_request_items(request_id);
CREATE INDEX IF NOT EXISTS idx_stock_request_items_product_id ON stock_request_items(product_id);
