package admin

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"strings"
	"time"

	"github.com/Techshrr/GoJet/internal/domains"
)

func resolveDomainEntitlementTx(ctx context.Context, tx *sql.Tx, workspaceID string, now time.Time) (domains.ResolvedEntitlement, error) {
	rows, err := tx.QueryContext(ctx, `
		SELECT id, workspace_id, source, source_key, status, domain_limit, starts_at, expires_at,
		       degraded_at, grace_until, granted_by, support_ticket_id, decision_reason, security_category
		FROM custom_domain_entitlement_sources
		WHERE workspace_id=?
		ORDER BY id`, workspaceID)
	if err != nil {
		return domains.ResolvedEntitlement{}, err
	}
	defer rows.Close()
	sources := []domains.EntitlementSource{}
	for rows.Next() {
		var source domains.EntitlementSource
		var expires, degraded, grace sql.NullTime
		var granted, ticket, reason, category sql.NullString
		if err := rows.Scan(&source.ID, &source.WorkspaceID, &source.Source, &source.SourceKey, &source.Status, &source.DomainLimit, &source.StartsAt, &expires, &degraded, &grace, &granted, &ticket, &reason, &category); err != nil {
			return domains.ResolvedEntitlement{}, err
		}
		if expires.Valid {
			t := expires.Time.UTC()
			source.ExpiresAt = &t
		}
		if degraded.Valid {
			t := degraded.Time.UTC()
			source.DegradedAt = &t
		}
		if grace.Valid {
			t := grace.Time.UTC()
			source.GraceUntil = &t
		}
		if granted.Valid {
			source.GrantedBy = granted.String
		}
		if ticket.Valid {
			source.SupportTicketID = ticket.String
		}
		if reason.Valid {
			source.DecisionReason = reason.String
		}
		if category.Valid {
			source.SecurityCategory = category.String
		}
		sources = append(sources, source)
	}
	if err := rows.Err(); err != nil {
		return domains.ResolvedEntitlement{}, err
	}
	var request *domains.AccessRequest
	var req domains.AccessRequest
	err = tx.QueryRowContext(ctx, `
		SELECT workspace_id,support_ticket_id,submitted_at
		FROM custom_domain_entitlement_requests
		WHERE workspace_id=? AND status='requested'
		ORDER BY submitted_at DESC,id DESC LIMIT 1`, workspaceID).Scan(&req.WorkspaceID, &req.SupportTicketID, &req.SubmittedAt)
	if err == nil {
		req.SubmittedAt = req.SubmittedAt.UTC()
		request = &req
	} else if !errors.Is(err, sql.ErrNoRows) {
		return domains.ResolvedEntitlement{}, err
	}
	base, err := domains.ResolveEntitlement(now.UTC(), sources, request)
	if err != nil {
		return domains.ResolvedEntitlement{}, err
	}
	return domains.ApplyAdminEntitlementControl(ctx, tx, workspaceID, now.UTC(), base)
}

func approveDomainEntitlementTx(ctx context.Context, tx *sql.Tx, workspaceID, actorID, decisionID string, input DomainEntitlementDecisionInput, authority MutationAuthority, now time.Time) (uint64, string, error) {
	ticket := strings.TrimSpace(input.SupportTicketID)
	var requestID uint64
	if err := tx.QueryRowContext(ctx, `SELECT id FROM custom_domain_entitlement_requests WHERE workspace_id=? AND support_ticket_id=? AND status='requested' FOR UPDATE`, workspaceID, ticket).Scan(&requestID); errors.Is(err, sql.ErrNoRows) {
		return 0, "", ErrConflict
	} else if err != nil {
		return 0, "", err
	}
	sourceKey := "p17:" + decisionID
	result, err := tx.ExecContext(ctx, `
		INSERT INTO custom_domain_entitlement_sources (
			workspace_id,source,source_key,status,domain_limit,starts_at,expires_at,
			granted_by,support_ticket_id,decision_reason
		) VALUES (?,'manual_approval',?,'active',?,?,?,?,?,?)`,
		workspaceID, sourceKey, *input.DomainLimit, input.StartsAt.UTC(), input.ExpiresAt.UTC(), actorID, ticket, authority.Reason)
	if err != nil {
		return 0, "", mapDuplicate(err)
	}
	id, err := result.LastInsertId()
	if err != nil {
		return 0, "", err
	}
	sourceID := uint64(id)
	if _, err := tx.ExecContext(ctx, `UPDATE custom_domain_entitlement_requests SET status='approved',decision_reason=? WHERE id=?`, authority.Reason, requestID); err != nil {
		return 0, "", err
	}
	if err := appendDomainEntitlementAuditTx(ctx, tx, workspaceID, &sourceID, actorID, "domain.entitlement.manual_approval.create", authority.Reason, authority.CorrelationID, map[string]any{
		"support_ticket_id": ticket,
		"domain_limit":      *input.DomainLimit,
	}); err != nil {
		return 0, "", err
	}
	return sourceID, ticket, nil
}

func denyDomainEntitlementTx(ctx context.Context, tx *sql.Tx, workspaceID, actorID, category string, authority MutationAuthority) (string, error) {
	var requestID uint64
	var ticket string
	if err := tx.QueryRowContext(ctx, `
		SELECT id,support_ticket_id FROM custom_domain_entitlement_requests
		WHERE workspace_id=? AND status='requested'
		ORDER BY submitted_at DESC,id DESC LIMIT 1 FOR UPDATE`, workspaceID).Scan(&requestID, &ticket); errors.Is(err, sql.ErrNoRows) {
		return "", ErrConflict
	} else if err != nil {
		return "", err
	}
	if _, err := tx.ExecContext(ctx, `UPDATE custom_domain_entitlement_requests SET status='denied',decision_reason=? WHERE id=?`, authority.Reason, requestID); err != nil {
		return "", err
	}
	if err := appendDomainEntitlementAuditTx(ctx, tx, workspaceID, nil, actorID, "domain.entitlement.request.deny", authority.Reason, authority.CorrelationID, map[string]any{
		"support_ticket_id":     ticket,
		"user_visible_category": strings.TrimSpace(category),
	}); err != nil {
		return "", err
	}
	return ticket, nil
}

func setDomainEntitlementControlTx(ctx context.Context, tx *sql.Tx, workspaceID, state, actorID, decisionID, reason string, effectiveAt time.Time) (uint32, error) {
	var currentState string
	err := tx.QueryRowContext(ctx, `SELECT state FROM admin_domain_entitlement_controls WHERE workspace_id=? FOR UPDATE`, workspaceID).Scan(&currentState)
	if err != nil && !errors.Is(err, sql.ErrNoRows) {
		return 0, err
	}
	if err == nil && currentState == string(domains.EntitlementRevoked) && state == string(domains.EntitlementSuspended) {
		return 0, ErrConflict
	}
	if errors.Is(err, sql.ErrNoRows) {
		if _, err := tx.ExecContext(ctx, `
			INSERT INTO admin_domain_entitlement_controls(workspace_id,state,reason,actor_id,decision_id,effective_at)
			VALUES (?,?,?,?,?,?)`, workspaceID, state, reason, actorID, decisionID, effectiveAt.UTC()); err != nil {
			return 0, err
		}
	} else {
		if _, err := tx.ExecContext(ctx, `
			UPDATE admin_domain_entitlement_controls
			SET state=?,reason=?,actor_id=?,decision_id=?,effective_at=?
			WHERE workspace_id=?`, state, reason, actorID, decisionID, effectiveAt.UTC(), workspaceID); err != nil {
			return 0, err
		}
	}
	// P17 never rewrites P06/P13 entitlement source rows. The administrator
	// control is a separate conjunctive overlay consumed by the inherited
	// resolver entry points. Existing routing is still disabled immediately.
	routingResult, err := tx.ExecContext(ctx, `
		UPDATE custom_domains
		SET routing_state='suspended'
		WHERE workspace_id=? AND routing_state IN ('pending','enabled')`, workspaceID)
	if err != nil {
		return 0, err
	}
	affected, err := routingResult.RowsAffected()
	if err != nil {
		return 0, err
	}
	if affected > int64(^uint32(0)) {
		return 0, ErrInvalid
	}
	return uint32(affected), nil
}

func restoreDomainEntitlementControlTx(ctx context.Context, tx *sql.Tx, workspaceID, evidence, actorID string, authority MutationAuthority, now time.Time) error {
	var state string
	if err := tx.QueryRowContext(ctx, `SELECT state FROM admin_domain_entitlement_controls WHERE workspace_id=? FOR UPDATE`, workspaceID).Scan(&state); errors.Is(err, sql.ErrNoRows) {
		return ErrConflict
	} else if err != nil {
		return err
	}
	var unsafe int
	if err := tx.QueryRowContext(ctx, `
		SELECT COUNT(*) FROM custom_domains
		WHERE workspace_id=? AND (
			(COALESCE(TRIM(security_category),'') <> '')
			OR ownership_status='lost'
			OR routing_state IN ('revoked','removed')
		)`, workspaceID).Scan(&unsafe); err != nil {
		return err
	}
	if unsafe != 0 {
		return ErrConflict
	}
	if _, err := tx.ExecContext(ctx, `DELETE FROM admin_domain_entitlement_controls WHERE workspace_id=?`, workspaceID); err != nil {
		return err
	}
	// Source rows were never rewritten by P17, so restore only removes the
	// overlay. Routing stays suspended and must pass inherited P06/P16 restore
	// authority separately before it can become enabled again.
	if err := appendDomainEntitlementAuditTx(ctx, tx, workspaceID, nil, actorID, "domain.entitlement.admin.restore", authority.Reason, authority.CorrelationID, map[string]any{
		"evidence_reference_present": true,
		"prior_control_state":        state,
	}); err != nil {
		return err
	}
	return nil
}

func appendDomainEntitlementAuditTx(ctx context.Context, tx *sql.Tx, workspaceID string, sourceID *uint64, actorID, action, reason, correlationID string, metadata map[string]any) error {
	raw, err := json.Marshal(metadata)
	if err != nil {
		return err
	}
	_, err = tx.ExecContext(ctx, `
		INSERT INTO custom_domain_audit_events (
			workspace_id,domain_id,entitlement_source_id,actor_id,action,result,reason,correlation_id,metadata_json
		) VALUES (?,NULL,?,?,?,'success',?,?,CAST(? AS JSON))`,
		workspaceID, sourceID, strings.TrimSpace(actorID), strings.TrimSpace(action), strings.TrimSpace(reason), strings.TrimSpace(correlationID), string(raw))
	return err
}

func nullableTime(value *time.Time) any {
	if value == nil || value.IsZero() {
		return nil
	}
	return value.UTC().Truncate(time.Microsecond)
}

func loadDomainEntitlementRequest(ctx context.Context, q interface {
	QueryRowContext(context.Context, string, ...any) *sql.Row
}, workspaceID string) (*DomainEntitlementRequestView, error) {
	var item DomainEntitlementRequestView
	var limit sql.NullInt64
	var reason sql.NullString
	if err := q.QueryRowContext(ctx, `
		SELECT support_ticket_id,requested_domain_limit,status,decision_reason,submitted_at
		FROM custom_domain_entitlement_requests
		WHERE workspace_id=?
		ORDER BY submitted_at DESC,id DESC LIMIT 1`, workspaceID).Scan(&item.SupportTicketID, &limit, &item.Status, &reason, &item.SubmittedAt); errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	} else if err != nil {
		return nil, err
	}
	if limit.Valid {
		value := uint32(limit.Int64)
		item.RequestedDomainLimit = &value
	}
	if reason.Valid {
		item.DecisionReason = reason.String
	}
	item.SubmittedAt = item.SubmittedAt.UTC()
	var category sql.NullString
	err := q.QueryRowContext(ctx, `
		SELECT user_visible_category
		FROM admin_domain_entitlement_decisions
		WHERE workspace_id=? AND action='deny' AND support_ticket_id=?
		ORDER BY created_at DESC,id DESC LIMIT 1`, workspaceID, item.SupportTicketID).Scan(&category)
	if err != nil && !errors.Is(err, sql.ErrNoRows) {
		return nil, err
	}
	if category.Valid {
		item.UserVisibleCategory = category.String
	}
	return &item, nil
}

func loadDomainEntitlementControl(ctx context.Context, q interface {
	QueryRowContext(context.Context, string, ...any) *sql.Row
}, workspaceID string) (*DomainEntitlementControlView, error) {
	var item DomainEntitlementControlView
	if err := q.QueryRowContext(ctx, `
		SELECT state,reason,actor_id,decision_id,effective_at,updated_at
		FROM admin_domain_entitlement_controls WHERE workspace_id=?`, workspaceID).Scan(&item.State, &item.Reason, &item.ActorID, &item.DecisionID, &item.EffectiveAt, &item.UpdatedAt); errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	} else if err != nil {
		return nil, err
	}
	item.EffectiveAt = item.EffectiveAt.UTC()
	item.UpdatedAt = item.UpdatedAt.UTC()
	return &item, nil
}

func loadDomainEntitlementDecisions(ctx context.Context, db *sql.DB, workspaceID string, limit int) ([]DomainEntitlementDecisionRecord, error) {
	if limit < 1 || limit > 500 {
		return nil, ErrInvalid
	}
	rows, err := db.QueryContext(ctx, `
		SELECT id,action,actor_id,request_correlation_id,reason,COALESCE(support_ticket_id,''),affected_routes,created_at
		FROM admin_domain_entitlement_decisions
		WHERE workspace_id=? ORDER BY created_at DESC,id DESC LIMIT ?`, workspaceID, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	items := []DomainEntitlementDecisionRecord{}
	for rows.Next() {
		var item DomainEntitlementDecisionRecord
		if err := rows.Scan(&item.ID, &item.Action, &item.ActorID, &item.RequestCorrelationID, &item.Reason, &item.SupportTicketID, &item.AffectedRoutes, &item.CreatedAt); err != nil {
			return nil, err
		}
		item.CreatedAt = item.CreatedAt.UTC()
		items = append(items, item)
	}
	return items, rows.Err()
}
