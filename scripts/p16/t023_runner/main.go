package main

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/Techshrr/GoJet/internal/trust"
	"github.com/Techshrr/GoJet/internal/workspace"
	"github.com/Techshrr/GoJet/scripts/p16/domainfixture"
	"github.com/Techshrr/GoJet/scripts/p16/runtimefixture"
)

type output struct {
	Case         string          `json:"case"`
	Status       string          `json:"status"`
	MySQLVersion string          `json:"mysql_version"`
	Fixture      string          `json:"fixture"`
	RecordCounts map[string]int  `json:"record_counts"`
	Checks       map[string]bool `json:"checks"`
}

func main() {
	out, err := run()
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		out.Case = "P16-T023"
		out.Status = "FAIL"
		if out.Checks == nil {
			out.Checks = map[string]bool{"runner_completed": false}
		}
	}
	_ = json.NewEncoder(os.Stdout).Encode(out)
	if err != nil || out.Status != "PASS" {
		os.Exit(1)
	}
}

func run() (output, error) {
	out := output{
		Case:    "P16-T023",
		Status:  "FAIL",
		Fixture: "real MySQL 8.x plus inherited P12 workspace notification producer/read-state/deep-link authority proving deduplicated redacted P16 security/domain notifications",
		RecordCounts: map[string]int{},
		Checks:       map[string]bool{},
	}
	db, err := domainfixture.OpenDB()
	if err != nil {
		return out, err
	}
	defer db.Close()
	ctx, cancel := context.WithTimeout(context.Background(), 45*time.Second)
	defer cancel()
	out.MySQLVersion, err = domainfixture.MySQLVersion(ctx, db)
	if err != nil {
		return out, err
	}

	workspaceStore := workspace.NewStore(db)
	primaryOwner := workspace.Principal{UserID: "p16-t023-owner-a", Email: "owner-a@p16.invalid", DisplayName: "P16 Owner A"}
	ws, _, err := workspaceStore.CreateWorkspace(ctx, primaryOwner, "P16 T023 Workspace")
	if err != nil {
		return out, err
	}
	secondOwner := "p16-t023-owner-b"
	if _, err := db.ExecContext(ctx, `
INSERT INTO workspace_memberships (workspace_id,user_id,email,display_name,role)
VALUES (?,?,?,'P16 Owner B','owner')`, ws.ID, secondOwner, "owner-b@p16.invalid"); err != nil {
		return out, err
	}

	foreignOwner := workspace.Principal{UserID: "p16-t023-foreign-owner", Email: "foreign@p16.invalid", DisplayName: "P16 Foreign Owner"}
	foreignWS, _, err := workspaceStore.CreateWorkspace(ctx, foreignOwner, "P16 T023 Foreign Workspace")
	if err != nil {
		return out, err
	}

	link, err := runtimefixture.CreateLink(ctx, db, ws.ID, "go.example.test", "official", "t023-link", "https://safe.example/t023", nil, nil)
	if err != nil {
		return out, err
	}
	foreignLink, err := runtimefixture.CreateLink(ctx, db, foreignWS.ID, "go.example.test", "official", "t023-foreign", "https://safe.example/t023-foreign", nil, nil)
	if err != nil {
		return out, err
	}
	now := time.Now().UTC().Truncate(time.Microsecond)
	if err := runtimefixture.CreateReadyCustomDomain(ctx, db, ws.ID, "t023-domain.p16.example.com", now); err != nil {
		return out, err
	}
	var domainID uint64
	if err := db.QueryRowContext(ctx, `SELECT id FROM custom_domains WHERE workspace_id=? AND hostname_ascii=?`, ws.ID, "t023-domain.p16.example.com").Scan(&domainID); err != nil {
		return out, err
	}

	tx, err := db.BeginTx(ctx, &sql.TxOptions{Isolation: sql.LevelReadCommitted})
	if err != nil {
		return out, err
	}
	defer func() { _ = tx.Rollback() }()

	blocked, err := trust.ProduceSecurityOwnerNotificationsTx(ctx, tx, trust.SecurityNotificationInput{
		WorkspaceID: ws.ID, Event: trust.NotificationDestinationBlocked, ResourceType: trust.AbuseShortLinkRisk,
		ResourceID: strconv.FormatUint(link.ID, 10), AuthorityRef: "hold-link-0001",
	})
	if err != nil {
		return out, err
	}
	blockedReplay, err := trust.ProduceSecurityOwnerNotificationsTx(ctx, tx, trust.SecurityNotificationInput{
		WorkspaceID: ws.ID, Event: trust.NotificationDestinationBlocked, ResourceType: trust.AbuseShortLinkRisk,
		ResourceID: strconv.FormatUint(link.ID, 10), AuthorityRef: "hold-link-0001",
	})
	if err != nil {
		return out, err
	}
	restored, err := trust.ProduceSecurityOwnerNotificationsTx(ctx, tx, trust.SecurityNotificationInput{
		WorkspaceID: ws.ID, Event: trust.NotificationDestinationRestored, ResourceType: trust.AbuseShortLinkRisk,
		ResourceID: strconv.FormatUint(link.ID, 10), AuthorityRef: "restore-link-0001",
	})
	if err != nil {
		return out, err
	}
	suspended, err := trust.ProduceSecurityOwnerNotificationsTx(ctx, tx, trust.SecurityNotificationInput{
		WorkspaceID: ws.ID, Event: trust.NotificationDomainSuspended, ResourceType: trust.AbuseCustomDomainRisk,
		ResourceID: strconv.FormatUint(domainID, 10), AuthorityRef: "hold-domain-0001",
	})
	if err != nil {
		return out, err
	}
	domainRestored, err := trust.ProduceSecurityOwnerNotificationsTx(ctx, tx, trust.SecurityNotificationInput{
		WorkspaceID: ws.ID, Event: trust.NotificationDomainRestored, ResourceType: trust.AbuseCustomDomainRisk,
		ResourceID: strconv.FormatUint(domainID, 10), AuthorityRef: "restore-domain-0001",
	})
	if err != nil {
		return out, err
	}

	_, crossTenantErr := trust.ProduceSecurityOwnerNotificationsTx(ctx, tx, trust.SecurityNotificationInput{
		WorkspaceID: ws.ID, Event: trust.NotificationDestinationBlocked, ResourceType: trust.AbuseShortLinkRisk,
		ResourceID: strconv.FormatUint(foreignLink.ID, 10), AuthorityRef: "hold-cross-0001",
	})
	_, sensitiveAuthorityErr := trust.ProduceSecurityOwnerNotificationsTx(ctx, tx, trust.SecurityNotificationInput{
		WorkspaceID: ws.ID, Event: trust.NotificationDestinationBlocked, ResourceType: trust.AbuseShortLinkRisk,
		ResourceID: strconv.FormatUint(link.ID, 10), AuthorityRef: "authorization:BearerSecret123",
	})
	if err := tx.Commit(); err != nil {
		return out, err
	}

	blockPrimaryID := notificationIDFor(blocked.Items, primaryOwner.UserID)
	blockReplayPrimaryID := notificationIDFor(blockedReplay.Items, primaryOwner.UserID)
	if blockPrimaryID == 0 || blockReplayPrimaryID == 0 {
		return out, fmt.Errorf("missing primary-owner notification")
	}
	if err := workspaceStore.SetNotificationRead(ctx, ws.ID, primaryOwner.UserID, blockPrimaryID, true); err != nil {
		return out, err
	}

	// A later duplicate producer call must preserve the inherited P12 read state.
	tx2, err := db.BeginTx(ctx, &sql.TxOptions{Isolation: sql.LevelReadCommitted})
	if err != nil {
		return out, err
	}
	if _, err := trust.ProduceSecurityOwnerNotificationsTx(ctx, tx2, trust.SecurityNotificationInput{
		WorkspaceID: ws.ID, Event: trust.NotificationDestinationBlocked, ResourceType: trust.AbuseShortLinkRisk,
		ResourceID: strconv.FormatUint(link.ID, 10), AuthorityRef: "hold-link-0001",
	}); err != nil {
		_ = tx2.Rollback()
		return out, err
	}
	if err := tx2.Commit(); err != nil {
		return out, err
	}

	primaryPage, err := workspaceStore.ListNotifications(ctx, ws.ID, primaryOwner.UserID, "all", 20)
	if err != nil {
		return out, err
	}
	secondPage, err := workspaceStore.ListNotifications(ctx, ws.ID, secondOwner, "all", 20)
	if err != nil {
		return out, err
	}
	foreignDeepLinkAllowed, err := workspaceStore.AuthorizeDeepLink(ctx, foreignWS.ID, foreignOwner.UserID, "/app/links/"+strconv.FormatUint(link.ID, 10))
	if err != nil {
		return out, err
	}

	total, err := scalarInt(ctx, db, `SELECT COUNT(*) FROM workspace_notifications WHERE workspace_id=?`, ws.ID)
	if err != nil {
		return out, err
	}
	securityCount, err := scalarInt(ctx, db, `SELECT COUNT(*) FROM workspace_notifications WHERE workspace_id=? AND category='security'`, ws.ID)
	if err != nil {
		return out, err
	}
	domainCount, err := scalarInt(ctx, db, `SELECT COUNT(*) FROM workspace_notifications WHERE workspace_id=? AND category='domains'`, ws.ID)
	if err != nil {
		return out, err
	}
	dedupeRows, err := scalarInt(ctx, db, `SELECT COUNT(DISTINCT dedupe_key) FROM workspace_notifications WHERE workspace_id=?`, ws.ID)
	if err != nil {
		return out, err
	}
	sensitiveRows, err := scalarInt(ctx, db, `
SELECT COUNT(*) FROM workspace_notifications
WHERE workspace_id=? AND (
  LOWER(title) LIKE '%authorization:%' OR LOWER(summary) LIKE '%authorization:%' OR LOWER(dedupe_key) LIKE '%authorization:%'
  OR LOWER(title) LIKE '%bearer %' OR LOWER(summary) LIKE '%bearer %' OR LOWER(dedupe_key) LIKE '%bearer %'
  OR LOWER(title) LIKE '%risk_evidence%' OR LOWER(summary) LIKE '%risk_evidence%' OR LOWER(dedupe_key) LIKE '%risk_evidence%'
  OR LOWER(title) LIKE '%https://%' OR LOWER(summary) LIKE '%https://%'
)`, ws.ID)
	if err != nil {
		return out, err
	}

	out.RecordCounts = map[string]int{
		"workspace_notifications": total,
		"security_notifications": securityCount,
		"domain_notifications": domainCount,
		"distinct_dedupe_keys": dedupeRows,
		"sensitive_rows": sensitiveRows,
	}
	out.Checks = map[string]bool{
		"p16_fans_out_only_through_current_p12_owner_set": len(blocked.Items) == 2 && len(restored.Items) == 2 && len(suspended.Items) == 2 && len(domainRestored.Items) == 2,
		"duplicate_security_event_is_deduplicated": total == 8 && dedupeRows == 4 && blockPrimaryID == blockReplayPrimaryID,
		"security_and_domain_categories_are_preserved": securityCount == 4 && domainCount == 4,
		"resource_deep_links_survive_p12_authorization": validResourceDeepLinks(primaryPage.Items, link.ID, domainID) && validResourceDeepLinks(secondPage.Items, link.ID, domainID),
		"cross_tenant_resource_cannot_be_notified": errors.Is(crossTenantErr, trust.ErrNotFound) && !foreignDeepLinkAllowed,
		"sensitive_authority_material_is_rejected": errors.Is(sensitiveAuthorityErr, trust.ErrInvalid) && sensitiveRows == 0,
		"p12_read_state_survives_duplicate_producer_delivery": primaryPage.UnreadCount == 3 && secondPage.UnreadCount == 4,
		"p12_notification_state_remains_authority": primaryPage.State.Status == "complete" && secondPage.State.Status == "complete",
		"producer_returns_persistent_owner_items": allPersistent(blocked.Items) && allPersistent(restored.Items) && allPersistent(suspended.Items) && allPersistent(domainRestored.Items),
	}
	if allTrue(out.Checks) {
		out.Status = "PASS"
	}
	return out, nil
}

func notificationIDFor(items []workspace.Notification, userID string) uint64 {
	for _, item := range items {
		if item.RecipientUserID == userID {
			return item.ID
		}
	}
	return 0
}

func validResourceDeepLinks(items []workspace.Notification, linkID, domainID uint64) bool {
	if len(items) != 4 {
		return false
	}
	linkPath := "/app/links/" + strconv.FormatUint(linkID, 10)
	domainPath := "/app/domains/" + strconv.FormatUint(domainID, 10)
	linkCount, domainCount := 0, 0
	for _, item := range items {
		switch item.DeepLink {
		case linkPath:
			linkCount++
		case domainPath:
			domainCount++
		default:
			return false
		}
		if strings.Contains(strings.ToLower(item.Title+" "+item.Summary), "https://") {
			return false
		}
	}
	return linkCount == 2 && domainCount == 2
}

func allPersistent(items []workspace.Notification) bool {
	if len(items) == 0 {
		return false
	}
	for _, item := range items {
		if item.ID == 0 || item.WorkspaceID == "" || item.RecipientUserID == "" || item.EventKey == "" || item.DedupeKey == "" {
			return false
		}
	}
	return true
}

func scalarInt(ctx context.Context, db *sql.DB, query string, args ...any) (int, error) {
	var value int
	err := db.QueryRowContext(ctx, query, args...).Scan(&value)
	return value, err
}

func allTrue(checks map[string]bool) bool {
	if len(checks) == 0 {
		return false
	}
	for _, ok := range checks {
		if !ok {
			return false
		}
	}
	return true
}
