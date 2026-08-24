-- GoJet V10 / P14 Support Tickets and Mail
-- Repository-global immutable migration: 000010
-- MySQL 8.x
-- Raw Turnstile tokens, raw idempotency material, SMTP credentials, provider responses,
-- attachment bytes and raw mail-worker claim tokens are intentionally not persisted.

CREATE TABLE support_public_contacts (
    id VARCHAR(64) CHARACTER SET ascii COLLATE ascii_bin NOT NULL,
    email VARCHAR(320) NOT NULL,
    name VARCHAR(160) NOT NULL,
    subject VARCHAR(300) NOT NULL,
    message TEXT NOT NULL,
    status VARCHAR(32) CHARACTER SET ascii COLLATE ascii_bin NOT NULL DEFAULT 'new',
    idempotency_key_hash BINARY(32) NOT NULL,
    correlation_id VARCHAR(128) CHARACTER SET ascii COLLATE ascii_bin NOT NULL,
    created_at DATETIME(6) NOT NULL DEFAULT CURRENT_TIMESTAMP(6),
    updated_at DATETIME(6) NOT NULL DEFAULT CURRENT_TIMESTAMP(6) ON UPDATE CURRENT_TIMESTAMP(6),
    PRIMARY KEY (id),
    UNIQUE KEY uq_support_public_contact_idempotency (idempotency_key_hash),
    KEY idx_support_public_contact_status (status, created_at, id),
    KEY idx_support_public_contact_correlation (correlation_id),
    CONSTRAINT chk_support_public_contact_email CHECK (CHAR_LENGTH(TRIM(email)) > 0),
    CONSTRAINT chk_support_public_contact_name CHECK (CHAR_LENGTH(TRIM(name)) > 0),
    CONSTRAINT chk_support_public_contact_subject CHECK (CHAR_LENGTH(TRIM(subject)) > 0),
    CONSTRAINT chk_support_public_contact_message CHECK (CHAR_LENGTH(TRIM(message)) > 0),
    CONSTRAINT chk_support_public_contact_correlation CHECK (CHAR_LENGTH(TRIM(correlation_id)) > 0)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_0900_ai_ci;

CREATE TABLE support_tickets (
    id VARCHAR(64) CHARACTER SET ascii COLLATE ascii_bin NOT NULL,
    workspace_id VARCHAR(64) CHARACTER SET ascii COLLATE ascii_bin NULL,
    requester_user_id VARCHAR(128) NULL,
    public_contact_id VARCHAR(64) CHARACTER SET ascii COLLATE ascii_bin NULL,
    category VARCHAR(64) CHARACTER SET ascii COLLATE ascii_bin NOT NULL,
    subject VARCHAR(300) NOT NULL,
    status ENUM('open','awaiting_user','awaiting_support','closed') NOT NULL DEFAULT 'open',
    idempotency_key_hash BINARY(32) NOT NULL,
    created_at DATETIME(6) NOT NULL DEFAULT CURRENT_TIMESTAMP(6),
    updated_at DATETIME(6) NOT NULL DEFAULT CURRENT_TIMESTAMP(6) ON UPDATE CURRENT_TIMESTAMP(6),
    closed_at DATETIME(6) NULL,
    version BIGINT UNSIGNED NOT NULL DEFAULT 1,
    correlation_id VARCHAR(128) CHARACTER SET ascii COLLATE ascii_bin NOT NULL,
    PRIMARY KEY (id),
    UNIQUE KEY uq_support_ticket_workspace_idempotency (workspace_id, idempotency_key_hash),
    UNIQUE KEY uq_support_ticket_public_contact (public_contact_id),
    KEY idx_support_ticket_workspace_status (workspace_id, status, updated_at, id),
    KEY idx_support_ticket_requester (workspace_id, requester_user_id, updated_at, id),
    KEY idx_support_ticket_category (category, status, updated_at, id),
    KEY idx_support_ticket_correlation (correlation_id),
    CONSTRAINT fk_support_ticket_workspace FOREIGN KEY (workspace_id) REFERENCES workspaces(id) ON DELETE RESTRICT,
    CONSTRAINT fk_support_ticket_public_contact FOREIGN KEY (public_contact_id) REFERENCES support_public_contacts(id) ON DELETE RESTRICT,
    CONSTRAINT chk_support_ticket_scope CHECK (
        (workspace_id IS NOT NULL AND requester_user_id IS NOT NULL AND public_contact_id IS NULL)
        OR (workspace_id IS NULL AND requester_user_id IS NULL AND public_contact_id IS NOT NULL)
    ),
    CONSTRAINT chk_support_ticket_category CHECK (CHAR_LENGTH(TRIM(category)) > 0),
    CONSTRAINT chk_support_ticket_subject CHECK (CHAR_LENGTH(TRIM(subject)) > 0),
    CONSTRAINT chk_support_ticket_version CHECK (version >= 1),
    CONSTRAINT chk_support_ticket_correlation CHECK (CHAR_LENGTH(TRIM(correlation_id)) > 0),
    CONSTRAINT chk_support_ticket_closed CHECK (
        (status = 'closed' AND closed_at IS NOT NULL)
        OR (status <> 'closed' AND closed_at IS NULL)
    )
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_0900_ai_ci;

CREATE TABLE support_ticket_messages (
    id VARCHAR(64) CHARACTER SET ascii COLLATE ascii_bin NOT NULL,
    ticket_id VARCHAR(64) CHARACTER SET ascii COLLATE ascii_bin NOT NULL,
    actor_type ENUM('requester','support') NOT NULL,
    actor_id VARCHAR(128) NOT NULL,
    kind ENUM('requester_reply','support_reply','internal_note') NOT NULL,
    body TEXT NOT NULL,
    idempotency_key_hash BINARY(32) NOT NULL,
    created_at DATETIME(6) NOT NULL DEFAULT CURRENT_TIMESTAMP(6),
    correlation_id VARCHAR(128) CHARACTER SET ascii COLLATE ascii_bin NOT NULL,
    PRIMARY KEY (id),
    UNIQUE KEY uq_support_ticket_message_idempotency (ticket_id, idempotency_key_hash),
    KEY idx_support_ticket_messages_ticket (ticket_id, created_at, id),
    KEY idx_support_ticket_messages_correlation (correlation_id),
    CONSTRAINT fk_support_ticket_message_ticket FOREIGN KEY (ticket_id) REFERENCES support_tickets(id) ON DELETE RESTRICT,
    CONSTRAINT chk_support_ticket_message_actor CHECK (
        (kind = 'requester_reply' AND actor_type = 'requester')
        OR (kind IN ('support_reply','internal_note') AND actor_type = 'support')
    ),
    CONSTRAINT chk_support_ticket_message_actor_id CHECK (CHAR_LENGTH(TRIM(actor_id)) > 0),
    CONSTRAINT chk_support_ticket_message_body CHECK (CHAR_LENGTH(TRIM(body)) > 0),
    CONSTRAINT chk_support_ticket_message_correlation CHECK (CHAR_LENGTH(TRIM(correlation_id)) > 0)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_0900_ai_ci;

CREATE TABLE support_ticket_attachments (
    id VARCHAR(64) CHARACTER SET ascii COLLATE ascii_bin NOT NULL,
    ticket_id VARCHAR(64) CHARACTER SET ascii COLLATE ascii_bin NOT NULL,
    message_id VARCHAR(64) CHARACTER SET ascii COLLATE ascii_bin NOT NULL,
    storage_key CHAR(64) CHARACTER SET ascii COLLATE ascii_bin NOT NULL,
    original_name_safe VARCHAR(255) NOT NULL,
    mime_type VARCHAR(160) NOT NULL,
    size_bytes BIGINT UNSIGNED NOT NULL,
    sha256 CHAR(64) CHARACTER SET ascii COLLATE ascii_bin NOT NULL,
    scan_status ENUM('quarantined','scanning','clean','infected','scan-error','rejected') NOT NULL DEFAULT 'quarantined',
    scan_updated_at DATETIME(6) NOT NULL DEFAULT CURRENT_TIMESTAMP(6),
    created_at DATETIME(6) NOT NULL DEFAULT CURRENT_TIMESTAMP(6),
    PRIMARY KEY (id),
    UNIQUE KEY uq_support_ticket_attachment_storage (storage_key),
    KEY idx_support_ticket_attachment_ticket (ticket_id, created_at, id),
    KEY idx_support_ticket_attachment_message (message_id, id),
    KEY idx_support_ticket_attachment_scan (scan_status, scan_updated_at, id),
    CONSTRAINT fk_support_ticket_attachment_ticket FOREIGN KEY (ticket_id) REFERENCES support_tickets(id) ON DELETE RESTRICT,
    CONSTRAINT fk_support_ticket_attachment_message FOREIGN KEY (message_id) REFERENCES support_ticket_messages(id) ON DELETE RESTRICT,
    CONSTRAINT chk_support_ticket_attachment_storage CHECK (storage_key REGEXP '^[0-9a-f]{64}$'),
    CONSTRAINT chk_support_ticket_attachment_sha CHECK (sha256 REGEXP '^[0-9a-f]{64}$'),
    CONSTRAINT chk_support_ticket_attachment_name CHECK (CHAR_LENGTH(TRIM(original_name_safe)) > 0),
    CONSTRAINT chk_support_ticket_attachment_mime CHECK (CHAR_LENGTH(TRIM(mime_type)) > 0),
    CONSTRAINT chk_support_ticket_attachment_size CHECK (size_bytes > 0),
    CONSTRAINT chk_support_ticket_attachment_scan_time CHECK (scan_updated_at >= created_at)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_0900_ai_ci;

CREATE TABLE mail_templates (
    template_key VARCHAR(96) CHARACTER SET ascii COLLATE ascii_bin NOT NULL,
    locale VARCHAR(32) CHARACTER SET ascii COLLATE ascii_bin NOT NULL,
    version BIGINT UNSIGNED NOT NULL,
    subject_template VARCHAR(500) NOT NULL,
    text_template MEDIUMTEXT NOT NULL,
    html_template MEDIUMTEXT NOT NULL,
    variable_allowlist_json JSON NOT NULL,
    internal_only TINYINT(1) NOT NULL DEFAULT 0,
    enabled TINYINT(1) NOT NULL DEFAULT 1,
    updated_at DATETIME(6) NOT NULL DEFAULT CURRENT_TIMESTAMP(6) ON UPDATE CURRENT_TIMESTAMP(6),
    PRIMARY KEY (template_key, locale, version),
    KEY idx_mail_templates_active (template_key, locale, enabled, version),
    CONSTRAINT chk_mail_template_key CHECK (CHAR_LENGTH(TRIM(template_key)) > 0),
    CONSTRAINT chk_mail_template_locale CHECK (CHAR_LENGTH(TRIM(locale)) > 0),
    CONSTRAINT chk_mail_template_version CHECK (version >= 1),
    CONSTRAINT chk_mail_template_allowlist CHECK (JSON_TYPE(variable_allowlist_json) = 'ARRAY')
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_0900_ai_ci;

CREATE TABLE mail_jobs (
    id VARCHAR(64) CHARACTER SET ascii COLLATE ascii_bin NOT NULL,
    template_key VARCHAR(96) CHARACTER SET ascii COLLATE ascii_bin NOT NULL,
    template_locale VARCHAR(32) CHARACTER SET ascii COLLATE ascii_bin NOT NULL,
    template_version BIGINT UNSIGNED NOT NULL,
    recipient_kind VARCHAR(64) CHARACTER SET ascii COLLATE ascii_bin NOT NULL,
    recipient_value VARCHAR(320) NOT NULL,
    resource_type VARCHAR(64) CHARACTER SET ascii COLLATE ascii_bin NOT NULL,
    resource_id VARCHAR(128) CHARACTER SET ascii COLLATE ascii_bin NOT NULL,
    status ENUM('queued','sending','retrying','sent','failed') NOT NULL DEFAULT 'queued',
    attempt_count INT UNSIGNED NOT NULL DEFAULT 0,
    next_attempt_at DATETIME(6) NULL,
    idempotency_key_hash BINARY(32) NOT NULL,
    claim_token_hash BINARY(32) NULL,
    claim_expires_at DATETIME(6) NULL,
    last_error_code VARCHAR(96) CHARACTER SET ascii COLLATE ascii_bin NULL,
    created_at DATETIME(6) NOT NULL DEFAULT CURRENT_TIMESTAMP(6),
    updated_at DATETIME(6) NOT NULL DEFAULT CURRENT_TIMESTAMP(6) ON UPDATE CURRENT_TIMESTAMP(6),
    PRIMARY KEY (id),
    UNIQUE KEY uq_mail_job_logical_idempotency (idempotency_key_hash),
    KEY idx_mail_job_claim (status, next_attempt_at, created_at, id),
    KEY idx_mail_job_resource (resource_type, resource_id, created_at, id),
    KEY idx_mail_job_template (template_key, template_locale, template_version, created_at, id),
    CONSTRAINT fk_mail_job_template FOREIGN KEY (template_key, template_locale, template_version) REFERENCES mail_templates(template_key, locale, version) ON DELETE RESTRICT,
    CONSTRAINT chk_mail_job_attempt_count CHECK (attempt_count <= 5),
    CONSTRAINT chk_mail_job_retry_due CHECK (
        (status = 'retrying' AND next_attempt_at IS NOT NULL)
        OR (status <> 'retrying' AND next_attempt_at IS NULL)
    ),
    CONSTRAINT chk_mail_job_claim CHECK (
        (status = 'sending' AND claim_token_hash IS NOT NULL AND claim_expires_at IS NOT NULL)
        OR (status <> 'sending' AND claim_token_hash IS NULL AND claim_expires_at IS NULL)
    ),
    CONSTRAINT chk_mail_job_recipient_kind CHECK (CHAR_LENGTH(TRIM(recipient_kind)) > 0),
    CONSTRAINT chk_mail_job_recipient_value CHECK (CHAR_LENGTH(TRIM(recipient_value)) > 0),
    CONSTRAINT chk_mail_job_resource CHECK (CHAR_LENGTH(TRIM(resource_type)) > 0 AND CHAR_LENGTH(TRIM(resource_id)) > 0)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_0900_ai_ci;

CREATE TABLE mail_attempts (
    id BIGINT UNSIGNED NOT NULL AUTO_INCREMENT,
    mail_job_id VARCHAR(64) CHARACTER SET ascii COLLATE ascii_bin NOT NULL,
    attempt_number INT UNSIGNED NOT NULL,
    status ENUM('sending','sent','transient_failure','terminal_failure') NOT NULL,
    smtp_code INT UNSIGNED NULL,
    error_code VARCHAR(96) CHARACTER SET ascii COLLATE ascii_bin NULL,
    started_at DATETIME(6) NOT NULL,
    completed_at DATETIME(6) NULL,
    correlation_id VARCHAR(128) CHARACTER SET ascii COLLATE ascii_bin NOT NULL,
    created_at DATETIME(6) NOT NULL DEFAULT CURRENT_TIMESTAMP(6),
    PRIMARY KEY (id),
    UNIQUE KEY uq_mail_attempt_number (mail_job_id, attempt_number),
    KEY idx_mail_attempt_correlation (correlation_id),
    CONSTRAINT fk_mail_attempt_job FOREIGN KEY (mail_job_id) REFERENCES mail_jobs(id) ON DELETE RESTRICT,
    CONSTRAINT chk_mail_attempt_number CHECK (attempt_number BETWEEN 1 AND 5),
    CONSTRAINT chk_mail_attempt_window CHECK (completed_at IS NULL OR completed_at >= started_at),
    CONSTRAINT chk_mail_attempt_correlation CHECK (CHAR_LENGTH(TRIM(correlation_id)) > 0)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_0900_ai_ci;

CREATE TABLE support_audit_events (
    id BIGINT UNSIGNED NOT NULL AUTO_INCREMENT,
    workspace_id VARCHAR(64) CHARACTER SET ascii COLLATE ascii_bin NULL,
    actor_id VARCHAR(128) NOT NULL,
    action VARCHAR(96) CHARACTER SET ascii COLLATE ascii_bin NOT NULL,
    resource_type VARCHAR(64) CHARACTER SET ascii COLLATE ascii_bin NOT NULL,
    resource_id VARCHAR(128) CHARACTER SET ascii COLLATE ascii_bin NOT NULL,
    reason VARCHAR(500) NULL,
    request_correlation_id VARCHAR(128) CHARACTER SET ascii COLLATE ascii_bin NOT NULL,
    result ENUM('success','denied','conflict','failed') NOT NULL,
    metadata_json JSON NOT NULL,
    created_at DATETIME(6) NOT NULL DEFAULT CURRENT_TIMESTAMP(6),
    PRIMARY KEY (id),
    KEY idx_support_audit_workspace (workspace_id, created_at, id),
    KEY idx_support_audit_resource (resource_type, resource_id, created_at, id),
    KEY idx_support_audit_actor (actor_id, created_at, id),
    KEY idx_support_audit_correlation (request_correlation_id),
    CONSTRAINT fk_support_audit_workspace FOREIGN KEY (workspace_id) REFERENCES workspaces(id) ON DELETE RESTRICT,
    CONSTRAINT chk_support_audit_actor CHECK (CHAR_LENGTH(TRIM(actor_id)) > 0),
    CONSTRAINT chk_support_audit_action CHECK (CHAR_LENGTH(TRIM(action)) > 0),
    CONSTRAINT chk_support_audit_resource CHECK (CHAR_LENGTH(TRIM(resource_type)) > 0 AND CHAR_LENGTH(TRIM(resource_id)) > 0),
    CONSTRAINT chk_support_audit_correlation CHECK (CHAR_LENGTH(TRIM(request_correlation_id)) > 0)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_0900_ai_ci;
