package billing

import "testing"

func TestResolveWorkspaceBillingState(t *testing.T) {
	active := &Subscription{Status: SubscriptionActive}
	overdue := &Subscription{Status: SubscriptionOverdue}
	canceled := &Subscription{Status: SubscriptionCanceled}
	cases := []struct {
		name  string
		sub   *Subscription
		order OrderStatus
		want  WorkspaceBillingState
	}{
		{"pending", active, OrderPending, WorkspaceBillingPaymentPending},
		{"processing", active, OrderProcessing, WorkspaceBillingProviderPartial},
		{"failed", active, OrderFailed, WorkspaceBillingPaymentFailed},
		{"overdue", overdue, OrderPaid, WorkspaceBillingOverdue},
		{"canceled", canceled, OrderPaid, WorkspaceBillingCanceled},
		{"active", active, OrderPaid, WorkspaceBillingActive},
		{"none", nil, "", WorkspaceBillingCanceled},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := resolveWorkspaceBillingState(tc.sub, tc.order); got != tc.want {
				t.Fatalf("state=%s want=%s", got, tc.want)
			}
		})
	}
}
