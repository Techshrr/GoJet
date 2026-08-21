package domains

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"strings"
	"time"
)

type CreateDomainInput struct {
	WorkspaceID   string
	ActorID       string
	CorrelationID string
	Reason        string
	Hostname      string
	Now           time.Time
}

type CreatedDomain struct {
	Domain            Domain `json:"domain"`
	OwnershipTXTName  string `json:"ownership_txt_name"`
	OwnershipTXTValue string `json:"ownership_txt_value"`
}

func (s *MySQLStore) CreateDomain(ctx context.Context, input CreateDomainInput) (CreatedDomain, error) {
	input.WorkspaceID = strings.TrimSpace(input.WorkspaceID)
	input.ActorID = strings.TrimSpace(input.ActorID)
	input.CorrelationID = strings.TrimSpace(input.CorrelationID)
	input.Reason = strings.TrimSpace(input.Reason)
	if input.WorkspaceID == "" || input.ActorID == "" || input.CorrelationID == "" || input.Reason == "" || input.Now.IsZero() {
		return CreatedDomain{}, ErrInvalidHostname
	}
	hostname, err := NormalizeASCIIHostname(input.Hostname)
	if err != nil {
		return CreatedDomain{}, err
	}
	now := input.Now.UTC()

	tx, err := s.db.BeginTx(ctx, &sql.TxOptions{Isolation: sql.LevelReadCommitted})
	if err != nil {
		return CreatedDomain{}, err
	}
	defer tx.Rollback()

	entitlement, err := s.resolveEntitlementTx(ctx, tx, input.WorkspaceID, now)
	if err != nil {
		return CreatedDomain{}, err
	}
	if !entitlement.MutationAllowed {
		if auditErr := appendDomainAuditTx(ctx, tx, input.WorkspaceID, nil, nil, input.ActorID, "domain.create", "denied", entitlement.DecisionReason, input.CorrelationID, map[string]any{
			"code": "entitlement_required",
		}); auditErr != nil {
			return CreatedDomain{}, auditErr
		}
		if err := tx.Commit(); err != nil {
			return CreatedDomain{}, err
		}
		return CreatedDomain{}, ErrEntitlementRequired
	}

	if _, err := tx.ExecContext(ctx, `
		INSERT INTO custom_domain_usage (workspace_id, allocated_count, version)
		VALUES (?, 0, 1)
		ON DUPLICATE KEY UPDATE workspace_id = VALUES(workspace_id)`, input.WorkspaceID); err != nil {
		return CreatedDomain{}, err
	}

	var allocated uint32
	if err := tx.QueryRowContext(ctx, `
		SELECT allocated_count
		FROM custom_domain_usage
		WHERE workspace_id = ?
		FOR UPDATE`, input.WorkspaceID).Scan(&allocated); err != nil {
		return CreatedDomain{}, err
	}
	if allocated >= entitlement.DomainLimit {
		if auditErr := appendDomainAuditTx(ctx, tx, input.WorkspaceID, nil, nil, input.ActorID, "domain.create", "denied", "domain limit reached", input.CorrelationID, map[string]any{
			"code":            "domain_limit_reached",
			"domain_limit":    entitlement.DomainLimit,
			"allocated_count": allocated,
		}); auditErr != nil {
			return CreatedDomain{}, auditErr
		}
		if err := tx.Commit(); err != nil {
			return CreatedDomain{}, err
		}
		return CreatedDomain{}, ErrDomainLimitReached
	}

	secret, hash, err := NewOwnershipSecret()
	if err != nil {
		return CreatedDomain{}, err
	}
	result, err := tx.ExecContext(ctx, `
		INSERT INTO custom_domains (
			workspace_id, hostname_ascii, display_hostname, routing_state,
			ownership_status, ingress_dns_status, https_status, risk_status,
			ownership_token_version, ownership_secret_hash, ownership_secret_issued_at
		) VALUES (?, ?, ?, 'pending', 'pending', 'pending', 'pending', 'missing', 1, ?, ?)`,
		input.WorkspaceID, hostname, hostname, hash[:], now,
	)
	if err != nil {
		if isDuplicateKey(err) {
			if auditErr := appendDomainAuditTx(ctx, tx, input.WorkspaceID, nil, nil, input.ActorID, "domain.create", "conflict", "hostname unavailable", input.CorrelationID, map[string]any{
				"code": "hostname_unavailable",
			}); auditErr != nil {
				return CreatedDomain{}, auditErr
			}
			if commitErr := tx.Commit(); commitErr != nil {
				return CreatedDomain{}, commitErr
			}
			return CreatedDomain{}, ErrHostnameConflict
		}
		return CreatedDomain{}, err
	}
	insertID, err := result.LastInsertId()
	if err != nil {
		return CreatedDomain{}, err
	}
	domainID := uint64(insertID)

	if _, err := tx.ExecContext(ctx, `
		UPDATE custom_domain_usage
		SET allocated_count = allocated_count + 1, version = version + 1
		WHERE workspace_id = ?`, input.WorkspaceID); err != nil {
		return CreatedDomain{}, err
	}

	domain, err := loadDomainByID(ctx, tx, input.WorkspaceID, domainID)
	if err != nil {
		return CreatedDomain{}, err
	}
	if err := appendDomainAuditTx(ctx, tx, input.WorkspaceID, &domain.ID, nil, input.ActorID, "domain.create", "success", input.Reason, input.CorrelationID, map[string]any{
		"hostname_ascii":          domain.HostnameASCII,
		"ownership_token_version": domain.OwnershipTokenVersion,
		"entitlement_source":      entitlement.Source,
		"domain_limit":            entitlement.DomainLimit,
		"allocated_after":         allocated + 1,
	}); err != nil {
		return CreatedDomain{}, err
	}
	if err := tx.Commit(); err != nil {
		return CreatedDomain{}, err
	}

	return CreatedDomain{
		Domain:            domain,
		OwnershipTXTName:  OwnershipTXTName(domain.HostnameASCII),
		OwnershipTXTValue: OwnershipTXTValue(secret),
	}, nil
}

func (s *MySQLStore) GetDomain(ctx context.Context, workspaceID string, domainID uint64) (Domain, error) {
	workspaceID = strings.TrimSpace(workspaceID)
	if workspaceID == "" || domainID == 0 {
		return Domain{}, ErrDomainNotFound
	}
	return loadDomainByID(ctx, s.db, workspaceID, domainID)
}

func (s *MySQLStore) ResolveDomainReadiness(ctx context.Context, workspaceID string, domainID uint64, now time.Time) (Domain, ResolvedEntitlement, DomainReadiness, error) {
	domain, err := s.GetDomain(ctx, workspaceID, domainID)
	if err != nil {
		return Domain{}, ResolvedEntitlement{}, DomainReadiness{}, err
	}
	entitlement, err := s.ResolveEntitlement(ctx, workspaceID, now)
	if err != nil {
		return Domain{}, ResolvedEntitlement{}, DomainReadiness{}, err
	}
	return domain, entitlement, domain.Readiness(entitlement), nil
}

func (s *MySQLStore) resolveEntitlementTx(ctx context.Context, tx *sql.Tx, workspaceID string, now time.Time) (ResolvedEntitlement, error) {
	sources, err := s.loadEntitlementSources(ctx, tx, workspaceID)
	if err != nil {
		return ResolvedEntitlement{}, err
	}
	request, err := s.loadLatestRequest(ctx, tx, workspaceID)
	if err != nil {
		return ResolvedEntitlement{}, err
	}
	return ResolveEntitlement(now, sources, request)
}

func loadDomainByID(ctx context.Context, q queryer, workspaceID string, domainID uint64) (Domain, error) {
	row := q.QueryRowContext(ctx, `
		SELECT id, workspace_id, hostname_ascii, display_hostname, routing_state,
		       ownership_status, ingress_dns_status, https_status, risk_status,
		       ownership_token_version, ownership_secret_issued_at, ownership_verified_at,
		       ingress_dns_checked_at, https_checked_at, risk_checked_at, risk_policy_version,
		       risk_evidence_ref, grace_started_at, grace_until, security_category,
		       created_at, updated_at, removed_at
		FROM custom_domains
		WHERE workspace_id = ? AND id = ?`, workspaceID, domainID)
	return scanDomain(row)
}

func scanDomain(row rowScanner) (Domain, error) {
	var domain Domain
	var ownershipVerifiedAt, ingressCheckedAt, httpsCheckedAt, riskCheckedAt sql.NullTime
	var graceStartedAt, graceUntil, removedAt sql.NullTime
	var riskPolicyVersion, riskEvidenceRef, securityCategory sql.NullString
	if err := row.Scan(
		&domain.ID, &domain.WorkspaceID, &domain.HostnameASCII, &domain.DisplayHostname, &domain.RoutingState,
		&domain.OwnershipStatus, &domain.IngressDNSStatus, &domain.HTTPSStatus, &domain.RiskStatus,
		&domain.OwnershipTokenVersion, &domain.OwnershipSecretIssuedAt, &ownershipVerifiedAt,
		&ingressCheckedAt, &httpsCheckedAt, &riskCheckedAt, &riskPolicyVersion,
		&riskEvidenceRef, &graceStartedAt, &graceUntil, &securityCategory,
		&domain.CreatedAt, &domain.UpdatedAt, &removedAt,
	); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return Domain{}, ErrDomainNotFound
		}
		return Domain{}, err
	}
	domain.OwnershipSecretIssuedAt = domain.OwnershipSecretIssuedAt.UTC()
	domain.CreatedAt = domain.CreatedAt.UTC()
	domain.UpdatedAt = domain.UpdatedAt.UTC()
	domain.OwnershipVerifiedAt = nullTimePtr(ownershipVerifiedAt)
	domain.IngressDNSCheckedAt = nullTimePtr(ingressCheckedAt)
	domain.HTTPSCheckedAt = nullTimePtr(httpsCheckedAt)
	domain.RiskCheckedAt = nullTimePtr(riskCheckedAt)
	domain.GraceStartedAt = nullTimePtr(graceStartedAt)
	domain.GraceUntil = nullTimePtr(graceUntil)
	domain.RemovedAt = nullTimePtr(removedAt)
	if riskPolicyVersion.Valid {
		domain.RiskPolicyVersion = riskPolicyVersion.String
	}
	if riskEvidenceRef.Valid {
		domain.RiskEvidenceRef = riskEvidenceRef.String
	}
	if securityCategory.Valid {
		domain.SecurityCategory = securityCategory.String
	}
	return domain, nil
}

func nullTimePtr(value sql.NullTime) *time.Time {
	if !value.Valid {
		return nil
	}
	utc := value.Time.UTC()
	return &utc
}

func (s *MySQLStore) Usage(ctx context.Context, workspaceID string) (allocated uint32, version uint64, err error) {
	err = s.db.QueryRowContext(ctx, `
		SELECT allocated_count, version
		FROM custom_domain_usage
		WHERE workspace_id = ?`, strings.TrimSpace(workspaceID)).Scan(&allocated, &version)
	if errors.Is(err, sql.ErrNoRows) {
		return 0, 0, nil
	}
	if err != nil {
		return 0, 0, fmt.Errorf("read domain usage: %w", err)
	}
	return allocated, version, nil
}
