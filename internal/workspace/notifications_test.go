package workspace

import "testing"

func TestNotificationSensitiveContextBoundary(t *testing.T) {
	t.Parallel()
	sensitive := []string{
		"victim@example.test",
		"Bearer abcdefghijklmnop",
		"eyJabcdefghijk.abcdefghijklmnop.abcdefghijklmnop",
		"token=p12-secret",
		"risk_evidence=private",
	}
	for _, value := range sensitive {
		if !notificationContainsSensitiveData(value) {
			t.Fatalf("expected sensitive value to be detected: %q", value)
		}
		if got := redactNotificationText(value); got != "[redacted]" {
			t.Fatalf("expected redaction for %q, got %q", value, got)
		}
	}

	safe := []string{"domains.certificate.expiring", "resource:link:123", "Workspace notice"}
	for _, value := range safe {
		if notificationContainsSensitiveData(value) {
			t.Fatalf("safe value classified sensitive: %q", value)
		}
	}
}

func TestNormalizeDeepLinkRejectsSecretBearingOrNonPathTargets(t *testing.T) {
	t.Parallel()
	for _, value := range []string{
		"https://evil.example/path",
		"//evil.example/path",
		"/app/notifications?token=secret",
		"/app/links/1#fragment",
		"/app/links/victim@example.test",
		`/app/links/1\\escape`,
	} {
		if got := normalizeDeepLink(value); got != "" {
			t.Fatalf("expected deep link %q to be rejected, got %q", value, got)
		}
	}

	for _, value := range []string{"/app", "/app/notifications", "/app/settings/workspace", "/app/billing", "/app/links/123"} {
		if got := normalizeDeepLink(value); got != value {
			t.Fatalf("expected deep link %q to survive normalization, got %q", value, got)
		}
	}
}
