package trust

import (
	"context"
	"database/sql"
	"errors"
	"regexp"
	"strconv"
	"strings"

	"github.com/Techshrr/GoJet/internal/workspace"
)

type SecurityNotificationEvent string

const (
	NotificationDestinationBlocked  SecurityNotificationEvent = "destination-blocked"
	NotificationDestinationRestored SecurityNotificationEvent = "destination-restored"
	NotificationDomainSuspended     SecurityNotificationEvent = "domain-suspended"
	NotificationDomainRestored      SecurityNotificationEvent = "domain-restored"
)

var notificationAuthorityRefPattern = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9._:-]{7,127}$`)

type SecurityNotificationInput struct {
	WorkspaceID  string
	Event        SecurityNotificationEvent
	ResourceType AbuseResourceType
	ResourceID   string
	AuthorityRef string
}

type SecurityNotificationResult struct {
	Items []workspace.Notification
}

// ProduceSecurityOwnerNotificationsTx contributes P16 security/domain producer
// events through the inherited P12 notification authority. P16 does not own
// notification read state, dedupe persistence, recipient membership, or deep
// link authorization. The caller owns commit/rollback.
func ProduceSecurityOwnerNotificationsTx(ctx context.Context, tx *sql.Tx, input SecurityNotificationInput) (SecurityNotificationResult, error) {
	input = normalizeSecurityNotificationInput(input)
	if tx == nil || !validSecurityNotificationInput(input) {
		return SecurityNotificationResult{}, ErrInvalid
	}

	resourceID, err := strconv.ParseUint(input.ResourceID, 10, 64)
	if err != nil || resourceID == 0 {
		return SecurityNotificationResult{}, ErrInvalid
	}
	if err := validateNotificationResourceAuthorityTx(ctx, tx, input.WorkspaceID, input.ResourceType, resourceID); err != nil {
		return SecurityNotificationResult{}, err
	}

	p12Input, err := buildP12Notification(input)
	if err != nil {
		return SecurityNotificationResult{}, err
	}
	items, err := workspace.ProduceOwnerNotificationsTx(ctx, tx, p12Input)
	if err != nil {
		if errors.Is(err, workspace.ErrForbidden) {
			return SecurityNotificationResult{}, ErrUnauthorized
		}
		if errors.Is(err, workspace.ErrInvalid) {
			return SecurityNotificationResult{}, ErrInvalid
		}
		return SecurityNotificationResult{}, err
	}
	return SecurityNotificationResult{Items: items}, nil
}

func normalizeSecurityNotificationInput(input SecurityNotificationInput) SecurityNotificationInput {
	input.WorkspaceID = strings.TrimSpace(input.WorkspaceID)
	input.ResourceID = strings.TrimSpace(input.ResourceID)
	input.AuthorityRef = strings.TrimSpace(input.AuthorityRef)
	return input
}

func validSecurityNotificationInput(input SecurityNotificationInput) bool {
	if input.WorkspaceID == "" || len(input.WorkspaceID) > 64 || input.ResourceID == "" || len(input.ResourceID) > 128 || !notificationAuthorityRefPattern.MatchString(input.AuthorityRef) {
		return false
	}
	switch input.Event {
	case NotificationDestinationBlocked, NotificationDestinationRestored:
		return input.ResourceType == AbuseShortLinkRisk
	case NotificationDomainSuspended, NotificationDomainRestored:
		return input.ResourceType == AbuseCustomDomainRisk
	default:
		return false
	}
}

func validateNotificationResourceAuthorityTx(ctx context.Context, tx *sql.Tx, workspaceID string, resourceType AbuseResourceType, resourceID uint64) error {
	var count uint64
	var err error
	switch resourceType {
	case AbuseShortLinkRisk:
		err = tx.QueryRowContext(ctx, `
SELECT COUNT(*)
FROM links
WHERE workspace_id=? AND id=? AND deleted_at IS NULL`, workspaceID, resourceID).Scan(&count)
	case AbuseCustomDomainRisk:
		err = tx.QueryRowContext(ctx, `
SELECT COUNT(*)
FROM custom_domains
WHERE workspace_id=? AND id=? AND removed_at IS NULL`, workspaceID, resourceID).Scan(&count)
	default:
		return ErrInvalid
	}
	if err != nil {
		return err
	}
	if count != 1 {
		return ErrNotFound
	}
	return nil
}

func buildP12Notification(input SecurityNotificationInput) (workspace.NotificationInput, error) {
	base := workspace.NotificationInput{
		WorkspaceID: input.WorkspaceID,
		EventKey:    "p16." + string(input.Event),
		DedupeKey:   "p16:" + string(input.Event) + ":" + string(input.ResourceType) + ":" + input.ResourceID + ":" + input.AuthorityRef,
		ResourceID:  input.ResourceID,
	}
	switch input.Event {
	case NotificationDestinationBlocked:
		base.Category = "security"
		base.Title = "Short link blocked for safety review"
		base.Summary = "GoJet Trust & Safety blocked a short link pending safe recovery authority."
		base.DeepLink = "/app/links/" + input.ResourceID
		base.ResourceType = "link"
	case NotificationDestinationRestored:
		base.Category = "security"
		base.Title = "Short link safety access restored"
		base.Summary = "A short link was restored after current safety authority allowed recovery."
		base.DeepLink = "/app/links/" + input.ResourceID
		base.ResourceType = "link"
	case NotificationDomainSuspended:
		base.Category = "domains"
		base.Title = "Custom domain suspended for safety"
		base.Summary = "A custom domain was suspended immediately by GoJet Trust & Safety."
		base.DeepLink = "/app/domains/" + input.ResourceID
		base.ResourceType = "domain"
	case NotificationDomainRestored:
		base.Category = "domains"
		base.Title = "Custom domain safety access restored"
		base.Summary = "A custom domain was restored after all current safety and domain readiness checks passed."
		base.DeepLink = "/app/domains/" + input.ResourceID
		base.ResourceType = "domain"
	default:
		return workspace.NotificationInput{}, ErrInvalid
	}
	return base, nil
}
