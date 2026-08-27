package admin

import (
	"bytes"
	"encoding/base32"
	"testing"
	"time"
)

func TestPermissionCatalogExactAndIndependent(t *testing.T) {
	want := []string{"platform.read", "admins.manage", "users.manage", "workspaces.manage", "links.manage", "domains.manage", "domains.risk.manage", "domains.entitlements.manage", "security.manage", "files.manage", "tickets.manage", "operations.manage", "billing.manage", "mail.manage", "settings.manage", "content.manage"}
	if len(PermissionCatalog) != len(want) {
		t.Fatalf("catalog length=%d want=%d", len(PermissionCatalog), len(want))
	}
	seen := map[string]bool{}
	for _, p := range PermissionCatalog {
		if !ValidPermission(p) {
			t.Fatalf("catalog contains invalid %q", p)
		}
		if seen[p] {
			t.Fatalf("duplicate %q", p)
		}
		seen[p] = true
	}
	for _, p := range want {
		if !seen[p] {
			t.Fatalf("missing %q", p)
		}
	}
	if ValidPermission("*") || ValidPermission("superuser") || ValidPermission("domains.*") {
		t.Fatal("wildcard/superuser authority must never be valid")
	}
	p := Principal{Permissions: map[string]struct{}{PermissionTicketsManage: {}}}
	if !p.Has(PermissionTicketsManage) {
		t.Fatal("expected tickets.manage")
	}
	if p.Has(PermissionDomainsEntitlementsManage) {
		t.Fatal("tickets.manage must not imply domains.entitlements.manage")
	}
	p = Principal{Permissions: map[string]struct{}{PermissionDomainsManage: {}}}
	if p.Has(PermissionDomainsRiskManage) || p.Has(PermissionDomainsEntitlementsManage) {
		t.Fatal("domains.manage must not imply risk/entitlement authority")
	}
	p = Principal{Permissions: map[string]struct{}{PermissionSecurityManage: {}}}
	if p.Has(PermissionAdminsManage) || p.Has(PermissionOperationsManage) || p.Has(PermissionDomainsEntitlementsManage) {
		t.Fatal("security.manage must not imply unrelated authority")
	}
}

func TestAuditJSONRejectsSecretOrUnregisteredFields(t *testing.T) {
	if _, err := safeAuditJSON(map[string]any{"status": "active", "permissions": []string{PermissionPlatformRead}}); err != nil {
		t.Fatalf("safe audit rejected: %v", err)
	}
	for _, key := range []string{"password", "token", "secret", "session_token", "oauth_secret", "private_path", "content"} {
		if _, err := safeAuditJSON(map[string]any{key: "x"}); err == nil {
			t.Fatalf("unsafe audit key %q accepted", key)
		}
	}
}

func TestPasswordHashAndTOTPAndCipher(t *testing.T) {
	hash, err := hashPassword("correct horse battery staple")
	if err != nil {
		t.Fatal(err)
	}
	if !verifyPassword(hash, "correct horse battery staple") {
		t.Fatal("password verification failed")
	}
	if verifyPassword(hash, "wrong") {
		t.Fatal("wrong password accepted")
	}
	key := bytes.Repeat([]byte{0x5a}, 32)
	cipher, err := NewSecretCipher("k1", key)
	if err != nil {
		t.Fatal(err)
	}
	secret, err := NewTOTPSecret()
	if err != nil {
		t.Fatal(err)
	}
	sealed, err := cipher.Encrypt(secret, "admin-totp:adm_test")
	if err != nil {
		t.Fatal(err)
	}
	plain, err := cipher.Decrypt(sealed, "k1", "admin-totp:adm_test")
	if err != nil || plain != secret {
		t.Fatalf("decrypt mismatch: %v", err)
	}
	sealed[len(sealed)-1] ^= 1
	if _, err := cipher.Decrypt(sealed, "k1", "admin-totp:adm_test"); err == nil {
		t.Fatal("tampered ciphertext accepted")
	}
	now := time.Unix(1700000000, 0).UTC()
	raw, err := decodeTOTPForTest(secret)
	if err != nil {
		t.Fatal(err)
	}
	code := totpCode(raw, uint64(now.Unix()/30))
	if !VerifyTOTP(secret, code, now) {
		t.Fatal("valid TOTP rejected")
	}
	if VerifyTOTP(secret, "000000", now) && code != "000000" {
		t.Fatal("invalid TOTP accepted")
	}
}

func decodeTOTPForTest(secret string) ([]byte, error) {
	return base32.StdEncoding.WithPadding(base32.NoPadding).DecodeString(secret)
}
