package admin

import (
	"testing"
	"time"
)

func TestNormalizeWorkspaceWebhookEvents(t *testing.T) {
	got, err := normalizeWorkspaceWebhookEvents([]string{"link.updated", "link.created", "link.updated"})
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 2 || got[0] != "link.created" || got[1] != "link.updated" {
		t.Fatalf("unexpected normalized events: %#v", got)
	}
	for _, invalid := range [][]string{{}, {"*"}, {"link.*"}, {"bad event"}, {"\n"}} {
		if _, err := normalizeWorkspaceWebhookEvents(invalid); err == nil {
			t.Fatalf("expected invalid events %#v", invalid)
		}
	}
}

func TestWorkspaceWebhookSigningCanonicalAndSecretBound(t *testing.T) {
	timestamp := time.Unix(1770000000, 0).UTC()
	body := []byte(`{"id":"evt_1","type":"link.updated"}`)
	sig := SignWorkspaceWebhookDelivery("secret-a", "whd_1", timestamp, body)
	if sig == "" || sig[:3] != "v1=" {
		t.Fatalf("unexpected signature %q", sig)
	}
	if !VerifyWorkspaceWebhookDeliverySignature("secret-a", "whd_1", timestamp, body, sig) {
		t.Fatal("canonical signature did not verify")
	}
	if VerifyWorkspaceWebhookDeliverySignature("secret-b", "whd_1", timestamp, body, sig) {
		t.Fatal("different secret verified")
	}
	if VerifyWorkspaceWebhookDeliverySignature("secret-a", "whd_2", timestamp, body, sig) {
		t.Fatal("different delivery id verified")
	}
	if VerifyWorkspaceWebhookDeliverySignature("secret-a", "whd_1", timestamp.Add(time.Second), body, sig) {
		t.Fatal("different timestamp verified")
	}
	if VerifyWorkspaceWebhookDeliverySignature("secret-a", "whd_1", timestamp, append(body, ' '), sig) {
		t.Fatal("different body verified")
	}
}

func TestWorkspaceWebhookRetryDelayBounded(t *testing.T) {
	want := []time.Duration{time.Minute, 5 * time.Minute, 15 * time.Minute, time.Hour, time.Hour}
	for i, expected := range want {
		if got := workspaceWebhookRetryDelay(i + 1); got != expected {
			t.Fatalf("attempt %d delay=%s want=%s", i+1, got, expected)
		}
	}
}
