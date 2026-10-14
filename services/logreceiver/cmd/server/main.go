package main

import (
	"context"
	"flag"
	"os"
	"os/signal"
	"syscall"

	"github.com/Techshrr/GoJet/internal/logging"
	"github.com/Techshrr/GoJet/internal/logreceiver"
)

func main() {
	check := flag.Bool("check", false, "check the private receiver socket")
	flag.Parse()
	logger := logging.NewLocal(os.Stdout, "logreceiver")
	socket := os.Getenv("GOJET_LOG_SOCKET")
	if socket == "" {
		logger.Error("logreceiver socket configuration missing")
		os.Exit(1)
	}
	if *check {
		if logging.SendLocal(socket, []byte("{\"type\":\"health\"}\n")) != nil {
			logger.Error("logreceiver health check failed")
			os.Exit(1)
		}
		return
	}
	listener, err := logreceiver.Open(socket)
	if err != nil {
		logger.Error("logreceiver socket unavailable")
		os.Exit(1)
	}
	defer listener.Close()
	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()
	logger.Info("logreceiver started")
	if logreceiver.Serve(ctx, listener, os.Stdout) != nil {
		logger.Error("logreceiver failed")
		os.Exit(1)
	}
	logger.Info("logreceiver stopped")
}
