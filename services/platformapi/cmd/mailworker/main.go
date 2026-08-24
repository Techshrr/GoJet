package main

import (
	"context"
	"log/slog"
	"os"
	"os/signal"
	"strings"
	"syscall"
	"time"

	"github.com/Techshrr/GoJet/internal/links"
	"github.com/Techshrr/GoJet/internal/support"
)

func main() {
	logger := slog.New(slog.NewJSONHandler(os.Stdout, &slog.HandlerOptions{Level: slog.LevelInfo}))
	dsn := strings.TrimSpace(os.Getenv("GOJET_MYSQL_DSN"))
	smtpAddress := strings.TrimSpace(os.Getenv("GOJET_SMTP_ADDR"))
	smtpFrom := strings.TrimSpace(os.Getenv("GOJET_SMTP_FROM"))
	if dsn == "" || smtpAddress == "" || smtpFrom == "" {
		logger.Error("required mailworker configuration missing",
			"mysql_dsn_present", dsn != "",
			"smtp_addr_present", smtpAddress != "",
			"smtp_from_present", smtpFrom != "")
		os.Exit(1)
	}

	db, err := links.OpenMySQL(dsn)
	if err != nil {
		logger.Error("mailworker database unavailable")
		os.Exit(1)
	}
	defer db.Close()
	pingCtx, pingCancel := context.WithTimeout(context.Background(), 5*time.Second)
	if err := db.PingContext(pingCtx); err != nil {
		pingCancel()
		logger.Error("mailworker database unavailable")
		os.Exit(1)
	}
	pingCancel()

	store, err := support.NewMySQLMailStore(db)
	if err != nil {
		logger.Error("mailworker store configuration invalid")
		os.Exit(1)
	}
	sender, err := support.NewSMTPSender(
		smtpAddress,
		smtpFrom,
		os.Getenv("GOJET_SMTP_USERNAME"),
		os.Getenv("GOJET_SMTP_PASSWORD"),
		10*time.Second,
	)
	if err != nil {
		logger.Error("mailworker SMTP configuration invalid")
		os.Exit(1)
	}
	worker, err := support.NewMailWorker(store, sender)
	if err != nil {
		logger.Error("mailworker initialization failed")
		os.Exit(1)
	}

	interval := 2 * time.Second
	if raw := strings.TrimSpace(os.Getenv("GOJET_MAILWORKER_INTERVAL")); raw != "" {
		parsed, parseErr := time.ParseDuration(raw)
		if parseErr != nil || parsed < 250*time.Millisecond || parsed > time.Minute {
			logger.Error("invalid GOJET_MAILWORKER_INTERVAL")
			os.Exit(1)
		}
		interval = parsed
	}

	ctx, cancel := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer cancel()
	ticker := time.NewTicker(interval)
	defer ticker.Stop()
	logger.Info("mailworker started", "interval", interval.String())
	for {
		worked, err := worker.RunOnce(ctx)
		if err != nil {
			logger.Warn("mailworker iteration failed")
		}
		if worked {
			continue
		}
		select {
		case <-ctx.Done():
			logger.Info("mailworker stopped")
			return
		case <-ticker.C:
		}
	}
}
