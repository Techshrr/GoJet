package main

import (
	"bytes"
	"context"
	"errors"
	"time"

	adminaccess "github.com/Techshrr/GoJet/internal/admin"
	"github.com/Techshrr/GoJet/scripts/p17/adminfixture"
)

func runT018(ctx context.Context, runtime *adminfixture.Runtime) (output, error) {
	out := newOutput("real MySQL encrypted Turnstile configuration with exact settings.manage and fail-closed incomplete/provider-error projection")
	now := time.Date(2026, 8, 28, 1, 20, 0, 0, time.UTC)
	service, root, _, err := bootstrapCaseRoot(ctx, runtime, "T018", []string{adminaccess.PermissionSettingsManage}, now)
	if err != nil {
		return out, err
	}
	other, _, err := createScopedMFAAdmin(ctx, service, root, "T018", "content-only", adminaccess.PermissionContentManage, now.Add(2*time.Second))
	if err != nil {
		return out, err
	}
	secret := "p17-turnstile-fixture-secret-value"
	item, _, err := service.UpdateTurnstileConfig(ctx, root, adminaccess.UpdateTurnstileInput{SiteKey: "site-key-p17", Secret: secret, Enabled: true, ProviderState: "healthy", ExpectedVersion: 0}, adminaccess.MutationAuthority{Reason: "configure reviewed bot protection", CorrelationID: "p17-t018-healthy", IdempotencyKey: "p17-t018-healthy-key"}, now.Add(3*time.Second))
	if err != nil {
		return out, err
	}
	var cipher []byte
	if err := runtime.DB.QueryRowContext(ctx, `SELECT secret_ciphertext FROM admin_turnstile_config WHERE id=1`).Scan(&cipher); err != nil {
		return out, err
	}
	storedEncrypted := len(cipher) > 0 && !bytes.Contains(cipher, []byte(secret))
	item, _, err = service.UpdateTurnstileConfig(ctx, root, adminaccess.UpdateTurnstileInput{SiteKey: "", Enabled: true, ProviderState: "incomplete", ExpectedVersion: item.Version}, adminaccess.MutationAuthority{Reason: "exercise incomplete configuration fail closed", CorrelationID: "p17-t018-incomplete", IdempotencyKey: "p17-t018-incomplete-key"}, now.Add(4*time.Second))
	if err != nil {
		return out, err
	}
	incompleteFailClosed := item.FailClosed && !item.ProtectionAvailable && item.SecretConfigured
	item, _, err = service.UpdateTurnstileConfig(ctx, root, adminaccess.UpdateTurnstileInput{SiteKey: "site-key-p17", Enabled: true, ProviderState: "provider_error", ExpectedVersion: item.Version}, adminaccess.MutationAuthority{Reason: "exercise provider error fail closed", CorrelationID: "p17-t018-provider", IdempotencyKey: "p17-t018-provider-key"}, now.Add(5*time.Second))
	if err != nil {
		return out, err
	}
	loaded, err := service.GetTurnstileConfig(ctx, root)
	if err != nil {
		return out, err
	}
	_, _, deniedErr := service.UpdateTurnstileConfig(ctx, other, adminaccess.UpdateTurnstileInput{SiteKey: "x", Enabled: false, ProviderState: "incomplete", ExpectedVersion: loaded.Version}, adminaccess.MutationAuthority{Reason: "permission probe", CorrelationID: "p17-t018-denied", IdempotencyKey: "p17-t018-denied-key"}, now.Add(6*time.Second))
	var leaked int
	if err := runtime.DB.QueryRowContext(ctx, `SELECT COUNT(*) FROM admin_audit_events WHERE CAST(before_json AS CHAR) LIKE ? OR CAST(after_json AS CHAR) LIKE ? OR CAST(metadata_json AS CHAR) LIKE ?`, "%"+secret+"%", "%"+secret+"%", "%"+secret+"%").Scan(&leaked); err != nil {
		return out, err
	}
	out.RecordCounts["turnstile_rows"] = 1
	out.Checks["secret_encrypted_at_rest"] = storedEncrypted
	out.Checks["read_projection_masks_secret"] = loaded.SecretConfigured && loaded.ProviderState == "provider_error"
	out.Checks["incomplete_fail_closed"] = incompleteFailClosed
	out.Checks["provider_error_fail_closed"] = loaded.FailClosed && !loaded.ProtectionAvailable
	out.Checks["settings_manage_required"] = errors.Is(deniedErr, adminaccess.ErrForbidden)
	out.Checks["audit_contains_no_secret"] = leaked == 0
	pass(&out)
	return out, nil
}
