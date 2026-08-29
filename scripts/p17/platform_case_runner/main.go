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

type output struct {
	Case         string          `json:"case"`
	Status       string          `json:"status"`
	MySQLVersion string          `json:"mysql_version"`
	RedisVersion string          `json:"redis_version"`
	Fixture      string          `json:"fixture"`
	RecordCounts map[string]int  `json:"record_counts"`
	Checks       map[string]bool `json:"checks"`
}

func main() {
	caseID := flag.String("case", "", "frozen P17 platform governance case id")
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
	case "P17-T016":
		out, err = runT016(ctx, runtime)
	case "P17-T017":
		out, err = runT017(ctx, runtime)
	case "P17-T018":
		out, err = runT018(ctx, runtime)
	case "P17-T019":
		out, err = runT019(ctx, runtime)
	case "P17-T020":
		out, err = runT020(ctx, runtime)
	case "P17-T021":
		out, err = runT021(ctx, runtime)
	default:
		return output{Case: caseID, Status: "FAIL"}, fmt.Errorf("unsupported P17 platform governance case %q", caseID)
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
