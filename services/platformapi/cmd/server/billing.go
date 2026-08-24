package main

import (
	"context"
	"database/sql"
	"encoding/json"
	"io"
	"net/http"
	"os"
	"strings"
	"time"

	"github.com/Techshrr/GoJet/internal/billing"
	"github.com/Techshrr/GoJet/internal/workspace"
)

type billingPrincipalResolver struct {
	testAuth bool
}

func (r billingPrincipalResolver) ResolvePrincipal(req *http.Request) (billing.RequestPrincipal, error) {
	if !r.testAuth {
		return billing.RequestPrincipal{}, billing.ErrAuthenticationUnavailable
	}
	principal := billing.RequestPrincipal{
		UserID:      strings.TrimSpace(req.Header.Get("X-GoJet-Test-Actor")),
		Email:       strings.TrimSpace(req.Header.Get("X-GoJet-Test-Email")),
		DisplayName: strings.TrimSpace(req.Header.Get("X-GoJet-Test-Display-Name")),
	}
	if principal.UserID == "" || principal.Email == "" {
		return billing.RequestPrincipal{}, billing.ErrAuthenticationRequired
	}
	return principal, nil
}

type billingMembershipResolver struct {
	store *workspace.Store
}

func (r billingMembershipResolver) ResolveWorkspaceRole(ctx context.Context, workspaceID, userID string) (string, error) {
	membership, err := r.store.GetMembership(ctx, workspaceID, userID)
	if err != nil {
		return "", err
	}
	return membership.Role, nil
}

type deterministicBillingCallbackVerifier struct {
	verifier billing.DeterministicTestVerifier
}

type deterministicBillingCallbackPayload struct {
	EventID       string `json:"event_id"`
	TransactionID string `json:"transaction_id"`
	OrderID       string `json:"order_id"`
	EventType     string `json:"event_type"`
	Outcome       string `json:"outcome"`
	Currency      string `json:"currency"`
	AmountMinor   int64  `json:"amount_minor"`
	ReceivedAt    string `json:"received_at"`
	CorrelationID string `json:"correlation_id"`
}

func (v deterministicBillingCallbackVerifier) VerifyAndNormalize(req *http.Request, provider billing.Provider) (billing.CallbackCommand, error) {
	const maxBody = 1 << 20
	raw, err := io.ReadAll(io.LimitReader(req.Body, maxBody+1))
	if err != nil || len(raw) == 0 || len(raw) > maxBody {
		return billing.CallbackCommand{}, billing.ErrCallbackUnauthorized
	}
	var payload deterministicBillingCallbackPayload
	decoderErr := json.Unmarshal(raw, &payload)
	if decoderErr != nil {
		return billing.CallbackCommand{}, billing.ErrCallbackUnauthorized
	}
	signature := strings.TrimSpace(req.Header.Get("X-GoJet-Test-Callback-Signature"))
	if err := v.verifier.Verify(provider, strings.TrimSpace(payload.EventID), strings.TrimSpace(payload.TransactionID), raw, signature); err != nil {
		return billing.CallbackCommand{}, billing.ErrCallbackUnauthorized
	}
	receivedAt, err := time.Parse(time.RFC3339Nano, strings.TrimSpace(payload.ReceivedAt))
	if err != nil {
		return billing.CallbackCommand{}, billing.ErrCallbackUnauthorized
	}
	return billing.CallbackCommand{
		Provider: provider, ProviderEventID: strings.TrimSpace(payload.EventID),
		ProviderTransactionID: strings.TrimSpace(payload.TransactionID), OrderID: strings.TrimSpace(payload.OrderID),
		EventType: strings.TrimSpace(payload.EventType), Outcome: billing.TransactionStatus(strings.TrimSpace(payload.Outcome)),
		Money:      billing.Money{Currency: strings.TrimSpace(payload.Currency), AmountMinor: payload.AmountMinor},
		ReceivedAt: receivedAt.UTC(), CorrelationID: strings.TrimSpace(payload.CorrelationID),
	}, nil
}

func buildBillingHandler(db *sql.DB, testAuth bool) (http.Handler, bool, error) {
	if os.Getenv("GOJET_BILLING_ENABLED") != "1" {
		return nil, false, nil
	}
	store := billing.NewStore(db)
	membershipStore := workspace.NewStore(db)
	var callbackVerifier billing.CallbackRequestVerifier
	if testAuth && os.Getenv("GOJET_TEST_BILLING_CALLBACKS_ENABLED") == "1" {
		secret := []byte(os.Getenv("GOJET_TEST_BILLING_CALLBACK_SECRET"))
		if len(secret) < 16 {
			return nil, false, billing.ErrCallbackUnavailable
		}
		secrets := make(map[billing.Provider][]byte, len(billing.FrozenProviders()))
		for _, provider := range billing.FrozenProviders() {
			secrets[provider] = append([]byte(nil), secret...)
		}
		callbackVerifier = deterministicBillingCallbackVerifier{verifier: billing.DeterministicTestVerifier{Secrets: secrets}}
	}
	api := billing.NewAPI(
		store,
		billingPrincipalResolver{testAuth: testAuth},
		billingMembershipResolver{store: membershipStore},
		callbackVerifier,
	)
	return api.Handler(), true, nil
}

func mountBillingRoutes(root *http.ServeMux, handler http.Handler) {
	patterns := []string{
		"GET /api/public/plans",
		"POST /api/workspaces/{workspaceId}/orders",
		"GET /api/workspaces/{workspaceId}/orders/{orderId}",
		"GET /api/workspaces/{workspaceId}/billing/entitlements/{capability}",
		"POST /api/workspaces/{workspaceId}/billing/downgrade",
		"POST /api/payments/callbacks/{provider}",
	}
	for _, pattern := range patterns {
		root.Handle(pattern, handler)
	}
}
