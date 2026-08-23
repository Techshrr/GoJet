-- GoJet V10 / P10 Text Sharing
-- Repository-global immutable migration: 000006
-- MySQL 8.x

CREATE TABLE text_workspace_counters (
    workspace_id VARCHAR(64) NOT NULL,
    active_count BIGINT UNSIGNED NOT NULL DEFAULT 0,
    updated_at DATETIME(6) NOT NULL DEFAULT CURRENT_TIMESTAMP(6) ON UPDATE CURRENT_TIMESTAMP(6),
    PRIMARY KEY (workspace_id)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_0900_ai_ci;

CREATE TABLE text_shares (
    id BIGINT UNSIGNED NOT NULL AUTO_INCREMENT,
    workspace_id VARCHAR(64) NOT NULL,
    public_slug VARCHAR(64) CHARACTER SET ascii COLLATE ascii_bin NOT NULL,
    title VARCHAR(160) NOT NULL,
    content MEDIUMTEXT NOT NULL,
    visibility ENUM('private','public') NOT NULL DEFAULT 'private',
    password_hash VARCHAR(255) NULL,
    expires_at DATETIME(6) NULL,
    one_time TINYINT(1) NOT NULL DEFAULT 0,
    consumed_at DATETIME(6) NULL,
    version BIGINT UNSIGNED NOT NULL DEFAULT 1,
    created_by VARCHAR(128) NOT NULL,
    created_at DATETIME(6) NOT NULL DEFAULT CURRENT_TIMESTAMP(6),
    updated_at DATETIME(6) NOT NULL DEFAULT CURRENT_TIMESTAMP(6) ON UPDATE CURRENT_TIMESTAMP(6),
    deleted_at DATETIME(6) NULL,
    PRIMARY KEY (id),
    UNIQUE KEY uq_text_shares_public_slug (public_slug),
    KEY idx_text_shares_workspace_active (workspace_id, deleted_at, updated_at, id),
    KEY idx_text_shares_public_authority (public_slug, visibility, expires_at, consumed_at, deleted_at),
    CONSTRAINT chk_text_workspace_nonempty CHECK (CHAR_LENGTH(TRIM(workspace_id)) > 0),
    CONSTRAINT chk_text_slug_nonempty CHECK (CHAR_LENGTH(TRIM(public_slug)) >= 16),
    CONSTRAINT chk_text_title_nonempty CHECK (CHAR_LENGTH(TRIM(title)) > 0),
    CONSTRAINT chk_text_content_nonempty CHECK (CHAR_LENGTH(content) > 0),
    CONSTRAINT chk_text_one_time_bool CHECK (one_time IN (0,1)),
    CONSTRAINT chk_text_consumed_one_time CHECK (consumed_at IS NULL OR one_time = 1),
    CONSTRAINT chk_text_version_positive CHECK (version > 0),
    CONSTRAINT chk_text_created_by_nonempty CHECK (CHAR_LENGTH(TRIM(created_by)) > 0)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_0900_ai_ci;

CREATE TABLE text_audit_events (
    id BIGINT UNSIGNED NOT NULL AUTO_INCREMENT,
    workspace_id VARCHAR(64) NOT NULL,
    text_share_id BIGINT UNSIGNED NULL,
    actor_id VARCHAR(128) NOT NULL,
    action VARCHAR(96) CHARACTER SET ascii COLLATE ascii_bin NOT NULL,
    request_correlation_id VARCHAR(128) CHARACTER SET ascii COLLATE ascii_bin NOT NULL,
    reason VARCHAR(500) NULL,
    result ENUM('success','denied','conflict','failed') NOT NULL,
    metadata_json JSON NOT NULL,
    created_at DATETIME(6) NOT NULL DEFAULT CURRENT_TIMESTAMP(6),
    PRIMARY KEY (id),
    KEY idx_text_audit_workspace_created (workspace_id, created_at, id),
    KEY idx_text_audit_share_created (text_share_id, created_at, id),
    KEY idx_text_audit_correlation (request_correlation_id),
    CONSTRAINT fk_text_audit_share FOREIGN KEY (text_share_id) REFERENCES text_shares(id) ON DELETE RESTRICT,
    CONSTRAINT chk_text_audit_actor_nonempty CHECK (CHAR_LENGTH(TRIM(actor_id)) > 0),
    CONSTRAINT chk_text_audit_correlation_nonempty CHECK (CHAR_LENGTH(TRIM(request_correlation_id)) > 0)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_0900_ai_ci;
