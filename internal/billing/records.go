package billing

import "time"

type BillingPeriod string

const (
	BillingOneTime BillingPeriod = "one_time"
	BillingMonthly BillingPeriod = "monthly"
	BillingYearly  BillingPeriod = "yearly"
)

type Plan struct {
	ID            uint64            `json:"id"`
	Code          string            `json:"code"`
	Name          string            `json:"name"`
	Status        PlanStatus        `json:"status"`
	Money         Money             `json:"money"`
	BillingPeriod BillingPeriod     `json:"billing_period"`
	Version       uint64            `json:"version"`
	Entitlements  []PlanEntitlement `json:"entitlements"`
	CreatedAt     time.Time         `json:"created_at"`
	UpdatedAt     time.Time         `json:"updated_at"`
}

type PlanEntitlement struct {
	Capability    string `json:"capability"`
	LimitValue    uint64 `json:"limit_value"`
	Unit          string `json:"unit"`
	SourceVersion uint64 `json:"source_version"`
}

type OrderKind string

const (
	OrderNew     OrderKind = "new"
	OrderUpgrade OrderKind = "upgrade"
	OrderRenewal OrderKind = "renewal"
)

type Order struct {
	ID          string      `json:"id"`
	WorkspaceID string      `json:"workspace_id"`
	PlanID      uint64      `json:"plan_id"`
	Kind        OrderKind   `json:"kind"`
	Money       Money       `json:"money"`
	Status      OrderStatus `json:"status"`
	CreatedAt   time.Time   `json:"created_at"`
	UpdatedAt   time.Time   `json:"updated_at"`
}

type Invoice struct {
	ID          string     `json:"id"`
	WorkspaceID string     `json:"workspace_id"`
	OrderID     string     `json:"order_id"`
	Money       Money      `json:"money"`
	Status      string     `json:"status"`
	IssuedAt    time.Time  `json:"issued_at"`
	PaidAt      *time.Time `json:"paid_at,omitempty"`
	RefundedAt  *time.Time `json:"refunded_at,omitempty"`
	CreatedAt   time.Time  `json:"created_at"`
}

type Transaction struct {
	ID                    uint64            `json:"id"`
	WorkspaceID           string            `json:"workspace_id"`
	OrderID               string            `json:"order_id"`
	Provider              Provider          `json:"provider"`
	ProviderTransactionID string            `json:"provider_transaction_id"`
	Money                 Money             `json:"money"`
	Status                TransactionStatus `json:"status"`
	CreatedAt             time.Time         `json:"created_at"`
	UpdatedAt             time.Time         `json:"updated_at"`
}

type Subscription struct {
	ID                string             `json:"id"`
	WorkspaceID       string             `json:"workspace_id"`
	PlanID            uint64             `json:"plan_id"`
	Status            SubscriptionStatus `json:"status"`
	StartsAt          time.Time          `json:"starts_at"`
	CurrentTermEndsAt *time.Time         `json:"current_term_ends_at,omitempty"`
	GraceEndsAt       *time.Time         `json:"grace_ends_at,omitempty"`
	CancelAt          *time.Time         `json:"cancel_at,omitempty"`
	Version           uint64             `json:"version"`
	CreatedAt         time.Time          `json:"created_at"`
	UpdatedAt         time.Time          `json:"updated_at"`
}

type CallbackCommand struct {
	Provider              Provider
	ProviderEventID       string
	ProviderTransactionID string
	OrderID               string
	EventType             string
	Outcome               TransactionStatus
	Money                 Money
	ReceivedAt            time.Time
	CorrelationID         string
}

type CallbackResult struct {
	Duplicate    bool                `json:"duplicate"`
	EventStatus  CallbackEventStatus `json:"event_status"`
	Order        Order               `json:"order"`
	Transaction  Transaction         `json:"transaction"`
	Subscription *Subscription       `json:"subscription,omitempty"`
}
