package domains

import (
	"context"
	"database/sql"
	"strings"
	"time"
)

type RotateOwnershipSecretInput struct {
	WorkspaceID   string
	DomainID      uint64
	ActorID       string
	CorrelationID string
	Reason        string
	Now           time.Time
}

type RotatedOwnershipSecret struct {
	Domain            Domain `json:"domain"`
	OwnershipTXTName  string `json:"ownership_txt_name"`
	OwnershipTXTValue string `json:"ownership_txt_value"`
}

// RotateOwnershipSecret replaces the persisted verifier under a row lock and
// returns the new TXT material exactly once. The plaintext secret is never
// persisted or written to audit metadata. Safety suspension and current
// entitlement are re-checked before any new secret material is generated.
func (s *MySQLStore) RotateOwnershipSecret(ctx context.Context, input RotateOwnershipSecretInput) (RotatedOwnershipSecret, error) {
	input.WorkspaceID = strings.TrimSpace(input.WorkspaceID)
	input.ActorID = strings.TrimSpace(input.ActorID)
	input.CorrelationID = strings.TrimSpace(input.CorrelationID)
	input.Reason = strings.TrimSpace(input.Reason)
	if input.WorkspaceID == "" || input.DomainID == 0 || input.ActorID == "" || input.CorrelationID == "" || input.Reason == "" || input.Now.IsZero() {
		return RotatedOwnershipSecret{}, ErrInvalidDomainMutation
	}
	now := input.Now.UTC()

	tx, err := s.db.BeginTx(ctx, &sql.TxOptions{Isolation: sql.LevelReadCommitted})
	if err != nil {
		return RotatedOwnershipSecret{}, err
	}
	defer tx.Rollback()

	domain, err := loadDomainByIDForUpdate(ctx, tx, input.WorkspaceID, input.DomainID)
	if err != nil {
		return RotatedOwnershipSecret{}, err
	}
	if strings.TrimSpace(domain.SecurityCategory) != "" || domain.OwnershipStatus == OwnershipLost || domain.RoutingState == RoutingRevoked || domain.RoutingState == RoutingRemoved {
		if auditErr := appendDomainAuditTx(ctx, tx, input.WorkspaceID, &domain.ID, nil, input.ActorID, "domain.ownership.rotate", "denied", "domain safety suspension active", input.CorrelationID, map[string]any{
			"code":                    "security_suspended",
			"ownership_token_version": domain.OwnershipTokenVersion,
		}); auditErr != nil {
			return RotatedOwnershipSecret{}, auditErr
		}
		if commitErr := tx.Commit(); commitErr != nil {
			return RotatedOwnershipSecret{}, commitErr
		}
		return RotatedOwnershipSecret{}, ErrDomainSecuritySuspended
	}
	entitlement, err := s.resolveEntitlementTx(ctx, tx, input.WorkspaceID, now)
	if err != nil {
		return RotatedOwnershipSecret{}, err
	}
	if !entitlement.MutationAllowed {
		if auditErr := appendDomainAuditTx(ctx, tx, input.WorkspaceID, &domain.ID, nil, input.ActorID, "domain.ownership.rotate", "denied", entitlement.DecisionReason, input.CorrelationID, map[string]any{
			"code":                    "entitlement_required",
			"ownership_token_version": domain.OwnershipTokenVersion,
		}); auditErr != nil {
			return RotatedOwnershipSecret{}, auditErr
		}
		if commitErr := tx.Commit(); commitErr != nil {
			return RotatedOwnershipSecret{}, commitErr
		}
		return RotatedOwnershipSecret{}, ErrEntitlementRequired
	}

	secret, hash, err := NewOwnershipSecret()
	if err != nil {
		return RotatedOwnershipSecret{}, err
	}
	previousVersion := domain.OwnershipTokenVersion
	nextVersion := previousVersion + 1
	if _, err := tx.ExecContext(ctx, `
		UPDATE custom_domains
		SET ownership_token_version = ?,
		    ownership_secret_hash = ?,
		    ownership_secret_issued_at = ?,
		    ownership_status = 'pending',
		    ownership_verified_at = NULL
		WHERE workspace_id = ? AND id = ?`,
		nextVersion, hash[:], now, input.WorkspaceID, input.DomainID,
	); err != nil {
		return RotatedOwnershipSecret{}, err
	}

	updated, err := loadDomainByID(ctx, tx, input.WorkspaceID, input.DomainID)
	if err != nil {
		return RotatedOwnershipSecret{}, err
	}
	if err := appendDomainAuditTx(ctx, tx, input.WorkspaceID, &updated.ID, nil, input.ActorID, "domain.ownership.rotate", "success", input.Reason, input.CorrelationID, map[string]any{
		"previous_token_version": previousVersion,
		"ownership_token_version": updated.OwnershipTokenVersion,
		"ownership_status":        updated.OwnershipStatus,
		"entitlement_source":      entitlement.Source,
	}); err != nil {
		return RotatedOwnershipSecret{}, err
	}
	if err := tx.Commit(); err != nil {
		return RotatedOwnershipSecret{}, err
	}

	return RotatedOwnershipSecret{
		Domain:            updated,
		OwnershipTXTName:  OwnershipTXTName(updated.HostnameASCII),
		OwnershipTXTValue: OwnershipTXTValue(secret),
	}, nil
}

func loadDomainByIDForUpdate(ctx context.Context, tx *sql.Tx, workspaceID string, domainID uint64) (Domain, error) {
	row := tx.QueryRowContext(ctx, `
		SELECT id, workspace_id, hostname_ascii, display_hostname, routing_state,
		       ownership_status, ingress_dns_status, https_status, risk_status,
		       ownership_token_version, ownership_secret_issued_at, ownership_verified_at,
		       ingress_dns_checked_at, https_checked_at, risk_checked_at, risk_policy_version,
		       risk_evidence_ref, grace_started_at, grace_until, security_category,
		       created_at, updated_at, removed_at
		FROM custom_domains
		WHERE workspace_id = ? AND id = ?
		FOR UPDATE`, workspaceID, domainID)
	return scanDomain(row)
}
