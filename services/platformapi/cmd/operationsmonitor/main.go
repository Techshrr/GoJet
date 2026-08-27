package main

import (
	"context"
	"flag"
	"log/slog"
	"os"
	"os/signal"
	"strings"
	"syscall"
	"time"

	"github.com/Techshrr/GoJet/internal/links"
	"github.com/Techshrr/GoJet/internal/trust"
)

func main() {
	once := flag.Bool("once", false, "run one risk-worker and projection iteration, then exit")
	flag.Parse()
	logger := slog.New(slog.NewJSONHandler(os.Stdout, &slog.HandlerOptions{Level: slog.LevelInfo}))

	dsn := strings.TrimSpace(os.Getenv("GOJET_MYSQL_DSN"))
	redisAddr := strings.TrimSpace(os.Getenv("GOJET_REDIS_ADDR"))
	providerName := strings.TrimSpace(os.Getenv("GOJET_RISK_PROVIDER_NAME"))
	providerEndpoint := strings.TrimSpace(os.Getenv("GOJET_RISK_PROVIDER_ENDPOINT"))
	policyVersion := strings.TrimSpace(os.Getenv("GOJET_RISK_POLICY_VERSION"))
	if dsn == "" || redisAddr == "" || providerName == "" || providerEndpoint == "" || policyVersion == "" {
		logger.Error("required operationsmonitor configuration missing",
			"mysql_dsn_present", dsn != "",
			"redis_addr_present", redisAddr != "",
			"provider_name_present", providerName != "",
			"provider_endpoint_present", providerEndpoint != "",
			"policy_version_present", policyVersion != "")
		os.Exit(1)
	}

	db, err := links.OpenMySQL(dsn)
	if err != nil {
		logger.Error("operationsmonitor database unavailable")
		os.Exit(1)
	}
	defer db.Close()
	pingCtx, pingCancel := context.WithTimeout(context.Background(), 5*time.Second)
	if err := db.PingContext(pingCtx); err != nil {
		pingCancel()
		logger.Error("operationsmonitor database unavailable")
		os.Exit(1)
	}
	pingCancel()

	redisClient := links.NewRedisClient(redisAddr, os.Getenv("GOJET_REDIS_PASSWORD"), 0)
	defer redisClient.Close()
	runtimeRisk := links.NewRedisRiskStore(redisClient)
	redisCtx, redisCancel := context.WithTimeout(context.Background(), 5*time.Second)
	if err := runtimeRisk.Ping(redisCtx); err != nil {
		redisCancel()
		logger.Error("operationsmonitor Redis unavailable")
		os.Exit(1)
	}
	redisCancel()

	allowTTL := durationEnv("GOJET_RISK_ALLOW_TTL", 15*time.Minute, time.Minute, 24*time.Hour, logger)
	projectionTTL := durationEnv("GOJET_RISK_PROJECTION_TTL", 5*time.Minute, time.Second, 24*time.Hour, logger)
	interval := durationEnv("GOJET_OPSMONITOR_INTERVAL", 2*time.Second, 250*time.Millisecond, time.Minute, logger)
	leaseTTL := durationEnv("GOJET_RISK_LEASE_TTL", 2*time.Minute, time.Second, 15*time.Minute, logger)
	retryBase := durationEnv("GOJET_RISK_RETRY_BASE", 2*time.Second, 0, time.Minute, logger)

	workerID := strings.TrimSpace(os.Getenv("GOJET_OPSMONITOR_WORKER_ID"))
	if workerID == "" {
		hostname, _ := os.Hostname()
		workerID = "operationsmonitor-" + strings.TrimSpace(hostname)
	}
	if workerID == "operationsmonitor-" {
		workerID = "operationsmonitor-local"
	}

	store := trust.NewStore(db)
	policy := trust.DestinationPolicy{
		Version:           policyVersion,
		RequiredProviders: []string{providerName},
		AllowTTL:          allowTTL,
	}
	processor := &trust.ProviderPolicyProcessor{
		Store:             store,
		Provider:          trust.SemanticProviderClient{Name: providerName, Endpoint: providerEndpoint, HTTPClient: trust.NewInspectionHTTPClient(nil, nil)},
		Policy:            policy,
		ActorID:           workerID,
		LocalSafetyPassed: true,
	}
	worker, err := trust.NewRiskWorker(store, processor, workerID)
	if err != nil {
		logger.Error("operationsmonitor risk worker initialization failed")
		os.Exit(1)
	}
	worker.LeaseTTL = leaseTTL
	worker.RetryBase = retryBase
	projector := &trust.RiskProjector{
		Store:         store,
		Runtime:       runtimeRisk,
		PolicyVersion: policyVersion,
		MaxTTL:        projectionTTL,
		BatchSize:     100,
	}

	runIteration := func(ctx context.Context) error {
		worked, err := worker.RunOnce(ctx)
		if err != nil {
			logger.Warn("operationsmonitor risk iteration failed", "worked", worked)
		}
		projected, projectionErr := projector.RunOnce(ctx)
		if projectionErr != nil {
			logger.Warn("operationsmonitor projection iteration failed")
			if err == nil {
				err = projectionErr
			}
		}
		if worked || projected > 0 {
			logger.Info("operationsmonitor iteration", "risk_worked", worked, "projected", projected)
		}
		return err
	}

	if *once {
		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()
		if err := runIteration(ctx); err != nil {
			os.Exit(1)
		}
		return
	}

	ctx, cancel := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer cancel()
	ticker := time.NewTicker(interval)
	defer ticker.Stop()
	logger.Info("operationsmonitor started", "service_id", trust.OperationsMonitorServiceID, "interval", interval.String())
	for {
		_ = runIteration(ctx)
		select {
		case <-ctx.Done():
			logger.Info("operationsmonitor stopped")
			return
		case <-ticker.C:
		}
	}
}

func durationEnv(name string, fallback, minimum, maximum time.Duration, logger *slog.Logger) time.Duration {
	raw := strings.TrimSpace(os.Getenv(name))
	if raw == "" {
		return fallback
	}
	parsed, err := time.ParseDuration(raw)
	if err != nil || parsed < minimum || parsed > maximum {
		logger.Error("invalid duration configuration", "name", name)
		os.Exit(1)
	}
	return parsed
}
