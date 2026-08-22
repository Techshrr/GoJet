package main

import (
	"context"
	"errors"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"strconv"
	"strings"
	"syscall"
	"time"

	"github.com/Techshrr/GoJet/internal/analytics"
	"github.com/Techshrr/GoJet/internal/domains"
	"github.com/Techshrr/GoJet/internal/links"
)

func main() {
	logger := slog.New(slog.NewJSONHandler(os.Stdout, &slog.HandlerOptions{Level: slog.LevelInfo}))
	mysqlDSN := strings.TrimSpace(os.Getenv("GOJET_MYSQL_DSN"))
	redisAddr := strings.TrimSpace(os.Getenv("GOJET_REDIS_ADDR"))
	if mysqlDSN == "" || redisAddr == "" {
		logger.Error("required configuration missing", "mysql_dsn_present", mysqlDSN != "", "redis_addr_present", redisAddr != "")
		os.Exit(1)
	}

	db, err := links.OpenMySQL(mysqlDSN)
	if err != nil {
		logger.Error("open mysql", "error", err)
		os.Exit(1)
	}
	defer db.Close()

	redisDB := 0
	if raw := strings.TrimSpace(os.Getenv("GOJET_REDIS_DB")); raw != "" {
		parsed, parseErr := strconv.Atoi(raw)
		if parseErr != nil || parsed < 0 {
			logger.Error("invalid GOJET_REDIS_DB")
			os.Exit(1)
		}
		redisDB = parsed
	}
	redisClient := links.NewRedisClient(redisAddr, os.Getenv("GOJET_REDIS_PASSWORD"), redisDB)
	defer redisClient.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	if err := db.PingContext(ctx); err != nil {
		cancel()
		logger.Error("mysql unavailable", "error", err)
		os.Exit(1)
	}
	if err := redisClient.Ping(ctx).Err(); err != nil {
		cancel()
		logger.Error("redis unavailable", "error", err)
		os.Exit(1)
	}
	cancel()

	domainStore := domains.NewMySQLStore(db)
	store := links.NewMySQLStoreWithCustomDomainAuthority(db, domainStore)
	risk := links.NewRedisRiskStore(redisClient)
	trustTestHeaders := os.Getenv("GOJET_TEST_ROUTING_HEADERS") == "1"
	if trustTestHeaders {
		logger.Warn("test-only routing headers enabled; never use this setting in production")
	}

	analyticsEnabled := os.Getenv("GOJET_ANALYTICS_ENABLED") == "1"
	var handler http.Handler
	if analyticsEnabled {
		analyticsPublisher := analytics.NewRedisStreamPublisher(redisClient)
		handler = links.NewRedirectHandlerWithAnalytics(store, risk, analyticsPublisher, trustTestHeaders)
	} else {
		handler = links.NewRedirectHandler(store, risk, trustTestHeaders)
	}

	address := strings.TrimSpace(os.Getenv("GOJET_REDIRECTENGINE_ADDR"))
	if address == "" {
		address = "127.0.0.1:8080"
	}
	server := &http.Server{
		Addr:              address,
		Handler:           handler,
		ReadHeaderTimeout: 5 * time.Second,
		ReadTimeout:       10 * time.Second,
		WriteTimeout:      10 * time.Second,
		IdleTimeout:       60 * time.Second,
	}

	stop := make(chan os.Signal, 1)
	signal.Notify(stop, syscall.SIGINT, syscall.SIGTERM)
	go func() {
		<-stop
		shutdownCtx, shutdownCancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer shutdownCancel()
		if err := server.Shutdown(shutdownCtx); err != nil {
			logger.Error("redirectengine shutdown", "error", err)
		}
	}()

	logger.Info("redirectengine listening", "address", address, "analytics_enabled", analyticsEnabled, "analytics_stream", analytics.ClickStreamKey)
	if err := server.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
		logger.Error("redirectengine failed", "error", err)
		os.Exit(1)
	}
}
