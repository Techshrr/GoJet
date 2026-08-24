package billing

import (
	"errors"
	"regexp"
	"strings"
	"time"
)

var (
	ErrInvalidMoney       = errors.New("invalid money")
	ErrInvalidProvider    = errors.New("invalid payment provider")
	ErrInvalidEntitlement = errors.New("invalid entitlement grant")
)

type Provider string

const (
	ProviderAlipay Provider = "alipay"
	ProviderWeChat Provider = "wechat"
	ProviderEpay   Provider = "epay"
	ProviderPayPal Provider = "paypal"
	ProviderStripe Provider = "stripe"
	ProviderCrypto Provider = "crypto"
)

var frozenProviders = []Provider{
	ProviderAlipay, ProviderWeChat, ProviderEpay, ProviderPayPal, ProviderStripe, ProviderCrypto,
}

func FrozenProviders() []Provider {
	out := make([]Provider, len(frozenProviders))
	copy(out, frozenProviders)
	return out
}

func IsFrozenProvider(provider Provider) bool {
	for _, item := range frozenProviders {
		if provider == item {
			return true
		}
	}
	return false
}

type Money struct {
	Currency    string `json:"currency"`
	AmountMinor int64  `json:"amount_minor"`
}

var isoCurrency = regexp.MustCompile(`^[A-Z]{3}$`)

func (m Money) Validate(allowZero bool) error {
	if !isoCurrency.MatchString(strings.TrimSpace(m.Currency)) || m.AmountMinor < 0 || (!allowZero && m.AmountMinor == 0) {
		return ErrInvalidMoney
	}
	return nil
}

type PlanStatus string

const (
	PlanDraft    PlanStatus = "draft"
	PlanActive   PlanStatus = "active"
	PlanArchived PlanStatus = "archived"
)

type SubscriptionStatus string

const (
	SubscriptionPending  SubscriptionStatus = "pending"
	SubscriptionActive   SubscriptionStatus = "active"
	SubscriptionGrace    SubscriptionStatus = "grace"
	SubscriptionOverdue  SubscriptionStatus = "overdue"
	SubscriptionCanceled SubscriptionStatus = "canceled"
	SubscriptionExpired  SubscriptionStatus = "expired"
)

type OrderStatus string

const (
	OrderPending    OrderStatus = "pending"
	OrderProcessing OrderStatus = "processing"
	OrderPaid       OrderStatus = "paid"
	OrderFailed     OrderStatus = "failed"
	OrderCanceled   OrderStatus = "canceled"
	OrderRefunded   OrderStatus = "refunded"
)

type TransactionStatus string

const (
	TransactionPending  TransactionStatus = "pending"
	TransactionPaid     TransactionStatus = "paid"
	TransactionFailed   TransactionStatus = "failed"
	TransactionRefunded TransactionStatus = "refunded"
)

type CallbackEventStatus string

const (
	CallbackAccepted  CallbackEventStatus = "accepted"
	CallbackDuplicate CallbackEventStatus = "duplicate"
	CallbackInvalid   CallbackEventStatus = "invalid"
	CallbackIgnored   CallbackEventStatus = "ignored"
	CallbackProcessed CallbackEventStatus = "processed"
)

type EntitlementSourceType string

const (
	SourceHardDeny  EntitlementSourceType = "hard_deny"
	SourceManual    EntitlementSourceType = "manual"
	SourceInherited EntitlementSourceType = "inherited"
	SourceBilling   EntitlementSourceType = "billing"
	SourceBaseline  EntitlementSourceType = "baseline"
)

type EntitlementGrant struct {
	ID          uint64                `json:"id"`
	WorkspaceID string                `json:"workspace_id"`
	Capability  string                `json:"capability"`
	SourceType  EntitlementSourceType `json:"source_type"`
	SourceID    string                `json:"source_id"`
	LimitValue  uint64                `json:"limit_value"`
	StartsAt    time.Time             `json:"starts_at"`
	EndsAt      *time.Time            `json:"ends_at,omitempty"`
	RevokedAt   *time.Time            `json:"revoked_at,omitempty"`
}

func (g EntitlementGrant) Validate() error {
	if strings.TrimSpace(g.WorkspaceID) == "" || strings.TrimSpace(g.Capability) == "" || strings.TrimSpace(g.SourceID) == "" || g.StartsAt.IsZero() {
		return ErrInvalidEntitlement
	}
	switch g.SourceType {
	case SourceHardDeny, SourceManual, SourceInherited, SourceBilling, SourceBaseline:
	default:
		return ErrInvalidEntitlement
	}
	if g.SourceType != SourceHardDeny && g.LimitValue == 0 {
		return ErrInvalidEntitlement
	}
	if g.EndsAt != nil && !g.EndsAt.After(g.StartsAt) {
		return ErrInvalidEntitlement
	}
	if g.RevokedAt != nil && g.RevokedAt.Before(g.StartsAt) {
		return ErrInvalidEntitlement
	}
	return nil
}
