package main

import (
	"context"
	"encoding/hex"
	"errors"
	"strings"
	"time"

	adminaccess "github.com/Techshrr/GoJet/internal/admin"
	"github.com/Techshrr/GoJet/scripts/p17/adminfixture"
)

func runT018(ctx context.Context, runtime *adminfixture.Runtime) (output, error) {
	out := newOutput("real MySQL Turnstile configuration encrypted at rest, settings.manage guarded and provider-error/incomplete states fail closed")
	now := time.Date(2026, 8, 28, 1, 20, 0, 0, time.UTC)
	service, root, _, err := bootstrapRoot(ctx, runtime, now)
	if err != nil {
		return out, err
	}
	secret := "p17-turnstile-secret-value-must-not-expose"
	item, replayed, err := service.UpdateTurnstileConfig(ctx, root, adminaccess.UpdateTurnstileInput{SiteKey: "0x4AAAA-p17-site", Secret: secret, Enabled: true, ProviderState: "healthy", ExpectedVersion: 0}, adminaccess.MutationAuthority{Reason: "reviewed bot protection", CorrelationID: "p17-t018-healthy", IdempotencyKey: "p17-t018-healthy-key"}, now.Add(time.Second))
	if err != nil {
		return out, err
	}
	var cipher []byte
	var keyID string
	if err := runtime.DB.QueryRowContext(ctx, `SELECT secret_ciphertext,secret_key_id FROM admin_turnstile_config WHERE id=1`).Scan(&cipher, &keyID); err != nil {
		return out, err
	}
	providerError, _, err := service.UpdateTurnstileConfig(ctx, root, adminaccess.UpdateTurnstileInput{SiteKey: item.SiteKey, Secret: "", Enabled: true, ProviderState: "provider_error", ExpectedVersion: item.Version}, adminaccess.MutationAuthority{Reason: "provider error observed", CorrelationID: "p17-t018-provider-error", IdempotencyKey: "p17-t018-provider-error-key"}, now.Add(2*time.Second))
	if err != nil {
		return out, err
	}
	_, other, _, err := createScopedMFAAdmin(ctx, service, root, "T018", "security-only", adminaccess.PermissionSecurityManage, now.Add(3*time.Second))
	if err != nil {
		return out, err
	}
	_, deniedErr := service.GetTurnstileConfig(ctx, other)
	var secretHits int
	if err := runtime.DB.QueryRowContext(ctx, `SELECT COUNT(*) FROM admin_audit_events WHERE CAST(before_json AS CHAR) LIKE ? OR CAST(after_json AS CHAR) LIKE ? OR CAST(metadata_json AS CHAR) LIKE ?`, "%"+secret+"%", "%"+secret+"%", "%"+secret+"%").Scan(&secretHits); err != nil {
		return out, err
	}
	cipherHex := hex.EncodeToString(cipher)
	out.RecordCounts["turnstile_versions"] = int(providerError.Version)
	out.Checks["settings_manage_required"] = errors.Is(deniedErr, adminaccess.ErrForbidden)
	out.Checks["healthy_configuration_available"] = item.Enabled && item.SecretConfigured && item.ProtectionAvailable && !item.FailClosed && !replayed
	out.Checks["provider_error_preserves_fail_closed"] = providerError.Enabled && providerError.SecretConfigured && !providerError.ProtectionAvailable && providerError.FailClosed
	out.Checks["secret_encrypted_at_rest"] = len(cipher) > 0 && keyID != "" && !strings.Contains(cipherHex, hex.EncodeToString([]byte(secret)))
	out.Checks["secret_never_projected_or_audited"] = !strings.Contains(strings.ToLower(string(mustJSON(item))), "turnstile-secret") && secretHits == 0
	pass(&out)
	return out, nil
}
