package main

import (
	"context"
	"errors"
	"time"

	adminaccess "github.com/Techshrr/GoJet/internal/admin"
	"github.com/Techshrr/GoJet/scripts/p17/adminfixture"
)

func runT016(ctx context.Context, runtime *adminfixture.Runtime) (output, error) {
	out := newOutput("real MySQL platform settings/brand governance with exact settings.manage, validation, optimistic conflict and secret-safe audit")
	now := time.Date(2026, 8, 28, 1, 0, 0, 0, time.UTC)
	service, root, _, err := bootstrapCaseRoot(ctx, runtime, "T016", []string{adminaccess.PermissionSettingsManage}, now)
	if err != nil {
		return out, err
	}
	other, _, err := createScopedMFAAdmin(ctx, service, root, "T016", "content-only", adminaccess.PermissionContentManage, now.Add(2*time.Second))
	if err != nil {
		return out, err
	}

	general, replayed, err := service.UpdatePlatformSetting(ctx, root, "general", adminaccess.UpdatePlatformSettingInput{Value: map[string]string{"site_name": "GoJet", "public_base_url": "https://gojet.cc", "support_url": "https://gojet.cc/support"}, ExpectedVersion: 0}, adminaccess.MutationAuthority{Reason: "review general platform settings", CorrelationID: "p17-t016-general", IdempotencyKey: "p17-t016-general-key"}, now.Add(3*time.Second))
	if err != nil {
		return out, err
	}
	brand, _, err := service.UpdatePlatformSetting(ctx, root, "brand", adminaccess.UpdatePlatformSettingInput{Value: map[string]string{"logo_path": "/assets/brand/logo.svg", "favicon_path": "/assets/brand/favicon.ico"}, ExpectedVersion: 0}, adminaccess.MutationAuthority{Reason: "review brand assets", CorrelationID: "p17-t016-brand", IdempotencyKey: "p17-t016-brand-key"}, now.Add(4*time.Second))
	if err != nil {
		return out, err
	}
	_, _, invalidErr := service.UpdatePlatformSetting(ctx, root, "brand", adminaccess.UpdatePlatformSettingInput{Value: map[string]string{"logo_path": "https://evil.test/logo.svg", "favicon_path": "/assets/favicon.ico"}, ExpectedVersion: brand.Version}, adminaccess.MutationAuthority{Reason: "invalid asset probe", CorrelationID: "p17-t016-invalid", IdempotencyKey: "p17-t016-invalid-key"}, now.Add(5*time.Second))
	_, _, conflictErr := service.UpdatePlatformSetting(ctx, root, "general", adminaccess.UpdatePlatformSettingInput{Value: map[string]string{"site_name": "GoJet", "public_base_url": "https://gojet.cc", "support_url": "https://gojet.cc/help"}, ExpectedVersion: 0}, adminaccess.MutationAuthority{Reason: "stale version probe", CorrelationID: "p17-t016-conflict", IdempotencyKey: "p17-t016-conflict-key"}, now.Add(6*time.Second))
	_, _, deniedErr := service.UpdatePlatformSetting(ctx, other, "general", adminaccess.UpdatePlatformSettingInput{Value: map[string]string{"site_name": "Other", "public_base_url": "https://gojet.cc", "support_url": "https://gojet.cc/help"}, ExpectedVersion: general.Version}, adminaccess.MutationAuthority{Reason: "cross permission probe", CorrelationID: "p17-t016-denied", IdempotencyKey: "p17-t016-denied-key"}, now.Add(7*time.Second))
	loaded, err := service.GetPlatformSetting(ctx, root, "brand")
	if err != nil {
		return out, err
	}
	var auditRows int
	if err := runtime.DB.QueryRowContext(ctx, `SELECT COUNT(*) FROM admin_audit_events WHERE action='admin.platform.setting.update'`).Scan(&auditRows); err != nil {
		return out, err
	}
	var leaked int
	if err := runtime.DB.QueryRowContext(ctx, `SELECT COUNT(*) FROM admin_audit_events WHERE CAST(before_json AS CHAR) LIKE '%logo.svg%' OR CAST(after_json AS CHAR) LIKE '%logo.svg%'`).Scan(&leaked); err != nil {
		return out, err
	}
	out.RecordCounts["settings"] = 2
	out.RecordCounts["audit_events"] = auditRows
	out.Checks["settings_manage_applied"] = general.Version == 1 && brand.Version == 1 && !replayed
	out.Checks["brand_assets_validated"] = loaded.Value["logo_path"] == "/assets/brand/logo.svg" && errors.Is(invalidErr, adminaccess.ErrInvalid)
	out.Checks["optimistic_conflict_enforced"] = errors.Is(conflictErr, adminaccess.ErrConflict)
	out.Checks["unrelated_permission_denied"] = errors.Is(deniedErr, adminaccess.ErrForbidden)
	out.Checks["audit_accountable_and_asset_safe"] = auditRows == 2 && leaked == 0
	pass(&out)
	return out, nil
}
