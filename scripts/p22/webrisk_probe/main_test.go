package main

import (
    "bytes"
    "context"
    "encoding/pem"
    "fmt"
    "io"
    "net"
    "net/http"
    "net/http/httptest"
    "os"
    "os/exec"
    "path/filepath"
    "testing"
    "time"

    "github.com/Techshrr/GoJet/internal/trust"
)

type fixtureResolver struct{}
func (fixtureResolver) LookupIPAddr(context.Context, string) ([]net.IPAddr, error) {
    return []net.IPAddr{{IP: net.ParseIP("93.184.215.14")}}, nil
}

func TestExistingConsumerAgainstPythonAdapter(t *testing.T) {
    root, err := filepath.Abs("../../../integrations/google-webrisk")
    if err != nil { t.Fatal(err) }
    ready := filepath.Join(t.TempDir(), "ready")
    ctx, cancel := context.WithCancel(context.Background())
    defer cancel()
    command := exec.CommandContext(ctx, "python3", "-B", filepath.Join(root, "fixture_server.py"), ready)
    var output bytes.Buffer
    command.Stdout, command.Stderr = &output, &output
    if err := command.Start(); err != nil { t.Fatal(err) }
    defer func() { cancel(); _ = command.Wait() }()
    var port string
    for i := 0; i < 100; i++ {
        data, err := os.ReadFile(ready)
        if err == nil { port = string(data); break }
        time.Sleep(25*time.Millisecond)
    }
    if port == "" { t.Fatal("Python fixture did not start") }
    // TLS proxy is a test fixture only. Production retains public-address policy;
    // only the injected test dialer routes the approved address to this listener.
    proxy := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
        defer r.Body.Close()
        client := &http.Client{Transport: &http.Transport{Proxy: nil}, Timeout: 3*time.Second}
        req, _ := http.NewRequestWithContext(r.Context(), "POST", "http://127.0.0.1:"+port+"/v1/check", r.Body)
        req.Header.Set("Content-Type", "application/json")
        response, err := client.Do(req)
        if err != nil { w.WriteHeader(503); return }
        defer response.Body.Close()
        w.WriteHeader(response.StatusCode)
        _, _ = io.Copy(w, response.Body)
    }))
    defer proxy.Close()
    ca := filepath.Join(t.TempDir(), "fixture-ca.pem")
    if err := os.WriteFile(ca, pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: proxy.Certificate().Raw}), 0600); err != nil { t.Fatal(err) }
    t.Setenv("SSL_CERT_FILE", ca)
    t.Setenv("SSL_CERT_DIR", t.TempDir())
    dialer := trust.DialContextFunc(func(ctx context.Context, network, address string) (net.Conn, error) {
        return (&net.Dialer{}).DialContext(ctx, "tcp", proxy.Listener.Addr().String())
    })
    client := trust.SemanticProviderClient{Name: "google-web-risk", Endpoint: "https://example.com/v1/check",
        HTTPClient: trust.NewInspectionHTTPClient(fixtureResolver{}, dialer)}
    policy := trust.DestinationPolicy{Version: "linketic-risk-v1", RequiredProviders: []string{"google-web-risk"}, AllowTTL: time.Minute}
    cases := []struct { target string; outcome trust.ProviderOutcome; decision trust.DecisionState }{
        {"https://example.com/", trust.ProviderAllow, trust.DecisionAllow},
        {"https://example.com/malware", trust.ProviderBlock, trust.DecisionBlock},
        {"https://example.com/quota", trust.ProviderUnavailable, trust.DecisionReview},
        {"https://example.com/malformed", trust.ProviderUnavailable, trust.DecisionReview},
        {"http://127.0.0.1/", trust.ProviderUnavailable, trust.DecisionReview},
    }
    for _, item := range cases {
        t.Run(item.target, func(t *testing.T) {
            got, err := client.Observe(context.Background(), item.target)
            if err != nil || got.Outcome != item.outcome { t.Fatalf("observation %v / %v", got, err) }
            result, err := trust.EvaluateDestinationPolicy(policy, []trust.ProviderObservation{got}, true, time.Now())
            if err != nil || result.State != item.decision { t.Fatalf("decision %v / %v", result, err) }
        })
    }
    // Explicit regression check: production loopback endpoints remain forbidden.
    client.Endpoint = fmt.Sprintf("http://127.0.0.1:%s/v1/check", port)
    got, err := client.Observe(context.Background(), "https://example.com/")
    if err != nil || got.Outcome != trust.ProviderUnavailable { t.Fatalf("loopback policy changed: %v / %v", got, err) }
}
