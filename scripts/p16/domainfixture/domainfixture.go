package domainfixture

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"fmt"
	"os"
	"strings"
	"time"

	_ "github.com/go-sql-driver/mysql"

	"github.com/Techshrr/GoJet/internal/trust"
)

type DomainFixture struct {
	ID          uint64
	WorkspaceID string
	Hostname    string
}

type Provider struct {
	Name       string
	Outcome    trust.ProviderOutcome
	SignalCode string
	Evidence   map[string]any
	Err        error
}

func (p Provider) Observe(_ context.Context, _ string) (trust.ProviderObservation, error) {
	if p.Err != nil {
		return trust.ProviderObservation{}, p.Err
	}
	return trust.ProviderObservation{
		Provider:   strings.TrimSpace(p.Name),
		Outcome:    p.Outcome,
		SignalCode: strings.TrimSpace(p.SignalCode),
		Evidence:   p.Evidence,
		ObservedAt: time.Now().UTC().Truncate(time.Microsecond),
	}, nil
}

func OpenDB() (*sql.DB, error) {
	dsn := strings.TrimSpace(os.Getenv("GOJET_MYSQL_DSN"))
	if dsn == "" {
		return nil, fmt.Errorf("GOJET_MYSQL_DSN is required")
	}
	db, err := sql.Open("mysql", dsn)
	if err != nil {
		return nil, err
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if err := db.PingContext(ctx); err != nil {
		_ = db.Close()
		return nil, err
	}
	return db, nil
}

func CreateReadyDomain(ctx context.Context, db *sql.DB, suffix string, now time.Time) (DomainFixture, error) {
	suffix = strings.ToLower(strings.TrimSpace(suffix))
	if db == nil || suffix == "" || now.IsZero() {
		return DomainFixture{}, fmt.Errorf("invalid domain fixture")
	}
	now = now.UTC().Truncate(time.Microsecond)
	workspace := "p16-" + suffix + "-workspace"
	hostname := suffix + ".p16.example.com"

	if _, err := db.ExecContext(ctx, `
INSERT INTO custom_domain_entitlement_sources
(workspace_id,source,source_key,status,domain_limit,starts_at,expires_at,created_at,updated_at)
VALUES (?,'plan',?,'active',10,?,NULL,?,?)`, workspace, "p16-domain-"+suffix, now.Add(-time.Hour), now, now); err != nil {
		return DomainFixture{}, err
	}
	if _, err := db.ExecContext(ctx, `
INSERT INTO custom_domain_usage (workspace_id,allocated_count,version,updated_at)
VALUES (?,1,1,?)
ON DUPLICATE KEY UPDATE allocated_count=GREATEST(allocated_count,1),updated_at=VALUES(updated_at)`, workspace, now); err != nil {
		return DomainFixture{}, err
	}
	secretHash := sha256.Sum256([]byte("p16-domain-fixture-secret-" + suffix))
	result, err := db.ExecContext(ctx, `
INSERT INTO custom_domains
(workspace_id,hostname_ascii,display_hostname,routing_state,ownership_status,ingress_dns_status,https_status,risk_status,
 ownership_token_version,ownership_secret_hash,ownership_secret_issued_at,ownership_verified_at,ingress_dns_checked_at,https_checked_at,
 risk_checked_at,risk_policy_version,risk_evidence_ref,created_at,updated_at)
VALUES (?,?,?,'enabled','verified','valid','active','allow',1,?,?,?,?,?,?,?,?,?,?)`,
		workspace, hostname, hostname, secretHash[:], now.Add(-time.Hour), now.Add(-30*time.Minute), now.Add(-30*time.Minute),
		now.Add(-30*time.Minute), now.Add(-30*time.Minute), "p16-domain-fixture-v1", "p16://redacted", now, now)
	if err != nil {
		return DomainFixture{}, err
	}
	id, err := result.LastInsertId()
	if err != nil || id <= 0 {
		if err == nil {
			err = fmt.Errorf("invalid inserted domain id")
		}
		return DomainFixture{}, err
	}
	return DomainFixture{ID: uint64(id), WorkspaceID: workspace, Hostname: hostname}, nil
}

func ScalarInt(ctx context.Context, db *sql.DB, query string, args ...any) (int, error) {
	var value int
	err := db.QueryRowContext(ctx, query, args...).Scan(&value)
	return value, err
}

func ScalarString(ctx context.Context, db *sql.DB, query string, args ...any) (string, error) {
	var value string
	err := db.QueryRowContext(ctx, query, args...).Scan(&value)
	return value, err
}

func MySQLVersion(ctx context.Context, db *sql.DB) (string, error) {
	return ScalarString(ctx, db, "SELECT VERSION()")
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
