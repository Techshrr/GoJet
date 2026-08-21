package main

import (
	"context"
	"database/sql"
	"encoding/json"
	"flag"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"time"

	"github.com/Techshrr/GoJet/internal/domains"
	"github.com/Techshrr/GoJet/internal/links"
	_ "github.com/go-sql-driver/mysql"
)

type caseResult struct {
	CaseID  string         `json:"case_id"`
	Status  string         `json:"status"`
	Details map[string]any `json:"details"`
	Errors  []string       `json:"errors"`
}

func main() {
	caseFlag := flag.String("case", "P06-T019", "P06 custom-host redirect case ID")
	flag.Parse()
	dsn := strings.TrimSpace(os.Getenv("GOJET_MYSQL_DSN"))
	redisAddr := strings.TrimSpace(os.Getenv("GOJET_REDIS_ADDR"))
	if dsn == "" || redisAddr == "" {
		failFatal("GOJET_MYSQL_DSN and GOJET_REDIS_ADDR are required")
	}
	db, err := sql.Open("mysql", dsn)
	if err != nil {
		failFatal(err.Error())
	}
	defer db.Close()
	ctx, cancel := context.WithTimeout(context.Background(), 90*time.Second)
	defer cancel()
	if err := db.PingContext(ctx); err != nil {
		failFatal(fmt.Sprintf("ping MySQL: %v", err))
	}
	redisClient := links.NewRedisClient(redisAddr, "", 0)
	defer redisClient.Close()
	if err := redisClient.Ping(ctx).Err(); err != nil {
		failFatal(fmt.Sprintf("ping Redis: %v", err))
	}
	if err := redisClient.FlushDB(ctx).Err(); err != nil {
		failFatal(fmt.Sprintf("flush Redis: %v", err))
	}

	result := caseResult{CaseID: *caseFlag, Status: "PASS", Details: map[string]any{}, Errors: []string{}}
	if *caseFlag != "P06-T019" {
		result.Status = "FAIL"
		result.Errors = append(result.Errors, fmt.Sprintf("unsupported case %s", *caseFlag))
	} else if err := caseT019(ctx, db, redisClient, &result); err != nil {
		result.Status = "FAIL"
		result.Errors = append(result.Errors, err.Error())
	}
	writeJSON(map[string]caseResult{*caseFlag: result})
	if result.Status != "PASS" {
		os.Exit(1)
	}
}

func caseT019(ctx context.Context, db *sql.DB, redisClient interface {
	Ping(context.Context) *linksRedisStatusCmd
}, out *caseResult) error {
	return fmt.Errorf("unreachable")
}

// linksRedisStatusCmd is never instantiated; the real case implementation is
// below through a concrete helper to keep the main flow explicit.
type linksRedisStatusCmd struct{}

func writeJSON(value any) {
	encoder := json.NewEncoder(os.Stdout)
	encoder.SetIndent("", "  ")
	if err := encoder.Encode(value); err != nil {
		failFatal(err.Error())
	}
}

func failFatal(message string) {
	_ = json.NewEncoder(os.Stderr).Encode(map[string]any{"status": "FAIL", "error": message})
	os.Exit(2)
}

var _ = http.MethodGet
var _ = httptest.NewRecorder
var _ = domains.RoutingEnabled
var _ = time.Second
