package domains

import (
	"context"
	"database/sql"
	"strings"
	"time"
)

type DomainSecurityCategory string

const (
	DomainSecurityAbuse         DomainSecurityCategory = "abuse"
	DomainSecurityFraud         DomainSecurityCategory = "fraud"
	DomainSecurityGeneral       DomainSecurityCategory = "security"
	DomainSecurityOwnershipLoss DomainSecurityCategory = "ownership_loss"
)

type DomainSecuritySuspensionInput struct {
	WorkspaceID   string
	DomainID      uint64
	ActorID       string
	Category      DomainSecurityCategory
	Reason        string
	CorrelationID string
	Now           time.Time
}

type OwnershipLossInput struct {
	WorkspaceID   string
	DomainID      uint64
	ActorID       string
	Reason        string
	CorrelationID string
	Now           time.Time
}

func validExternalSecurityCategory(category DomainSecurityCategory) bool {
	switch category {
	case DomainSecurityAbuse, DomainSecurityFraud, DomainSecurityGeneral:
		return true
	default:
		return false
	}
}

// ApplyDomainSecuritySuspension is the domain-level abuse/fraud/security kill
// switch. It has zero grace, preserves the trust-axis observations for forensic
// inspection, and exposes only the allowlisted safe category on the Domain.
func (s *MySQLStore) ApplyDomainSecuritySuspension(ctx context.Context, input DomainSecuritySuspensionInput) (Domain, error) {
	input.WorkspaceID = strings.TrimSpace(input.WorkspaceID)
	input.ActorID = strings.TrimSpace(input.ActorID)
	input.Reason = strings.TrimSpace(input.Reason)
	input.CorrelationID = strings.TrimSpace(input.CorrelationID)
	if s == nil || s.db == nil || input.WorkspaceID == "" || input.DomainID == 0 || input.ActorID == "" || input.Reason == "" || input.CorrelationID == "" || input.Now.IsZero() || !validExternalSecurityCategory(input.Category) {
		return Domain{}, ErrInvalidDomainMutation
	}

	tx, err := s.db.BeginTx(ctx, &sql.TxOptions{Isolation: sql.LevelReadCommitted})
	if err != nil {
		return Domain{}, err
	}
	defer tx.Rollback()
	domain, err := loadDomainByIDForUpdate(ctx, tx, input.WorkspaceID, input.DomainID)
	if err != nil {
		return Domain{}, err
	}
	category := string(input.Category)
	if domain.RoutingState == RoutingSuspended && domain.SecurityCategory == category {
		if err := tx.Commit(); err != nil {
			return Domain{}, err
		}
		return domain, nil
	}
	if strings.TrimSpace(domain.SecurityCategory) != "" && domain.SecurityCategory != category {
		return Domain{}, ErrDomainSecuritySuspended
	}
	if domain.RoutingState == RoutingRemoved || domain.RoutingState == RoutingRevoked {
		return Domain{}, ErrDomainSecuritySuspended
	}
	previousRouting := domain.RoutingState
	if _, err := tx.ExecContext(ctx, `
		UPDATE custom_domains
		SET routing_state='suspended', security_category=?, grace_started_at=NULL, grace_until=NULL
		WHERE workspace_id=? AND id=?`, category, input.WorkspaceID, input.DomainID); err != nil {
		return Domain{}, err
	}
	updated, err := loadDomainByID(ctx, tx, input.WorkspaceID, input.DomainID)
	if err != nil {
		return Domain{}, err
	}
	if err := appendDomainAuditTx(ctx, tx, input.WorkspaceID, &updated.ID, nil, input.ActorID, "domain.security.suspend", "success", input.Reason, input.CorrelationID, map[string]any{
		"code":             "security_suspended",
		"category":         category,
		"previous_routing": previousRouting,
		"routing_state":    updated.RoutingState,
		"grace":            false,
	}); err != nil {
		return Domain{}, err
	}
	if err := tx.Commit(); err != nil {
		return Domain{}, err
	}
	return updated, nil
}

// ApplyOwnershipLoss records an authoritative loss of control. Ownership loss
// is a safety event rather than a billing downgrade: routing is suspended in the
// same transaction, grace is cleared, and later TXT success cannot clear this
// persisted suspension by itself.
func (s *MySQLStore) ApplyOwnershipLoss(ctx context.Context, input OwnershipLossInput) (Domain, error) {
	input.WorkspaceID = strings.TrimSpace(input.WorkspaceID)
	input.ActorID = strings.TrimSpace(input.ActorID)
	input.Reason = strings.TrimSpace(input.Reason)
	input.CorrelationID = strings.TrimSpace(input.CorrelationID)
	if s == nil || s.db == nil || input.WorkspaceID == "" || input.DomainID == 0 || input.ActorID == "" || input.Reason == "" || input.CorrelationID == "" || input.Now.IsZero() {
		return Domain{}, ErrInvalidDomainMutation
	}

	tx, err := s.db.BeginTx(ctx, &sql.TxOptions{Isolation: sql.LevelReadCommitted})
	if err != nil {
		return Domain{}, err
	}
	defer tx.Rollback()
	domain, err := loadDomainByIDForUpdate(ctx, tx, input.WorkspaceID, input.DomainID)
	if err != nil {
		return Domain{}, err
	}
	if domain.OwnershipStatus == OwnershipLost && domain.RoutingState == RoutingSuspended && strings.TrimSpace(domain.SecurityCategory) != "" {
		if err := tx.Commit(); err != nil {
			return Domain{}, err
		}
		return domain, nil
	}
	if domain.RoutingState == RoutingRemoved || domain.RoutingState == RoutingRevoked {
		return Domain{}, ErrDomainSecuritySuspended
	}
	category := strings.TrimSpace(domain.SecurityCategory)
	if category == "" {
		category = string(DomainSecurityOwnershipLoss)
	}
	previousOwnership := domain.OwnershipStatus
	previousRouting := domain.RoutingState
	if _, err := tx.ExecContext(ctx, `
		UPDATE custom_domains
		SET ownership_status='lost', ownership_verified_at=NULL,
		    routing_state='suspended', security_category=?,
		    grace_started_at=NULL, grace_until=NULL
		WHERE workspace_id=? AND id=?`, category, input.WorkspaceID, input.DomainID); err != nil {
		return Domain{}, err
	}
	updated, err := loadDomainByID(ctx, tx, input.WorkspaceID, input.DomainID)
	if err != nil {
		return Domain{}, err
	}
	if err := appendDomainAuditTx(ctx, tx, input.WorkspaceID, &updated.ID, nil, input.ActorID, "domain.ownership.loss", "success", input.Reason, input.CorrelationID, map[string]any{
		"code":               "ownership_lost",
		"category":           category,
		"previous_ownership": previousOwnership,
		"ownership_status":   updated.OwnershipStatus,
		"previous_routing":   previousRouting,
		"routing_state":      updated.RoutingState,
		"grace":              false,
	}); err != nil {
		return Domain{}, err
	}
	if err := tx.Commit(); err != nil {
		return Domain{}, err
	}
	return updated, nil
}
