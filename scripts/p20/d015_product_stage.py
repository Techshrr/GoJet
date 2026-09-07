#!/usr/bin/env python3
from __future__ import annotations

import json
import subprocess
from pathlib import Path

BASE = "11b0d924bdda4f0166f9292af4433ed1fa056b45"


def require(condition: bool, message: str) -> None:
    if not condition:
        raise SystemExit(message)


def main() -> int:
    subprocess.run(["git", "merge-base", "--is-ancestor", BASE, "HEAD"], check=True)

    ledger = json.loads(Path("artifacts/v10/P20/defect-ledger.json").read_text(encoding="utf-8"))
    require([row.get("id") for row in ledger.get("open", [])] == ["P20-D015"], "D015 is not the sole open blocker")
    require(ledger["open"][0].get("issue") == 108, "D015 canonical issue drift")

    billing_path = Path("services/platformapi/cmd/server/billing.go")
    billing_text = billing_path.read_text(encoding="utf-8")
    old_import = '\t"github.com/Techshrr/GoJet/internal/workspace"\n)'
    new_import = '\t"github.com/Techshrr/GoJet/internal/workspace"\n\t"github.com/redis/go-redis/v9"\n)'
    require(old_import in billing_text and new_import not in billing_text, "billing import boundary drift")
    billing_text = billing_text.replace(old_import, new_import, 1)

    old_builder = '''func buildBillingHandler(db *sql.DB, testAuth bool) (http.Handler, bool, error) {
\tif os.Getenv("GOJET_BILLING_ENABLED") != "1" {
\t\treturn nil, false, nil
\t}
\tstore := billing.NewStore(db)
\tmembershipStore := workspace.NewStore(db)
\tprincipalResolver := billingPrincipalResolver{testAuth: testAuth}
\tmembershipResolver := billingMembershipResolver{store: membershipStore}
'''
    new_builder = '''func buildBillingHandler(db *sql.DB, redisClient *redis.Client, testAuth bool) (http.Handler, bool, error) {
\tif os.Getenv("GOJET_BILLING_ENABLED") != "1" {
\t\treturn nil, false, nil
\t}
\tstore := billing.NewStore(db)
\tmembershipStore := workspace.NewStore(db)
\tvar principalResolver billing.PrincipalResolver
\tif testAuth {
\t\tprincipalResolver = billingPrincipalResolver{testAuth: true}
\t} else {
\t\tsessionResolver, err := buildBillingSessionPrincipalResolver(db, redisClient)
\t\tif err != nil {
\t\t\treturn nil, false, err
\t\t}
\t\tprincipalResolver = sessionResolver
\t}
\tmembershipResolver := billingMembershipResolver{store: membershipStore}
'''
    require(old_builder in billing_text, "billing builder boundary drift")
    billing_text = billing_text.replace(old_builder, new_builder, 1)
    billing_path.write_text(billing_text, encoding="utf-8")

    main_path = Path("services/platformapi/cmd/server/main.go")
    main_text = main_path.read_text(encoding="utf-8")
    old_call = "billingHandler, billingEnabled, err := buildBillingHandler(db, testAuth)"
    new_call = "billingHandler, billingEnabled, err := buildBillingHandler(db, redisClient, testAuth)"
    require(main_text.count(old_call) == 1, "main Billing builder call boundary drift")
    main_path.write_text(main_text.replace(old_call, new_call, 1), encoding="utf-8")

    Path("services/platformapi/cmd/server/billing_session_auth.go").write_text(r'''package main

import (
	"database/sql"
	"errors"
	"net/http"
	"strings"

	"github.com/Techshrr/GoJet/internal/billing"
	"github.com/Techshrr/GoJet/internal/workspace"
	"github.com/redis/go-redis/v9"
)

// billingSessionPrincipalResolver adapts the established P15 customer session
// boundary to the P13 Billing customer principal contract. Workspace membership
// and role authority remain server-authoritative inside Billing.
type billingSessionPrincipalResolver struct {
	resolvePrincipal func(*http.Request) (workspace.Principal, error)
}

func buildBillingSessionPrincipalResolver(db *sql.DB, redisClient *redis.Client) (*billingSessionPrincipalResolver, error) {
	if db == nil || redisClient == nil {
		return nil, billing.ErrAuthenticationUnavailable
	}
	workspaceAuthority, err := buildWorkspaceSessionAuthority(db, redisClient)
	if err != nil {
		return nil, err
	}
	return &billingSessionPrincipalResolver{resolvePrincipal: workspaceAuthority.resolve}, nil
}

func (r *billingSessionPrincipalResolver) ResolvePrincipal(req *http.Request) (billing.RequestPrincipal, error) {
	if r == nil || r.resolvePrincipal == nil || req == nil {
		return billing.RequestPrincipal{}, billing.ErrAuthenticationUnavailable
	}
	principal, err := r.resolvePrincipal(req)
	if err != nil {
		return billing.RequestPrincipal{}, billingCustomerAuthorityError(err)
	}
	userID := strings.TrimSpace(principal.UserID)
	email := strings.TrimSpace(principal.Email)
	if userID == "" || email == "" {
		return billing.RequestPrincipal{}, billing.ErrAuthenticationUnavailable
	}
	return billing.RequestPrincipal{
		UserID:      userID,
		Email:       email,
		DisplayName: strings.TrimSpace(principal.DisplayName),
	}, nil
}

func billingCustomerAuthorityError(err error) error {
	switch {
	case errors.Is(err, workspace.ErrAuthenticationRequired), errors.Is(err, workspace.ErrForbidden):
		// P15 has already enforced Origin and one-time CSRF/replay before this map.
		return billing.ErrAuthenticationRequired
	default:
		return billing.ErrAuthenticationUnavailable
	}
}
''', encoding="utf-8")

    Path("services/platformapi/cmd/server/billing_session_auth_test.go").write_text(r'''package main

import (
	"errors"
	"net/http"
	"testing"

	"github.com/Techshrr/GoJet/internal/billing"
	"github.com/Techshrr/GoJet/internal/workspace"
)

func TestBillingSessionPrincipalResolverMapsRealPrincipal(t *testing.T) {
	resolver := &billingSessionPrincipalResolver{
		resolvePrincipal: func(*http.Request) (workspace.Principal, error) {
			return workspace.Principal{UserID: "usr_real", Email: "real@example.test", DisplayName: "Real User"}, nil
		},
	}
	req, _ := http.NewRequest(http.MethodGet, "http://localhost/api/workspaces/ws_real/billing", nil)
	principal, err := resolver.ResolvePrincipal(req)
	if err != nil {
		t.Fatalf("ResolvePrincipal() error = %v", err)
	}
	if principal.UserID != "usr_real" || principal.Email != "real@example.test" || principal.DisplayName != "Real User" {
		t.Fatalf("unexpected principal: %#v", principal)
	}
}

func TestBillingSessionPrincipalResolverErrorMapping(t *testing.T) {
	tests := []struct {
		name string
		in   error
		want error
	}{
		{name: "authentication required", in: workspace.ErrAuthenticationRequired, want: billing.ErrAuthenticationRequired},
		{name: "unsafe request forbidden", in: workspace.ErrForbidden, want: billing.ErrAuthenticationRequired},
		{name: "dependency unavailable", in: workspace.ErrAuthenticationUnavailable, want: billing.ErrAuthenticationUnavailable},
		{name: "unknown dependency failure", in: errors.New("boom"), want: billing.ErrAuthenticationUnavailable},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			resolver := &billingSessionPrincipalResolver{
				resolvePrincipal: func(*http.Request) (workspace.Principal, error) { return workspace.Principal{}, tt.in },
			}
			req, _ := http.NewRequest(http.MethodPost, "http://localhost/api/workspaces/ws_real/orders", nil)
			_, err := resolver.ResolvePrincipal(req)
			if !errors.Is(err, tt.want) {
				t.Fatalf("ResolvePrincipal() error = %v, want %v", err, tt.want)
			}
		})
	}
}

func TestBillingSessionPrincipalResolverRejectsIncompleteAuthority(t *testing.T) {
	resolver := &billingSessionPrincipalResolver{
		resolvePrincipal: func(*http.Request) (workspace.Principal, error) {
			return workspace.Principal{UserID: "usr_real"}, nil
		},
	}
	req, _ := http.NewRequest(http.MethodGet, "http://localhost/api/workspaces/ws_real/billing", nil)
	_, err := resolver.ResolvePrincipal(req)
	if !errors.Is(err, billing.ErrAuthenticationUnavailable) {
		t.Fatalf("ResolvePrincipal() error = %v, want auth unavailable", err)
	}
}
''', encoding="utf-8")

    targets = [
        "services/platformapi/cmd/server/billing.go",
        "services/platformapi/cmd/server/billing_session_auth.go",
        "services/platformapi/cmd/server/billing_session_auth_test.go",
        "services/platformapi/cmd/server/main.go",
    ]
    subprocess.run(["gofmt", "-w", *targets], check=True)
    subprocess.run(["git", "diff", "--check"], check=True)
    status = subprocess.check_output(["git", "status", "--porcelain=v1"], text=True).splitlines()
    changed = [line[3:] for line in status if len(line) >= 4]
    require(sorted(changed) == sorted(targets), f"unexpected product working-tree boundary: {changed}")

    final_billing = billing_path.read_text(encoding="utf-8")
    require('if testAuth && os.Getenv("GOJET_TEST_BILLING_CALLBACKS_ENABLED") == "1" {' in final_billing, "callback CI gate changed")
    require('X-GoJet-Test-Actor' in final_billing, "P13 test principal adapter removed")
    require('buildBillingSessionPrincipalResolver(db, redisClient)' in final_billing, "real Billing session resolver not composed")
    return 0


if __name__ == "__main__":
    raise SystemExit(main())
