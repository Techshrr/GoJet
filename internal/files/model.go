package files

import (
	"errors"
	"time"
)

var (
	ErrInvalidInput       = errors.New("invalid file input")
	ErrNotFound           = errors.New("file not found")
	ErrDeleted            = errors.New("file deleted")
	ErrQuota              = errors.New("file quota reached")
	ErrNoScanJobs         = errors.New("no file scan jobs")
	ErrScanClaimConflict  = errors.New("file scan claim conflict")
	ErrStorageUnavailable = errors.New("file storage unavailable")
	ErrSignatureStale     = errors.New("clamav signatures stale")
	ErrScanIndeterminate  = errors.New("clamav scan indeterminate")
)

type ScanState string

const (
	ScanQuarantined ScanState = "quarantined"
	ScanScanning    ScanState = "scanning"
	ScanSafe        ScanState = "safe"
	ScanBlocked     ScanState = "blocked"
	ScanError       ScanState = "scan_error"
)

type Resource struct {
	ID             uint64     `json:"id"`
	WorkspaceID    string     `json:"workspace_id"`
	PublicSlug     string     `json:"public_slug"`
	OriginalName   string     `json:"original_name"`
	StorageKey     string     `json:"-"`
	SizeBytes      uint64     `json:"size_bytes"`
	ContentSHA256  string     `json:"content_sha256"`
	DeclaredMIME   string     `json:"declared_mime"`
	DetectedMIME   string     `json:"detected_mime"`
	ScanState      ScanState  `json:"scan_state"`
	ScanGeneration uint64     `json:"scan_generation"`
	Published      bool       `json:"published"`
	PublishedAt    *time.Time `json:"published_at,omitempty"`
	ExpiresAt      *time.Time `json:"expires_at,omitempty"`
	RetentionUntil *time.Time `json:"retention_until,omitempty"`
	DownloadLimit  *uint64    `json:"download_limit,omitempty"`
	DownloadCount  uint64     `json:"download_count"`
	CreatedBy      string     `json:"created_by"`
	CreatedAt      time.Time  `json:"created_at"`
	UpdatedAt      time.Time  `json:"updated_at"`
	DeletedAt      *time.Time `json:"deleted_at,omitempty"`
}

type ScanJob struct {
	AttemptID   uint64
	FileID      uint64
	WorkspaceID string
	Generation  uint64
	StorageKey  string
	SizeBytes   uint64
	ClaimToken  string
}

type ScanVerdict string

const (
	VerdictClean    ScanVerdict = "clean"
	VerdictInfected ScanVerdict = "infected"
	VerdictError    ScanVerdict = "error"
)

type ScanResult struct {
	Verdict          ScanVerdict
	EngineVersion    string
	SignatureVersion string
	SignatureDate    *time.Time
	VerdictCode      string
	Reason           string
	ErrorCode        string
}

type ClamAVHealth struct {
	EngineVersion    string    `json:"engine_version"`
	SignatureVersion string    `json:"signature_version"`
	SignatureDate    time.Time `json:"signature_date"`
	CheckedAt        time.Time `json:"checked_at"`
	Fresh            bool      `json:"fresh"`
}
