package admin

import (
	"context"
	"database/sql"
	"fmt"
	"strings"
	"time"

	"github.com/Techshrr/GoJet/internal/domains"
)

const (
	domainEntitlementControlCategory = "p17_admin_entitlement_control"
	domainEntitlementScopeWorkspace  = "workspace_entitlement"
	domainEntitlementRevokeConfirm   = "REVOKE"
	domainEntitlementDisableRouting  = "disable_existing_routing"
)

type DomainEntitlementRequestView struct {
	SupportTicketID      string    `json:"support_ticket_id"`
	RequestedDomainLimit *uint32   `json:"requested_domain_limit,omitempty"`
	Status               string    `json:"status"`
	DecisionReason       string    `json:"decision_reason,omitempty"`
	UserVisibleCategory  string    `json:"user_visible_category,omitempty"`
	SubmittedAt          time.Time `json:"submitted_at"`
}

type DomainEntitlementControlView struct {
	State       string    `json:"state"`
	Reason      string    `json:"reason"`
	ActorID     string    `json:"actor_id"`
	DecisionID  string    `json:"decision_id"`
	EffectiveAt time.Time `json:"effective_at"`
	UpdatedAt   time.Time `json:"updated_at"`
}

type DomainEntitlementDecisionRecord struct {
	ID                   string    `json:"id"`
	Action               string    `json:"action"`
	ActorID              string    `json:"actor_id"`
	RequestCorrelationID string    `json:"request_correlation_id"`
	Reason               string    `json:"reason"`
	SupportTicketID      string    `json:"support_ticket_id,omitempty"`
	AffectedRoutes       uint32    `json:"affected_routes"`
	CreatedAt            time.Time `json:"created_at"`
}

type DomainEntitlementView struct {
	WorkspaceID string                            `json:"workspace_id"`
	Entitlement domains.ResolvedEntitlement       `json:"entitlement"`
	Request     *DomainEntitlementRequestView     `json:"request,omitempty"`
	Control     *DomainEntitlementControlView     `json:"control,omitempty"`
	Decisions   []DomainEntitlementDecisionRecord `json:"decisions,omitempty"`
}

type DomainEntitlementDecisionInput struct {
	Action                           string
	DomainLimit                      *uint32
	StartsAt                         *time.Time
	ExpiresAt                        *time.Time
	SupportTicketID                  string
	UserVisibleCategory              string
	EffectiveAt                      *time.Time
	Scope                            string
	Confirmation                     string
	ExistingLinkImpact               string
	CurrentSecurityOwnershipEvidence string
}

type DomainEntitlementDecisionResult struct {
	DecisionID     string                      `json:"decision_id"`
	WorkspaceID    string                      `json:"workspace_id"`
	Action         string                      `json:"action"`
	Entitlement    domains.ResolvedEntitlement `json:"entitlement"`
	AffectedRoutes uint32                      `json:"affected_routes"`
}

func VerifyDomainEntitlementSchema(ctx context.Context, db *sql.DB) error {
	if db == nil {
		return ErrInvalid
	}
	var tables int
	if err := db.QueryRowContext(ctx, `
		SELECT COUNT(*)
		FROM information_schema.tables
		WHERE table_schema = DATABASE()
		  AND table_name IN (
		    'admin_domain_entitlement_controls',
		    'admin_domain_entitlement_decisions'
		  )`).Scan(&tables); err != nil {
		return err
	}
	if tables != 2 {
		return fmt.Errorf("p17 domain-entitlement schema incomplete: %w", ErrInvalid)
	}
	var triggers int
	if err := db.QueryRowContext(ctx, `
		SELECT COUNT(*)
		FROM information_schema.triggers
		WHERE trigger_schema = DATABASE()
		  AND trigger_name IN (
		    'trg_admin_domain_entitlement_decisions_no_update',
		    'trg_admin_domain_entitlement_decisions_no_delete'
		  )`).Scan(&triggers); err != nil {
		return err
	}
	if triggers != 2 {
		return fmt.Errorf("p17 domain-entitlement trigger authority incomplete: %w", ErrInvalid)
	}
	return nil
}

func (s *Service) ListDomainEntitlements(ctx context.Context, p Principal, now time.Time) ([]DomainEntitlementView, error) {
	if err := s.Require(p, PermissionDomainsEntitlementsManage); err != nil {
		return nil, err
	}
	rows, err := s.db.QueryContext(ctx, `
		SELECT workspace_id
		FROM (
			SELECT workspace_id FROM custom_domain_entitlement_sources
			UNION
			SELECT workspace_id FROM custom_domain_entitlement_requests
			UNION
			SELECT workspace_id FROM admin_domain_entitlement_controls
		) AS entitlement_workspaces
		ORDER BY workspace_id`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	ids := []string{}
	for rows.Next() {
		var id string
		if err := rows.Scan(&id); err != nil {
			return nil, err
		}
		ids = append(ids, id)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	items := make([]DomainEntitlementView, 0, len(ids))
	for _, id := range ids {
		item, err := s.domainEntitlementView(ctx, id, now, false)
		if err != nil {
			return nil, err
		}
		items = append(items, item)
	}
	return items, nil
}

func (s *Service) GetDomainEntitlement(ctx context.Context, p Principal, workspaceID string, now time.Time) (DomainEntitlementView, error) {
	if err := s.Require(p, PermissionDomainsEntitlementsManage); err != nil {
		return DomainEntitlementView{}, err
	}
	return s.domainEntitlementView(ctx, workspaceID, now, true)
}

func (s *Service) domainEntitlementView(ctx context.Context, workspaceID string, now time.Time, includeDecisions bool) (DomainEntitlementView, error) {
	workspaceID = strings.TrimSpace(workspaceID)
	if !validID(workspaceID, 64) {
		return DomainEntitlementView{}, ErrInvalid
	}
	var exists int
	if err := s.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM workspaces WHERE id=?`, workspaceID).Scan(&exists); err != nil {
		return DomainEntitlementView{}, err
	}
	if exists != 1 {
		return DomainEntitlementView{}, ErrNotFound
	}
	entitlement, err := domains.NewMySQLStore(s.db).ResolveEntitlement(ctx, workspaceID, now.UTC())
	if err != nil {
		return DomainEntitlementView{}, err
	}
	request, err := loadDomainEntitlementRequest(ctx, s.db, workspaceID)
	if err != nil {
		return DomainEntitlementView{}, err
	}
	control, err := loadDomainEntitlementControl(ctx, s.db, workspaceID)
	if err != nil {
		return DomainEntitlementView{}, err
	}
	item := DomainEntitlementView{WorkspaceID: workspaceID, Entitlement: entitlement, Request: request, Control: control}
	if includeDecisions {
		item.Decisions, err = loadDomainEntitlementDecisions(ctx, s.db, workspaceID, 100)
		if err != nil {
			return DomainEntitlementView{}, err
		}
	}
	return item, nil
}
