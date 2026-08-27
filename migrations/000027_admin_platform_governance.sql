-- GoJet V10 / P17 Admin, Permissions and Audit
-- Repository-global immutable migration: 000027
-- MySQL 8.x
-- P17 platform governance contribution. Secrets remain encrypted and audit-safe.

CREATE TABLE admin_platform_settings (
    setting_key ENUM('general','brand') NOT NULL,
    value_json JSON NOT NULL,
    version BIGINT UNSIGNED NOT NULL DEFAULT 1,
    updated_by VARCHAR(128) CHARACTER SET ascii COLLATE ascii_bin NOT NULL,
    updated_at DATETIME(6) NOT NULL DEFAULT CURRENT_TIMESTAMP(6) ON UPDATE CURRENT_TIMESTAMP(6),
    PRIMARY KEY (setting_key),
    CONSTRAINT fk_admin_platform_settings_actor FOREIGN KEY (updated_by) REFERENCES admin_administrators(id) ON DELETE RESTRICT,
    CONSTRAINT chk_admin_platform_settings_version CHECK (version >= 1)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_0900_ai_ci;

CREATE TABLE admin_turnstile_config (
    id TINYINT UNSIGNED NOT NULL,
    site_key VARCHAR(255) CHARACTER SET ascii COLLATE ascii_bin NOT NULL,
    secret_ciphertext VARBINARY(2048) NULL,
    secret_key_id VARCHAR(128) CHARACTER SET ascii COLLATE ascii_bin NULL,
    enabled BOOLEAN NOT NULL DEFAULT FALSE,
    provider_state ENUM('healthy','incomplete','provider_error') NOT NULL DEFAULT 'incomplete',
    version BIGINT UNSIGNED NOT NULL DEFAULT 1,
    updated_by VARCHAR(128) CHARACTER SET ascii COLLATE ascii_bin NOT NULL,
    updated_at DATETIME(6) NOT NULL DEFAULT CURRENT_TIMESTAMP(6) ON UPDATE CURRENT_TIMESTAMP(6),
    PRIMARY KEY (id),
    CONSTRAINT fk_admin_turnstile_actor FOREIGN KEY (updated_by) REFERENCES admin_administrators(id) ON DELETE RESTRICT,
    CONSTRAINT chk_admin_turnstile_singleton CHECK (id = 1),
    CONSTRAINT chk_admin_turnstile_version CHECK (version >= 1),
    CONSTRAINT chk_admin_turnstile_secret_pair CHECK ((secret_ciphertext IS NULL AND secret_key_id IS NULL) OR (secret_ciphertext IS NOT NULL AND secret_key_id IS NOT NULL))
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_0900_ai_ci;

CREATE TABLE admin_official_domains (
    id VARCHAR(64) CHARACTER SET ascii COLLATE ascii_bin NOT NULL,
    hostname_ascii VARCHAR(253) CHARACTER SET ascii COLLATE ascii_bin NOT NULL,
    enabled BOOLEAN NOT NULL DEFAULT TRUE,
    is_default BOOLEAN NOT NULL DEFAULT FALSE,
    https_state ENUM('pending','active','failed') NOT NULL DEFAULT 'pending',
    version BIGINT UNSIGNED NOT NULL DEFAULT 1,
    created_by VARCHAR(128) CHARACTER SET ascii COLLATE ascii_bin NOT NULL,
    updated_by VARCHAR(128) CHARACTER SET ascii COLLATE ascii_bin NOT NULL,
    created_at DATETIME(6) NOT NULL DEFAULT CURRENT_TIMESTAMP(6),
    updated_at DATETIME(6) NOT NULL DEFAULT CURRENT_TIMESTAMP(6) ON UPDATE CURRENT_TIMESTAMP(6),
    PRIMARY KEY (id),
    UNIQUE KEY uq_admin_official_domains_hostname (hostname_ascii),
    KEY idx_admin_official_domains_state (enabled,is_default,https_state,hostname_ascii),
    CONSTRAINT fk_admin_official_domains_creator FOREIGN KEY (created_by) REFERENCES admin_administrators(id) ON DELETE RESTRICT,
    CONSTRAINT fk_admin_official_domains_updater FOREIGN KEY (updated_by) REFERENCES admin_administrators(id) ON DELETE RESTRICT,
    CONSTRAINT chk_admin_official_domains_version CHECK (version >= 1)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_0900_ai_ci;

CREATE TABLE admin_announcements (
    id VARCHAR(64) CHARACTER SET ascii COLLATE ascii_bin NOT NULL,
    title VARCHAR(200) NOT NULL,
    summary VARCHAR(500) NOT NULL,
    body MEDIUMTEXT NOT NULL,
    scope ENUM('global','workspace') NOT NULL,
    workspace_id VARCHAR(64) NULL,
    lifecycle_state ENUM('draft','scheduled','published','archived') NOT NULL DEFAULT 'draft',
    scheduled_for DATETIME(6) NULL,
    published_at DATETIME(6) NULL,
    archived_at DATETIME(6) NULL,
    cache_generation BIGINT UNSIGNED NOT NULL DEFAULT 1,
    version BIGINT UNSIGNED NOT NULL DEFAULT 1,
    created_by VARCHAR(128) CHARACTER SET ascii COLLATE ascii_bin NOT NULL,
    updated_by VARCHAR(128) CHARACTER SET ascii COLLATE ascii_bin NOT NULL,
    created_at DATETIME(6) NOT NULL DEFAULT CURRENT_TIMESTAMP(6),
    updated_at DATETIME(6) NOT NULL DEFAULT CURRENT_TIMESTAMP(6) ON UPDATE CURRENT_TIMESTAMP(6),
    PRIMARY KEY (id),
    KEY idx_admin_announcements_scope_state (scope,workspace_id,lifecycle_state,scheduled_for,id),
    CONSTRAINT fk_admin_announcements_creator FOREIGN KEY (created_by) REFERENCES admin_administrators(id) ON DELETE RESTRICT,
    CONSTRAINT fk_admin_announcements_updater FOREIGN KEY (updated_by) REFERENCES admin_administrators(id) ON DELETE RESTRICT,
    CONSTRAINT chk_admin_announcements_scope CHECK ((scope='global' AND workspace_id IS NULL) OR (scope='workspace' AND workspace_id IS NOT NULL)),
    CONSTRAINT chk_admin_announcements_version CHECK (version >= 1),
    CONSTRAINT chk_admin_announcements_cache_generation CHECK (cache_generation >= 1)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_0900_ai_ci;

CREATE TABLE admin_content_cache_state (
    cache_key VARCHAR(128) CHARACTER SET ascii COLLATE ascii_bin NOT NULL,
    generation BIGINT UNSIGNED NOT NULL DEFAULT 1,
    updated_at DATETIME(6) NOT NULL DEFAULT CURRENT_TIMESTAMP(6) ON UPDATE CURRENT_TIMESTAMP(6),
    PRIMARY KEY (cache_key),
    CONSTRAINT chk_admin_content_cache_generation CHECK (generation >= 1)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_0900_ai_ci;

INSERT INTO admin_content_cache_state(cache_key,generation) VALUES ('announcements',1);
