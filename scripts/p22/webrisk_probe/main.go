// Live, read-only acceptance using the same client and policy as operationsmonitor.
package main

import (
    "context"
    "encoding/json"
    "flag"
    "os"
    "time"

    "github.com/Techshrr/GoJet/internal/trust"
)

var source = "development"

func main() {
    endpoint := flag.String("endpoint", "", "semantic provider HTTPS endpoint")
    flag.Parse()
    if *endpoint == "" || flag.NArg() != 0 { os.Exit(2) }
    client := trust.SemanticProviderClient{Name: "google-web-risk", Endpoint: *endpoint,
        HTTPClient: trust.NewInspectionHTTPClient(nil, nil)}
    checks := map[string]bool{}
    observations := map[string]trust.ProviderObservation{}
    cases := []struct { name, target string; outcome trust.ProviderOutcome }{
        {"no_match", "https://example.com/", trust.ProviderAllow},
        {"malware", "http://testsafebrowsing.appspot.com/s/malware.html", trust.ProviderBlock},
        {"invalid_target", "http://127.0.0.1/", trust.ProviderUnavailable},
    }
    for _, item := range cases {
        ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
        observation, err := client.Observe(ctx, item.target)
        cancel()
        checks[item.name] = err == nil && observation.Outcome == item.outcome
        observations[item.name] = observation
    }
    // A provider observation alone must never override the local safety gate.
    policy := trust.DestinationPolicy{Version: "linketic-risk-v1", RequiredProviders: []string{"google-web-risk"}, AllowTTL: 15*time.Minute}
    evaluation, err := trust.EvaluateDestinationPolicy(policy,
        []trust.ProviderObservation{observations["no_match"]}, false, time.Now())
    checks["local_safety_gate_retained"] = err == nil && evaluation.State == trust.DecisionReview
    passed := true
    for _, ok := range checks { passed = passed && ok }
    report := map[string]any{"schema": "gojet.webrisk.consumer-probe.v1", "source": source,
        "passed": passed, "checks": checks, "observations": observations,
        "business_installation_complete": false}
    if json.NewEncoder(os.Stdout).Encode(report) != nil { os.Exit(1) }
    if !passed { os.Exit(1) }
}
