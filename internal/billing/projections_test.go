package billing

import "testing"

func TestBillingNotificationSpecs(t *testing.T) {
	t.Parallel()

	upgrade := billingNotificationSpecs(Order{Kind: OrderUpgrade}, TransactionPaid)
	if len(upgrade) != 2 || upgrade[0].EventKey != "payment_succeeded" || upgrade[1].EventKey != "plan_upgraded" {
		t.Fatalf("unexpected upgrade notification specs: %#v", upgrade)
	}

	failed := billingNotificationSpecs(Order{Kind: OrderNew}, TransactionFailed)
	if len(failed) != 1 || failed[0].EventKey != "payment_failed" {
		t.Fatalf("unexpected failed notification specs: %#v", failed)
	}

	refunded := billingNotificationSpecs(Order{Kind: OrderNew}, TransactionRefunded)
	if len(refunded) != 1 || refunded[0].EventKey != "refund_processed" {
		t.Fatalf("unexpected refund notification specs: %#v", refunded)
	}

	if got := billingNotificationSpecs(Order{Kind: OrderNew}, TransactionPending); len(got) != 0 {
		t.Fatalf("pending outcome must not produce a billing notification: %#v", got)
	}
}
