-- GoJet V10 / P08 QR
-- Repository-global immutable migration: 000004
-- MySQL 8.x

CREATE TABLE qr_workspace_counters (
    workspace_id VARCHAR(64) NOT NULL,
    active_count BIGINT UNSIGNED NOT NULL DEFAULT 0,
    updated_at DATETIME(6) NOT NULL DEFAULT CURRENT_TIMESTAMP(6) ON UPDATE CURRENT_TIMESTAMP(6),
    PRIMARY KEY (workspace_id),
    CONSTRAINT chk_qr_workspace_active_count CHECK (active_count >= 0)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_0900_ai_ci;

CREATE TABLE qr_codes (
    id BIGINT UNSIGNED NOT NULL AUTO_INCREMENT,
    workspace_id VARCHAR(64) NOT NULL,
    source_link_id BIGINT UNSIGNED NOT NULL,
    label VARCHAR(120) NOT NULL DEFAULT '',
    created_by VARCHAR(128) NOT NULL,
    created_at DATETIME(6) NOT NULL DEFAULT CURRENT_TIMESTAMP(6),
    updated_at DATETIME(6) NOT NULL DEFAULT CURRENT_TIMESTAMP(6) ON UPDATE CURRENT_TIMESTAMP(6),
    deleted_at DATETIME(6) NULL,
    PRIMARY KEY (id),
    KEY idx_qr_codes_workspace_active (workspace_id, deleted_at, updated_at, id),
    KEY idx_qr_codes_workspace_source (workspace_id, source_link_id, deleted_at, id),
    CONSTRAINT fk_qr_codes_source_link FOREIGN KEY (source_link_id) REFERENCES links(id) ON DELETE RESTRICT,
    CONSTRAINT chk_qr_codes_workspace_nonempty CHECK (CHAR_LENGTH(TRIM(workspace_id)) > 0),
    CONSTRAINT chk_qr_codes_created_by_nonempty CHECK (CHAR_LENGTH(TRIM(created_by)) > 0)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_0900_ai_ci;

CREATE TABLE qr_audit_events (
    id BIGINT UNSIGNED NOT NULL AUTO_INCREMENT,
    workspace_id VARCHAR(64) NOT NULL,
    qr_id BIGINT UNSIGNED NULL,
    source_link_id BIGINT UNSIGNED NULL,
    actor_id VARCHAR(128) NOT NULL,
    action VARCHAR(96) CHARACTER SET ascii COLLATE ascii_bin NOT NULL,
    request_correlation_id VARCHAR(128) CHARACTER SET ascii COLLATE ascii_bin NOT NULL,
    reason VARCHAR(500) NULL,
    result ENUM('success', 'denied', 'conflict', 'failed') NOT NULL,
    metadata_json JSON NOT NULL,
    created_at DATETIME(6) NOT NULL DEFAULT CURRENT_TIMESTAMP(6),
    PRIMARY KEY (id),
    KEY idx_qr_audit_workspace_created (workspace_id, created_at, id),
    KEY idx_qr_audit_qr_created (qr_id, created_at, id),
    KEY idx_qr_audit_correlation (request_correlation_id),
    CONSTRAINT fk_qr_audit_qr FOREIGN KEY (qr_id) REFERENCES qr_codes(id) ON DELETE RESTRICT,
    CONSTRAINT fk_qr_audit_source_link FOREIGN KEY (source_link_id) REFERENCES links(id) ON DELETE RESTRICT,
    CONSTRAINT chk_qr_audit_actor_nonempty CHECK (CHAR_LENGTH(TRIM(actor_id)) > 0),
    CONSTRAINT chk_qr_audit_correlation_nonempty CHECK (CHAR_LENGTH(TRIM(request_correlation_id)) > 0)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_0900_ai_ci;
