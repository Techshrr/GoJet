package workspace

import (
	"strings"
	"testing"
)

func TestInvitationTokenIsOpaqueAndHashed(t *testing.T) {
	token, hash, err := newInvitationToken()
	if err != nil {
		t.Fatal(err)
	}
	if len(token) < 40 || strings.Contains(hash, token) {
		t.Fatalf("unexpected token/hash shape")
	}
	if got := hashInvitationToken(token); got != hash {
		t.Fatalf("hash mismatch: %s != %s", got, hash)
	}
}

func TestP12RoleHelpers(t *testing.T) {
	if !canManageWorkspace(RoleOwner) || !canManageWorkspace(RoleAdmin) || canManageWorkspace(RoleMember) || canManageWorkspace(RoleViewer) {
		t.Fatal("workspace management role boundary drift")
	}
	if !canManageResources(RoleMember) || canManageResources(RoleViewer) {
		t.Fatal("resource management role boundary drift")
	}
	if validInvitationRole(RoleOwner) {
		t.Fatal("invitation must never grant owner")
	}
}

func TestNotificationRedactionAndDeepLinkNormalization(t *testing.T) {
	if got := redactNotificationText("token=secret"); got != "[redacted]" {
		t.Fatalf("secret not redacted: %q", got)
	}
	if got := normalizeDeepLink("https://evil.example/app"); got != "" {
		t.Fatalf("external deep link accepted: %q", got)
	}
	if got := normalizeDeepLink("/app/notifications"); got != "/app/notifications" {
		t.Fatalf("registered deep link rejected: %q", got)
	}
}
