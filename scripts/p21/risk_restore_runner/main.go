// Native MySQL/Redis fixture for bounded recovery and continuous projection.
package main

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"os"
	"os/exec"
	"strconv"
	"strings"
	"time"

	"github.com/Techshrr/GoJet/internal/links"
	"github.com/Techshrr/GoJet/internal/trust"
	"github.com/Techshrr/GoJet/scripts/p16/runtimefixture"
)

const workspace = "p21-risk-restore"

func must(err error) {
	if err != nil {
		panic(err)
	}
}
func need(ok bool, message string) {
	if !ok {
		panic(message)
	}
}

func main() {
	seedOnly := flag.Bool("seed-only", false, "seed the disposable upgrade database")
	checkOnly := flag.Bool("check-only", false, "check the restored disposable projection")
	flag.Parse()
	defer func() {
		if r := recover(); r != nil {
			fmt.Fprintln(os.Stderr, "risk restore fixture failed:", r)
			os.Exit(1)
		}
	}()
	ctx, cancel := context.WithTimeout(context.Background(), 90*time.Second)
	defer cancel()
	db, err := sql.Open("mysql", os.Getenv("GOJET_MYSQL_DSN"))
	must(err)
	defer db.Close()
	var database string
	must(db.QueryRowContext(ctx, "SELECT DATABASE()").Scan(&database))
	need(strings.HasPrefix(database, "gojet_p21_"), "disposable database required")
	redisDB, err := strconv.Atoi(os.Getenv("GOJET_REDIS_DB"))
	must(err)
	client := links.NewRedisClient(os.Getenv("GOJET_REDIS_ADDR"), os.Getenv("GOJET_REDIS_PASSWORD"), redisDB)
	defer client.Close()
	runtime := links.NewRedisRiskStore(client)
	must(runtime.Ping(ctx))
	policy := os.Getenv("GOJET_RISK_POLICY_VERSION")
	need(policy != "", "explicit policy required")
	store := trust.NewStore(db)
	projector := &trust.RiskProjector{Store: store, Runtime: runtime, PolicyVersion: policy, MaxTTL: 5 * time.Minute, BatchSize: 100}
	if !*checkOnly {
		var count int
		must(db.QueryRowContext(ctx, "SELECT COUNT(*) FROM links WHERE workspace_id=?", workspace).Scan(&count))
		need(count == 0, "fixture already exists")
		for i := 0; i < 209; i++ {
			code := fmt.Sprintf("restore-%03d", i)
			link, err := runtimefixture.CreateLink(ctx, db, workspace, "risk.example.test", "official", code, "https://safe.example/"+code, nil, nil)
			must(err)
			state := []trust.DecisionState{trust.DecisionAllow, trust.DecisionReview, trust.DecisionBlock}[i%3]
			until := time.Now().UTC().Add(30 * time.Minute)
			if i == 205 {
				state = trust.DecisionAllow
				until = time.Now().UTC().Add(-time.Minute)
			}
			if i == 206 {
				state = trust.DecisionUnknown
			}
			if i == 208 {
				state = trust.DecisionReview
			}
			decision, err := runtimefixture.InsertRawDecision(ctx, db, store, link, policy, code, state, "isolated-recovery", &until)
			must(err)
			if i == 207 {
				target := link.Primary + "/changed"
				fingerprint, _, err := links.RiskFingerprint(target, nil, nil)
				must(err)
				_, err = db.ExecContext(ctx, "UPDATE links SET primary_destination=?,risk_fingerprint=? WHERE id=?", target, fingerprint, link.ID)
				must(err)
			}
			if i == 208 {
				now := time.Now().UTC()
				_, err = store.CreateDestinationOverride(ctx, trust.CreateDestinationOverrideInput{
					WorkspaceID: workspace, LinkID: link.ID, RiskFingerprint: link.Fingerprint, PolicyVersion: policy,
					Decision: trust.DecisionAllow, Reason: "isolated recovery override", ActorID: "p21-fixture", CorrelationID: "p21-recovery-override", ExpiresAt: until,
					PolicyContext: trust.DestinationOverridePolicyContext{AuthorityVersion: trust.OverrideAuthorityVersion, PolicyVersion: policy, BaseDecisionID: decision.ID, BaseDecisionState: decision.State},
				}, &runtimefixture.PermissionFixture{Allow: true}, now)
				must(err)
			}
		}
	}
	type row struct {
		id                uint64
		fingerprint, code string
	}
	rows, err := db.QueryContext(ctx, "SELECT id,risk_fingerprint,code FROM links WHERE workspace_id=? ORDER BY id", workspace)
	must(err)
	var fixtures []row
	for rows.Next() {
		var r row
		must(rows.Scan(&r.id, &r.fingerprint, &r.code))
		fixtures = append(fixtures, r)
	}
	must(rows.Err())
	must(rows.Close())
	need(len(fixtures) == 209, "incomplete fixture inventory")
	check := func() {
		for _, r := range fixtures {
			i, err := strconv.Atoi(strings.TrimPrefix(r.code, "restore-"))
			must(err)
			decision, state, err := runtime.Resolve(ctx, r.id, r.fingerprint, time.Now().UTC())
			must(err)
			want := []links.RiskState{links.RiskAllow, links.RiskReview, links.RiskBlock}[i%3]
			if i >= 205 && i <= 207 {
				want = links.RiskMissing
			}
			if i == 208 {
				want = links.RiskAllow
			}
			need(state == want, fmt.Sprintf("projection mismatch at %d: %s", i, state))
			if state == links.RiskAllow {
				authority, err := store.CurrentDestinationAuthority(ctx, workspace, r.id, policy, time.Now().UTC())
				must(err)
				need(authority.ValidUntil != nil && !decision.ValidUntil.After(*authority.ValidUntil), "allow deadline extended")
			}
		}
	}
	if *checkOnly {
		check()
		fmt.Println(`{"risk_projection_restored":true,"links_checked":209}`)
		return
	}
	if *seedOnly {
		// Completed fixture jobs prevent any external provider request by live services.
		_, err = db.ExecContext(ctx, "UPDATE destination_risk_scans SET status='completed',completed_at=UTC_TIMESTAMP(6) WHERE workspace_id=?", workspace)
		must(err)
		result, err := projector.Rebuild(ctx, 100000)
		must(err)
		need(result.Complete, "seed projection incomplete")
		check()
		fmt.Println(`{"risk_projection_seeded":true,"links":209}`)
		return
	}
	for range 3 {
		_, err = projector.RunOnce(ctx)
		must(err)
	}
	check()
	_, err = projector.RunOnce(ctx)
	must(err) // wraps to refresh the beginning after the final page
	clear := func() {
		for _, r := range fixtures {
			must(runtime.DeleteDecision(ctx, r.id, r.fingerprint))
		}
	}
	defer clear()
	clear()
	bounded, err := projector.Rebuild(ctx, 100)
	need(err != nil && !bounded.Complete && bounded.Visited == 100, "truncated rebuild claimed success")
	canceled, stop := context.WithCancel(ctx)
	stop()
	failed, err := projector.Rebuild(canceled, 100000)
	need(err != nil && !failed.Complete, "canceled rebuild claimed success")
	clear()
	// Even a stale allow for an unknown decision must be removed, not skipped.
	_, err = runtime.PutDecision(ctx, fixtures[206].id, fixtures[206].fingerprint, links.RiskAllow, policy, time.Minute)
	must(err)
	keep := "gojet:p21:risk-restore:unrelated"
	stream := "gojet:p21:risk-restore:events"
	ok, err := client.SetNX(ctx, keep, "retain", time.Minute).Result()
	must(err)
	need(ok, "foreign fixture key")
	defer client.Del(context.Background(), keep, stream)
	must(client.Do(ctx, "XADD", stream, "*", "event", "retain").Err())
	snapshot := func() []string {
		var result []string
		for _, table := range []string{"destination_risk_scans", "destination_risk_decisions", "destination_risk_overrides", "destination_risk_audit_events"} {
			var name, sum string
			must(db.QueryRowContext(ctx, "CHECKSUM TABLE "+table+" EXTENDED").Scan(&name, &sum))
			result = append(result, sum)
		}
		return result
	}
	before := strings.Join(snapshot(), ",")
	command := exec.CommandContext(ctx, os.Getenv("P21_OPSMONITOR_BIN"), "--restore-risk-projections")
	command.Env = os.Environ() // deliberately no provider configuration; recovery is local authority only
	raw, err := command.CombinedOutput()
	need(err == nil, "native projection command failed")
	var report trust.ProjectionRestore
	must(json.Unmarshal(raw, &report))
	need(report.Complete && report.Visited == 208 && report.Projected == 206 && report.Withheld == 2, "wrong complete inventory")
	check()
	need(before == strings.Join(snapshot(), ","), "rebuild consumed jobs or changed durable authority")
	again, err := projector.Rebuild(ctx, 100000)
	must(err)
	need(again == report, "repeated rebuild differs")
	need(client.Get(ctx, keep).Val() == "retain" && client.XLen(ctx, stream).Val() == 1, "unrelated Redis data changed")
	broken := links.NewRedisClient(os.Getenv("GOJET_REDIS_ADDR"), "", redisDB)
	must(broken.Close())
	projector.Runtime = links.NewRedisRiskStore(broken)
	failed, err = projector.Rebuild(ctx, 100000)
	need(err != nil && !failed.Complete, "failed Redis rebuild claimed success")
	need(!errors.Is(err, trust.ErrNotFound), "unexpected failure classification")
	fmt.Println(`{"native_mysql":true,"native_redis":true,"links":209,"background_pagination":true,"complete_rebuild":true,"native_recovery_command":true,"unknown_expired_changed_targets_withheld":true,"manual_override_preserved":true,"absolute_allow_deadline_preserved":true,"bounded_and_canceled_failure":true,"redis_failure_blocks_completion":true,"jobs_authority_and_unrelated_redis_preserved":true,"production_acceptance":false}`)
}
