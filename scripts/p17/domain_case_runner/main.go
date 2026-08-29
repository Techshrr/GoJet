package main

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"time"

	adminaccess "github.com/Techshrr/GoJet/internal/admin"
	"github.com/Techshrr/GoJet/scripts/p17/adminfixture"
)

type output struct {
	Case         string          `json:"case"`
	Status       string          `json:"status"`
	MySQLVersion string          `json:"mysql_version"`
	RedisVersion string          `json:"redis_version"`
	Fixture      string          `json:"fixture"`
	RecordCounts map[string]int  `json:"record_counts"`
	Checks       map[string]bool `json:"checks"`
}

const (
	rootEmail    = "root-admin@p17.test"
	rootPassword = "P17-root-password-fixture-2026"
)

func main() {
	caseID := flag.String("case", "", "frozen P17 domain-entitlement case id")
	flag.Parse()
	out, err := run(*caseID)
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		out.Case = *caseID
		out.Status = "FAIL"
		if out.Checks == nil {
			out.Checks = map[string]bool{"runner_completed": false}
		}
	}
	_ = json.NewEncoder(os.Stdout).Encode(out)
	if err != nil || out.Status != "PASS" {
		os.Exit(1)
	}
}

func run(caseID string) (output, error) {
	runtime, err := adminfixture.Open()
	if err != nil {
		return output{Case: caseID, Status: "FAIL"}, err
	}
	defer runtime.Close()
	ctx, cancel := context.WithTimeout(context.Background(), 120*time.Second)
	defer cancel()
	if err := runtime.DB.PingContext(ctx); err != nil {
		return output{Case: caseID, Status: "FAIL"}, err
	}
	if err := runtime.Redis.Ping(ctx).Err(); err != nil {
		return output{Case: caseID, Status: "FAIL"}, err
	}
	if err := adminfixture.Reset(ctx, runtime); err != nil {
		return output{Case: caseID, Status: "FAIL"}, err
	}
	if err := adminaccess.VerifyDomainEntitlementSchema(ctx, runtime.DB); err != nil {
		return output{Case: caseID, Status: "FAIL"}, err
	}
	mysqlVersion, err := adminfixture.MySQLVersion(ctx, runtime.DB)
	if err != nil {
		return output{Case: caseID, Status: "FAIL"}, err
	}
	redisVersion, err := adminfixture.RedisVersion(ctx, runtime.Redis)
	if err != nil {
		return output{Case: caseID, Status: "FAIL"}, err
	}
	var out output
	switch caseID {
	case "P17-T006":
		out, err = runT006(ctx, runtime)
	case "P17-T007":
		out, err = runT007(ctx, runtime)
	case "P17-T008":
		out, err = runT008(ctx, runtime)
	case "P17-T009":
		out, err = runT009(ctx, runtime)
	default:
		return output{Case: caseID, Status: "FAIL"}, fmt.Errorf("unsupported P17 domain-entitlement case %q", caseID)
	}
	out.Case = caseID
	out.MySQLVersion = mysqlVersion
	out.RedisVersion = redisVersion
	return out, err
}

func newOutput(fixture string) output {
	return output{Status: "FAIL", Fixture: fixture, RecordCounts: map[string]int{}, Checks: map[string]bool{}}
}

func pass(out *output) {
	if adminfixture.AllTrue(out.Checks) {
		out.Status = "PASS"
	}
}

func newDomainHTTPServer(service *adminaccess.Service) (*httptest.Server, error) {
	api, err := adminaccess.NewHTTPAPI(service)
	if err != nil {
		return nil, err
	}
	root := http.NewServeMux()
	root.Handle("/", api.Handler())
	domainHandler := api.DomainEntitlementHandler()
	for _, pattern := range []string{
		"GET /api/admin/domain-entitlements",
		"GET /api/admin/domain-entitlements/{workspaceId}",
		"POST /api/admin/domain-entitlements/{workspaceId}/decisions",
	} {
		root.Handle(pattern, domainHandler)
	}
	return httptest.NewServer(root), nil
}
