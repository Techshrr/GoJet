-- GoJet V10 / P05 Links Vertical Slice
-- Repository-global immutable migration: 000001
-- MySQL 8.x

CREATE TABLE links (
    id BIGINT UNSIGNED NOT NULL AUTO_INCREMENT,
    workspace_id VARCHAR(64) NOT NULL,
    hostname VARCHAR(253) NOT NULL,
    domain_kind ENUM('official', 'custom') NOT NULL DEFAULT 'official',
    code VARCHAR(128) CHARACTER SET ascii COLLATE ascii_bin NOT NULL,
    title VARCHAR(255) NOT NULL DEFAULT '',
    primary_destination TEXT NOT NULL,
    redirect_status SMALLINT UNSIGNED NOT NULL DEFAULT 302,
    status ENUM('active', 'paused', 'deleted') NOT NULL DEFAULT 'active',
    version BIGINT UNSIGNED NOT NULL DEFAULT 1,
    risk_fingerprint CHAR(64) CHARACTER SET ascii COLLATE ascii_bin NOT NULL,
    routing_json JSON NOT NULL,
    ab_json JSON NOT NULL,
    utm_json JSON NOT NULL,
    access_json JSON NOT NULL,
    expires_at DATETIME(6) NULL,
    click_limit BIGINT UNSIGNED NULL,
    click_count BIGINT UNSIGNED NOT NULL DEFAULT 0,
    one_time TINYINT(1) NOT NULL DEFAULT 0,
    created_at DATETIME(6) NOT NULL DEFAULT CURRENT_TIMESTAMP(6),
    updated_at DATETIME(6) NOT NULL DEFAULT CURRENT_TIMESTAMP(6) ON UPDATE CURRENT_TIMESTAMP(6),
    deleted_at DATETIME(6) NULL,
    PRIMARY KEY (id),
    UNIQUE KEY uq_links_hostname_code (hostname, code),
    KEY idx_links_workspace_status_updated (workspace_id, status, updated_at, id),
    KEY idx_links_workspace_hostname (workspace_id, hostname, id),
    KEY idx_links_risk_fingerprint (risk_fingerprint),
    CONSTRAINT chk_links_redirect_status CHECK (redirect_status IN (301, 302, 307, 308)),
    CONSTRAINT chk_links_version_positive CHECK (version >= 1),
    CONSTRAINT chk_links_click_limit_positive CHECK (click_limit IS NULL OR click_limit > 0),
    CONSTRAINT chk_links_one_time CHECK (one_time IN (0, 1))
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_0900_ai_ci;

CREATE TABLE link_versions (
    id BIGINT UNSIGNED NOT NULL AUTO_INCREMENT,
    link_id BIGINT UNSIGNED NOT NULL,
    workspace_id VARCHAR(64) NOT NULL,
    version BIGINT UNSIGNED NOT NULL,
    actor_id VARCHAR(128) NOT NULL,
    change_reason VARCHAR(500) NOT NULL,
    snapshot_json JSON NOT NULL,
    risk_fingerprint CHAR(64) CHARACTER SET ascii COLLATE ascii_bin NOT NULL,
    created_at DATETIME(6) NOT NULL DEFAULT CURRENT_TIMESTAMP(6),
    PRIMARY KEY (id),
    UNIQUE KEY uq_link_versions_link_version (link_id, version),
    KEY idx_link_versions_workspace_link (workspace_id, link_id, version),
    CONSTRAINT fk_link_versions_link FOREIGN KEY (link_id) REFERENCES links(id) ON DELETE RESTRICT,
    CONSTRAINT chk_link_versions_version_positive CHECK (version >= 1),
    CONSTRAINT chk_link_versions_reason_nonempty CHECK (CHAR_LENGTH(TRIM(change_reason)) > 0)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_0900_ai_ci;

CREATE TABLE link_audit_events (
    id BIGINT UNSIGNED NOT NULL AUTO_INCREMENT,
    workspace_id VARCHAR(64) NOT NULL,
    link_id BIGINT UNSIGNED NULL,
    actor_id VARCHAR(128) NOT NULL,
    action VARCHAR(96) CHARACTER SET ascii COLLATE ascii_bin NOT NULL,
    request_correlation_id VARCHAR(128) CHARACTER SET ascii COLLATE ascii_bin NOT NULL,
    reason VARCHAR(500) NULL,
    result ENUM('success', 'denied', 'conflict', 'failed') NOT NULL,
    metadata_json JSON NOT NULL,
    created_at DATETIME(6) NOT NULL DEFAULT CURRENT_TIMESTAMP(6),
    PRIMARY KEY (id),
    KEY idx_link_audit_workspace_created (workspace_id, created_at, id),
    KEY idx_link_audit_link_created (link_id, created_at, id),
    KEY idx_link_audit_correlation (request_correlation_id),
    CONSTRAINT fk_link_audit_link FOREIGN KEY (link_id) REFERENCES links(id) ON DELETE RESTRICT
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_0900_ai_ci;
