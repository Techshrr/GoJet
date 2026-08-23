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
	qrcodes "github.com/Techshrr/GoJet/internal/qr"
)

func main() {
	logger := slog.New(slog.NewJSONHandler(os.Stdout, &slog.HandlerOptions{Level: slog.LevelInfo}))
	dsn := strings.TrimSpace(os.Getenv("GOJET_MYSQL_DSN"))
	redisAddr := strings.TrimSpace(os.Getenv("GOJET_REDIS_ADDR"))
	if dsn == "" || redisAddr == "" {
		logger.Error("required configuration missing", "mysql_dsn_present", dsn != "", "redis_addr_present", redisAddr != "")
		os.Exit(1)
	}

	db, err := links.OpenMySQL(dsn)
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
	testAuth := os.Getenv("GOJET_TEST_AUTH_ENABLED") == "1"
	analyticsEnabled := os.Getenv("GOJET_ANALYTICS_ENABLED") == "1"
	if testAuth {
		logger.Warn("test-only auth adapter enabled; never use this setting in production")
	}

	qrQuota := uint64(100)
	if raw := strings.TrimSpace(os.Getenv("GOJET_QR_WORKSPACE_QUOTA")); raw != "" {
		parsed, parseErr := strconv.ParseUint(raw, 10, 64)
		if parseErr != nil || parsed == 0 || parsed > 100000 {
			logger.Error("invalid GOJET_QR_WORKSPACE_QUOTA")
			os.Exit(1)
		}
		qrQuota = parsed
	}

	linksAPI := links.NewAPI(store, testAuth)
	domainsAPI := domains.NewWorkspaceDomainsAPI(domainStore, testAuth)
	analyticsAPI := analytics.NewAPI(analytics.NewStore(db), testAuth, analyticsEnabled)
	qrAPI := qrcodes.NewAPI(qrcodes.NewStore(db, qrQuota), store, risk, testAuth)
	domainsHandler := domainsAPI.Handler()
	analyticsHandler := analyticsAPI.Handler()
	qrHandler := qrAPI.Handler()
	filesHandler, filesEnabled, err := buildFilesHandler(db, testAuth)
	if err != nil {
		logger.Error("configure files", "error", err)
		os.Exit(1)
	}
	textHandler, textEnabled, err := buildTextHandler(db, testAuth)
	if err != nil {
		logger.Error("configure Text Sharing", "error", err)
		os.Exit(1)
	}

	// Keep the established P05 Links surface as the fallback while mounting the
	// P06 Domains, P07 Analytics, P08 QR, conditionally enabled P09 Files and P10 Text routes
	// explicitly. Each inner handler retains its own security headers and test-only auth boundary.
	root := http.NewServeMux()
	root.Handle("GET /api/workspaces/{workspaceId}/domains", domainsHandler)
	root.Handle("POST /api/workspaces/{workspaceId}/domains", domainsHandler)
	root.Handle("GET /api/workspaces/{workspaceId}/domains/{domainId}", domainsHandler)
	root.Handle("GET /api/workspaces/{workspaceId}/analytics/overview", analyticsHandler)
	root.Handle("GET /api/workspaces/{workspaceId}/analytics/links/{linkId}", analyticsHandler)
	root.Handle("POST /api/workspaces/{workspaceId}/analytics/conversions", analyticsHandler)
	root.Handle("GET /api/workspaces/{workspaceId}/qr-codes", qrHandler)
	root.Handle("POST /api/workspaces/{workspaceId}/qr-codes", qrHandler)
	root.Handle("GET /api/workspaces/{workspaceId}/qr-codes/{qrId}", qrHandler)
	root.Handle("DELETE /api/workspaces/{workspaceId}/qr-codes/{qrId}", qrHandler)
	root.Handle("GET /api/workspaces/{workspaceId}/qr-codes/{qrId}/preview", qrHandler)
	root.Handle("GET /api/workspaces/{workspaceId}/qr-codes/{qrId}/download", qrHandler)
	if filesEnabled {
		root.Handle("GET /api/workspaces/{workspaceId}/files", filesHandler)
		root.Handle("POST /api/workspaces/{workspaceId}/files", filesHandler)
		root.Handle("GET /api/workspaces/{workspaceId}/files/{fileId}", filesHandler)
		root.Handle("PATCH /api/workspaces/{workspaceId}/files/{fileId}", filesHandler)
		root.Handle("DELETE /api/workspaces/{workspaceId}/files/{fileId}", filesHandler)
		root.Handle("POST /api/workspaces/{workspaceId}/files/{fileId}/publish", filesHandler)
		root.Handle("POST /api/workspaces/{workspaceId}/files/{fileId}/rescan", filesHandler)
		root.Handle("GET /api/workspaces/{workspaceId}/files/{fileId}/download", filesHandler)
		root.Handle("GET /f/{slug}", filesHandler)
		root.Handle("POST /f/{slug}", filesHandler)
		root.Handle("GET /api/public/files/{slug}", filesHandler)
		root.Handle("GET /api/admin/platform/storage", filesHandler)
	}
	if textEnabled {
		root.Handle("GET /api/workspaces/{workspaceId}/text-shares", textHandler)
		root.Handle("POST /api/workspaces/{workspaceId}/text-shares", textHandler)
		root.Handle("GET /api/workspaces/{workspaceId}/text-shares/{shareId}", textHandler)
		root.Handle("PATCH /api/workspaces/{workspaceId}/text-shares/{shareId}", textHandler)
		root.Handle("DELETE /api/workspaces/{workspaceId}/text-shares/{shareId}", textHandler)
		root.Handle("GET /t/{slug}", textHandler)
		root.Handle("POST /t/{slug}", textHandler)
		root.Handle("POST /api/public/text/{slug}", textHandler)
	}
	root.Handle("/", linksAPI.FullHandlerWithRisk(risk))

	address := strings.TrimSpace(os.Getenv("GOJET_PLATFORMAPI_ADDR"))
	if address == "" {
		address = "127.0.0.1:8081"
	}
	server := &http.Server{
		Addr:              address,
		Handler:           root,
		ReadHeaderTimeout: 5 * time.Second,
		ReadTimeout:       15 * time.Second,
		WriteTimeout:      15 * time.Second,
		IdleTimeout:       60 * time.Second,
	}

	stop := make(chan os.Signal, 1)
	signal.Notify(stop, syscall.SIGINT, syscall.SIGTERM)
	go func() {
		<-stop
		shutdownCtx, shutdownCancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer shutdownCancel()
		if err := server.Shutdown(shutdownCtx); err != nil {
			logger.Error("platformapi shutdown", "error", err)
		}
	}()

	logger.Info("platformapi listening", "address", address, "analytics_enabled", analyticsEnabled, "qr_workspace_quota", qrQuota, "files_enabled", filesEnabled, "text_enabled", textEnabled)
	if err := server.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
		logger.Error("platformapi failed", "error", err)
		os.Exit(1)
	}
}
