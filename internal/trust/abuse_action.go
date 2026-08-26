package trust

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"errors"
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/Techshrr/GoJet/internal/domains"
	"github.com/Techshrr/GoJet/internal/links"
)

type AbuseResourceAction string

const (
	AbuseActionBlock   AbuseResourceAction = "block"
	AbuseActionSuspend AbuseResourceAction = "suspend"
	AbuseActionRestore AbuseResourceAction = "restore"

	abuseRuntimePolicyVersion = "p16-abuse-hold-v1"
)

type AbuseResourceHold struct {
	ID                   uint64
	ReportID             uint64
	WorkspaceID          string
	ResourceType         AbuseResourceType
	ResourceID           string
	ExactFingerprint     string
	State                string
	ReasonCategory       string
	ActorID              string
	CorrelationID        string
	IdempotencyKeyHash   string
	RequestFingerprint   string
	ReleasedAt           *time.Time
	ReleasedBy           string
	ReleaseReason        string
	ReleaseCorrelationID string
	CreatedAt            time.Time
	UpdatedAt            time.Time
}

type AbuseResourceActionInput struct {
	ReportID         uint64
	Action           AbuseResourceAction
	ExactFingerprint string
	Reason           string
	ActorID          string
	CorrelationID    string
	IdempotencyKey   string
	Now              time.Time
}

type AbuseResourceActionResult struct {
	Report  AbuseReport
	Hold    AbuseResourceHold
	Changed bool
}

type AbuseActionService struct {
	Store                    *Store
	Runtime                  *links.RedisRiskStore
	DestinationPolicyVersion string
	RuntimeTTL               time.Duration
}

func NewAbuseActionService(store *Store, runtime *links.RedisRiskStore, destinationPolicyVersion string, runtimeTTL time.Duration) (*AbuseActionService, error) {
	destinationPolicyVersion = strings.TrimSpace(destinationPolicyVersion)
	if store == nil || store.db == nil || runtime == nil || destinationPolicyVersion == "" || len(destinationPolicyVersion) > 64 || runtimeTTL <= 0 || runtimeTTL > 24*time.Hour {
		return nil, ErrInvalid
	}
	return &AbuseActionService{Store: store, Runtime: runtime, DestinationPolicyVersion: destinationPolicyVersion, RuntimeTTL: runtimeTTL}, nil
}

func (s *AbuseActionService) Apply(ctx context.Context, input AbuseResourceActionInput, authorizer PermissionAuthorizer) (AbuseResourceActionResult, error) {
	input = normalizeAbuseResourceActionInput(input)
	if s == nil || s.Store == nil || s.Store.db == nil || s.Runtime == nil || authorizer == nil || !validAbuseResourceActionInput(input) {
		return AbuseResourceActionResult{}, ErrInvalid
	}
	actionName := abuseResourceActionName(input.Action)
	idempotencyHash := abuseAdminIdempotencyHash(input.IdempotencyKey)
	requestFingerprint := abuseResourceActionFingerprint(input)

	tx, err := s.Store.db.BeginTx(ctx, &sql.TxOptions{Isolation: sql.LevelReadCommitted})
	if err != nil {
		return AbuseResourceActionResult{}, err
	}
	defer func() { _ = tx.Rollback() }()
	report, err := loadAbuseByIDForUpdate(ctx, tx, input.ReportID)
	if err != nil {
		return AbuseResourceActionResult{}, err
	}

	if existing, eventErr := loadAbuseEventIdempotency(ctx, tx, report.ID, actionName, idempotencyHash); eventErr == nil {
		if existing.RequestFingerprint != requestFingerprint || existing.Result != "success" {
			return AbuseResourceActionResult{}, ErrConflict
		}
		hold, holdErr := loadLatestAbuseHoldTx(ctx, tx, report.WorkspaceID, report.ResourceType, report.ResourceID)
		if holdErr != nil {
			return AbuseResourceActionResult{}, holdErr
		}
		if err := tx.Commit(); err != nil {
			return AbuseResourceActionResult{}, err
		}
		result := AbuseResourceActionResult{Report: report, Hold: hold, Changed: false}
		if input.Action == AbuseActionBlock {
			if err := s.projectImmediateBlock(ctx, report, hold); err != nil {
				return AbuseResourceActionResult{}, err
			}
		}
		if input.Action == AbuseActionRestore && report.ResourceType == AbuseShortLinkRisk {
			if _, err := ProjectCurrentDestinationDecision(ctx, s.Store, s.Runtime, report.WorkspaceID, mustResourceID(report.ResourceID), s.DestinationPolicyVersion, input.Now, s.RuntimeTTL); err != nil {
				return AbuseResourceActionResult{}, err
			}
		}
		return result, nil
	} else if !errors.Is(eventErr, ErrNotFound) {
		return AbuseResourceActionResult{}, eventErr
	}

	if err := authorizer.Authorize(ctx, input.ActorID, SecurityManagePermission); err != nil {
		if auditErr := appendAbuseReportEventTx(ctx, tx, report, input.ActorID, actionName, &report.Status, nil, "denied", "permission-denied", input.CorrelationID, "", requestFingerprint, map[string]any{"resource_type": report.ResourceType}); auditErr != nil {
			return AbuseResourceActionResult{}, auditErr
		}
		if err := tx.Commit(); err != nil {
			return AbuseResourceActionResult{}, err
		}
		return AbuseResourceActionResult{}, ErrUnauthorized
	}
	if report.Status != AbuseInvestigating {
		if auditErr := appendAbuseReportEventTx(ctx, tx, report, input.ActorID, actionName, &report.Status, nil, "conflict", "report-not-investigating", input.CorrelationID, "", requestFingerprint, map[string]any{"resource_type": report.ResourceType}); auditErr != nil {
			return AbuseResourceActionResult{}, auditErr
		}
		if err := tx.Commit(); err != nil {
			return AbuseResourceActionResult{}, err
		}
		return AbuseResourceActionResult{}, ErrConflict
	}

	switch report.ResourceType {
	case AbuseShortLinkRisk:
		return s.applyShortLinkAction(ctx, tx, report, input, actionName, idempotencyHash, requestFingerprint)
	case AbuseCustomDomainRisk:
		return s.applyCustomDomainAction(ctx, tx, report, input, actionName, idempotencyHash, requestFingerprint)
	default:
		return AbuseResourceActionResult{}, ErrInvalid
	}
}

// EffectiveDestinationAuthority overlays an active P16 short-link abuse hold on
// the normal policy/manual-override authority. A hold whose exact fingerprint
// no longer matches the link fails stale instead of allowing target mutation to
// escape the security action.
func (s *Store) EffectiveDestinationAuthority(ctx context.Context, workspaceID string, linkID uint64, policyVersion string, now time.Time) (DestinationAuthority, error) {
	authority, err := s.CurrentDestinationAuthority(ctx, workspaceID, linkID, policyVersion, now)
	if err != nil {
		return DestinationAuthority{}, err
	}
	hold, err := s.ActiveAbuseHold(ctx, workspaceID, AbuseShortLinkRisk, strconv.FormatUint(linkID, 10))
	if errors.Is(err, ErrNotFound) {
		return authority, nil
	}
	if err != nil {
		return DestinationAuthority{}, err
	}
	if hold.ExactFingerprint != authority.Fingerprint {
		return DestinationAuthority{}, ErrStaleFingerprint
	}
	authority.State = DecisionBlock
	authority.ValidUntil = nil
	authority.Source = "abuse-hold"
	return authority, nil
}

func (s *Store) ActiveAbuseHold(ctx context.Context, workspaceID string, resourceType AbuseResourceType, resourceID string) (AbuseResourceHold, error) {
	workspaceID = strings.TrimSpace(workspaceID)
	resourceID = strings.TrimSpace(resourceID)
	if s == nil || s.db == nil || workspaceID == "" || !validAbuseResourceType(resourceType) || resourceID == "" {
		return AbuseResourceHold{}, ErrInvalid
	}
	return scanAbuseHold(s.db.QueryRowContext(ctx, abuseHoldSelect+` WHERE workspace_id=? AND resource_type=? AND resource_id=? AND state='active' ORDER BY id DESC LIMIT 1`, workspaceID, string(resourceType), resourceID))
}

func (s *AbuseActionService) applyShortLinkAction(ctx context.Context, tx *sql.Tx, report AbuseReport, input AbuseResourceActionInput, actionName, idempotencyHash, requestFingerprint string) (AbuseResourceActionResult, error) {
	linkID, err := strconv.ParseUint(report.ResourceID, 10, 64)
	if err != nil || linkID == 0 || !validFingerprint(input.ExactFingerprint) || report.DestinationFingerprint != input.ExactFingerprint {
		return AbuseResourceActionResult{}, ErrStaleFingerprint
	}
	currentFingerprint, err := currentLinkFingerprintTx(ctx, tx, report.WorkspaceID, linkID)
	if err != nil {
		return AbuseResourceActionResult{}, err
	}
	if currentFingerprint != input.ExactFingerprint {
		return AbuseResourceActionResult{}, ErrStaleFingerprint
	}

	switch input.Action {
	case AbuseActionBlock:
		hold, err := insertActiveAbuseHoldTx(ctx, tx, report, input, idempotencyHash, requestFingerprint, "abuse-block")
		if err != nil {
			return AbuseResourceActionResult{}, err
		}
		if _, err := s.Runtime.PutDecision(ctx, linkID, currentFingerprint, links.RiskBlock, abuseRuntimePolicyVersion, s.RuntimeTTL); err != nil {
			return AbuseResourceActionResult{}, err
		}
		if err := appendAbuseReportEventTx(ctx, tx, report, input.ActorID, actionName, &report.Status, &report.Status, "success", "resource-blocked", input.CorrelationID, idempotencyHash, requestFingerprint, abuseActionMetadata(report, hold, input)); err != nil {
			return AbuseResourceActionResult{}, err
		}
		if err := appendRiskAuditTx(ctx, tx, report.WorkspaceID, &linkID, nil, input.ActorID, "destination-risk.abuse-block", "success", input.Reason, input.CorrelationID, map[string]any{"report_id": report.PublicID, "exact_fingerprint": currentFingerprint, "hold_id": hold.ID}); err != nil {
			return AbuseResourceActionResult{}, err
		}
		if err := tx.Commit(); err != nil {
			return AbuseResourceActionResult{}, err
		}
		return AbuseResourceActionResult{Report: report, Hold: hold, Changed: true}, nil
	case AbuseActionRestore:
		hold, err := loadActiveAbuseHoldTx(ctx, tx, report.WorkspaceID, AbuseShortLinkRisk, report.ResourceID)
		if err != nil {
			return AbuseResourceActionResult{}, err
		}
		if hold.ReportID != report.ID || hold.ExactFingerprint != currentFingerprint {
			return AbuseResourceActionResult{}, ErrConflict
		}
		authority, err := destinationSafetyAuthorityTx(ctx, tx, report.WorkspaceID, linkID, currentFingerprint, s.DestinationPolicyVersion, input.Now)
		if err != nil {
			return AbuseResourceActionResult{}, err
		}
		if authority.State != DecisionAllow || authority.ValidUntil == nil || !authority.ValidUntil.After(input.Now) {
			return AbuseResourceActionResult{}, ErrConflict
		}
		released, err := releaseAbuseHoldTx(ctx, tx, hold, input)
		if err != nil {
			return AbuseResourceActionResult{}, err
		}
		if err := appendAbuseReportEventTx(ctx, tx, report, input.ActorID, actionName, &report.Status, &report.Status, "success", "resource-restored", input.CorrelationID, idempotencyHash, requestFingerprint, abuseActionMetadata(report, released, input)); err != nil {
			return AbuseResourceActionResult{}, err
		}
		if err := appendRiskAuditTx(ctx, tx, report.WorkspaceID, &linkID, nil, input.ActorID, "destination-risk.abuse-restore", "success", input.Reason, input.CorrelationID, map[string]any{"report_id": report.PublicID, "exact_fingerprint": currentFingerprint, "hold_id": hold.ID, "safety_source": authority.Source}); err != nil {
			return AbuseResourceActionResult{}, err
		}
		if err := tx.Commit(); err != nil {
			return AbuseResourceActionResult{}, err
		}
		if _, err := ProjectCurrentDestinationDecision(ctx, s.Store, s.Runtime, report.WorkspaceID, linkID, s.DestinationPolicyVersion, input.Now, s.RuntimeTTL); err != nil {
			return AbuseResourceActionResult{}, err
		}
		return AbuseResourceActionResult{Report: report, Hold: released, Changed: true}, nil
	default:
		return AbuseResourceActionResult{}, ErrInvalid
	}
}

func (s *AbuseActionService) applyCustomDomainAction(ctx context.Context, tx *sql.Tx, report AbuseReport, input AbuseResourceActionInput, actionName, idempotencyHash, requestFingerprint string) (AbuseResourceActionResult, error) {
	if input.ExactFingerprint != "" {
		return AbuseResourceActionResult{}, ErrInvalid
	}
	domainID, err := strconv.ParseUint(report.ResourceID, 10, 64)
	if err != nil || domainID == 0 {
		return AbuseResourceActionResult{}, ErrInvalid
	}
	domainStore := domains.NewMySQLStore(s.Store.db)

	switch input.Action {
	case AbuseActionSuspend:
		updated, err := domainStore.ApplyDomainSecuritySuspension(ctx, domains.DomainSecuritySuspensionInput{WorkspaceID: report.WorkspaceID, DomainID: domainID, ActorID: input.ActorID, Category: domains.DomainSecurityAbuse, Reason: input.Reason, CorrelationID: input.CorrelationID + ":p06", Now: input.Now})
		if err != nil {
			return AbuseResourceActionResult{}, err
		}
		if err := (&DomainRiskService{Store: s.Store}).appendDomainSecurityAudit(ctx, domains.DomainSecuritySuspensionInput{WorkspaceID: report.WorkspaceID, DomainID: domainID, ActorID: input.ActorID, Category: domains.DomainSecurityAbuse, Reason: input.Reason, CorrelationID: input.CorrelationID + ":p16", Now: input.Now}, updated); err != nil {
			return AbuseResourceActionResult{}, err
		}
		hold, err := insertActiveAbuseHoldTx(ctx, tx, report, input, idempotencyHash, requestFingerprint, "abuse-suspension")
		if err != nil {
			return AbuseResourceActionResult{}, err
		}
		if err := appendAbuseReportEventTx(ctx, tx, report, input.ActorID, actionName, &report.Status, &report.Status, "success", "resource-suspended", input.CorrelationID, idempotencyHash, requestFingerprint, abuseActionMetadata(report, hold, input)); err != nil {
			return AbuseResourceActionResult{}, err
		}
		if err := tx.Commit(); err != nil {
			return AbuseResourceActionResult{}, err
		}
		return AbuseResourceActionResult{Report: report, Hold: hold, Changed: true}, nil
	case AbuseActionRestore:
		hold, err := loadActiveAbuseHoldTx(ctx, tx, report.WorkspaceID, AbuseCustomDomainRisk, report.ResourceID)
		if err != nil {
			return AbuseResourceActionResult{}, err
		}
		if hold.ReportID != report.ID {
			return AbuseResourceActionResult{}, ErrConflict
		}
		if err := domainSafetyAllowsRestoreTx(ctx, tx, report.WorkspaceID, domainID, input.Now); err != nil {
			return AbuseResourceActionResult{}, err
		}
		updated, err := domainStore.RestoreDomainSecuritySuspension(ctx, domains.DomainSecurityRestoreInput{WorkspaceID: report.WorkspaceID, DomainID: domainID, ActorID: input.ActorID, ExpectedCategory: domains.DomainSecurityAbuse, Reason: input.Reason, CorrelationID: input.CorrelationID + ":p06", Now: input.Now})
		if err != nil {
			return AbuseResourceActionResult{}, err
		}
		released, err := releaseAbuseHoldTx(ctx, tx, hold, input)
		if err != nil {
			return AbuseResourceActionResult{}, err
		}
		if err := appendAbuseReportEventTx(ctx, tx, report, input.ActorID, actionName, &report.Status, &report.Status, "success", "resource-restored", input.CorrelationID, idempotencyHash, requestFingerprint, abuseActionMetadata(report, released, input)); err != nil {
			return AbuseResourceActionResult{}, err
		}
		if err := appendDomainRiskAuditTx(ctx, tx, report.WorkspaceID, domainID, nil, input.ActorID, "domain-risk.abuse-restore", "success", "policy-allow", input.CorrelationID, map[string]any{"report_id": report.PublicID, "hold_id": hold.ID, "routing_state": updated.RoutingState, "grace": false}); err != nil {
			return AbuseResourceActionResult{}, err
		}
		if err := tx.Commit(); err != nil {
			return AbuseResourceActionResult{}, err
		}
		return AbuseResourceActionResult{Report: report, Hold: released, Changed: true}, nil
	default:
		return AbuseResourceActionResult{}, ErrInvalid
	}
}

const abuseHoldSelect = `
SELECT id,report_id,workspace_id,resource_type,resource_id,COALESCE(exact_fingerprint,''),state,reason_category,actor_id,correlation_id,
       idempotency_key_hash,request_fingerprint,released_at,COALESCE(released_by,''),COALESCE(release_reason,''),COALESCE(release_correlation_id,''),created_at,updated_at
FROM abuse_resource_holds`

func insertActiveAbuseHoldTx(ctx context.Context, tx *sql.Tx, report AbuseReport, input AbuseResourceActionInput, idempotencyHash, requestFingerprint, reasonCategory string) (AbuseResourceHold, error) {
	var exact any
	if report.ResourceType == AbuseShortLinkRisk {
		exact = input.ExactFingerprint
	}
	res, err := tx.ExecContext(ctx, `
INSERT INTO abuse_resource_holds
(report_id,workspace_id,resource_type,resource_id,exact_fingerprint,state,reason_category,actor_id,correlation_id,idempotency_key_hash,request_fingerprint)
VALUES (?,?,?,?,?,'active',?,?,?,?,?)`, report.ID, report.WorkspaceID, string(report.ResourceType), report.ResourceID, exact, reasonCategory, input.ActorID, input.CorrelationID, idempotencyHash, requestFingerprint)
	if err != nil {
		if mysqlDuplicate(err) {
			return AbuseResourceHold{}, ErrConflict
		}
		return AbuseResourceHold{}, err
	}
	id, err := res.LastInsertId()
	if err != nil || id <= 0 {
		return AbuseResourceHold{}, ErrConflict
	}
	return scanAbuseHold(tx.QueryRowContext(ctx, abuseHoldSelect+` WHERE id=?`, id))
}

func loadActiveAbuseHoldTx(ctx context.Context, tx *sql.Tx, workspaceID string, resourceType AbuseResourceType, resourceID string) (AbuseResourceHold, error) {
	return scanAbuseHold(tx.QueryRowContext(ctx, abuseHoldSelect+` WHERE workspace_id=? AND resource_type=? AND resource_id=? AND state='active' ORDER BY id DESC LIMIT 1 FOR UPDATE`, workspaceID, string(resourceType), resourceID))
}

func loadLatestAbuseHoldTx(ctx context.Context, tx *sql.Tx, workspaceID string, resourceType AbuseResourceType, resourceID string) (AbuseResourceHold, error) {
	return scanAbuseHold(tx.QueryRowContext(ctx, abuseHoldSelect+` WHERE workspace_id=? AND resource_type=? AND resource_id=? ORDER BY id DESC LIMIT 1`, workspaceID, string(resourceType), resourceID))
}

func releaseAbuseHoldTx(ctx context.Context, tx *sql.Tx, hold AbuseResourceHold, input AbuseResourceActionInput) (AbuseResourceHold, error) {
	res, err := tx.ExecContext(ctx, `
UPDATE abuse_resource_holds
SET state='released',released_at=?,released_by=?,release_reason=?,release_correlation_id=?
WHERE id=? AND state='active'`, input.Now, input.ActorID, SanitizeAbuseDetails(input.Reason), input.CorrelationID, hold.ID)
	if err != nil {
		return AbuseResourceHold{}, err
	}
	rows, err := res.RowsAffected()
	if err != nil || rows != 1 {
		return AbuseResourceHold{}, ErrConflict
	}
	return scanAbuseHold(tx.QueryRowContext(ctx, abuseHoldSelect+` WHERE id=?`, hold.ID))
}

func scanAbuseHold(row rowScanner) (AbuseResourceHold, error) {
	var hold AbuseResourceHold
	var resourceType string
	var releasedAt sql.NullTime
	if err := row.Scan(&hold.ID, &hold.ReportID, &hold.WorkspaceID, &resourceType, &hold.ResourceID, &hold.ExactFingerprint, &hold.State, &hold.ReasonCategory, &hold.ActorID, &hold.CorrelationID, &hold.IdempotencyKeyHash, &hold.RequestFingerprint, &releasedAt, &hold.ReleasedBy, &hold.ReleaseReason, &hold.ReleaseCorrelationID, &hold.CreatedAt, &hold.UpdatedAt); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return AbuseResourceHold{}, ErrNotFound
		}
		return AbuseResourceHold{}, err
	}
	hold.ResourceType = AbuseResourceType(resourceType)
	if releasedAt.Valid {
		v := releasedAt.Time.UTC()
		hold.ReleasedAt = &v
	}
	return hold, nil
}

func destinationSafetyAuthorityTx(ctx context.Context, tx *sql.Tx, workspaceID string, linkID uint64, fingerprint, policyVersion string, now time.Time) (DestinationAuthority, error) {
	decision, err := latestDestinationDecisionTx(ctx, tx, workspaceID, linkID, fingerprint, policyVersion)
	if err != nil {
		return DestinationAuthority{}, err
	}
	authority := DestinationAuthority{Decision: decision, State: decision.State, Fingerprint: fingerprint, PolicyVersion: policyVersion, ValidUntil: decision.ValidUntil, Source: "policy"}
	override, err := activeDestinationOverrideTx(ctx, tx, workspaceID, linkID, fingerprint, policyVersion, decision, now)
	if err == nil {
		authority.Override = &override
		authority.State = override.Decision
		v := override.ExpiresAt
		authority.ValidUntil = &v
		authority.Source = "manual-override"
	} else if !errors.Is(err, ErrNotFound) {
		return DestinationAuthority{}, err
	}
	return authority, nil
}

func domainSafetyAllowsRestoreTx(ctx context.Context, tx *sql.Tx, workspaceID string, domainID uint64, now time.Time) error {
	var state, policyVersion string
	var validUntil sql.NullTime
	err := tx.QueryRowContext(ctx, `
SELECT state,policy_version,valid_until
FROM domain_risk_evaluations
WHERE workspace_id=? AND domain_id=?
ORDER BY COALESCE(checked_at,created_at) DESC,id DESC
LIMIT 1`, workspaceID, domainID).Scan(&state, &policyVersion, &validUntil)
	if errors.Is(err, sql.ErrNoRows) {
		return ErrConflict
	}
	if err != nil {
		return err
	}
	if state != string(DomainRiskAllow) || !validUntil.Valid || !validUntil.Time.UTC().After(now.UTC()) {
		return ErrConflict
	}
	var projectedRisk, projectedPolicy string
	if err := tx.QueryRowContext(ctx, `SELECT risk_status,COALESCE(risk_policy_version,'') FROM custom_domains WHERE workspace_id=? AND id=? AND removed_at IS NULL`, workspaceID, domainID).Scan(&projectedRisk, &projectedPolicy); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return ErrNotFound
		}
		return err
	}
	if projectedRisk != string(domains.RiskAllow) || projectedPolicy != policyVersion {
		return ErrConflict
	}
	return nil
}

func (s *AbuseActionService) projectImmediateBlock(ctx context.Context, report AbuseReport, hold AbuseResourceHold) error {
	if report.ResourceType != AbuseShortLinkRisk || hold.State != "active" || !validFingerprint(hold.ExactFingerprint) {
		return nil
	}
	linkID, err := strconv.ParseUint(report.ResourceID, 10, 64)
	if err != nil || linkID == 0 {
		return ErrInvalid
	}
	_, err = s.Runtime.PutDecision(ctx, linkID, hold.ExactFingerprint, links.RiskBlock, abuseRuntimePolicyVersion, s.RuntimeTTL)
	return err
}

func abuseActionMetadata(report AbuseReport, hold AbuseResourceHold, input AbuseResourceActionInput) map[string]any {
	metadata := map[string]any{
		"resource_type":   report.ResourceType,
		"hold_id":         hold.ID,
		"hold_state":      hold.State,
		"reason_redacted": SanitizeAbuseDetails(input.Reason),
	}
	if report.ResourceType == AbuseShortLinkRisk {
		metadata["exact_fingerprint"] = hold.ExactFingerprint
	}
	return metadata
}

func normalizeAbuseResourceActionInput(input AbuseResourceActionInput) AbuseResourceActionInput {
	input.ExactFingerprint = strings.ToLower(strings.TrimSpace(input.ExactFingerprint))
	input.Reason = strings.TrimSpace(input.Reason)
	input.ActorID = strings.TrimSpace(input.ActorID)
	input.CorrelationID = strings.TrimSpace(input.CorrelationID)
	input.IdempotencyKey = strings.TrimSpace(input.IdempotencyKey)
	input.Now = input.Now.UTC().Truncate(time.Microsecond)
	return input
}

func validAbuseResourceActionInput(input AbuseResourceActionInput) bool {
	if input.ReportID == 0 || input.Reason == "" || len(input.Reason) > 500 || input.ActorID == "" || len(input.ActorID) > 128 || input.CorrelationID == "" || len(input.CorrelationID) > 128 || len(input.IdempotencyKey) < 8 || len(input.IdempotencyKey) > 200 || input.Now.IsZero() {
		return false
	}
	switch input.Action {
	case AbuseActionBlock, AbuseActionSuspend, AbuseActionRestore:
		return true
	default:
		return false
	}
}

func abuseResourceActionName(action AbuseResourceAction) string {
	return "abuse.resource-" + string(action)
}

func abuseResourceActionFingerprint(input AbuseResourceActionInput) string {
	canonical := fmt.Sprintf("%d\n%s\n%s\n%s", input.ReportID, input.Action, input.ExactFingerprint, SanitizeAbuseDetails(input.Reason))
	sum := sha256.Sum256([]byte(canonical))
	return hex.EncodeToString(sum[:])
}

func mustResourceID(value string) uint64 {
	id, _ := strconv.ParseUint(strings.TrimSpace(value), 10, 64)
	return id
}
