package main

import (
	"context"
	"database/sql"
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"strings"
	"time"

	"github.com/Techshrr/GoJet/internal/billing"
	_ "github.com/go-sql-driver/mysql"
)

func main() {
	action := flag.String("action", "", "entitlement-expiring")
	horizonHours := flag.Int("horizon-hours", 192, "future horizon in hours")
	flag.Parse()

	if *action != "entitlement-expiring" {
		fatal(fmt.Errorf("unsupported --action %q", *action))
	}
	if *horizonHours <= 0 || *horizonHours > 30*24 {
		fatal(fmt.Errorf("invalid --horizon-hours"))
	}

	dsn := strings.TrimSpace(os.Getenv("GOJET_MYSQL_DSN"))
	if dsn == "" {
		fatal(fmt.Errorf("GOJET_MYSQL_DSN is required"))
	}
	db, err := sql.Open("mysql", dsn)
	if err != nil {
		fatal(err)
	}
	defer db.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	if err := db.PingContext(ctx); err != nil {
		fatal(err)
	}

	result, err := billing.NewStore(db).ProduceExpiringEntitlementNotifications(
		ctx,
		time.Now().UTC(),
		time.Duration(*horizonHours)*time.Hour,
	)
	if err != nil {
		fatal(err)
	}
	emit(result)
}

func emit(value any) {
	if err := json.NewEncoder(os.Stdout).Encode(value); err != nil {
		fatal(err)
	}
}

func fatal(err error) {
	_ = json.NewEncoder(os.Stdout).Encode(map[string]any{"error": err.Error()})
	os.Exit(1)
}
