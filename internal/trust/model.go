package trust

import "time"

type ScanRequestKind string

const (
	ScanRequestInitial ScanRequestKind = "initial"
	ScanRequestRescan  ScanRequestKind = "rescan"
)

type ScanStatus string

const (
	ScanStatusQueued    ScanStatus = "queued"
	ScanStatusLeased    ScanStatus = "leased"
	ScanStatusRetry     ScanStatus = "retry"
	ScanStatusCompleted ScanStatus = "completed"
	ScanStatusFailed    ScanStatus = "failed"
)

type DecisionState string

const (
	DecisionPending DecisionState = "pending"
	DecisionAllow   DecisionState = "allow"
	DecisionReview  DecisionState = "review"
	DecisionBlock   DecisionState = "block"
	DecisionUnknown DecisionState = "unknown"
)

type DestinationScan struct {
	ID              uint64          `json:"id"`
	WorkspaceID     string          `json:"workspace_id"`
	LinkID          uint64          `json:"link_id"`
	RiskFingerprint string          `json:"risk_fingerprint"`
	PolicyVersion   string          `json:"policy_version"`
	RequestKind     ScanRequestKind `json:"request_kind"`
	IdempotencyKey  string          `json:"idempotency_key"`
	Status          ScanStatus      `json:"status"`
	Attempts        uint32          `json:"attempts"`
	MaxAttempts     uint32          `json:"max_attempts"`
	AvailableAt     time.Time       `json:"available_at"`
	LeaseOwner      string          `json:"lease_owner,omitempty"`
	LeaseExpiresAt  *time.Time      `json:"lease_expires_at,omitempty"`
	CorrelationID   string          `json:"correlation_id"`
	LastErrorCode   string          `json:"last_error_code,omitempty"`
	CompletedAt     *time.Time      `json:"completed_at,omitempty"`
	CreatedAt       time.Time       `json:"created_at"`
	UpdatedAt       time.Time       `json:"updated_at"`
}

type ScanTarget struct {
	ScanID        uint64    `json:"scan_id"`
	Order         uint32    `json:"order"`
	NormalizedURL string    `json:"normalized_url"`
	TargetHash    string    `json:"target_hash"`
	CreatedAt     time.Time `json:"created_at"`
}

type ProviderObservation struct {
	ID         uint64          `json:"id"`
	ScanID     uint64          `json:"scan_id"`
	Provider   string          `json:"provider"`
	Outcome    ProviderOutcome `json:"outcome"`
	SignalCode string          `json:"signal_code"`
	Evidence   map[string]any  `json:"evidence"`
	ObservedAt time.Time       `json:"observed_at"`
	CreatedAt  time.Time       `json:"created_at"`
}

type ProviderOutcome string

const (
	ProviderAllow       ProviderOutcome = "allow"
	ProviderReview      ProviderOutcome = "review"
	ProviderBlock       ProviderOutcome = "block"
	ProviderUnknown     ProviderOutcome = "unknown"
	ProviderUnavailable ProviderOutcome = "unavailable"
)

type DestinationDecision struct {
	ID               uint64         `json:"id"`
	WorkspaceID      string         `json:"workspace_id"`
	LinkID           uint64         `json:"link_id"`
	ScanID           uint64         `json:"scan_id"`
	RiskFingerprint  string         `json:"risk_fingerprint"`
	PolicyVersion    string         `json:"policy_version"`
	State            DecisionState  `json:"state"`
	ReasonCategory   string         `json:"reason_category"`
	DecisionMetadata map[string]any `json:"decision_metadata"`
	ValidUntil       *time.Time     `json:"valid_until,omitempty"`
	DecidedAt        time.Time      `json:"decided_at"`
	CreatedAt        time.Time      `json:"created_at"`
}

type EnqueueDestinationScanInput struct {
	WorkspaceID     string
	LinkID          uint64
	RiskFingerprint string
	PolicyVersion   string
	RequestKind     ScanRequestKind
	IdempotencyKey  string
	CorrelationID   string
	ActorID         string
	MaxAttempts     uint32
}

type EnqueueDestinationScanResult struct {
	Scan    DestinationScan
	Targets []ScanTarget
	Created bool
}
