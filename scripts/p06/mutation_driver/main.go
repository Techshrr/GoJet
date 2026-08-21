package main

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"os"
	"strings"
	"sync"
	"time"

	"github.com/Techshrr/GoJet/internal/domains"
	_ "github.com/go-sql-driver/mysql"
)

type caseResult struct {
	CaseID  string         `json:"case_id"`
	Status  string         `json:"status"`
	Details map[string]any `json:"details"`
	Errors  []string       `json:"errors"`
}

type permissionChecker struct {
	mu      sync.Mutex
	allowed map[string]bool
	calls   int
}

func (p *permissionChecker) CanManageCustomDomains(_ context.Context, workspaceID, actorID string) (bool, error) {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.calls++
	return p.allowed[workspaceID+"|"+actorID], nil
}

func (p *permissionChecker) set(workspaceID, actorID string, allowed bool) {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.allowed[workspaceID+"|"+actorID] = allowed
}

func (p *permissionChecker) count() int {
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.calls
}

type txtResolver struct { mu sync.Mutex; records map[string][]string; calls int }
func (r *txtResolver) LookupTXT(_ context.Context, name string) ([]string, error) {
	r.mu.Lock(); defer r.mu.Unlock(); r.calls++
	return append([]string(nil), r.records[strings.TrimSuffix(strings.ToLower(name), ".")]...), nil
}
func (r *txtResolver) count() int { r.mu.Lock(); defer r.mu.Unlock(); return r.calls }

type cnameResolver struct { mu sync.Mutex; target string; calls int }
func (r *cnameResolver) LookupCNAME(context.Context, string) (string, error) { r.mu.Lock(); defer r.mu.Unlock(); r.calls++; return r.target, nil }
func (r *cnameResolver) count() int { r.mu.Lock(); defer r.mu.Unlock(); return r.calls }

type tlsProbe struct { mu sync.Mutex; calls int }
func (p *tlsProbe) Probe(context.Context, string) (domains.TLSReadinessObservation, error) { p.mu.Lock(); defer p.mu.Unlock(); p.calls++; return domains.TLSReadinessObservation{Outcome: domains.TLSProbeActive, HandshakeComplete: true, TLSVersion: "TLS1.3"}, nil }
func (p *tlsProbe) count() int { p.mu.Lock(); defer p.mu.Unlock(); return p.calls }

type riskEvaluator struct { mu sync.Mutex; calls int }
func (e *riskEvaluator) Evaluate(context.Context, string) (domains.DomainRiskObservation, error) { e.mu.Lock(); defer e.mu.Unlock(); e.calls++; return domains.DomainRiskObservation{Status: domains.RiskAllow, PolicyVersion: "t017-risk-v1", EvidenceRef: "risk:t017:allow"}, nil }
func (e *riskEvaluator) count() int { e.mu.Lock(); defer e.mu.Unlock(); return e.calls }

func main() {
	caseFlag := flag.String("case", "P06-T017", "P06 mutation enforcement case ID")
	flag.Parse()
	dsn := strings.TrimSpace(os.Getenv("GOJET_MYSQL_DSN"))
	if dsn == "" { failFatal("GOJET_MYSQL_DSN is required") }
	db, err := sql.Open("mysql", dsn); if err != nil { failFatal(err.Error()) }
	defer db.Close()
	ctx, cancel := context.WithTimeout(context.Background(), 90*time.Second); defer cancel()
	if err := db.PingContext(ctx); err != nil { failFatal(fmt.Sprintf("ping MySQL: %v", err)) }
	result := caseResult{CaseID:*caseFlag, Status:"PASS", Details:map[string]any{}, Errors:[]string{}}
	if *caseFlag != "P06-T017" { result.Status="FAIL"; result.Errors=append(result.Errors, "unsupported case")
	} else if err := caseT017(ctx, db, &result); err != nil { result.Status="FAIL"; result.Errors=append(result.Errors, err.Error()) }
	writeJSON(map[string]caseResult{*caseFlag:result}); if result.Status!="PASS" { os.Exit(1) }
}

func caseT017(ctx context.Context, db *sql.DB, out *caseResult) error {
	now := time.Date(2026,8,21,19,0,0,0,time.UTC)
	store := domains.NewMySQLStore(db)
	permissions := &permissionChecker{allowed:map[string]bool{}}
	txt := &txtResolver{records:map[string][]string{}}
	cname := &cnameResolver{target:"ingress-t017.example.net"}
	tls := &tlsProbe{}
	risk := &riskEvaluator{}
	ownership := domains.NewOwnershipVerifier(store, txt)
	ingress, err := domains.NewIngressDNSVerifier(store, cname, "ingress-t017.example.net"); if err != nil { return err }
	https := domains.NewHTTPSVerifier(store, tls)
	riskVerifier := domains.NewDomainRiskVerifier(store, risk)
	authority, err := domains.NewDomainAuthorityService(store, permissions, ownership, ingress, https, riskVerifier); if err != nil { return err }

	workspace := "p06-t017-authority"
	manager := "manager-t017"
	viewer := "viewer-t017"
	permissions.set(workspace, manager, true)
	permissions.set(workspace, viewer, false)
	if _, err := store.UpsertPlanSource(ctx, domains.PlanSourceInput{WorkspaceID:workspace, SourceKey:"business-t017", Status:domains.EntitlementActive, DomainLimit:5, StartsAt:now.Add(-24*time.Hour), DecisionReason:"T017 active entitlement"}, "corr-p06-t017-plan"); err != nil { return err }
	created, err := store.CreateDomain(ctx, domains.CreateDomainInput{WorkspaceID:workspace, ActorID:manager, CorrelationID:"corr-p06-t017-create", Reason:"create verification domain", Hostname:"authority-t017.example.com", Now:now}); if err != nil { return err }
	txt.records[strings.ToLower(created.OwnershipTXTName)] = []string{created.OwnershipTXTValue}

	before, err := store.GetDomain(ctx, workspace, created.Domain.ID); if err != nil { return err }
	callsBefore := []int{txt.count(), cname.count(), tls.count(), risk.count()}
	if _, err := authority.VerifyOwnershipTXT(ctx, domains.VerifyOwnershipTXTInput{WorkspaceID:workspace,DomainID:created.Domain.ID,ActorID:viewer,CorrelationID:"corr-p06-t017-denied-owner",Reason:"crafted ownership verify without manage permission",Now:now.Add(time.Minute)}); !errors.Is(err, domains.ErrWorkspacePermissionRequired) { return fmt.Errorf("ownership permission denial=%v", err) }
	if _, err := authority.VerifyIngressDNS(ctx, domains.VerifyIngressDNSInput{WorkspaceID:workspace,DomainID:created.Domain.ID,ActorID:viewer,CorrelationID:"corr-p06-t017-denied-ingress",Reason:"crafted ingress verify without manage permission",Now:now.Add(time.Minute)}); !errors.Is(err, domains.ErrWorkspacePermissionRequired) { return fmt.Errorf("ingress permission denial=%v", err) }
	if _, err := authority.VerifyHTTPS(ctx, domains.VerifyHTTPSInput{WorkspaceID:workspace,DomainID:created.Domain.ID,ActorID:viewer,CorrelationID:"corr-p06-t017-denied-https",Reason:"crafted HTTPS verify without manage permission",Now:now.Add(time.Minute)}); !errors.Is(err, domains.ErrWorkspacePermissionRequired) { return fmt.Errorf("HTTPS permission denial=%v", err) }
	if _, err := authority.VerifyDomainRisk(ctx, domains.VerifyDomainRiskInput{WorkspaceID:workspace,DomainID:created.Domain.ID,ActorID:viewer,CorrelationID:"corr-p06-t017-denied-risk",Reason:"crafted risk verify without manage permission",Now:now.Add(time.Minute)}); !errors.Is(err, domains.ErrWorkspacePermissionRequired) { return fmt.Errorf("risk permission denial=%v", err) }
	if callsAfter := []int{txt.count(), cname.count(), tls.count(), risk.count()}; fmt.Sprint(callsAfter) != fmt.Sprint(callsBefore) { return fmt.Errorf("permission denial reached external authority: before=%v after=%v", callsBefore, callsAfter) }
	afterDenied, err := store.GetDomain(ctx, workspace, created.Domain.ID); if err != nil { return err }
	if afterDenied.OwnershipStatus!=before.OwnershipStatus || afterDenied.IngressDNSStatus!=before.IngressDNSStatus || afterDenied.HTTPSStatus!=before.HTTPSStatus || afterDenied.RiskStatus!=before.RiskStatus || afterDenied.OwnershipTokenVersion!=before.OwnershipTokenVersion { return fmt.Errorf("permission-denied verification advanced state: before=%+v after=%+v", before, afterDenied) }

	// A denied actor cannot learn whether an arbitrary domain ID exists in the Workspace.
	if _, err := authority.ActivateDomain(ctx, domains.DomainRoutingMutationInput{WorkspaceID:workspace,DomainID:999999999,ActorID:viewer,CorrelationID:"corr-p06-t017-private",Reason:"crafted activate against unknown domain",Now:now.Add(time.Minute)}); !errors.Is(err, domains.ErrWorkspacePermissionRequired) { return fmt.Errorf("permission gate leaked domain existence: %v", err) }

	verified, err := authority.VerifyOwnershipTXT(ctx, domains.VerifyOwnershipTXTInput{WorkspaceID:workspace,DomainID:created.Domain.ID,ActorID:manager,CorrelationID:"corr-p06-t017-owner",Reason:"authorized ownership verification",Now:now.Add(2*time.Minute)}); if err != nil { return err }
	if verified.Domain.OwnershipStatus != domains.OwnershipVerified || txt.count()!=callsBefore[0]+1 { return fmt.Errorf("authorized ownership verification failed: %+v calls=%d", verified, txt.count()) }
	ingressResult, err := authority.VerifyIngressDNS(ctx, domains.VerifyIngressDNSInput{WorkspaceID:workspace,DomainID:created.Domain.ID,ActorID:manager,CorrelationID:"corr-p06-t017-ingress",Reason:"authorized ingress verification",Now:now.Add(3*time.Minute)}); if err != nil { return err }
	if ingressResult.Domain.IngressDNSStatus != domains.IngressValid { return fmt.Errorf("authorized ingress not valid: %+v", ingressResult) }
	httpsResult, err := authority.VerifyHTTPS(ctx, domains.VerifyHTTPSInput{WorkspaceID:workspace,DomainID:created.Domain.ID,ActorID:manager,CorrelationID:"corr-p06-t017-https",Reason:"authorized HTTPS verification",Now:now.Add(4*time.Minute)}); if err != nil { return err }
	if httpsResult.Domain.HTTPSStatus != domains.HTTPSActive { return fmt.Errorf("authorized HTTPS not active: %+v", httpsResult) }
	riskResult, err := authority.VerifyDomainRisk(ctx, domains.VerifyDomainRiskInput{WorkspaceID:workspace,DomainID:created.Domain.ID,ActorID:manager,CorrelationID:"corr-p06-t017-risk",Reason:"authorized risk verification",Now:now.Add(5*time.Minute)}); if err != nil { return err }
	if riskResult.Domain.RiskStatus != domains.RiskAllow { return fmt.Errorf("authorized risk not allow: %+v", riskResult) }
	activated, err := authority.ActivateDomain(ctx, domains.DomainRoutingMutationInput{WorkspaceID:workspace,DomainID:created.Domain.ID,ActorID:manager,CorrelationID:"corr-p06-t017-activate",Reason:"activate fully ready domain",Now:now.Add(6*time.Minute)}); if err != nil { return err }
	if activated.RoutingState != domains.RoutingEnabled { return fmt.Errorf("activation did not enable routing: %+v", activated) }

	// Restore is permission- and trust-gated independently from activation.
	if _, err := db.ExecContext(ctx, `UPDATE custom_domains SET routing_state='suspended', security_category=NULL WHERE workspace_id=? AND id=?`, workspace, created.Domain.ID); err != nil { return err }
	if _, err := authority.RestoreDomain(ctx, domains.DomainRoutingMutationInput{WorkspaceID:workspace,DomainID:created.Domain.ID,ActorID:viewer,CorrelationID:"corr-p06-t017-restore-denied",Reason:"crafted restore without manage permission",Now:now.Add(7*time.Minute)}); !errors.Is(err, domains.ErrWorkspacePermissionRequired) { return fmt.Errorf("restore permission denial=%v", err) }
	restored, err := authority.RestoreDomain(ctx, domains.DomainRoutingMutationInput{WorkspaceID:workspace,DomainID:created.Domain.ID,ActorID:manager,CorrelationID:"corr-p06-t017-restore",Reason:"authorized restore",Now:now.Add(8*time.Minute)}); if err != nil { return err }
	if restored.RoutingState != domains.RoutingEnabled { return fmt.Errorf("restore did not enable routing: %+v", restored) }

	// Activate must fail closed on each independent trust prerequisite and may not
	// change routing_state until all axes are current and ready.
	axisDomain, err := store.CreateDomain(ctx, domains.CreateDomainInput{WorkspaceID:workspace,ActorID:manager,CorrelationID:"corr-p06-t017-axis-create",Reason:"axis mutation fixture",Hostname:"axes-t017.example.com",Now:now.Add(9*time.Minute)}); if err != nil { return err }
	setAxes := func(ownership string, ingressState string, httpsState string, riskState string) error { _, e := db.ExecContext(ctx, `UPDATE custom_domains SET ownership_status=?, ingress_dns_status=?, https_status=?, risk_status=?, routing_state='pending', security_category=NULL WHERE workspace_id=? AND id=?`, ownership, ingressState, httpsState, riskState, workspace, axisDomain.Domain.ID); return e }
	axisChecks := []struct{ ownership, ingress, https, risk string; want error }{
		{"pending","valid","active","allow",domains.ErrOwnershipRequired},
		{"verified","pending","active","allow",domains.ErrIngressDNSRequired},
		{"verified","valid","pending","allow",domains.ErrHTTPSRequired},
		{"verified","valid","active","missing",domains.ErrDomainRiskEvaluation},
	}
	for i, check := range axisChecks {
		if err := setAxes(check.ownership, check.ingress, check.https, check.risk); err != nil { return err }
		_, mutationErr := authority.ActivateDomain(ctx, domains.DomainRoutingMutationInput{WorkspaceID:workspace,DomainID:axisDomain.Domain.ID,ActorID:manager,CorrelationID:fmt.Sprintf("corr-p06-t017-axis-%d",i),Reason:"axis denial fixture",Now:now.Add(time.Duration(10+i)*time.Minute)})
		if !errors.Is(mutationErr, check.want) { return fmt.Errorf("axis case %d error=%v want=%v", i, mutationErr, check.want) }
		state, err := store.GetDomain(ctx, workspace, axisDomain.Domain.ID); if err != nil { return err }
		if state.RoutingState != domains.RoutingPending { return fmt.Errorf("axis denial %d advanced routing: %+v", i, state) }
	}
	if err := setAxes("verified","valid","active","allow"); err != nil { return err }
	if _, err := authority.ActivateDomain(ctx, domains.DomainRoutingMutationInput{WorkspaceID:workspace,DomainID:axisDomain.Domain.ID,ActorID:manager,CorrelationID:"corr-p06-t017-axis-allowed",Reason:"all axes ready",Now:now.Add(15*time.Minute)}); err != nil { return err }

	// Rotate permission and entitlement are both current server-side checks.
	rotateDomain, err := store.CreateDomain(ctx, domains.CreateDomainInput{WorkspaceID:workspace,ActorID:manager,CorrelationID:"corr-p06-t017-rotate-create",Reason:"rotation permission fixture",Hostname:"rotate-t017.example.com",Now:now.Add(16*time.Minute)}); if err != nil { return err }
	originalToken := rotateDomain.Domain.OwnershipTokenVersion
	if _, err := authority.RotateOwnershipSecret(ctx, domains.RotateOwnershipSecretInput{WorkspaceID:workspace,DomainID:rotateDomain.Domain.ID,ActorID:viewer,CorrelationID:"corr-p06-t017-rotate-denied",Reason:"crafted rotation without manage permission",Now:now.Add(17*time.Minute)}); !errors.Is(err, domains.ErrWorkspacePermissionRequired) { return fmt.Errorf("rotate permission denial=%v", err) }
	unchanged, err := store.GetDomain(ctx, workspace, rotateDomain.Domain.ID); if err != nil { return err }
	if unchanged.OwnershipTokenVersion != originalToken { return fmt.Errorf("permission-denied rotation changed token") }
	rotated, err := authority.RotateOwnershipSecret(ctx, domains.RotateOwnershipSecretInput{WorkspaceID:workspace,DomainID:rotateDomain.Domain.ID,ActorID:manager,CorrelationID:"corr-p06-t017-rotate",Reason:"authorized rotation",Now:now.Add(18*time.Minute)}); if err != nil { return err }
	if rotated.Domain.OwnershipTokenVersion != originalToken+1 || rotated.Domain.OwnershipStatus != domains.OwnershipPending { return fmt.Errorf("authorized rotation incorrect: %+v", rotated.Domain) }

	expiredWorkspace := "p06-t017-expired"
	permissions.set(expiredWorkspace, manager, true)
	expiresAt := now.Add(30*time.Minute)
	if _, err := store.UpsertPlanSource(ctx, domains.PlanSourceInput{WorkspaceID:expiredWorkspace,SourceKey:"business-t017-expired",Status:domains.EntitlementActive,DomainLimit:2,StartsAt:now.Add(-time.Hour),ExpiresAt:&expiresAt,DecisionReason:"expires for T017"}, "corr-p06-t017-expired-plan"); err != nil { return err }
	expiredDomain, err := store.CreateDomain(ctx, domains.CreateDomainInput{WorkspaceID:expiredWorkspace,ActorID:manager,CorrelationID:"corr-p06-t017-expired-create",Reason:"create before entitlement expiry",Hostname:"expired-t017.example.com",Now:now.Add(20*time.Minute)}); if err != nil { return err }
	txt.records[strings.ToLower(expiredDomain.OwnershipTXTName)] = []string{expiredDomain.OwnershipTXTValue}
	txtBeforeExpired := txt.count()
	if _, err := authority.VerifyOwnershipTXT(ctx, domains.VerifyOwnershipTXTInput{WorkspaceID:expiredWorkspace,DomainID:expiredDomain.Domain.ID,ActorID:manager,CorrelationID:"corr-p06-t017-expired-verify",Reason:"verify after entitlement expiry",Now:expiresAt}); !errors.Is(err, domains.ErrEntitlementRequired) { return fmt.Errorf("expired verify error=%v", err) }
	if txt.count()!=txtBeforeExpired { return fmt.Errorf("expired entitlement allowed DNS traffic") }
	beforeExpiredRotate, err := store.GetDomain(ctx, expiredWorkspace, expiredDomain.Domain.ID); if err != nil { return err }
	if _, err := authority.RotateOwnershipSecret(ctx, domains.RotateOwnershipSecretInput{WorkspaceID:expiredWorkspace,DomainID:expiredDomain.Domain.ID,ActorID:manager,CorrelationID:"corr-p06-t017-expired-rotate",Reason:"rotate after entitlement expiry",Now:expiresAt}); !errors.Is(err, domains.ErrEntitlementRequired) { return fmt.Errorf("expired rotate error=%v", err) }
	afterExpiredRotate, err := store.GetDomain(ctx, expiredWorkspace, expiredDomain.Domain.ID); if err != nil { return err }
	if afterExpiredRotate.OwnershipTokenVersion != beforeExpiredRotate.OwnershipTokenVersion || afterExpiredRotate.OwnershipStatus != beforeExpiredRotate.OwnershipStatus { return fmt.Errorf("entitlement-denied rotate advanced state") }

	out.Details = map[string]any{
		"permission_checks": permissions.count(),
		"permission_denial_prevented_external_calls": true,
		"permission_denial_hid_domain_existence": true,
		"authorized_verification_sequence": []string{"ownership","ingress_dns","https","risk"},
		"authorized_activation": true,
		"authorized_restore": true,
		"axis_denials_preserved_routing": true,
		"axis_denial_count": len(axisChecks),
		"permission_denied_rotation_preserved_token": true,
		"authorized_rotation_advanced_token": true,
		"expired_entitlement_prevented_dns_lookup": true,
		"expired_entitlement_rotation_preserved_state": true,
		"external_call_counts": map[string]int{"txt":txt.count(),"cname":cname.count(),"tls":tls.count(),"risk":risk.count()},
	}
	return nil
}

func writeJSON(value any) { encoder:=json.NewEncoder(os.Stdout); encoder.SetIndent("","  "); if err:=encoder.Encode(value); err!=nil { failFatal(err.Error()) } }
func failFatal(message string) { _=json.NewEncoder(os.Stderr).Encode(map[string]any{"status":"FAIL","error":message}); os.Exit(2) }
