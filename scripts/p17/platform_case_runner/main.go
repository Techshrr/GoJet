package main

import (
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"os"
	"time"

	adminaccess "github.com/Techshrr/GoJet/internal/admin"
	"github.com/Techshrr/GoJet/scripts/p17/adminfixture"
)

type output struct {
	Case         string          `json:"case"`
	Status       string          `json:"status"`
	Fixture      string          `json:"fixture"`
	MySQLVersion string          `json:"mysql_version"`
	RedisVersion string          `json:"redis_version"`
	RecordCounts map[string]int  `json:"record_counts"`
	Checks       map[string]bool `json:"checks"`
}

const (
	fixturePassword = "p17-platform-governance-root-password-fixture"
	fixtureOrigin   = "https://admin.p17.test"
)

func main() {
	caseID := flag.String("case", "", "frozen P17 case")
	flag.Parse()
	if *caseID == "" {
		fmt.Fprintln(os.Stderr, "--case is required")
		os.Exit(2)
	}
	out, err := run(*caseID)
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
	}
	if err != nil {
		out.Status = "FAIL"
	}
	_ = json.NewEncoder(os.Stdout).Encode(out)
	if err != nil || out.Status != "PASS" {
		os.Exit(1)
	}
}

func run(caseID string) (output, error) {
	ctx := context.Background()
	runtime, err := adminfixture.Open(ctx)
	if err != nil {
		return output{Case: caseID, Status: "FAIL"}, err
	}
	defer runtime.Close()
	mysqlVersion, redisVersion, err := runtime.Versions(ctx)
	if err != nil {
		return output{Case: caseID, Status: "FAIL"}, err
	}
	out := output{Case: caseID, Status: "FAIL", MySQLVersion: mysqlVersion, RedisVersion: redisVersion, RecordCounts: map[string]int{}, Checks: map[string]bool{}}
	if err := adminaccess.VerifyPlatformGovernanceSchema(ctx, runtime.DB); err != nil {
		return out, err
	}
	switch caseID {
	case "P17-T016":
		return runT016(ctx, runtime)
	case "P17-T017":
		return runT017(ctx, runtime)
	case "P17-T018":
		return runT018(ctx, runtime)
	case "P17-T019":
		return runT019(ctx, runtime)
	case "P17-T020":
		return runT020(ctx, runtime)
	case "P17-T021":
		return runT021(ctx, runtime)
	default:
		return out, errors.New("case not implemented by platform runner")
	}
}

func newOutput(fixture string) output {
	return output{Status: "FAIL", Fixture: fixture, RecordCounts: map[string]int{}, Checks: map[string]bool{}}
}

func pass(out *output) {
	if len(out.Checks) == 0 {
		return
	}
	for _, ok := range out.Checks {
		if !ok {
			return
		}
	}
	out.Status = "PASS"
}

func bootstrapRoot(ctx context.Context, runtime *adminfixture.Runtime, now time.Time) (*adminaccess.Service, adminaccess.Principal, adminaccess.SessionSecret, error) {
	service, err := adminfixture.Bootstrap(ctx, runtime, fixturePassword, []string{adminaccess.PermissionSettingsManage, adminaccess.PermissionDomainsManage, adminaccess.PermissionContentManage, adminaccess.PermissionPlatformRead}, now)
	if err != nil {
		return nil, adminaccess.Principal{}, adminaccess.SessionSecret{}, err
	}
	login, err := service.Login(ctx, adminaccess.LoginInput{Email: adminfixture.RootEmail, Password: fixturePassword, TOTP: adminaccess.TOTPCode(adminfixture.TOTPSecret, now), IP: "203.0.113.77", CorrelationID: "p17-platform-login"}, now)
	if err != nil {
		return nil, adminaccess.Principal{}, adminaccess.SessionSecret{}, err
	}
	root, err := service.Authenticate(ctx, login.Token, now.Add(time.Second))
	return service, root, login, err
}
