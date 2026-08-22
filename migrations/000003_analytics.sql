-- GoJet V10 / P07 Analytics
-- Repository-global immutable migration: 000003
-- MySQL 8.x

CREATE TABLE analytics_outbox (
    event_id CHAR(64) CHARACTER SET ascii COLLATE ascii_bin NOT NULL,
    workspace_id VARCHAR(64) NOT NULL,
    link_id BIGINT UNSIGNED NOT NULL,
    click_sequence BIGINT UNSIGNED NOT NULL,
    occurred_at DATETIME(6) NOT NULL,
    country_code VARCHAR(8) CHARACTER SET ascii COLLATE ascii_bin NOT NULL DEFAULT '',
    device VARCHAR(16) CHARACTER SET ascii COLLATE ascii_bin NOT NULL DEFAULT '',
    language VARCHAR(32) CHARACTER SET ascii COLLATE ascii_bin NOT NULL DEFAULT '',
    source_hostname VARCHAR(253) CHARACTER SET ascii COLLATE ascii_bin NOT NULL DEFAULT '',
    campaign_id VARCHAR(64) CHARACTER SET ascii COLLATE ascii_bin NOT NULL DEFAULT '',
    payload_json JSON NOT NULL,
    published_at DATETIME(6) NULL,
    published_stream_id VARCHAR(64) CHARACTER SET ascii COLLATE ascii_bin NULL,
    publish_attempts BIGINT UNSIGNED NOT NULL DEFAULT 0,
    last_publish_error VARCHAR(500) NULL,
    created_at DATETIME(6) NOT NULL DEFAULT CURRENT_TIMESTAMP(6),
    updated_at DATETIME(6) NOT NULL DEFAULT CURRENT_TIMESTAMP(6) ON UPDATE CURRENT_TIMESTAMP(6),
    PRIMARY KEY (event_id),
    UNIQUE KEY uq_analytics_outbox_link_click (link_id, click_sequence),
    KEY idx_analytics_outbox_pending (published_at, created_at, event_id),
    KEY idx_analytics_outbox_workspace_time (workspace_id, occurred_at, event_id),
    CONSTRAINT fk_analytics_outbox_link FOREIGN KEY (link_id) REFERENCES links(id) ON DELETE RESTRICT,
    CONSTRAINT chk_analytics_outbox_click_sequence CHECK (click_sequence > 0)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_0900_ai_ci;

CREATE TABLE analytics_events (
    event_id CHAR(64) CHARACTER SET ascii COLLATE ascii_bin NOT NULL,
    workspace_id VARCHAR(64) NOT NULL,
    link_id BIGINT UNSIGNED NOT NULL,
    click_sequence BIGINT UNSIGNED NOT NULL,
    occurred_at DATETIME(6) NOT NULL,
    country_code VARCHAR(8) CHARACTER SET ascii COLLATE ascii_bin NOT NULL DEFAULT '',
    device VARCHAR(16) CHARACTER SET ascii COLLATE ascii_bin NOT NULL DEFAULT '',
    language VARCHAR(32) CHARACTER SET ascii COLLATE ascii_bin NOT NULL DEFAULT '',
    source_hostname VARCHAR(253) CHARACTER SET ascii COLLATE ascii_bin NOT NULL DEFAULT '',
    campaign_id VARCHAR(64) CHARACTER SET ascii COLLATE ascii_bin NOT NULL DEFAULT '',
    stream_id VARCHAR(64) CHARACTER SET ascii COLLATE ascii_bin NOT NULL,
    consumed_at DATETIME(6) NOT NULL DEFAULT CURRENT_TIMESTAMP(6),
    PRIMARY KEY (event_id),
    UNIQUE KEY uq_analytics_events_link_click (link_id, click_sequence),
    KEY idx_analytics_events_workspace_time (workspace_id, occurred_at, event_id),
    KEY idx_analytics_events_workspace_link_time (workspace_id, link_id, occurred_at, event_id),
    CONSTRAINT fk_analytics_events_link FOREIGN KEY (link_id) REFERENCES links(id) ON DELETE RESTRICT,
    CONSTRAINT chk_analytics_events_click_sequence CHECK (click_sequence > 0)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_0900_ai_ci;

CREATE TABLE analytics_hourly_aggregates (
    workspace_id VARCHAR(64) NOT NULL,
    link_id BIGINT UNSIGNED NOT NULL,
    bucket_start DATETIME(6) NOT NULL,
    country_code VARCHAR(8) CHARACTER SET ascii COLLATE ascii_bin NOT NULL DEFAULT '',
    device VARCHAR(16) CHARACTER SET ascii COLLATE ascii_bin NOT NULL DEFAULT '',
    language VARCHAR(32) CHARACTER SET ascii COLLATE ascii_bin NOT NULL DEFAULT '',
    source_hostname VARCHAR(253) CHARACTER SET ascii COLLATE ascii_bin NOT NULL DEFAULT '',
    campaign_id VARCHAR(64) CHARACTER SET ascii COLLATE ascii_bin NOT NULL DEFAULT '',
    clicks BIGINT UNSIGNED NOT NULL DEFAULT 0,
    updated_at DATETIME(6) NOT NULL DEFAULT CURRENT_TIMESTAMP(6) ON UPDATE CURRENT_TIMESTAMP(6),
    PRIMARY KEY (
        workspace_id, link_id, bucket_start, country_code, device,
        language, source_hostname, campaign_id
    ),
    KEY idx_analytics_hourly_workspace_bucket (workspace_id, bucket_start, link_id),
    CONSTRAINT fk_analytics_hourly_link FOREIGN KEY (link_id) REFERENCES links(id) ON DELETE RESTRICT
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_0900_ai_ci;

CREATE TABLE analytics_reconciliation_runs (
    id BIGINT UNSIGNED NOT NULL AUTO_INCREMENT,
    run_id VARCHAR(64) CHARACTER SET ascii COLLATE ascii_bin NOT NULL,
    scope VARCHAR(96) CHARACTER SET ascii COLLATE ascii_bin NOT NULL,
    source_event_total BIGINT UNSIGNED NOT NULL,
    aggregate_total_before BIGINT UNSIGNED NOT NULL,
    aggregate_total_after BIGINT UNSIGNED NOT NULL,
    repaired TINYINT(1) NOT NULL DEFAULT 0,
    metadata_json JSON NOT NULL,
    created_at DATETIME(6) NOT NULL DEFAULT CURRENT_TIMESTAMP(6),
    PRIMARY KEY (id),
    UNIQUE KEY uq_analytics_reconciliation_run_id (run_id),
    KEY idx_analytics_reconciliation_created (created_at, id),
    CONSTRAINT chk_analytics_reconciliation_repaired CHECK (repaired IN (0, 1))
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_0900_ai_ci;
