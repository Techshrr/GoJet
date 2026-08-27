package main

import (
	"context"
	"errors"
	"time"

	adminaccess "github.com/Techshrr/GoJet/internal/admin"
	"github.com/Techshrr/GoJet/scripts/p17/adminfixture"
)

func runT019(ctx context.Context, runtime *adminfixture.Runtime) (output, error) {
	out := newOutput("real MySQL announcement draft/scheduled/published/archived lifecycle with content.manage, scope, cache generation and secret-safe audit")
	now := time.Date(2026, 8, 28, 1, 30, 0, 0, time.UTC)
	service, root, _, err := bootstrapCaseRoot(ctx, runtime, "T019", []string{adminaccess.PermissionContentManage}, now)
	if err != nil {
		return out, err
	}
	other, _, err := createScopedMFAAdmin(ctx, service, root, "T019", "settings-only", adminaccess.PermissionSettingsManage, now.Add(2*time.Second))
	if err != nil {
		return out, err
	}
	item, _, err := service.CreateAnnouncement(ctx, root, adminaccess.CreateAnnouncementInput{Title: "Platform maintenance", Summary: "Reviewed lifecycle fixture", Body: "Detailed announcement body not admitted to audit.", Scope: "global"}, adminaccess.MutationAuthority{Reason: "create reviewed announcement draft", CorrelationID: "p17-t019-create", IdempotencyKey: "p17-t019-create-key"}, now.Add(3*time.Second))
	if err != nil {
		return out, err
	}
	initialGeneration := item.CacheGeneration
	scheduledFor := now.Add(2 * time.Hour)
	item, _, err = service.MutateAnnouncement(ctx, root, item.ID, adminaccess.AnnouncementActionInput{Action: "schedule", ExpectedVersion: item.Version, ScheduledFor: &scheduledFor}, adminaccess.MutationAuthority{Reason: "schedule reviewed announcement", CorrelationID: "p17-t019-schedule", IdempotencyKey: "p17-t019-schedule-key"}, now.Add(4*time.Second))
	if err != nil {
		return out, err
	}
	scheduledGeneration := item.CacheGeneration
	item, _, err = service.MutateAnnouncement(ctx, root, item.ID, adminaccess.AnnouncementActionInput{Action: "publish", ExpectedVersion: item.Version}, adminaccess.MutationAuthority{Reason: "publish reviewed announcement", CorrelationID: "p17-t019-publish", IdempotencyKey: "p17-t019-publish-key"}, now.Add(5*time.Second))
	if err != nil {
		return out, err
	}
	publishedGeneration := item.CacheGeneration
	_, _, invalidTransition := service.MutateAnnouncement(ctx, root, item.ID, adminaccess.AnnouncementActionInput{Action: "schedule", ExpectedVersion: item.Version, ScheduledFor: &scheduledFor}, adminaccess.MutationAuthority{Reason: "invalid transition probe", CorrelationID: "p17-t019-invalid-transition", IdempotencyKey: "p17-t019-invalid-transition-key"}, now.Add(6*time.Second))
	item, _, err = service.MutateAnnouncement(ctx, root, item.ID, adminaccess.AnnouncementActionInput{Action: "archive", ExpectedVersion: item.Version}, adminaccess.MutationAuthority{Reason: "archive reviewed announcement", CorrelationID: "p17-t019-archive", IdempotencyKey: "p17-t019-archive-key"}, now.Add(7*time.Second))
	if err != nil {
		return out, err
	}
	_, _, deniedErr := service.CreateAnnouncement(ctx, other, adminaccess.CreateAnnouncementInput{Title: "Denied", Body: "Denied", Scope: "global"}, adminaccess.MutationAuthority{Reason: "permission probe", CorrelationID: "p17-t019-denied", IdempotencyKey: "p17-t019-denied-key"}, now.Add(8*time.Second))
	_, _, badScopeErr := service.CreateAnnouncement(ctx, root, adminaccess.CreateAnnouncementInput{Title: "Bad scope", Body: "Body", Scope: "workspace"}, adminaccess.MutationAuthority{Reason: "scope validation probe", CorrelationID: "p17-t019-scope", IdempotencyKey: "p17-t019-scope-key"}, now.Add(9*time.Second))
	var bodyLeaks int
	if err := runtime.DB.QueryRowContext(ctx, `SELECT COUNT(*) FROM admin_audit_events WHERE CAST(before_json AS CHAR) LIKE '%Detailed announcement body%' OR CAST(after_json AS CHAR) LIKE '%Detailed announcement body%' OR CAST(metadata_json AS CHAR) LIKE '%Detailed announcement body%'`).Scan(&bodyLeaks); err != nil {
		return out, err
	}
	var auditRows int
	if err := runtime.DB.QueryRowContext(ctx, `SELECT COUNT(*) FROM admin_audit_events WHERE resource_type='announcement'`).Scan(&auditRows); err != nil {
		return out, err
	}
	out.RecordCounts["announcements"] = 1
	out.RecordCounts["audit_events"] = auditRows
	out.Checks["lifecycle_reaches_archived"] = item.State == "archived" && item.PublishedAt != nil && item.ArchivedAt != nil && item.Version == 4
	out.Checks["invalid_transition_rejected"] = errors.Is(invalidTransition, adminaccess.ErrConflict)
	out.Checks["scope_validation_enforced"] = errors.Is(badScopeErr, adminaccess.ErrInvalid)
	out.Checks["content_manage_required"] = errors.Is(deniedErr, adminaccess.ErrForbidden)
	out.Checks["cache_generation_advances"] = initialGeneration < scheduledGeneration && scheduledGeneration < publishedGeneration && publishedGeneration < item.CacheGeneration
	out.Checks["audit_excludes_body"] = auditRows == 4 && bodyLeaks == 0
	pass(&out)
	return out, nil
}
