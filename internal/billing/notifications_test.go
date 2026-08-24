package billing

import (
	"testing"
	"time"
)

func TestExpiringNotificationDedupeKeyIsBoundaryScoped(t *testing.T) {
	at := time.Date(2026, 8, 31, 4, 5, 6, 7000, time.UTC)
	first := expiringNotificationDedupeKey("sub_abc", at)
	second := expiringNotificationDedupeKey("sub_abc", at)
	otherBoundary := expiringNotificationDedupeKey("sub_abc", at.Add(time.Microsecond))
	otherSubscription := expiringNotificationDedupeKey("sub_xyz", at)

	if first == "" || first != second {
		t.Fatalf("dedupe key is not deterministic: %q %q", first, second)
	}
	if first == otherBoundary {
		t.Fatal("dedupe key did not scope the effective boundary")
	}
	if first == otherSubscription {
		t.Fatal("dedupe key did not scope the subscription")
	}
}
