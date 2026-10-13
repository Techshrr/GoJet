// Drive the production P16 provider adapter, durable policy and projection APIs.
// Local protocol responses are explicit test inputs, never live-provider claims.
package main

import (
    "context"
    "database/sql"
    "encoding/json"
    "errors"
    "fmt"
    "net/http"
    "net/http/httptest"
    "os"
    "strconv"
    "time"

    _ "github.com/go-sql-driver/mysql"
    "github.com/Techshrr/GoJet/internal/links"
    "github.com/Techshrr/GoJet/internal/trust"
)

var stage = "configuration"

func main() {
    if err := run(); err != nil {
        fmt.Fprintln(os.Stderr, "T044 native policy preparation failed at", stage)
        os.Exit(1)
    }
}

func run() error {
    mode, workspace, head := os.Getenv("P20_RISK_MODE"), os.Getenv("P20_RISK_WORKSPACE"), os.Getenv("P20_EXACT_HEAD")
    id, err := strconv.ParseUint(os.Getenv("P20_RISK_LINK"), 10, 64)
    if err != nil || id == 0 || workspace == "" || len(head) != 40 { return fmt.Errorf("configuration") }
    phase := os.Getenv("P20_RISK_PHASE")
    ttl := time.Minute
    if mode == "allow-short" { ttl = 2*time.Second }
    policy := trust.DestinationPolicy{Version:"p20-t044-recovery-v1", RequiredProviders:[]string{"p20-protocol-provider"}, AllowTTL:ttl}
    db, err := sql.Open("mysql", os.Getenv("GOJET_MYSQL_DSN")); if err != nil { return err }; defer db.Close()
    ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second); defer cancel()
    stage = "read current fingerprint"
    var fp, target string
    if err := db.QueryRowContext(ctx, "SELECT risk_fingerprint,primary_destination FROM links WHERE id=? AND workspace_id=? AND deleted_at IS NULL", id, workspace).Scan(&fp,&target); err != nil { return err }
    store := trust.NewStore(db)
    key := "p20-t044-"+head[:12]+"-"+phase
    stage = "enqueue current target"
    scan, err := store.EnqueueDestinationScan(ctx, trust.EnqueueDestinationScanInput{WorkspaceID:workspace, LinkID:id, RiskFingerprint:fp, PolicyVersion:policy.Version, RequestKind:trust.ScanRequestRescan, IdempotencyKey:key, CorrelationID:key, ActorID:"p20-recovery-fixture", MaxAttempts:3})
    if err != nil { return err }
    out := map[string]any{"implementation_commit":head,"workspace_id":workspace,"link_id":id,"fingerprint":fp,"scan_id":scan.Scan.ID,"mode":mode,"policy_version":policy.Version,"protocol_fixture":true,"live_provider_claim":false}
    if mode == "pending" {
        _, err := store.FinalizeDestinationDecision(ctx, trust.FinalizeDestinationDecisionInput{WorkspaceID:workspace,ScanID:scan.Scan.ID,Policy:policy,LocalSafetyPassed:true,ActorID:"p20-recovery-fixture",CorrelationID:key})
        if !errors.Is(err,trust.ErrConflict) { return fmt.Errorf("pending unexpectedly finalized") }
        var decisions int
        if err := db.QueryRowContext(ctx,"SELECT COUNT(*) FROM destination_risk_decisions WHERE scan_id=?",scan.Scan.ID).Scan(&decisions); err != nil { return err }
        if decisions != 0 { return fmt.Errorf("pending has decision") }
        out["state"],out["decision_count"],out["pending_finalize_rejected"]="pending",decisions,true
        return json.NewEncoder(os.Stdout).Encode(out)
    }
    stage = "provider protocol observation"
    server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter,r *http.Request){
        w.Header().Set("Content-Type","application/json")
        switch mode {
        case "unavailable": w.WriteHeader(http.StatusServiceUnavailable)
        case "malformed": fmt.Fprint(w,"{")
        case "partial": fmt.Fprint(w,`{"complete":false,"verdict":"allow"}`)
        case "timeout": select { case <-r.Context().Done(): case <-time.After(time.Second): }
        case "review", "block": json.NewEncoder(w).Encode(map[string]any{"complete":true,"verdict":mode})
        case "allow", "allow-short": fmt.Fprint(w,`{"complete":true,"verdict":"allow"}`)
        default: w.WriteHeader(http.StatusBadRequest)
        }
    })); defer server.Close()
    client := trust.SemanticProviderClient{Name:"p20-protocol-provider",Endpoint:server.URL,HTTPClient:&http.Client{Timeout:150*time.Millisecond}}
    observation, err := client.Observe(ctx,target); if err != nil { return err }
    _, err = store.RecordProviderObservation(ctx, trust.RecordProviderObservationInput{WorkspaceID:workspace,ScanID:scan.Scan.ID,Observation:observation,ActorID:"p20-recovery-fixture",CorrelationID:key}); if err != nil { return err }
    stage = "native policy finalization"
    decision, err := store.FinalizeDestinationDecision(ctx,trust.FinalizeDestinationDecisionInput{WorkspaceID:workspace,ScanID:scan.Scan.ID,Policy:policy,LocalSafetyPassed:true,ActorID:"p20-recovery-fixture",CorrelationID:key}); if err != nil { return err }
    clientRedis := links.NewRedisClient(os.Getenv("GOJET_REDIS_ADDR"),os.Getenv("GOJET_REDIS_PASSWORD"),0); defer clientRedis.Close()
    stage = "native current projection"
    projected, err := trust.ProjectCurrentDestinationDecision(ctx,store,links.NewRedisRiskStore(clientRedis),workspace,id,policy.Version,time.Now().UTC(),time.Minute); if err != nil { return err }
    out["state"],out["provider_outcome"],out["signal_code"]=decision.State,observation.Outcome,observation.SignalCode
    out["decision_id"],out["reason"],out["valid_until"]=decision.ID,decision.ReasonCategory,decision.ValidUntil
    out["runtime_state"],out["runtime_fingerprint"]=projected.Runtime.Decision,projected.Runtime.Fingerprint
    return json.NewEncoder(os.Stdout).Encode(out)
}
