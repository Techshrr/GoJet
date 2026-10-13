// Prepare the inherited P16 controlled-provider decision fixture for the real
// T013 link. No click, analytics event, mail, notification or audit is fabricated.
package main

import (
 "context"
 "database/sql"
 "encoding/json"
 "fmt"
 "os"
 "time"
 _ "github.com/go-sql-driver/mysql"
 "github.com/Techshrr/GoJet/internal/links"
 "github.com/Techshrr/GoJet/internal/trust"
)

var stage = "read predecessor"

func main() {
 if err := run(); err != nil { fmt.Fprintln(os.Stderr, "T039 risk preparation failed at", stage); os.Exit(1) }
}

func run() error {
 var p struct { Status string `json:"status"`; Head string `json:"implementation_commit"`; Details struct { Workspace string `json:"workspace_id"`; Link uint64 `json:"link_id"` } `json:"details"` }
 raw, err := os.ReadFile("artifacts/v10/P20/p0/P20-T013.json"); if err != nil { return err }
 if err = json.Unmarshal(raw, &p); err != nil { return err }
 if p.Status != "PASS" || p.Head != os.Getenv("P20_EXACT_HEAD") || p.Details.Link == 0 { return fmt.Errorf("invalid predecessor") }
 stage = "open durable authority"
 db, err := sql.Open("mysql", os.Getenv("GOJET_MYSQL_DSN")); if err != nil { return err }; defer db.Close()
 ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second); defer cancel()
 var fp string
 if err = db.QueryRowContext(ctx, "SELECT risk_fingerprint FROM links WHERE id=? AND workspace_id=? AND deleted_at IS NULL", p.Details.Link, p.Details.Workspace).Scan(&fp); err != nil { return err }
 store := trust.NewStore(db)
 policy := trust.DestinationPolicy{Version:"p20-t039-recovery-v1",RequiredProviders:[]string{"p20-controlled-provider"},AllowTTL:15*time.Minute}
 key := "p20-t039-"+p.Head[:12]
 stage = "enqueue controlled observation"
 enqueued, err := store.EnqueueDestinationScan(ctx,trust.EnqueueDestinationScanInput{WorkspaceID:p.Details.Workspace,LinkID:p.Details.Link,RiskFingerprint:fp,PolicyVersion:policy.Version,RequestKind:trust.ScanRequestInitial,IdempotencyKey:key,CorrelationID:key,ActorID:"p20-recovery-fixture",MaxAttempts:3})
 if err != nil { return err }
 stage = "record controlled observation"
 _, err = store.RecordProviderObservation(ctx,trust.RecordProviderObservationInput{WorkspaceID:p.Details.Workspace,ScanID:enqueued.Scan.ID,Observation:trust.ProviderObservation{Provider:"p20-controlled-provider",Outcome:trust.ProviderAllow,SignalCode:"controlled-recovery-input",Evidence:map[string]any{"fixture":"P16 inherited controlled observation; not a live provider claim"},ObservedAt:time.Now().UTC()},ActorID:"p20-recovery-fixture",CorrelationID:key})
 if err != nil { return err }
 stage = "evaluate local policy"
 decision, err := store.FinalizeDestinationDecision(ctx,trust.FinalizeDestinationDecisionInput{WorkspaceID:p.Details.Workspace,ScanID:enqueued.Scan.ID,Policy:policy,LocalSafetyPassed:true,ActorID:"p20-recovery-fixture",CorrelationID:key})
 if err != nil { return err }
 client := links.NewRedisClient(os.Getenv("GOJET_REDIS_ADDR"),os.Getenv("GOJET_REDIS_PASSWORD"),0); defer client.Close()
 stage = "project initial decision"
 projected, err := trust.ProjectCurrentDestinationDecision(ctx,store,links.NewRedisRiskStore(client),p.Details.Workspace,p.Details.Link,policy.Version,time.Now().UTC(),5*time.Minute)
 if err != nil { return err }
 return json.NewEncoder(os.Stdout).Encode(map[string]any{"implementation_commit":p.Head,"workspace_id":p.Details.Workspace,"link_id":p.Details.Link,"fingerprint":fp,"decision_id":decision.ID,"scan_id":enqueued.Scan.ID,"policy_version":policy.Version,"state":projected.Runtime.Decision,"valid_until":decision.ValidUntil,"controlled_provider_fixture":true,"live_provider_claim":false})
}
