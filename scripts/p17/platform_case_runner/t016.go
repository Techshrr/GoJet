package main

import (
	"context"
	"errors"
	"strings"
	"time"

	adminaccess "github.com/Techshrr/GoJet/internal/admin"
	"github.com/Techshrr/GoJet/scripts/p17/adminfixture"
)

func runT016(ctx context.Context, runtime *adminfixture.Runtime) (output, error) {
	out := newOutput("real MySQL settings/brand governance with exact settings.manage, validation, version conflict, idempotency and secret-safe audit")
	now := time.Date(2026, 8, 28, 1, 0, 0, 0, time.UTC)
	service, root, _, err := bootstrapRoot(ctx, runtime, now)
	if err != nil {
		return out, err
	}
	input := adminaccess.UpdatePlatformSettingInput{Value: map[string]string{"site_name": "GoJet", "public_base_url": "https://gojet.cc", "support_url": "https://gojet.cc/support"}, ExpectedVersion: 0}
	authority := adminaccess.MutationAuthority{Reason: "reviewed public settings", CorrelationID: "p17-t016-general", IdempotencyKey: "p17-t016-general-key"}
	item, replayed, err := service.UpdatePlatformSetting(ctx, root, "general", input, authority, now.Add(time.Second))
	if err != nil {
		return out, err
	}
	replay, replayedAgain, err := service.UpdatePlatformSetting(ctx, root, "general", input, authority, now.Add(2*time.Second))
	if err != nil {
		return out, err
	}
	_, _, conflictErr := service.UpdatePlatformSetting(ctx, root, "general", adminaccess.UpdatePlatformSettingInput{Value: input.Value, ExpectedVersion: 0}, adminaccess.MutationAuthority{Reason: "stale settings probe", CorrelationID: "p17-t016-conflict", IdempotencyKey: "p17-t016-conflict-key"}, now.Add(3*time.Second))
	_, _, validationErr := service.UpdatePlatformSetting(ctx, root, "brand", adminaccess.UpdatePlatformSettingInput{Value: map[string]string{"logo_path": "/assets/../private/token.svg", "favicon_path": "/assets/favicon.svg"}, ExpectedVersion: 0}, adminaccess.MutationAuthority{Reason: "asset validation probe", CorrelationID: "p17-t016-validation", IdempotencyKey: "p17-t016-validation-key"}, now.Add(4*time.Second))
	_, other, _, err := createScopedMFAAdmin(ctx, service, root, "T016", "content-only", adminaccess.PermissionContentManage, now.Add(5*time.Second))
	if err != nil {
		return out, err
	}
	_, deniedErr := service.GetPlatformSetting(ctx, other, "general")
	var auditRows, secretHits int
	if err := runtime.DB.QueryRowContext(ctx, `SELECT COUNT(*) FROM admin_audit_events WHERE action='admin.platform.setting.update'`).Scan(&auditRows); err != nil {
		return out, err
	}
	if err := runtime.DB.QueryRowContext(ctx, `SELECT COUNT(*) FROM admin_audit_events WHERE LOWER(CAST(metadata_json AS CHAR)) LIKE '%secret%' OR LOWER(CAST(after_json AS CHAR)) LIKE '%token%'`).Scan(&secretHits); err != nil {
		return out, err
	}
	out.RecordCounts["settings"] = 1
	out.RecordCounts["audit_events"] = auditRows
	out.Checks["settings_manage_required"] = errors.Is(deniedErr, adminaccess.ErrForbidden)
	out.Checks["reviewed_setting_persisted"] = item.Key == "general" && item.Value["site_name"] == "GoJet" && item.Version == 1
	out.Checks["idempotency_replay_exact"] = !replayed && replayedAgain && replay.Version == item.Version
	out.Checks["stale_version_conflicts"] = errors.Is(conflictErr, adminaccess.ErrConflict)
	out.Checks["brand_asset_validation_rejects_traversal_and_secret_path"] = errors.Is(validationErr, adminaccess.ErrInvalid)
	out.Checks["audit_is_secret_safe"] = auditRows == 1 && secretHits == 0 && !strings.Contains(strings.ToLower(authority.Reason), "secret")
	pass(&out)
	return out, nil
}
