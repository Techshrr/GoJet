-- GoJet V10 / P11 Bio
-- Repository-global immutable migration: 000007
-- MySQL 8.x
-- Rollback: data-destructive; restore from tested database backup instead of a fake down migration.

CREATE TABLE bio_workspace_counters (
    workspace_id VARCHAR(64) NOT NULL,
    active_count BIGINT UNSIGNED NOT NULL DEFAULT 0,
    updated_at DATETIME(6) NOT NULL DEFAULT CURRENT_TIMESTAMP(6) ON UPDATE CURRENT_TIMESTAMP(6),
    PRIMARY KEY (workspace_id)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_0900_ai_ci;

CREATE TABLE bio_pages (
    id BIGINT UNSIGNED NOT NULL AUTO_INCREMENT,
    workspace_id VARCHAR(64) NOT NULL,
    slug VARCHAR(64) CHARACTER SET ascii COLLATE ascii_bin NOT NULL,
    title VARCHAR(160) NOT NULL,
    bio TEXT NOT NULL,
    status ENUM('draft','published','paused') NOT NULL DEFAULT 'draft',
    version BIGINT UNSIGNED NOT NULL DEFAULT 1,
    published_at DATETIME(6) NULL,
    created_by VARCHAR(128) NOT NULL,
    created_at DATETIME(6) NOT NULL DEFAULT CURRENT_TIMESTAMP(6),
    updated_at DATETIME(6) NOT NULL DEFAULT CURRENT_TIMESTAMP(6) ON UPDATE CURRENT_TIMESTAMP(6),
    deleted_at DATETIME(6) NULL,
    PRIMARY KEY (id),
    UNIQUE KEY uq_bio_pages_slug (slug),
    KEY idx_bio_pages_workspace_active (workspace_id, deleted_at, updated_at, id),
    KEY idx_bio_pages_public_authority (slug, status, deleted_at),
    CONSTRAINT chk_bio_workspace_nonempty CHECK (CHAR_LENGTH(TRIM(workspace_id)) > 0),
    CONSTRAINT chk_bio_slug_nonempty CHECK (CHAR_LENGTH(TRIM(slug)) >= 16),
    CONSTRAINT chk_bio_title_nonempty CHECK (CHAR_LENGTH(TRIM(title)) > 0),
    CONSTRAINT chk_bio_version_positive CHECK (version > 0),
    CONSTRAINT chk_bio_created_by_nonempty CHECK (CHAR_LENGTH(TRIM(created_by)) > 0)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_0900_ai_ci;

CREATE TABLE bio_child_links (
    id BIGINT UNSIGNED NOT NULL AUTO_INCREMENT,
    bio_page_id BIGINT UNSIGNED NOT NULL,
    position INT UNSIGNED NOT NULL,
    label VARCHAR(160) NOT NULL,
    destination_url VARCHAR(2048) CHARACTER SET utf8mb4 COLLATE utf8mb4_bin NOT NULL,
    destination_fingerprint CHAR(64) CHARACTER SET ascii COLLATE ascii_bin NOT NULL,
    risk_status ENUM('review','allowed','blocked') NOT NULL DEFAULT 'review',
    risk_checked_at DATETIME(6) NULL,
    created_at DATETIME(6) NOT NULL DEFAULT CURRENT_TIMESTAMP(6),
    updated_at DATETIME(6) NOT NULL DEFAULT CURRENT_TIMESTAMP(6) ON UPDATE CURRENT_TIMESTAMP(6),
    PRIMARY KEY (id),
    UNIQUE KEY uq_bio_child_position (bio_page_id, position),
    KEY idx_bio_child_risk (bio_page_id, risk_status, position),
    KEY idx_bio_child_fingerprint (destination_fingerprint),
    CONSTRAINT fk_bio_child_page FOREIGN KEY (bio_page_id) REFERENCES bio_pages(id) ON DELETE RESTRICT,
    CONSTRAINT chk_bio_child_label_nonempty CHECK (CHAR_LENGTH(TRIM(label)) > 0),
    CONSTRAINT chk_bio_child_destination_nonempty CHECK (CHAR_LENGTH(TRIM(destination_url)) > 0),
    CONSTRAINT chk_bio_child_fingerprint CHECK (destination_fingerprint REGEXP '^[0-9a-f]{64}$')
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_0900_ai_ci;

CREATE TABLE bio_audit_events (
    id BIGINT UNSIGNED NOT NULL AUTO_INCREMENT,
    workspace_id VARCHAR(64) NOT NULL,
    bio_page_id BIGINT UNSIGNED NULL,
    actor_id VARCHAR(128) NOT NULL,
    action VARCHAR(96) CHARACTER SET ascii COLLATE ascii_bin NOT NULL,
    request_correlation_id VARCHAR(128) CHARACTER SET ascii COLLATE ascii_bin NOT NULL,
    reason VARCHAR(500) NULL,
    result ENUM('success','denied','conflict','failed') NOT NULL,
    metadata_json JSON NOT NULL,
    created_at DATETIME(6) NOT NULL DEFAULT CURRENT_TIMESTAMP(6),
    PRIMARY KEY (id),
    KEY idx_bio_audit_workspace_created (workspace_id, created_at, id),
    KEY idx_bio_audit_page_created (bio_page_id, created_at, id),
    KEY idx_bio_audit_correlation (request_correlation_id),
    CONSTRAINT fk_bio_audit_page FOREIGN KEY (bio_page_id) REFERENCES bio_pages(id) ON DELETE RESTRICT,
    CONSTRAINT chk_bio_audit_actor_nonempty CHECK (CHAR_LENGTH(TRIM(actor_id)) > 0),
    CONSTRAINT chk_bio_audit_correlation_nonempty CHECK (CHAR_LENGTH(TRIM(request_correlation_id)) > 0)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_0900_ai_ci;
