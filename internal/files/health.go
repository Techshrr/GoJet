package files

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"time"
)

type DependencyState string

const (
	DependencyHealthy         DependencyState = "healthy"
	DependencyUnavailable     DependencyState = "unavailable"
	DependencyPermissionError DependencyState = "permission_error"
	DependencyStale           DependencyState = "stale"
	DependencyIndeterminate   DependencyState = "indeterminate"
)

type StorageHealth struct {
	State    DependencyState `json:"state"`
	Writable bool            `json:"writable"`
}

type ScannerHealth struct {
	State            DependencyState `json:"state"`
	EngineVersion    string          `json:"engine_version,omitempty"`
	SignatureVersion string          `json:"signature_version,omitempty"`
	SignatureDate    *time.Time      `json:"signature_date,omitempty"`
	CheckedAt        time.Time       `json:"checked_at"`
}

type DependencyHealthReport struct {
	Ready   bool            `json:"ready"`
	Status  DependencyState `json:"status"`
	Storage StorageHealth   `json:"storage"`
	ClamAV  ScannerHealth   `json:"clamav"`
}

type HealthAuthority struct {
	storageRoot string
	clamav      *ClamAVClient
	now         func() time.Time
}

func NewHealthAuthority(storageRoot string, clamav *ClamAVClient) (*HealthAuthority, error) {
	storageRoot = strings.TrimSpace(storageRoot)
	if storageRoot == "" || !filepath.IsAbs(storageRoot) || clamav == nil {
		return nil, ErrInvalidInput
	}
	return &HealthAuthority{storageRoot: filepath.Clean(storageRoot), clamav: clamav, now: time.Now}, nil
}

func (a *HealthAuthority) Check(ctx context.Context) DependencyHealthReport {
	now := time.Now().UTC()
	if a != nil && a.now != nil {
		now = a.now().UTC()
	}
	report := DependencyHealthReport{
		Status:  DependencyUnavailable,
		Storage: StorageHealth{State: DependencyUnavailable},
		ClamAV:  ScannerHealth{State: DependencyUnavailable, CheckedAt: now},
	}
	if a == nil || a.clamav == nil || a.storageRoot == "" {
		return report
	}
	report.Storage = checkStorageHealth(a.storageRoot)
	report.ClamAV = checkScannerHealth(ctx, a.clamav, now)
	report.Ready = report.Storage.State == DependencyHealthy && report.ClamAV.State == DependencyHealthy
	if report.Ready {
		report.Status = DependencyHealthy
		return report
	}
	if report.Storage.State == DependencyPermissionError {
		report.Status = DependencyPermissionError
	} else if report.ClamAV.State == DependencyStale {
		report.Status = DependencyStale
	} else if report.ClamAV.State == DependencyIndeterminate {
		report.Status = DependencyIndeterminate
	} else {
		report.Status = DependencyUnavailable
	}
	return report
}

func checkStorageHealth(root string) StorageHealth {
	for _, path := range []string{root, filepath.Join(root, "quarantine"), filepath.Join(root, "published")} {
		info, err := os.Lstat(path)
		if err != nil || !info.IsDir() || info.Mode()&os.ModeSymlink != 0 {
			return StorageHealth{State: DependencyUnavailable}
		}
		if info.Mode().Perm() != 0o700 {
			return StorageHealth{State: DependencyPermissionError}
		}
		if err := probeStorageDirectory(path); err != nil {
			if errors.Is(err, os.ErrPermission) {
				return StorageHealth{State: DependencyPermissionError}
			}
			return StorageHealth{State: DependencyUnavailable}
		}
	}
	return StorageHealth{State: DependencyHealthy, Writable: true}
}

func probeStorageDirectory(path string) error {
	probe, err := os.CreateTemp(path, ".gojet-files-health-*")
	if err != nil {
		return err
	}
	name := probe.Name()
	probeErr := probe.Chmod(0o600)
	if closeErr := probe.Close(); probeErr == nil {
		probeErr = closeErr
	}
	removeErr := os.Remove(name)
	if probeErr != nil {
		return probeErr
	}
	return removeErr
}

func checkScannerHealth(ctx context.Context, client *ClamAVClient, checkedAt time.Time) ScannerHealth {
	health, err := client.Health(ctx)
	result := ScannerHealth{State: DependencyUnavailable, CheckedAt: checkedAt}
	if health.EngineVersion != "" {
		result.EngineVersion = health.EngineVersion
		result.SignatureVersion = health.SignatureVersion
		date := health.SignatureDate.UTC()
		result.SignatureDate = &date
		result.CheckedAt = health.CheckedAt.UTC()
	}
	if err == nil {
		result.State = DependencyHealthy
		return result
	}
	if errors.Is(err, ErrSignatureStale) {
		result.State = DependencyStale
		return result
	}
	if errors.Is(err, ErrScanIndeterminate) {
		result.State = DependencyIndeterminate
		return result
	}
	return result
}
