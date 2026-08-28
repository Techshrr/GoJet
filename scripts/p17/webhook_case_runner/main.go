package main

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"time"

	"github.com/Techshrr/GoJet/scripts/p17/adminfixture"
)

type caseResult struct {
	Case         string          `json:"case"`
	Status       string          `json:"status"`
	MySQLVersion string          `json:"mysql_version"`
	RedisVersion string          `json:"redis_version"`
	Fixture      string          `json:"fixture"`
	Checks       map[string]bool `json:"checks"`
	RecordCounts map[string]int  `json:"record_counts"`
}

func main() {
	caseID := flag.String("case", "", "P17 case")
	flag.Parse()
	ctx, cancel := context.WithTimeout(context.Background(), 45*time.Second)
	defer cancel()
	runtime, err := adminfixture.Open()
	if err != nil {
		fail(*caseID, err)
	}
	defer runtime.Close()
	if err := runtime.DB.PingContext(ctx); err != nil {
		fail(*caseID, err)
	}
	if err := runtime.Redis.Ping(ctx).Err(); err != nil {
		fail(*caseID, err)
	}
	mysqlVersion, err := adminfixture.MySQLVersion(ctx, runtime.DB)
	if err != nil {
		fail(*caseID, err)
	}
	redisVersion, err := adminfixture.RedisVersion(ctx, runtime.Redis)
	if err != nil {
		fail(*caseID, err)
	}
	var checks map[string]bool
	var counts map[string]int
	switch *caseID {
	case "P17-T025":
		checks, counts, err = runT025(ctx, runtime)
	case "P17-T026":
		checks, counts, err = runT026(ctx, runtime)
	case "P17-T027":
		checks, counts, err = runT027(ctx, runtime)
	case "P17-T028":
		checks, counts, err = runT028(ctx, runtime)
	case "P17-T029":
		checks, counts, err = runT029(ctx, runtime)
	default:
		err = fmt.Errorf("unsupported case %q", *caseID)
	}
	status := "PASS"
	if err != nil || !adminfixture.AllTrue(checks) {
		status = "FAIL"
	}
	result := caseResult{
		Case: *caseID, Status: status, MySQLVersion: mysqlVersion, RedisVersion: redisVersion,
		Fixture: "real MySQL/Redis + production Workspace outbound-webhook authority + deterministic DNS/socket fixture",
		Checks: checks, RecordCounts: counts,
	}
	_ = json.NewEncoder(os.Stdout).Encode(result)
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	if status != "PASS" {
		os.Exit(1)
	}
}

func fail(caseID string, err error) {
	_ = json.NewEncoder(os.Stdout).Encode(caseResult{Case: caseID, Status: "FAIL", Checks: map[string]bool{"runner": false}})
	fmt.Fprintln(os.Stderr, err)
	os.Exit(1)
}
