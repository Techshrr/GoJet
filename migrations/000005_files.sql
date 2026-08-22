-- GoJet V10 / P09 Files and Mandatory ClamAV
-- Repository-global immutable migration: 000005
-- MySQL 8.x

CREATE TABLE file_workspace_counters (
    workspace_id VARCHAR(64) NOT NULL,
    active_count BIGINT UNSIGNED NOT NULL DEFAULT 0,
    active_bytes BIGINT UNSIGNED NOT NULL DEFAULT 0,
    updated_at DATETIME(6) NOT NULL DEFAULT CURRENT_TIMESTAMP(6) ON UPDATE CURRENT_TIMESTAMP(6),
    PRIMARY KEY (workspace_id)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_0900_ai_ci;

CREATE TABLE files (
    id BIGINT UNSIGNED NOT NULL AUTO_INCREMENT,
    workspace_id VARCHAR(64) NOT NULL,
    public_slug VARCHAR(64) CHARACTER SET ascii COLLATE ascii_bin NOT NULL,
    original_name VARCHAR(255) NOT NULL,
    storage_key CHAR(64) CHARACTER SET ascii COLLATE ascii_bin NOT NULL,
    size_bytes BIGINT UNSIGNED NOT NULL,
    content_sha256 CHAR(64) CHARACTER SET ascii COLLATE ascii_bin NOT NULL,
    declared_mime VARCHAR(160) NOT NULL DEFAULT '',
    detected_mime VARCHAR(160) NOT NULL,
    scan_state ENUM('quarantined','scanning','safe','blocked','scan_error') NOT NULL DEFAULT 'quarantined',
    scan_generation BIGINT UNSIGNED NOT NULL DEFAULT 1,
    published TINYINT(1) NOT NULL DEFAULT 0,
    published_at DATETIME(6) NULL,
    password_hash VARCHAR(255) NULL,
    expires_at DATETIME(6) NULL,
    retention_until DATETIME(6) NULL,
    download_limit BIGINT UNSIGNED NULL,
    download_count BIGINT UNSIGNED NOT NULL DEFAULT 0,
    created_by VARCHAR(128) NOT NULL,
    created_at DATETIME(6) NOT NULL DEFAULT CURRENT_TIMESTAMP(6),
    updated_at DATETIME(6) NOT NULL DEFAULT CURRENT_TIMESTAMP(6) ON UPDATE CURRENT_TIMESTAMP(6),
    deleted_at DATETIME(6) NULL,
    PRIMARY KEY (id),
    UNIQUE KEY uq_files_public_slug (public_slug),
    UNIQUE KEY uq_files_storage_key (storage_key),
    KEY idx_files_workspace_active (workspace_id, deleted_at, updated_at, id),
    KEY idx_files_workspace_scan (workspace_id, scan_state, deleted_at, id),
    KEY idx_files_public_authority (public_slug, published, scan_state, deleted_at),
    CONSTRAINT chk_files_workspace_nonempty CHECK (CHAR_LENGTH(TRIM(workspace_id)) > 0),
    CONSTRAINT chk_files_original_name_nonempty CHECK (CHAR_LENGTH(TRIM(original_name)) > 0),
    CONSTRAINT chk_files_storage_key_hex CHECK (storage_key REGEXP '^[0-9a-f]{64}$'),
    CONSTRAINT chk_files_content_sha_hex CHECK (content_sha256 REGEXP '^[0-9a-f]{64}$'),
    CONSTRAINT chk_files_mime_nonempty CHECK (CHAR_LENGTH(TRIM(detected_mime)) > 0),
    CONSTRAINT chk_files_created_by_nonempty CHECK (CHAR_LENGTH(TRIM(created_by)) > 0),
    CONSTRAINT chk_files_publish_safe CHECK (published = 0 OR scan_state = 'safe'),
    CONSTRAINT chk_files_download_limit CHECK (download_limit IS NULL OR download_limit > 0)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_0900_ai_ci;

CREATE TABLE file_scan_attempts (
    id BIGINT UNSIGNED NOT NULL AUTO_INCREMENT,
    file_id BIGINT UNSIGNED NOT NULL,
    workspace_id VARCHAR(64) NOT NULL,
    generation BIGINT UNSIGNED NOT NULL,
    status ENUM('queued','processing','clean','infected','error') NOT NULL DEFAULT 'queued',
    claim_token CHAR(64) CHARACTER SET ascii COLLATE ascii_bin NULL,
    worker_id VARCHAR(128) CHARACTER SET ascii COLLATE ascii_bin NULL,
    engine_version VARCHAR(128) NULL,
    signature_version VARCHAR(128) NULL,
    signature_date DATETIME(6) NULL,
    verdict_code VARCHAR(96) CHARACTER SET ascii COLLATE ascii_bin NULL,
    reason VARCHAR(500) NULL,
    error_code VARCHAR(96) CHARACTER SET ascii COLLATE ascii_bin NULL,
    queued_at DATETIME(6) NOT NULL DEFAULT CURRENT_TIMESTAMP(6),
    claimed_at DATETIME(6) NULL,
    completed_at DATETIME(6) NULL,
    created_at DATETIME(6) NOT NULL DEFAULT CURRENT_TIMESTAMP(6),
    PRIMARY KEY (id),
    UNIQUE KEY uq_file_scan_generation (file_id, generation),
    KEY idx_file_scan_claim (status, queued_at, id),
    KEY idx_file_scan_workspace (workspace_id, created_at, id),
    CONSTRAINT fk_file_scan_file FOREIGN KEY (file_id) REFERENCES files(id) ON DELETE RESTRICT,
    CONSTRAINT chk_file_scan_generation CHECK (generation > 0)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_0900_ai_ci;

CREATE TABLE file_audit_events (
    id BIGINT UNSIGNED NOT NULL AUTO_INCREMENT,
    workspace_id VARCHAR(64) NOT NULL,
    file_id BIGINT UNSIGNED NULL,
    actor_id VARCHAR(128) NOT NULL,
    action VARCHAR(96) CHARACTER SET ascii COLLATE ascii_bin NOT NULL,
    request_correlation_id VARCHAR(128) CHARACTER SET ascii COLLATE ascii_bin NOT NULL,
    reason VARCHAR(500) NULL,
    result ENUM('success','denied','conflict','failed') NOT NULL,
    metadata_json JSON NOT NULL,
    created_at DATETIME(6) NOT NULL DEFAULT CURRENT_TIMESTAMP(6),
    PRIMARY KEY (id),
    KEY idx_file_audit_workspace_created (workspace_id, created_at, id),
    KEY idx_file_audit_file_created (file_id, created_at, id),
    KEY idx_file_audit_correlation (request_correlation_id),
    CONSTRAINT fk_file_audit_file FOREIGN KEY (file_id) REFERENCES files(id) ON DELETE RESTRICT,
    CONSTRAINT chk_file_audit_actor_nonempty CHECK (CHAR_LENGTH(TRIM(actor_id)) > 0),
    CONSTRAINT chk_file_audit_correlation_nonempty CHECK (CHAR_LENGTH(TRIM(request_correlation_id)) > 0)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_0900_ai_ci;
