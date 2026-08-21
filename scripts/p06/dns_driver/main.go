package main

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"net"
	"os"
	"strings"
	"sync"
	"time"

	"github.com/Techshrr/GoJet/internal/domains"
	_ "github.com/go-sql-driver/mysql"
	"golang.org/x/net/dns/dnsmessage"
)

type caseResult struct {
	CaseID  string         `json:"case_id"`
	Status  string         `json:"status"`
	Details map[string]any `json:"details"`
	Errors  []string       `json:"errors"`
}

type txtAuthority struct {
	conn       *net.UDPConn
	expected   string
	mu         sync.RWMutex
	records    []string
	queries    int
	queryNames []string
	done       chan struct{}
}

func main() {
	caseFlag := flag.String("case", "P06-T010", "P06 DNS ownership case ID")
	flag.Parse()

	dsn := strings.TrimSpace(os.Getenv("GOJET_MYSQL_DSN"))
	if dsn == "" {
		failFatal("GOJET_MYSQL_DSN is required")
	}
	db, err := sql.Open("mysql", dsn)
	if err != nil {
		failFatal(err.Error())
	}
	defer db.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 90*time.Second)
	defer cancel()
	if err := db.PingContext(ctx); err != nil {
		failFatal(fmt.Sprintf("ping MySQL: %v", err))
	}

	result := caseResult{CaseID: *caseFlag, Status: "PASS", Details: map[string]any{}, Errors: []string{}}
	switch *caseFlag {
	case "P06-T010":
		if err := caseT010(ctx, db, &result); err != nil {
			result.Status = "FAIL"
			result.Errors = append(result.Errors, err.Error())
		}
	default:
		result.Status = "FAIL"
		result.Errors = append(result.Errors, fmt.Sprintf("unsupported case %s", *caseFlag))
	}
	writeJSON(map[string]caseResult{*caseFlag: result})
	if result.Status != "PASS" {
		os.Exit(1)
	}
}

func caseT010(ctx context.Context, db *sql.DB, out *caseResult) error {
	workspace := "p06-t010-dns"
	store := domains.NewMySQLStore(db)
	now := fixedNow()
	if _, err := store.UpsertPlanSource(ctx, domains.PlanSourceInput{
		WorkspaceID: workspace,
		SourceKey: "subscription-business-t010",
		Status: domains.EntitlementActive,
		DomainLimit: 1,
		StartsAt: now.Add(-24 * time.Hour),
		DecisionReason: "T010 active entitlement fixture",
	}, "corr-p06-t010-plan"); err != nil {
		return err
	}

	created, err := store.CreateDomain(ctx, domains.CreateDomainInput{
		WorkspaceID: workspace,
		ActorID: "actor-t010",
		CorrelationID: "corr-p06-t010-create",
		Reason: "create domain before DNS ownership verification",
		Hostname: "ownership-t010.example.com",
		Now: now,
	})
	if err != nil {
		return err
	}
	oldSecret, err := secretFromTXTValue(created.OwnershipTXTValue)
	if err != nil {
		return err
	}
	rotated, err := store.RotateOwnershipSecret(ctx, domains.RotateOwnershipSecretInput{
		WorkspaceID: workspace,
		DomainID: created.Domain.ID,
		ActorID: "actor-t010",
		CorrelationID: "corr-p06-t010-rotate",
		Reason: "establish current proof before DNS verification",
		Now: now.Add(2 * time.Minute),
	})
	if err != nil {
		return err
	}
	currentSecret, err := secretFromTXTValue(rotated.OwnershipTXTValue)
	if err != nil {
		return err
	}
	if currentSecret == oldSecret {
		return errors.New("rotation did not produce a distinct T010 proof")
	}

	authority, err := startTXTAuthority(rotated.OwnershipTXTName)
	if err != nil {
		return err
	}
	defer authority.Close()
	verifier := domains.NewOwnershipVerifier(store, authority.Resolver())

	authority.SetRecords(nil)
	missing, err := verifier.VerifyTXT(ctx, domains.VerifyOwnershipTXTInput{
		WorkspaceID: workspace,
		DomainID: created.Domain.ID,
		ActorID: "actor-t010",
		CorrelationID: "corr-p06-t010-missing",
		Reason: "verify missing ownership TXT proof",
		Now: now.Add(4 * time.Minute),
	})
	if err != nil {
		return fmt.Errorf("missing TXT verification: %w", err)
	}
	if missing.Outcome != domains.OwnershipVerificationMissing || missing.Domain.OwnershipStatus != domains.OwnershipPending {
		return fmt.Errorf("missing TXT advanced ownership unexpectedly: outcome=%s status=%s", missing.Outcome, missing.Domain.OwnershipStatus)
	}

	wrongValue := "gojet-verification=wrong-proof-t010"
	authority.SetRecords([]string{wrongValue})
	wrong, err := verifier.VerifyTXT(ctx, domains.VerifyOwnershipTXTInput{
		WorkspaceID: workspace,
		DomainID: created.Domain.ID,
		ActorID: "actor-t010",
		CorrelationID: "corr-p06-t010-wrong",
		Reason: "verify incorrect ownership TXT proof",
		Now: now.Add(5 * time.Minute),
	})
	if err != nil {
		return fmt.Errorf("wrong TXT verification: %w", err)
	}
	if wrong.Outcome != domains.OwnershipVerificationMismatch || wrong.Domain.OwnershipStatus != domains.OwnershipFailed {
		return fmt.Errorf("wrong TXT did not fail ownership: outcome=%s status=%s", wrong.Outcome, wrong.Domain.OwnershipStatus)
	}

	authority.SetRecords([]string{created.OwnershipTXTValue})
	oldRotated, err := verifier.VerifyTXT(ctx, domains.VerifyOwnershipTXTInput{
		WorkspaceID: workspace,
		DomainID: created.Domain.ID,
		ActorID: "actor-t010",
		CorrelationID: "corr-p06-t010-old",
		Reason: "verify old rotated ownership TXT proof",
		Now: now.Add(6 * time.Minute),
	})
	if err != nil {
		return fmt.Errorf("old rotated TXT verification: %w", err)
	}
	if oldRotated.Outcome != domains.OwnershipVerificationMismatch || oldRotated.Domain.OwnershipStatus != domains.OwnershipFailed {
		return fmt.Errorf("old rotated TXT regained authority: outcome=%s status=%s", oldRotated.Outcome, oldRotated.Domain.OwnershipStatus)
	}

	authority.SetRecords([]string{rotated.OwnershipTXTValue})
	verifiedAt := now.Add(7 * time.Minute)
	verified, err := verifier.VerifyTXT(ctx, domains.VerifyOwnershipTXTInput{
		WorkspaceID: workspace,
		DomainID: created.Domain.ID,
		ActorID: "actor-t010",
		CorrelationID: "corr-p06-t010-current",
		Reason: "verify current ownership TXT proof",
		Now: verifiedAt,
	})
	if err != nil {
		return fmt.Errorf("current TXT verification: %w", err)
	}
	if verified.Outcome != domains.OwnershipVerificationVerified || verified.Domain.OwnershipStatus != domains.OwnershipVerified || verified.Domain.OwnershipVerifiedAt == nil || !verified.Domain.OwnershipVerifiedAt.Equal(verifiedAt.UTC()) {
		return fmt.Errorf("current TXT did not verify ownership: outcome=%s status=%s verified_at=%v", verified.Outcome, verified.Domain.OwnershipStatus, verified.Domain.OwnershipVerifiedAt)
	}
	if verified.Domain.OwnershipTokenVersion != 2 {
		return fmt.Errorf("DNS verification changed token version: got=%d want=2", verified.Domain.OwnershipTokenVersion)
	}

	revalidationResults, err := queryStrings(ctx, db, `
		SELECT result
		FROM custom_domain_revalidations
		WHERE workspace_id = ? AND domain_id = ? AND axis = 'ownership'
		ORDER BY id`, workspace, created.Domain.ID)
	if err != nil {
		return err
	}
	wantRevalidations := []string{"pending", "fail", "fail", "pass"}
	if strings.Join(revalidationResults, ",") != strings.Join(wantRevalidations, ",") {
		return fmt.Errorf("ownership revalidation sequence=%v want=%v", revalidationResults, wantRevalidations)
	}

	auditMetadata, err := queryStrings(ctx, db, `
		SELECT CAST(metadata_json AS CHAR)
		FROM custom_domain_audit_events
		WHERE workspace_id = ? AND domain_id = ? AND action = 'domain.ownership.verify'
		ORDER BY id`, workspace, created.Domain.ID)
	if err != nil {
		return err
	}
	if len(auditMetadata) != 4 {
		return fmt.Errorf("ownership verify audit count=%d want=4", len(auditMetadata))
	}
	joinedAudit := strings.Join(auditMetadata, "\n")
	for _, secretMaterial := range []string{oldSecret, currentSecret, created.OwnershipTXTValue, rotated.OwnershipTXTValue, wrongValue} {
		if strings.Contains(joinedAudit, secretMaterial) {
			return errors.New("ownership verification audit leaked TXT secret material")
		}
	}

	queryCount, queryNames := authority.QueryEvidence()
	if queryCount < 4 {
		return fmt.Errorf("authoritative DNS received only %d queries, want at least 4", queryCount)
	}
	expectedFQDN := strings.ToLower(rotated.OwnershipTXTName) + "."
	for _, name := range queryNames {
		if name != expectedFQDN {
			return fmt.Errorf("authoritative DNS received unexpected qname %q want %q", name, expectedFQDN)
		}
	}

	out.Details = map[string]any{
		"domain_id": created.Domain.ID,
		"hostname_ascii": created.Domain.HostnameASCII,
		"ownership_token_version": verified.Domain.OwnershipTokenVersion,
		"dns_transport": "udp",
		"authoritative_dns_queries": queryCount,
		"missing_outcome": missing.Outcome,
		"wrong_outcome": wrong.Outcome,
		"old_rotated_outcome": oldRotated.Outcome,
		"current_outcome": verified.Outcome,
		"final_ownership_status": verified.Domain.OwnershipStatus,
		"revalidation_sequence": revalidationResults,
		"verify_audit_events": len(auditMetadata),
		"audit_secret_leak": false,
	}
	return nil
}

func startTXTAuthority(expectedName string) (*txtAuthority, error) {
	conn, err := net.ListenUDP("udp4", &net.UDPAddr{IP: net.ParseIP("127.0.0.1"), Port: 0})
	if err != nil {
		return nil, err
	}
	authority := &txtAuthority{
		conn: conn,
		expected: strings.ToLower(strings.TrimSuffix(expectedName, ".")) + ".",
		done: make(chan struct{}),
	}
	go authority.serve()
	return authority, nil
}

func (a *txtAuthority) SetRecords(records []string) {
	a.mu.Lock()
	defer a.mu.Unlock()
	a.records = append([]string(nil), records...)
}

func (a *txtAuthority) Resolver() domains.TXTResolver {
	address := a.conn.LocalAddr().String()
	return domains.NetTXTResolver{Resolver: &net.Resolver{
		PreferGo: true,
		StrictErrors: true,
		Dial: func(ctx context.Context, network, _ string) (net.Conn, error) {
			var dialer net.Dialer
			return dialer.DialContext(ctx, "udp4", address)
		},
	}}
}

func (a *txtAuthority) QueryEvidence() (int, []string) {
	a.mu.RLock()
	defer a.mu.RUnlock()
	return a.queries, append([]string(nil), a.queryNames...)
}

func (a *txtAuthority) Close() {
	_ = a.conn.Close()
	<-a.done
}

func (a *txtAuthority) serve() {
	defer close(a.done)
	buffer := make([]byte, 4096)
	for {
		n, remote, err := a.conn.ReadFromUDP(buffer)
		if err != nil {
			return
		}
		var query dnsmessage.Message
		if err := query.Unpack(buffer[:n]); err != nil || len(query.Questions) == 0 {
			continue
		}
		question := query.Questions[0]
		qname := strings.ToLower(question.Name.String())
		a.mu.Lock()
		a.queries++
		a.queryNames = append(a.queryNames, qname)
		records := append([]string(nil), a.records...)
		a.mu.Unlock()

		answers := []dnsmessage.Resource{}
		if qname == a.expected && question.Type == dnsmessage.TypeTXT {
			for _, record := range records {
				answers = append(answers, dnsmessage.Resource{
					Header: dnsmessage.ResourceHeader{Name: question.Name, Class: dnsmessage.ClassINET, TTL: 0},
					Body: &dnsmessage.TXTResource{TXT: []string{record}},
				})
			}
		}
		response := dnsmessage.Message{
			Header: dnsmessage.Header{
				ID: query.Header.ID,
				Response: true,
				Authoritative: true,
				RecursionDesired: query.Header.RecursionDesired,
				RCode: dnsmessage.RCodeSuccess,
			},
			Questions: query.Questions,
			Answers: answers,
		}
		packed, err := response.Pack()
		if err != nil {
			continue
		}
		_, _ = a.conn.WriteToUDP(packed, remote)
	}
}

func secretFromTXTValue(value string) (string, error) {
	const prefix = "gojet-verification="
	if !strings.HasPrefix(value, prefix) {
		return "", fmt.Errorf("unexpected ownership TXT value contract: %q", value)
	}
	secret := strings.TrimPrefix(value, prefix)
	if secret == "" {
		return "", errors.New("empty ownership TXT secret")
	}
	return secret, nil
}

func queryStrings(ctx context.Context, db *sql.DB, query string, args ...any) ([]string, error) {
	rows, err := db.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	values := []string{}
	for rows.Next() {
		var value string
		if err := rows.Scan(&value); err != nil {
			return nil, err
		}
		values = append(values, value)
	}
	return values, rows.Err()
}

func fixedNow() time.Time {
	return time.Date(2026, 8, 21, 16, 0, 0, 0, time.UTC)
}

func writeJSON(value any) {
	encoder := json.NewEncoder(os.Stdout)
	encoder.SetIndent("", "  ")
	if err := encoder.Encode(value); err != nil {
		failFatal(err.Error())
	}
}

func failFatal(message string) {
	_ = json.NewEncoder(os.Stderr).Encode(map[string]any{"status": "FAIL", "error": message})
	os.Exit(2)
}
