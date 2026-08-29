package main

import (
	"context"
	"database/sql"
	"net"
	"os"
	"os/exec"
	"strings"
	"time"

	adminaccess "github.com/Techshrr/GoJet/internal/admin"
	"github.com/redis/go-redis/v9"
)

type adminRuntimeProbe struct {
	db    *sql.DB
	redis *redis.Client
}

func (p adminRuntimeProbe) Probe(ctx context.Context, serviceID string) map[string]bool {
	deps := map[string]bool{"unit": probeSystemdUnit(ctx, serviceID)}
	if runtimeNeedsMySQL(serviceID) {
		probeCtx, cancel := context.WithTimeout(ctx, 2*time.Second)
		deps["mysql"] = p.db != nil && p.db.PingContext(probeCtx) == nil
		cancel()
	}
	if runtimeNeedsRedis(serviceID) {
		probeCtx, cancel := context.WithTimeout(ctx, 2*time.Second)
		deps["redis"] = p.redis != nil && p.redis.Ping(probeCtx).Err() == nil
		cancel()
	}
	if serviceID == "fileworker" {
		deps["clamav"] = probeTCPDependency(ctx, os.Getenv("GOJET_CLAMAV_NETWORK"), os.Getenv("GOJET_CLAMAV_ADDRESS"))
	}
	return deps
}

func runtimeNeedsMySQL(serviceID string) bool {
	switch serviceID {
	case "redirectengine", "analyticsworker", "analyticsreconciler", "platformapi", "mailworker", "fileworker", "operationsmonitor":
		return true
	default:
		return false
	}
}

func runtimeNeedsRedis(serviceID string) bool {
	switch serviceID {
	case "redirectengine", "analyticsworker", "platformapi", "operationsmonitor":
		return true
	default:
		return false
	}
}

func probeTCPDependency(ctx context.Context, network, address string) bool {
	network, address = strings.TrimSpace(network), strings.TrimSpace(address)
	if network == "" || address == "" {
		return false
	}
	dialer := net.Dialer{Timeout: 2 * time.Second}
	conn, err := dialer.DialContext(ctx, network, address)
	if err != nil {
		return false
	}
	_ = conn.Close()
	return true
}

var adminSystemdUnits = map[string]string{
	"redirectengine":      "gojet-redirectengine.service",
	"analyticsworker":     "gojet-analyticsworker.service",
	"analyticsreconciler": "gojet-analyticsreconciler.service",
	"platformapi":         "gojet-platformapi.service",
	"mailworker":          "gojet-mailworker.service",
	"fileworker":          "gojet-fileworker.service",
	"operationsmonitor":   "gojet-operationsmonitor.service",
	"logreceiver":         "gojet-logreceiver.service",
}

func adminSystemdUnit(serviceID string) (string, bool) {
	unit, ok := adminSystemdUnits[strings.TrimSpace(serviceID)]
	return unit, ok
}

func probeSystemdUnit(ctx context.Context, serviceID string) bool {
	unit, ok := adminSystemdUnit(serviceID)
	if !ok {
		return false
	}
	probeCtx, cancel := context.WithTimeout(ctx, 2*time.Second)
	defer cancel()
	return exec.CommandContext(probeCtx, "systemctl", "is-active", "--quiet", unit).Run() == nil
}

type systemdAdminRestarter struct{}

func (systemdAdminRestarter) Restart(ctx context.Context, serviceID string) error {
	unit, ok := adminSystemdUnit(serviceID)
	if !ok {
		return adminaccess.ErrInvalid
	}
	cmd := exec.CommandContext(ctx, "systemctl", "restart", unit)
	cmd.Stdout = nil
	cmd.Stderr = nil
	return cmd.Run()
}

func buildAdminOperationsGovernance(service *adminaccess.Service, db *sql.DB, redisClient *redis.Client) (*adminaccess.OperationsGovernance, error) {
	var restarter adminaccess.ServiceRestarter
	if os.Getenv("GOJET_ADMIN_RESTART_ENABLED") == "1" {
		restarter = systemdAdminRestarter{}
	}
	return adminaccess.NewOperationsGovernance(service, adminRuntimeProbe{db: db, redis: redisClient}, restarter)
}
