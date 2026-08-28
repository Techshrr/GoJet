package main

import (
	"context"
	"errors"
	"time"

	adminaccess "github.com/Techshrr/GoJet/internal/admin"
	"github.com/Techshrr/GoJet/scripts/p17/adminfixture"
)

func runT023(ctx context.Context, runtime *adminfixture.Runtime) (map[string]bool, map[string]int, error) {
	now := time.Date(2026, 8, 28, 8, 23, 0, 0, time.UTC)
	workspaceID := "ws-p17-t023"
	if err := seedWorkspaceRoles(ctx, runtime.DB, workspaceID, map[string]string{"owner-t023": "owner"}, now); err != nil {
		return nil, nil, err
	}
	authority, err := adminaccess.NewWorkspaceAPIKeyAuthority(runtime.DB, runtime.Redis)
	if err != nil {
		return nil, nil, err
	}
	created, err := authority.Create(ctx, workspaceID, "owner-t023", adminaccess.WorkspaceAPIKeyInput{Name: "lifecycle", Scopes: []string{"links:read"}, RateLimitPerMinute: 5}, "p17-t023-create", now)
	if err != nil {
		return nil, nil, err
	}
	_, beforeRotateErr := authority.Authenticate(ctx, created.Secret, "links:read", now.Add(time.Second))
	rotated, err := authority.Rotate(ctx, workspaceID, "owner-t023", created.Key.ID, "p17-t023-rotate", now.Add(2*time.Second))
	if err != nil {
		return nil, nil, err
	}
	_, oldSecretErr := authority.Authenticate(ctx, created.Secret, "links:read", now.Add(3*time.Second))
	_, newSecretErr := authority.Authenticate(ctx, rotated.Secret, "links:read", now.Add(3*time.Second))
	_, wrongScopeErr := authority.Authenticate(ctx, rotated.Secret, "links:write", now.Add(3*time.Second))

	rateKey, err := authority.Create(ctx, workspaceID, "owner-t023", adminaccess.WorkspaceAPIKeyInput{Name: "rate", Scopes: []string{"links:read"}, RateLimitPerMinute: 1}, "p17-t023-rate", now.Add(4*time.Second))
	if err != nil {
		return nil, nil, err
	}
	_, firstRateErr := authority.Authenticate(ctx, rateKey.Secret, "links:read", now.Add(5*time.Second))
	_, secondRateErr := authority.Authenticate(ctx, rateKey.Secret, "links:read", now.Add(6*time.Second))

	expiresAt := now.Add(30 * time.Second)
	expiringKey, err := authority.Create(ctx, workspaceID, "owner-t023", adminaccess.WorkspaceAPIKeyInput{Name: "expiry", Scopes: []string{"links:read"}, ExpiresAt: &expiresAt, RateLimitPerMinute: 2}, "p17-t023-expiry", now.Add(7*time.Second))
	if err != nil {
		return nil, nil, err
	}
	_, beforeExpiryErr := authority.Authenticate(ctx, expiringKey.Secret, "links:read", now.Add(10*time.Second))
	_, afterExpiryErr := authority.Authenticate(ctx, expiringKey.Secret, "links:read", now.Add(31*time.Second))

	if _, err := authority.Revoke(ctx, workspaceID, "owner-t023", created.Key.ID, "p17-t023-revoke", now.Add(8*time.Second)); err != nil {
		return nil, nil, err
	}
	_, revokedErr := authority.Authenticate(ctx, rotated.Secret, "links:read", now.Add(9*time.Second))

	rotateAudit, err := scalar(ctx, runtime.DB, `SELECT COUNT(*) FROM workspace_audit_events WHERE workspace_id=? AND action='api_key.rotate' AND result='success'`, workspaceID)
	if err != nil {
		return nil, nil, err
	}
	revokeAudit, err := scalar(ctx, runtime.DB, `SELECT COUNT(*) FROM workspace_audit_events WHERE workspace_id=? AND action='api_key.revoke' AND result='success'`, workspaceID)
	if err != nil {
		return nil, nil, err
	}
	keyRows, err := scalar(ctx, runtime.DB, `SELECT COUNT(*) FROM workspace_api_keys WHERE workspace_id=?`, workspaceID)
	if err != nil {
		return nil, nil, err
	}
	checks := map[string]bool{
		"old_secret_invalid_after_rotate": beforeRotateErr == nil && errors.Is(oldSecretErr, adminaccess.ErrUnauthorized) && newSecretErr == nil && created.Secret != rotated.Secret,
		"revoke_immediate":                errors.Is(revokedErr, adminaccess.ErrUnauthorized),
		"expiry_immediate":                beforeExpiryErr == nil && errors.Is(afterExpiryErr, adminaccess.ErrUnauthorized),
		"scope_server_enforced":           errors.Is(wrongScopeErr, adminaccess.ErrForbidden),
		"rate_server_enforced":            firstRateErr == nil && errors.Is(secondRateErr, adminaccess.ErrRateLimited),
		"lifecycle_audited":               rotateAudit == 1 && revokeAudit == 1,
	}
	counts := map[string]int{"api_keys": keyRows, "rotate_audit_events": rotateAudit, "revoke_audit_events": revokeAudit}
	return checks, counts, nil
}
