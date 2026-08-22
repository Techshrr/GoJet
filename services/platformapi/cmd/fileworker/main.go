package main

import (
	"context"
	"errors"
	"log/slog"
	"os"
	"os/signal"
	"strconv"
	"strings"
	"syscall"
	"time"

	filedomain "github.com/Techshrr/GoJet/internal/files"
	"github.com/Techshrr/GoJet/internal/links"
)

func main() {
	logger := slog.New(slog.NewJSONHandler(os.Stdout, &slog.HandlerOptions{Level: slog.LevelInfo}))
	mysqlDSN := strings.TrimSpace(os.Getenv("GOJET_MYSQL_DSN"))
	storageRoot := strings.TrimSpace(os.Getenv("GOJET_FILE_STORAGE_ROOT"))
	clamNetwork := strings.TrimSpace(os.Getenv("GOJET_CLAMAV_NETWORK"))
	clamAddress := strings.TrimSpace(os.Getenv("GOJET_CLAMAV_ADDRESS"))
	if mysqlDSN == "" || storageRoot == "" || clamNetwork == "" || clamAddress == "" {
		logger.Error("required configuration missing",
			"mysql_dsn_present", mysqlDSN != "",
			"storage_root_present", storageRoot != "",
			"clamav_network_present", clamNetwork != "",
			"clamav_address_present", clamAddress != "")
		os.Exit(1)
	}

	db, err := links.OpenMySQL(mysqlDSN)
	if err != nil {
		logger.Error("open mysql", "error", err)
		os.Exit(1)
	}
	defer db.Close()
	store := filedomain.NewStore(db)
	storage, err := filedomain.NewNativeStorage(storageRoot)
	if err != nil {
		logger.Error("initialize native file storage", "error", err)
		os.Exit(1)
	}

	dialTimeout := durationEnv("GOJET_CLAMAV_DIAL_TIMEOUT", 2*time.Second, logger)
	scanTimeout := durationEnv("GOJET_CLAMAV_SCAN_TIMEOUT", 30*time.Second, logger)
	maxSignatureAge := durationEnv("GOJET_CLAMAV_MAX_SIGNATURE_AGE", 48*time.Hour, logger)
	claimLease := durationEnv("GOJET_FILE_SCAN_CLAIM_LEASE", 2*time.Minute, logger)
	pollInterval := durationEnv("GOJET_FILE_WORKER_POLL_INTERVAL", 250*time.Millisecond, logger)
	client, err := filedomain.NewClamAVClient(clamNetwork, clamAddress, dialTimeout, scanTimeout, maxSignatureAge)
	if err != nil {
		logger.Error("configure ClamAV client", "error", err)
		os.Exit(1)
	}

	workerID := strings.TrimSpace(os.Getenv("GOJET_FILE_WORKER_ID"))
	if workerID == "" {
		hostname, hostErr := os.Hostname()
		if hostErr != nil || strings.TrimSpace(hostname) == "" {
			workerID = "fileworker"
		} else {
			workerID = hostname
		}
	}
	if len(workerID) > 128 {
		logger.Error("GOJET_FILE_WORKER_ID exceeds 128 characters")
		os.Exit(1)
	}
	maxJobs := intEnv("GOJET_FILE_WORKER_MAX_JOBS", 0, logger)

	startupCtx, startupCancel := context.WithTimeout(context.Background(), 5*time.Second)
	if err := store.Ping(startupCtx); err != nil {
		startupCancel()
		logger.Error("mysql unavailable", "error", err)
		os.Exit(1)
	}
	if health, healthErr := client.Health(startupCtx); healthErr != nil {
		logger.Warn("ClamAV health is not ready; scans will fail closed until restored", "error", healthErr)
	} else {
		logger.Info("ClamAV health ready", "engine_version", health.EngineVersion, "signature_version", health.SignatureVersion, "signature_date", health.SignatureDate)
	}
	startupCancel()

	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()
	logger.Info("fileworker started", "worker_id", workerID)

	processed := 0
	lastRecovery := time.Time{}
	for ctx.Err() == nil {
		now := time.Now().UTC()
		if lastRecovery.IsZero() || now.Sub(lastRecovery) >= claimLease/2 {
			recovered, recoverErr := store.RecoverStaleClaims(ctx, now.Add(-claimLease))
			if recoverErr != nil {
				logger.Error("recover stale file scan claims", "error", recoverErr)
			} else if recovered > 0 {
				logger.Warn("recovered interrupted file scans", "count", recovered)
			}
			lastRecovery = now
		}

		job, claimErr := store.ClaimNextScan(ctx, workerID)
		if errors.Is(claimErr, filedomain.ErrNoScanJobs) {
			if !sleepContext(ctx, pollInterval) {
				break
			}
			continue
		}
		if claimErr != nil {
			if ctx.Err() != nil {
				break
			}
			logger.Error("claim file scan", "error", claimErr)
			if !sleepContext(ctx, pollInterval) {
				break
			}
			continue
		}

		result := filedomain.ScanResult{Verdict: filedomain.VerdictError, ErrorCode: "storage_read_failed", Reason: "Quarantine bytes could not be opened."}
		quarantine, openErr := storage.OpenQuarantine(job.StorageKey)
		if openErr != nil {
			logger.Error("open quarantine bytes", "file_id", job.FileID, "attempt_id", job.AttemptID, "error", openErr)
		} else {
			result, err = client.Scan(ctx, quarantine)
			_ = quarantine.Close()
			if err != nil {
				logger.Warn("file scan failed closed", "file_id", job.FileID, "attempt_id", job.AttemptID, "error_code", result.ErrorCode, "error", err)
			}
		}
		if completeErr := store.CompleteScan(ctx, job, result); completeErr != nil {
			logger.Error("complete file scan", "file_id", job.FileID, "attempt_id", job.AttemptID, "error", completeErr)
			continue
		}
		processed++
		logger.Info("file scan completed", "file_id", job.FileID, "attempt_id", job.AttemptID, "generation", job.Generation, "verdict", result.Verdict, "error_code", result.ErrorCode)
		if maxJobs > 0 && processed >= maxJobs {
			logger.Info("fileworker job target reached", "processed", processed)
			return
		}
	}
	logger.Info("fileworker stopped", "processed", processed)
}

func durationEnv(name string, fallback time.Duration, logger *slog.Logger) time.Duration {
	raw := strings.TrimSpace(os.Getenv(name))
	if raw == "" {
		return fallback
	}
	value, err := time.ParseDuration(raw)
	if err != nil || value <= 0 {
		logger.Error("invalid duration configuration", "name", name)
		os.Exit(1)
	}
	return value
}

func intEnv(name string, fallback int, logger *slog.Logger) int {
	raw := strings.TrimSpace(os.Getenv(name))
	if raw == "" {
		return fallback
	}
	value, err := strconv.Atoi(raw)
	if err != nil || value < 0 {
		logger.Error("invalid integer configuration", "name", name)
		os.Exit(1)
	}
	return value
}

func sleepContext(ctx context.Context, delay time.Duration) bool {
	timer := time.NewTimer(delay)
	defer timer.Stop()
	select {
	case <-ctx.Done():
		return false
	case <-timer.C:
		return true
	}
}
