package domains

import (
	"context"
	"database/sql"
	"errors"
	"strings"
	"time"
)

type WorkspaceDomainViewState string

const (
	WorkspaceDomainLocked    WorkspaceDomainViewState = "locked"
	WorkspaceDomainRequested WorkspaceDomainViewState = "requested"
	WorkspaceDomainActive    WorkspaceDomainViewState = "active"
	WorkspaceDomainGrace     WorkspaceDomainViewState = "grace"
	WorkspaceDomainSuspended WorkspaceDomainViewState = "suspended"
	WorkspaceDomainExpired   WorkspaceDomainViewState = "expired"
	WorkspaceDomainRevoked   WorkspaceDomainViewState = "revoked"
	WorkspaceDomainPartial   WorkspaceDomainViewState = "partial-axis"
)

type WorkspaceEntitlementView struct {
	State                  WorkspaceDomainViewState `json:"state"`
	Source                 EntitlementSourceKind    `json:"source"`
	Status                 EntitlementStatus        `json:"status"`
	DomainLimit            uint32                   `json:"domain_limit"`
	Allocated              uint32                   `json:"allocated"`
	Remaining              uint32                   `json:"remaining"`
	GracePeriod            bool                     `json:"grace_period"`
	Deadline               *time.Time               `json:"deadline,omitempty"`
	MutationAllowed        bool                     `json:"mutation_allowed"`
	ExistingRoutingAllowed bool                     `json:"existing_routing_allowed"`
	SupportTicketID        string                   `json:"support_ticket_id,omitempty"`
	SecurityCategory       string                   `json:"security_category,omitempty"`
}

type DomainView struct {
	ID                    uint64            `json:"id"`
	Hostname              string            `json:"hostname"`
	DisplayHostname       string            `json:"display_hostname"`
	RoutingState          RoutingState      `json:"routing_state"`
	OwnershipStatus       OwnershipStatus    `json:"ownership_status"`
	IngressDNSStatus      IngressDNSStatus   `json:"ingress_dns_status"`
	HTTPSStatus           HTTPSStatus        `json:"https_status"`
	RiskStatus            DomainRiskStatus   `json:"risk_status"`
	OwnershipTokenVersion uint64             `json:"ownership_token_version"`
	OwnershipVerifiedAt   *time.Time         `json:"ownership_verified_at,omitempty"`
	IngressDNSCheckedAt   *time.Time         `json:"ingress_dns_checked_at,omitempty"`
	HTTPSCheckedAt        *time.Time         `json:"https_checked_at,omitempty"`
	RiskCheckedAt         *time.Time         `json:"risk_checked_at,omitempty"`
	RiskPolicyVersion     string             `json:"risk_policy_version,omitempty"`
	SecurityCategory      string             `json:"security_category,omitempty"`
	ReadyForNewLinks      bool               `json:"ready_for_new_links"`
	ReadyForRouting       bool               `json:"ready_for_routing"`
	CreatedAt             time.Time          `json:"created_at"`
	UpdatedAt             time.Time          `json:"updated_at"`
}

type DomainRevalidationView struct {
	Axis          RevalidationAxis `json:"axis"`
	Result        string           `json:"result"`
	PolicyVersion string           `json:"policy_version"`
	CheckedAt     time.Time        `json:"checked_at"`
	NextDueAt     *time.Time       `json:"next_due_at,omitempty"`
}

type AssignedResourceView struct {
	ID     uint64 `json:"id"`
	Code   string `json:"code"`
	Status string `json:"status"`
}

type WorkspaceDomainsView struct {
	Entitlement WorkspaceEntitlementView `json:"entitlement"`
	Items       []DomainView             `json:"items"`
}

type DomainDetailView struct {
	Entitlement     WorkspaceEntitlementView `json:"entitlement"`
	Domain          DomainView               `json:"domain"`
	AssignedLinks   []AssignedResourceView   `json:"assigned_links"`
	Revalidations   []DomainRevalidationView `json:"revalidations"`
}

func (s *MySQLStore) WorkspaceDomainsView(ctx context.Context, workspaceID string, now time.Time) (WorkspaceDomainsView, error) {
	workspaceID = strings.TrimSpace(workspaceID)
	if s == nil || s.db == nil || workspaceID == "" || now.IsZero() {
		return WorkspaceDomainsView{}, ErrInvalidDomainMutation
	}
	entitlement, err := s.ResolveEntitlement(ctx, workspaceID, now.UTC())
	if err != nil {
		return WorkspaceDomainsView{}, err
	}
	allocated, err := loadAllocatedCount(ctx, s.db, workspaceID)
	if err != nil {
		return WorkspaceDomainsView{}, err
	}
	domains, err := loadWorkspaceDomains(ctx, s.db, workspaceID)
	if err != nil {
		return WorkspaceDomainsView{}, err
	}
	view := entitlementView(entitlement, allocated)
	if view.State == WorkspaceDomainActive {
		for _, domain := range domains {
			if !domain.Readiness(entitlement).ReadyForNewLinks {
				view.State = WorkspaceDomainPartial
				break
			}
		}
	}
	items := make([]DomainView, 0, len(domains))
	for _, domain := range domains {
		items = append(items, safeDomainView(domain, entitlement))
	}
	// Distinguish a genuinely absent entitlement (locked) from a historical
	// plan/manual source that has expired. The resolver correctly returns no
	// authority for both; this read-model distinction is display-only.
	if view.State == WorkspaceDomainLocked {
		var count uint32
		if err := s.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM custom_domain_entitlement_sources WHERE workspace_id=? AND status='expired'`, workspaceID).Scan(&count); err != nil {
			return WorkspaceDomainsView{}, err
		}
		if count > 0 {
			view.State = WorkspaceDomainExpired
		}
	}
	return WorkspaceDomainsView{Entitlement: view, Items: items}, nil
}

func (s *MySQLStore) DomainDetailView(ctx context.Context, workspaceID string, domainID uint64, now time.Time) (DomainDetailView, error) {
	workspaceID = strings.TrimSpace(workspaceID)
	if s == nil || s.db == nil || workspaceID == "" || domainID == 0 || now.IsZero() {
		return DomainDetailView{}, ErrInvalidDomainMutation
	}
	entitlement, err := s.ResolveEntitlement(ctx, workspaceID, now.UTC())
	if err != nil {
		return DomainDetailView{}, err
	}
	allocated, err := loadAllocatedCount(ctx, s.db, workspaceID)
	if err != nil {
		return DomainDetailView{}, err
	}
	domain, err := loadDomainByIDDB(ctx, s.db, workspaceID, domainID)
	if err != nil {
		return DomainDetailView{}, err
	}
	links, err := loadAssignedLinks(ctx, s.db, workspaceID, domain.HostnameASCII)
	if err != nil {
		return DomainDetailView{}, err
	}
	revalidations, err := loadSafeRevalidations(ctx, s.db, workspaceID, domainID)
	if err != nil {
		return DomainDetailView{}, err
	}
	view := entitlementView(entitlement, allocated)
	if view.State == WorkspaceDomainActive && !domain.Readiness(entitlement).ReadyForNewLinks {
		view.State = WorkspaceDomainPartial
	}
	return DomainDetailView{
		Entitlement: view,
		Domain: safeDomainView(domain, entitlement),
		AssignedLinks: links,
		Revalidations: revalidations,
	}, nil
}

func entitlementView(entitlement ResolvedEntitlement, allocated uint32) WorkspaceEntitlementView {
	state := WorkspaceDomainLocked
	switch {
	case entitlement.Status == EntitlementRequested:
		state = WorkspaceDomainRequested
	case entitlement.Status == EntitlementSuspended:
		state = WorkspaceDomainSuspended
	case entitlement.Status == EntitlementRevoked:
		state = WorkspaceDomainRevoked
	case entitlement.GracePeriod:
		state = WorkspaceDomainGrace
	case entitlement.Status == EntitlementActive && entitlement.MutationAllowed:
		state = WorkspaceDomainActive
	}
	remaining := uint32(0)
	if entitlement.DomainLimit > allocated {
		remaining = entitlement.DomainLimit - allocated
	}
	securityCategory := ""
	for _, source := range entitlement.ValidSources {
		if strings.TrimSpace(source.SecurityCategory) != "" {
			securityCategory = source.SecurityCategory
			break
		}
	}
	return WorkspaceEntitlementView{
		State: state,
		Source: entitlement.Source,
		Status: entitlement.Status,
		DomainLimit: entitlement.DomainLimit,
		Allocated: allocated,
		Remaining: remaining,
		GracePeriod: entitlement.GracePeriod,
		Deadline: entitlement.GraceUntil,
		MutationAllowed: entitlement.MutationAllowed,
		ExistingRoutingAllowed: entitlement.ExistingRoutingAllowed,
		SupportTicketID: entitlement.SupportTicketID,
		SecurityCategory: securityCategory,
	}
}

func safeDomainView(domain Domain, entitlement ResolvedEntitlement) DomainView {
	readiness := domain.Readiness(entitlement)
	return DomainView{
		ID: domain.ID,
		Hostname: domain.HostnameASCII,
		DisplayHostname: domain.DisplayHostname,
		RoutingState: domain.RoutingState,
		OwnershipStatus: domain.OwnershipStatus,
		IngressDNSStatus: domain.IngressDNSStatus,
		HTTPSStatus: domain.HTTPSStatus,
		RiskStatus: domain.RiskStatus,
		OwnershipTokenVersion: domain.OwnershipTokenVersion,
		OwnershipVerifiedAt: domain.OwnershipVerifiedAt,
		IngressDNSCheckedAt: domain.IngressDNSCheckedAt,
		HTTPSCheckedAt: domain.HTTPSCheckedAt,
		RiskCheckedAt: domain.RiskCheckedAt,
		RiskPolicyVersion: domain.RiskPolicyVersion,
		SecurityCategory: domain.SecurityCategory,
		ReadyForNewLinks: readiness.ReadyForNewLinks,
		ReadyForRouting: readiness.ReadyForRouting,
		CreatedAt: domain.CreatedAt,
		UpdatedAt: domain.UpdatedAt,
	}
}

func loadAllocatedCount(ctx context.Context, db *sql.DB, workspaceID string) (uint32, error) {
	var allocated uint32
	err := db.QueryRowContext(ctx, `SELECT allocated_count FROM custom_domain_usage WHERE workspace_id=?`, workspaceID).Scan(&allocated)
	if errors.Is(err, sql.ErrNoRows) {
		return 0, nil
	}
	return allocated, err
}

func loadWorkspaceDomains(ctx context.Context, db *sql.DB, workspaceID string) ([]Domain, error) {
	rows, err := db.QueryContext(ctx, `
		SELECT id, workspace_id, hostname_ascii, display_hostname, routing_state,
		       ownership_status, ingress_dns_status, https_status, risk_status,
		       ownership_token_version, ownership_secret_issued_at, ownership_verified_at,
		       ingress_dns_checked_at, https_checked_at, risk_checked_at, risk_policy_version,
		       risk_evidence_ref, grace_started_at, grace_until, security_category,
		       created_at, updated_at, removed_at
		FROM custom_domains WHERE workspace_id=? AND removed_at IS NULL ORDER BY id`, workspaceID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	items := []Domain{}
	for rows.Next() {
		domain, err := scanDomain(rows)
		if err != nil {
			return nil, err
		}
		items = append(items, domain)
	}
	return items, rows.Err()
}

func loadDomainByIDDB(ctx context.Context, db *sql.DB, workspaceID string, domainID uint64) (Domain, error) {
	row := db.QueryRowContext(ctx, `
		SELECT id, workspace_id, hostname_ascii, display_hostname, routing_state,
		       ownership_status, ingress_dns_status, https_status, risk_status,
		       ownership_token_version, ownership_secret_issued_at, ownership_verified_at,
		       ingress_dns_checked_at, https_checked_at, risk_checked_at, risk_policy_version,
		       risk_evidence_ref, grace_started_at, grace_until, security_category,
		       created_at, updated_at, removed_at
		FROM custom_domains WHERE workspace_id=? AND id=? AND removed_at IS NULL`, workspaceID, domainID)
	domain, err := scanDomain(row)
	if errors.Is(err, sql.ErrNoRows) {
		return Domain{}, ErrDomainNotFound
	}
	return domain, err
}

func loadAssignedLinks(ctx context.Context, db *sql.DB, workspaceID, hostname string) ([]AssignedResourceView, error) {
	rows, err := db.QueryContext(ctx, `SELECT id, code, status FROM links WHERE workspace_id=? AND domain_kind='custom' AND hostname=? AND deleted_at IS NULL ORDER BY id`, workspaceID, hostname)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	items := []AssignedResourceView{}
	for rows.Next() {
		var item AssignedResourceView
		if err := rows.Scan(&item.ID, &item.Code, &item.Status); err != nil {
			return nil, err
		}
		items = append(items, item)
	}
	return items, rows.Err()
}

func loadSafeRevalidations(ctx context.Context, db *sql.DB, workspaceID string, domainID uint64) ([]DomainRevalidationView, error) {
	rows, err := db.QueryContext(ctx, `
		SELECT axis, result, policy_version, checked_at, next_due_at
		FROM custom_domain_revalidations
		WHERE workspace_id=? AND domain_id=? ORDER BY checked_at DESC, id DESC LIMIT 100`, workspaceID, domainID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	items := []DomainRevalidationView{}
	for rows.Next() {
		var item DomainRevalidationView
		var next sql.NullTime
		if err := rows.Scan(&item.Axis, &item.Result, &item.PolicyVersion, &item.CheckedAt, &next); err != nil {
			return nil, err
		}
		item.CheckedAt = item.CheckedAt.UTC()
		if next.Valid {
			value := next.Time.UTC()
			item.NextDueAt = &value
		}
		items = append(items, item)
	}
	return items, rows.Err()
}
