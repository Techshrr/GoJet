package main

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"strings"
	"time"

	"github.com/Techshrr/GoJet/internal/files"
)

func main() {
	report, err := run()
	if err != nil {
		report = files.DependencyHealthReport{Status: files.DependencyUnavailable}
	}
	encoder := json.NewEncoder(os.Stdout)
	encoder.SetEscapeHTML(true)
	if encodeErr := encoder.Encode(report); encodeErr != nil {
		os.Exit(3)
	}
	if err != nil || !report.Ready {
		os.Exit(2)
	}
}

func run() (files.DependencyHealthReport, error) {
	root := strings.TrimSpace(os.Getenv("GOJET_FILE_STORAGE_ROOT"))
	network := strings.TrimSpace(os.Getenv("GOJET_CLAMAV_NETWORK"))
	address := strings.TrimSpace(os.Getenv("GOJET_CLAMAV_ADDRESS"))
	if root == "" || network == "" || address == "" {
		return files.DependencyHealthReport{}, fmt.Errorf("required file preflight configuration missing")
	}
	dialTimeout, err := durationEnv("GOJET_CLAMAV_DIAL_TIMEOUT", 2*time.Second)
	if err != nil {
		return files.DependencyHealthReport{}, err
	}
	scanTimeout, err := durationEnv("GOJET_CLAMAV_SCAN_TIMEOUT", 30*time.Second)
	if err != nil {
		return files.DependencyHealthReport{}, err
	}
	maxSignatureAge, err := durationEnv("GOJET_CLAMAV_MAX_SIGNATURE_AGE", 48*time.Hour)
	if err != nil {
		return files.DependencyHealthReport{}, err
	}
	client, err := files.NewClamAVClient(network, address, dialTimeout, scanTimeout, maxSignatureAge)
	if err != nil {
		return files.DependencyHealthReport{}, err
	}
	authority, err := files.NewHealthAuthority(root, client)
	if err != nil {
		return files.DependencyHealthReport{}, err
	}
	ctx, cancel := context.WithTimeout(context.Background(), dialTimeout+3*time.Second)
	defer cancel()
	return authority.Check(ctx), nil
}

func durationEnv(name string, fallback time.Duration) (time.Duration, error) {
	raw := strings.TrimSpace(os.Getenv(name))
	if raw == "" {
		return fallback, nil
	}
	value, err := time.ParseDuration(raw)
	if err != nil || value <= 0 {
		return 0, fmt.Errorf("invalid duration configuration")
	}
	return value, nil
}
