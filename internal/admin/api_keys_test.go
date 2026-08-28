package admin

import (
	"errors"
	"reflect"
	"testing"
	"time"
)

func TestNormalizeWorkspaceAPIKeyScopes(t *testing.T) {
	got, err := normalizeWorkspaceAPIKeyScopes([]string{"links:write", "links:read", "links:read"})
	if err != nil {
		t.Fatal(err)
	}
	want := []string{"links:read", "links:write"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("got %v want %v", got, want)
	}
	for _, scopes := range [][]string{{"*"}, {"links:*"}, {""}, {"links:read write"}} {
		if _, err := normalizeWorkspaceAPIKeyScopes(scopes); !errors.Is(err, ErrInvalid) {
			t.Fatalf("scope %v should fail: %v", scopes, err)
		}
	}
}
func TestValidateWorkspaceAPIKeyInput(t *testing.T) {
	now := time.Date(2026, 8, 28, 0, 0, 0, 0, time.UTC)
	exp := now.Add(time.Hour)
	got, err := validateWorkspaceAPIKeyInput(WorkspaceAPIKeyInput{Name: " CI ", Scopes: []string{"links:read"}, ExpiresAt: &exp, RateLimitPerMinute: 2}, now)
	if err != nil {
		t.Fatal(err)
	}
	if got.Name != "CI" || got.RateLimitPerMinute != 2 {
		t.Fatalf("unexpected %#v", got)
	}
	past := now.Add(-time.Second)
	_, err = validateWorkspaceAPIKeyInput(WorkspaceAPIKeyInput{Name: "x", Scopes: []string{"links:read"}, ExpiresAt: &past, RateLimitPerMinute: 1}, now)
	if !errors.Is(err, ErrInvalid) {
		t.Fatalf("expired input should fail: %v", err)
	}
}
func TestAPIKeyPrefixDoesNotExposeFullSecret(t *testing.T) {
	secret := "gak_abcdefghijklmnopqrstuvwxyz0123456789"
	prefix := apiKeyPrefix(secret)
	if prefix == secret || len(prefix) > 12 {
		t.Fatalf("unsafe prefix %q", prefix)
	}
}
