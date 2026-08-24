package billing

import (
	"errors"
	"testing"
	"time"
)

func TestValidateCallbackCommand(t *testing.T) {
	cmd := CallbackCommand{Provider: ProviderPayPal, ProviderEventID: "event", ProviderTransactionID: "txn", OrderID: "ord", EventType: "payment.paid", Outcome: TransactionPaid, Money: Money{Currency: "USD", AmountMinor: 100}, ReceivedAt: time.Now().UTC(), CorrelationID: "corr"}
	if err := validateCallbackCommand(cmd); err != nil {
		t.Fatal(err)
	}
	cmd.Provider = "unknown"
	if !errors.Is(validateCallbackCommand(cmd), ErrInvalidInput) {
		t.Fatal("unknown provider accepted")
	}
}

func TestTransactionTransitions(t *testing.T) {
	if !transactionTransitionAllowed(TransactionPending, TransactionPaid) {
		t.Fatal("pending->paid rejected")
	}
	if !transactionTransitionAllowed(TransactionPaid, TransactionRefunded) {
		t.Fatal("paid->refunded rejected")
	}
	if transactionTransitionAllowed(TransactionFailed, TransactionPaid) {
		t.Fatal("failed->paid accepted")
	}
	if transactionTransitionAllowed(TransactionRefunded, TransactionPaid) {
		t.Fatal("refunded->paid accepted")
	}
}

func TestSubscriptionIDDeterministic(t *testing.T) {
	a := subscriptionIDForOrder("ord_123")
	b := subscriptionIDForOrder("ord_123")
	if a != b || len(a) != 28 {
		t.Fatalf("a=%q b=%q", a, b)
	}
	if a == subscriptionIDForOrder("ord_456") {
		t.Fatal("collision in fixture")
	}
}

func TestTermEnd(t *testing.T) {
	start := time.Date(2026, 8, 24, 0, 0, 0, 0, time.UTC)
	monthly := termEndFor(BillingMonthly, start)
	if monthly == nil || !monthly.Equal(time.Date(2026, 9, 24, 0, 0, 0, 0, time.UTC)) {
		t.Fatalf("monthly=%v", monthly)
	}
	if termEndFor(BillingOneTime, start) != nil {
		t.Fatal("one-time term should be open")
	}
}
