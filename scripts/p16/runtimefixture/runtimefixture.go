package runtimefixture

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"strings"
	"time"

	_ "github.com/go-sql-driver/mysql"
	"github.com/redis/go-redis/v9"

	"github.com/Techshrr/GoJet/internal/links"
	"github.com/Techshrr/GoJet/internal/trust"
)

type LinkFixture struct {
	ID          uint64
	WorkspaceID string
	Hostname    string
	DomainKind  string
	Code        string
	Primary     string
	Routing     []links.RoutingRule
	AB          []links.ABVariant
	Fingerprint string
	Targets     []string
}

type HTTPResult struct {
	Status   int
	Location string
	Body     string
	Headers  http.Header
}

type PermissionFixture struct {
	Allow       bool
	ActorID     string
	Permissions []string
}

func (p *PermissionFixture) Authorize(_ context.Context, actorID, permission string) error {
	p.ActorID = actorID
	p.Permissions = append(p.Permissions, permission)
	if !p.Allow || strings.TrimSpace(actorID) == "" || permission != trust.SecurityManagePermission {
		return trust.ErrUnauthorized
	}
	return nil
}

func Open() (*sql.DB, *redis.Client, error) {
	dsn := strings.TrimSpace(os.Getenv("GOJET_MYSQL_DSN"))
	redisAddr := strings.TrimSpace(os.Getenv("GOJET_REDIS_ADDR"))
	if dsn == "" || redisAddr == "" {
		return nil, nil, fmt.Errorf("GOJET_MYSQL_DSN and GOJET_REDIS_ADDR are required")
	}
	db, err := sql.Open("mysql", dsn)
	if err != nil {
		return nil, nil, err
	}
	client := links.NewRedisClient(redisAddr, "", 0)
	return db, client, nil
}

func CreateLink(ctx context.Context, db *sql.DB, workspace, hostname, domainKind, code, primary string, routing []links.RoutingRule, variants []links.ABVariant) (LinkFixture, error) {
	fingerprint, targets, err := links.RiskFingerprint(primary, routing, variants)
	if err != nil {
		return LinkFixture{}, err
	}
	if err := links.ValidateABWeights(variants); err != nil {
		return LinkFixture{}, err
	}
	routingRaw, _ := json.Marshal(routing)
	abRaw, _ := json.Marshal(variants)
	res, err := db.ExecContext(ctx, `
INSERT INTO links
(workspace_id,hostname,domain_kind,code,title,primary_destination,redirect_status,status,version,risk_fingerprint,routing_json,ab_json,utm_json,access_json)
VALUES (?,?,?,?,?,?,302,'active',1,?,?,?,'{"source":"p16-runtime","campaign":"safety-order"}','{}')`,
		workspace, hostname, domainKind, code, "P16 runtime fixture", primary, fingerprint, string(routingRaw), string(abRaw))
	if err != nil {
		return LinkFixture{}, err
	}
	id, err := res.LastInsertId()
	if err != nil {
		return LinkFixture{}, err
	}
	return LinkFixture{
		ID: uint64(id), WorkspaceID: workspace, Hostname: hostname, DomainKind: domainKind, Code: code,
		Primary: primary, Routing: routing, AB: variants, Fingerprint: fingerprint, Targets: targets,
	}, nil
}

func CreateReadyCustomDomain(ctx context.Context, db *sql.DB, workspace, hostname string, now time.Time) error {
	now = now.UTC().Truncate(time.Microsecond)
	if _, err := db.ExecContext(ctx, `
INSERT INTO custom_domain_entitlement_sources
(workspace_id,source,source_key,status,domain_limit,starts_at,expires_at,created_at,updated_at)
VALUES (?,'plan',?,'active',10,?,NULL,?,?)`, workspace, "p16-runtime-"+hostname, now.Add(-time.Hour), now, now); err != nil {
		return err
	}
	if _, err := db.ExecContext(ctx, `
INSERT INTO custom_domain_usage (workspace_id,allocated_count,version,updated_at)
VALUES (?,1,1,?)
ON DUPLICATE KEY UPDATE allocated_count=GREATEST(allocated_count,1),updated_at=VALUES(updated_at)`, workspace, now); err != nil {
		return err
	}
	secretHash := sha256.Sum256([]byte("p16-runtime-domain-secret-" + hostname))
	_, err := db.ExecContext(ctx, `
INSERT INTO custom_domains
(workspace_id,hostname_ascii,display_hostname,routing_state,ownership_status,ingress_dns_status,https_status,risk_status,
 ownership_token_version,ownership_secret_hash,ownership_secret_issued_at,ownership_verified_at,ingress_dns_checked_at,https_checked_at,
 risk_checked_at,risk_policy_version,risk_evidence_ref,created_at,updated_at)
VALUES (?,?,?,'enabled','verified','valid','active','allow',1,?,?,?,?,?,?,?,?,?,?)`,
		workspace, hostname, hostname, secretHash[:], now.Add(-time.Hour), now.Add(-30*time.Minute), now.Add(-30*time.Minute),
		now.Add(-30*time.Minute), now.Add(-30*time.Minute), "p16-domain-runtime-v1", "p16://redacted", now, now)
	return err
}

func RequestRedirect(ctx context.Context, hostname, code string) (HTTPResult, error) {
	return RequestRedirectWithHeaders(ctx, hostname, code, nil)
}

func RequestRedirectWithHeaders(ctx context.Context, hostname, code string, headers http.Header) (HTTPResult, error) {
	base := strings.TrimRight(strings.TrimSpace(os.Getenv("GOJET_REDIRECT_URL")), "/")
	if base == "" {
		return HTTPResult{}, fmt.Errorf("GOJET_REDIRECT_URL is required")
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, base+"/"+code, nil)
	if err != nil {
		return HTTPResult{}, err
	}
	req.Host = hostname
	for name, values := range headers {
		for _, value := range values {
			req.Header.Add(name, value)
		}
	}
	client := &http.Client{
		Timeout:       5 * time.Second,
		CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse },
	}
	resp, err := client.Do(req)
	if err != nil {
		return HTTPResult{}, err
	}
	defer resp.Body.Close()
	raw, err := io.ReadAll(io.LimitReader(resp.Body, 256*1024))
	if err != nil {
		return HTTPResult{}, err
	}
	return HTTPResult{Status: resp.StatusCode, Location: resp.Header.Get("Location"), Body: string(raw), Headers: resp.Header.Clone()}, nil
}

func PutRuntimeDecision(ctx context.Context, runtime *links.RedisRiskStore, link LinkFixture, state links.RiskState, ttl time.Duration) (links.RiskDecision, error) {
	return runtime.PutDecision(ctx, link.ID, link.Fingerprint, state, "p16-runtime-policy-v1", ttl)
}

func PutMalformedRuntime(ctx context.Context, client *redis.Client, link LinkFixture, raw string, ttl time.Duration) error {
	return client.Set(ctx, links.RiskDecisionKey(link.ID, link.Fingerprint), raw, ttl).Err()
}

func PutStaleRuntime(ctx context.Context, client *redis.Client, link LinkFixture, now time.Time) error {
	decision := links.RiskDecision{
		SchemaVersion: 1,
		Decision:      links.RiskAllow,
		Fingerprint:   link.Fingerprint,
		CheckedAt:     now.Add(-2 * time.Hour).UTC(),
		ValidUntil:    now.Add(-time.Hour).UTC(),
		PolicyVersion: "p16-runtime-policy-v1",
	}
	raw, err := json.Marshal(decision)
	if err != nil {
		return err
	}
	return client.Set(ctx, links.RiskDecisionKey(link.ID, link.Fingerprint), raw, time.Minute).Err()
}

func FinalizeDecision(ctx context.Context, store *trust.Store, link LinkFixture, policy trust.DestinationPolicy, suffix string, outcome trust.ProviderOutcome) (trust.DestinationDecision, error) {
	key := "p16-runtime-" + suffix
	enqueued, err := store.EnqueueDestinationScan(ctx, trust.EnqueueDestinationScanInput{
		WorkspaceID: link.WorkspaceID, LinkID: link.ID, RiskFingerprint: link.Fingerprint, PolicyVersion: policy.Version,
		RequestKind: trust.ScanRequestRescan, IdempotencyKey: key, CorrelationID: key, ActorID: "p16-runtime-worker", MaxAttempts: 3,
	})
	if err != nil {
		return trust.DestinationDecision{}, err
	}
	if _, err := store.RecordProviderObservation(ctx, trust.RecordProviderObservationInput{
		WorkspaceID: link.WorkspaceID, ScanID: enqueued.Scan.ID,
		Observation: trust.ProviderObservation{Provider: "semantic-fixture", Outcome: outcome, SignalCode: "runtime-" + string(outcome), Evidence: map[string]any{"fixture": "p16-runtime"}, ObservedAt: time.Now().UTC()},
		ActorID: "p16-runtime-worker", CorrelationID: key,
	}); err != nil {
		return trust.DestinationDecision{}, err
	}
	return store.FinalizeDestinationDecision(ctx, trust.FinalizeDestinationDecisionInput{
		WorkspaceID: link.WorkspaceID, ScanID: enqueued.Scan.ID, Policy: policy, LocalSafetyPassed: true,
		ActorID: "p16-runtime-worker", CorrelationID: key,
	})
}

func InsertRawDecision(ctx context.Context, db *sql.DB, store *trust.Store, link LinkFixture, policyVersion, suffix string, state trust.DecisionState, reason string, validUntil *time.Time) (trust.DestinationDecision, error) {
	key := "p16-runtime-raw-" + suffix
	enqueued, err := store.EnqueueDestinationScan(ctx, trust.EnqueueDestinationScanInput{
		WorkspaceID: link.WorkspaceID, LinkID: link.ID, RiskFingerprint: link.Fingerprint, PolicyVersion: policyVersion,
		RequestKind: trust.ScanRequestRescan, IdempotencyKey: key, CorrelationID: key, ActorID: "p16-runtime-worker", MaxAttempts: 3,
	})
	if err != nil {
		return trust.DestinationDecision{}, err
	}
	now := time.Now().UTC().Truncate(time.Microsecond)
	metadata := `{"fixture":"p16-runtime-raw"}`
	res, err := db.ExecContext(ctx, `
INSERT INTO destination_risk_decisions
(workspace_id,link_id,scan_id,risk_fingerprint,policy_version,state,reason_category,decision_metadata_json,valid_until,decided_at,created_at)
VALUES (?,?,?,?,?,?,?,?,?,?,?)`, link.WorkspaceID, link.ID, enqueued.Scan.ID, link.Fingerprint, policyVersion, string(state), reason, metadata, validUntil, now, now)
	if err != nil {
		return trust.DestinationDecision{}, err
	}
	id, err := res.LastInsertId()
	if err != nil {
		return trust.DestinationDecision{}, err
	}
	return trust.DestinationDecision{ID: uint64(id), WorkspaceID: link.WorkspaceID, LinkID: link.ID, ScanID: enqueued.Scan.ID, RiskFingerprint: link.Fingerprint, PolicyVersion: policyVersion, State: state, ReasonCategory: reason, ValidUntil: validUntil, DecidedAt: now, CreatedAt: now}, nil
}

func ScalarInt(ctx context.Context, db *sql.DB, query string, args ...any) (int, error) {
	var n int
	err := db.QueryRowContext(ctx, query, args...).Scan(&n)
	return n, err
}

func AllTrue(checks map[string]bool) bool {
	if len(checks) == 0 {
		return false
	}
	for _, ok := range checks {
		if !ok {
			return false
		}
	}
	return true
}
