package main

import (
	"context"
	"time"

	adminaccess "github.com/Techshrr/GoJet/internal/admin"
	"github.com/Techshrr/GoJet/scripts/p17/adminfixture"
)

func runT021(ctx context.Context, runtime *adminfixture.Runtime) (output, error) {
	out := newOutput("real MySQL P17 workspace announcement producer reusing P12 owner membership, dedupe and safe deep-link notification authority")
	now := time.Date(2026, 8, 28, 1, 50, 0, 0, time.UTC)
	service, root, _, err := bootstrapCaseRoot(ctx, runtime, "T021", []string{adminaccess.PermissionContentManage}, now)
	if err != nil {
		return out, err
	}
	if err := seedWorkspace(ctx, runtime.DB, "ws-p17-t021", "P17 Notification Workspace", "user-p17-owner", "owner@p17.test", "user-p17-member", "member@p17.test", now.Add(time.Second)); err != nil {
		return out, err
	}
	item, _, err := service.CreateAnnouncement(ctx, root, adminaccess.CreateAnnouncementInput{Title: "Workspace notice", Summary: "A reviewed workspace event", Body: "Workspace announcement body.", Scope: "workspace", WorkspaceID: "ws-p17-t021"}, adminaccess.MutationAuthority{Reason: "create workspace announcement", CorrelationID: "p17-t021-create", IdempotencyKey: "p17-t021-create-key"}, now.Add(2*time.Second))
	if err != nil {
		return out, err
	}
	authority := adminaccess.MutationAuthority{Reason: "publish workspace announcement", CorrelationID: "p17-t021-publish", IdempotencyKey: "p17-t021-publish-key"}
	published, replayed, err := service.MutateAnnouncement(ctx, root, item.ID, adminaccess.AnnouncementActionInput{Action: "publish", ExpectedVersion: item.Version}, authority, now.Add(3*time.Second))
	if err != nil {
		return out, err
	}
	replayedItem, replayedAgain, err := service.MutateAnnouncement(ctx, root, item.ID, adminaccess.AnnouncementActionInput{Action: "publish", ExpectedVersion: item.Version}, authority, now.Add(4*time.Second))
	if err != nil {
		return out, err
	}
	var ownerCount, memberCount int
	if err := runtime.DB.QueryRowContext(ctx, `SELECT COUNT(*) FROM workspace_notifications WHERE workspace_id='ws-p17-t021' AND recipient_user_id='user-p17-owner' AND event_key='announcement.published'`).Scan(&ownerCount); err != nil {
		return out, err
	}
	if err := runtime.DB.QueryRowContext(ctx, `SELECT COUNT(*) FROM workspace_notifications WHERE workspace_id='ws-p17-t021' AND recipient_user_id='user-p17-member' AND event_key='announcement.published'`).Scan(&memberCount); err != nil {
		return out, err
	}
	var deepLink, resourceType, dedupeKey string
	if err := runtime.DB.QueryRowContext(ctx, `SELECT COALESCE(deep_link,''),resource_type,dedupe_key FROM workspace_notifications WHERE workspace_id='ws-p17-t021' AND recipient_user_id='user-p17-owner' AND event_key='announcement.published'`).Scan(&deepLink, &resourceType, &dedupeKey); err != nil {
		return out, err
	}
	var auditRows int
	if err := runtime.DB.QueryRowContext(ctx, `SELECT COUNT(*) FROM admin_audit_events WHERE action='admin.announcement.mutate' AND resource_id=?`, item.ID).Scan(&auditRows); err != nil {
		return out, err
	}
	out.RecordCounts["owner_notifications"] = ownerCount
	out.RecordCounts["member_notifications"] = memberCount
	out.RecordCounts["publish_audit_events"] = auditRows
	out.Checks["p12_owner_membership_reused"] = ownerCount == 1 && memberCount == 0 && published.NotificationCount == 1
	out.Checks["dedupe_and_idempotency_prevent_duplicates"] = !replayed && replayedAgain && replayedItem.ID == published.ID && ownerCount == 1
	out.Checks["safe_deep_link_reused"] = deepLink == "/app/notifications"
	out.Checks["notification_resource_is_non_authoritative"] = resourceType == "announcement" && dedupeKey == "p17-announcement-"+item.ID
	out.Checks["audit_remains_separate_authority"] = auditRows == 1
	pass(&out)
	return out, nil
}
