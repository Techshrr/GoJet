-- GoJet V10 / P13 Billing, Payments and Entitlements
-- Repository-global immutable migration: 000009
-- MySQL 8.x
-- Money is stored only as integer minor units plus ISO currency.
-- Raw provider callback bodies, signatures and secrets are intentionally not persisted.

CREATE TABLE billing_plans (
    id BIGINT UNSIGNED NOT NULL AUTO_INCREMENT,
    code VARCHAR(64) CHARACTER SET ascii COLLATE ascii_bin NOT NULL,
    name VARCHAR(160) NOT NULL,
    status ENUM('draft','active','archived') NOT NULL DEFAULT 'draft',
    currency CHAR(3) CHARACTER SET ascii COLLATE ascii_bin NOT NULL,
    amount_minor BIGINT UNSIGNED NOT NULL,
    billing_period ENUM('one_time','monthly','yearly') NOT NULL,
    version BIGINT UNSIGNED NOT NULL DEFAULT 1,
    created_at DATETIME(6) NOT NULL DEFAULT CURRENT_TIMESTAMP(6),
    updated_at DATETIME(6) NOT NULL DEFAULT CURRENT_TIMESTAMP(6) ON UPDATE CURRENT_TIMESTAMP(6),
    PRIMARY KEY (id),
    UNIQUE KEY uq_billing_plan_code (code),
    KEY idx_billing_plan_status (status, id),
    CONSTRAINT chk_billing_plan_version CHECK (version >= 1),
    CONSTRAINT chk_billing_plan_currency CHECK (currency REGEXP '^[A-Z]{3}$')
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_0900_ai_ci;

CREATE TABLE billing_plan_entitlements (
    plan_id BIGINT UNSIGNED NOT NULL,
    capability VARCHAR(96) CHARACTER SET ascii COLLATE ascii_bin NOT NULL,
    limit_value BIGINT UNSIGNED NOT NULL,
    unit VARCHAR(32) CHARACTER SET ascii COLLATE ascii_bin NOT NULL DEFAULT 'count',
    source_version BIGINT UNSIGNED NOT NULL DEFAULT 1,
    PRIMARY KEY (plan_id, capability),
    CONSTRAINT fk_billing_plan_entitlements_plan FOREIGN KEY (plan_id) REFERENCES billing_plans(id) ON DELETE RESTRICT,
    CONSTRAINT chk_billing_plan_entitlement_limit CHECK (limit_value > 0),
    CONSTRAINT chk_billing_plan_entitlement_version CHECK (source_version >= 1)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_0900_ai_ci;

CREATE TABLE workspace_subscriptions (
    id VARCHAR(64) CHARACTER SET ascii COLLATE ascii_bin NOT NULL,
    workspace_id VARCHAR(64) CHARACTER SET ascii COLLATE ascii_bin NOT NULL,
    plan_id BIGINT UNSIGNED NOT NULL,
    status ENUM('pending','active','grace','overdue','canceled','expired') NOT NULL DEFAULT 'pending',
    starts_at DATETIME(6) NOT NULL,
    current_term_ends_at DATETIME(6) NULL,
    grace_ends_at DATETIME(6) NULL,
    cancel_at DATETIME(6) NULL,
    version BIGINT UNSIGNED NOT NULL DEFAULT 1,
    created_at DATETIME(6) NOT NULL DEFAULT CURRENT_TIMESTAMP(6),
    updated_at DATETIME(6) NOT NULL DEFAULT CURRENT_TIMESTAMP(6) ON UPDATE CURRENT_TIMESTAMP(6),
    PRIMARY KEY (id),
    KEY idx_workspace_subscriptions_workspace (workspace_id, status, updated_at, id),
    CONSTRAINT fk_workspace_subscriptions_workspace FOREIGN KEY (workspace_id) REFERENCES workspaces(id) ON DELETE RESTRICT,
    CONSTRAINT fk_workspace_subscriptions_plan FOREIGN KEY (plan_id) REFERENCES billing_plans(id) ON DELETE RESTRICT,
    CONSTRAINT chk_workspace_subscription_version CHECK (version >= 1),
    CONSTRAINT chk_workspace_subscription_term CHECK (current_term_ends_at IS NULL OR current_term_ends_at > starts_at),
    CONSTRAINT chk_workspace_subscription_grace CHECK (grace_ends_at IS NULL OR current_term_ends_at IS NULL OR grace_ends_at >= current_term_ends_at)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_0900_ai_ci;

CREATE TABLE entitlement_grants (
    id BIGINT UNSIGNED NOT NULL AUTO_INCREMENT,
    workspace_id VARCHAR(64) CHARACTER SET ascii COLLATE ascii_bin NOT NULL,
    capability VARCHAR(96) CHARACTER SET ascii COLLATE ascii_bin NOT NULL,
    source_type ENUM('hard_deny','manual','inherited','billing','baseline') NOT NULL,
    source_id VARCHAR(128) CHARACTER SET ascii COLLATE ascii_bin NOT NULL,
    limit_value BIGINT UNSIGNED NOT NULL DEFAULT 0,
    starts_at DATETIME(6) NOT NULL,
    ends_at DATETIME(6) NULL,
    revoked_at DATETIME(6) NULL,
    provenance_json JSON NOT NULL,
    created_at DATETIME(6) NOT NULL DEFAULT CURRENT_TIMESTAMP(6),
    updated_at DATETIME(6) NOT NULL DEFAULT CURRENT_TIMESTAMP(6) ON UPDATE CURRENT_TIMESTAMP(6),
    PRIMARY KEY (id),
    UNIQUE KEY uq_entitlement_grant_source (workspace_id, capability, source_type, source_id),
    KEY idx_entitlement_grants_resolve (workspace_id, capability, starts_at, ends_at, revoked_at, id),
    CONSTRAINT fk_entitlement_grants_workspace FOREIGN KEY (workspace_id) REFERENCES workspaces(id) ON DELETE RESTRICT,
    CONSTRAINT chk_entitlement_grant_limit CHECK (source_type = 'hard_deny' OR limit_value > 0),
    CONSTRAINT chk_entitlement_grant_window CHECK (ends_at IS NULL OR ends_at > starts_at),
    CONSTRAINT chk_entitlement_grant_revoke CHECK (revoked_at IS NULL OR revoked_at >= starts_at)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_0900_ai_ci;

CREATE TABLE billing_orders (
    id VARCHAR(64) CHARACTER SET ascii COLLATE ascii_bin NOT NULL,
    workspace_id VARCHAR(64) CHARACTER SET ascii COLLATE ascii_bin NOT NULL,
    plan_id BIGINT UNSIGNED NOT NULL,
    kind ENUM('new','upgrade','renewal') NOT NULL,
    currency CHAR(3) CHARACTER SET ascii COLLATE ascii_bin NOT NULL,
    amount_minor BIGINT UNSIGNED NOT NULL,
    status ENUM('pending','processing','paid','failed','canceled','refunded') NOT NULL DEFAULT 'pending',
    idempotency_key_hash BINARY(32) NOT NULL,
    created_at DATETIME(6) NOT NULL DEFAULT CURRENT_TIMESTAMP(6),
    updated_at DATETIME(6) NOT NULL DEFAULT CURRENT_TIMESTAMP(6) ON UPDATE CURRENT_TIMESTAMP(6),
    PRIMARY KEY (id),
    UNIQUE KEY uq_billing_order_idempotency (workspace_id, idempotency_key_hash),
    KEY idx_billing_orders_workspace (workspace_id, created_at, id),
    CONSTRAINT fk_billing_orders_workspace FOREIGN KEY (workspace_id) REFERENCES workspaces(id) ON DELETE RESTRICT,
    CONSTRAINT fk_billing_orders_plan FOREIGN KEY (plan_id) REFERENCES billing_plans(id) ON DELETE RESTRICT,
    CONSTRAINT chk_billing_order_currency CHECK (currency REGEXP '^[A-Z]{3}$'),
    CONSTRAINT chk_billing_order_amount CHECK (amount_minor > 0)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_0900_ai_ci;

CREATE TABLE billing_invoices (
    id VARCHAR(64) CHARACTER SET ascii COLLATE ascii_bin NOT NULL,
    workspace_id VARCHAR(64) CHARACTER SET ascii COLLATE ascii_bin NOT NULL,
    order_id VARCHAR(64) CHARACTER SET ascii COLLATE ascii_bin NOT NULL,
    currency CHAR(3) CHARACTER SET ascii COLLATE ascii_bin NOT NULL,
    amount_minor BIGINT UNSIGNED NOT NULL,
    status ENUM('open','paid','void','refunded') NOT NULL DEFAULT 'open',
    issued_at DATETIME(6) NOT NULL,
    paid_at DATETIME(6) NULL,
    refunded_at DATETIME(6) NULL,
    created_at DATETIME(6) NOT NULL DEFAULT CURRENT_TIMESTAMP(6),
    PRIMARY KEY (id),
    UNIQUE KEY uq_billing_invoice_order (order_id),
    KEY idx_billing_invoices_workspace (workspace_id, issued_at, id),
    CONSTRAINT fk_billing_invoices_workspace FOREIGN KEY (workspace_id) REFERENCES workspaces(id) ON DELETE RESTRICT,
    CONSTRAINT fk_billing_invoices_order FOREIGN KEY (order_id) REFERENCES billing_orders(id) ON DELETE RESTRICT,
    CONSTRAINT chk_billing_invoice_currency CHECK (currency REGEXP '^[A-Z]{3}$'),
    CONSTRAINT chk_billing_invoice_amount CHECK (amount_minor > 0)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_0900_ai_ci;

CREATE TABLE billing_transactions (
    id BIGINT UNSIGNED NOT NULL AUTO_INCREMENT,
    workspace_id VARCHAR(64) CHARACTER SET ascii COLLATE ascii_bin NOT NULL,
    order_id VARCHAR(64) CHARACTER SET ascii COLLATE ascii_bin NOT NULL,
    provider ENUM('alipay','wechat','epay','paypal','stripe','crypto') NOT NULL,
    provider_transaction_id VARCHAR(191) CHARACTER SET ascii COLLATE ascii_bin NOT NULL,
    currency CHAR(3) CHARACTER SET ascii COLLATE ascii_bin NOT NULL,
    amount_minor BIGINT UNSIGNED NOT NULL,
    status ENUM('pending','paid','failed','refunded') NOT NULL DEFAULT 'pending',
    created_at DATETIME(6) NOT NULL DEFAULT CURRENT_TIMESTAMP(6),
    updated_at DATETIME(6) NOT NULL DEFAULT CURRENT_TIMESTAMP(6) ON UPDATE CURRENT_TIMESTAMP(6),
    PRIMARY KEY (id),
    UNIQUE KEY uq_billing_transaction_provider (provider, provider_transaction_id),
    KEY idx_billing_transactions_order (order_id, id),
    KEY idx_billing_transactions_workspace (workspace_id, created_at, id),
    CONSTRAINT fk_billing_transactions_workspace FOREIGN KEY (workspace_id) REFERENCES workspaces(id) ON DELETE RESTRICT,
    CONSTRAINT fk_billing_transactions_order FOREIGN KEY (order_id) REFERENCES billing_orders(id) ON DELETE RESTRICT,
    CONSTRAINT chk_billing_transaction_currency CHECK (currency REGEXP '^[A-Z]{3}$'),
    CONSTRAINT chk_billing_transaction_amount CHECK (amount_minor > 0)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_0900_ai_ci;

CREATE TABLE payment_callback_events (
    id BIGINT UNSIGNED NOT NULL AUTO_INCREMENT,
    provider ENUM('alipay','wechat','epay','paypal','stripe','crypto') NOT NULL,
    provider_event_id VARCHAR(191) CHARACTER SET ascii COLLATE ascii_bin NOT NULL,
    provider_transaction_id VARCHAR(191) CHARACTER SET ascii COLLATE ascii_bin NOT NULL,
    event_type VARCHAR(96) CHARACTER SET ascii COLLATE ascii_bin NOT NULL,
    status ENUM('accepted','duplicate','invalid','ignored','processed') NOT NULL,
    received_at DATETIME(6) NOT NULL,
    processed_at DATETIME(6) NULL,
    correlation_id VARCHAR(128) CHARACTER SET ascii COLLATE ascii_bin NOT NULL,
    created_at DATETIME(6) NOT NULL DEFAULT CURRENT_TIMESTAMP(6),
    PRIMARY KEY (id),
    UNIQUE KEY uq_payment_callback_provider_event (provider, provider_event_id),
    KEY idx_payment_callback_transaction (provider, provider_transaction_id, id),
    KEY idx_payment_callback_correlation (correlation_id),
    CONSTRAINT chk_payment_callback_processed CHECK (processed_at IS NULL OR processed_at >= received_at)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_0900_ai_ci;

CREATE TABLE billing_fx_rates (
    base_currency CHAR(3) CHARACTER SET ascii COLLATE ascii_bin NOT NULL,
    quote_currency CHAR(3) CHARACTER SET ascii COLLATE ascii_bin NOT NULL,
    rate DECIMAL(28,12) NOT NULL,
    source VARCHAR(96) CHARACTER SET ascii COLLATE ascii_bin NOT NULL,
    as_of DATETIME(6) NOT NULL,
    status ENUM('current','stale','provider-error','override') NOT NULL,
    override_reason VARCHAR(500) NULL,
    updated_at DATETIME(6) NOT NULL DEFAULT CURRENT_TIMESTAMP(6) ON UPDATE CURRENT_TIMESTAMP(6),
    PRIMARY KEY (base_currency, quote_currency),
    CONSTRAINT chk_billing_fx_base CHECK (base_currency REGEXP '^[A-Z]{3}$'),
    CONSTRAINT chk_billing_fx_quote CHECK (quote_currency REGEXP '^[A-Z]{3}$'),
    CONSTRAINT chk_billing_fx_rate CHECK (rate > 0),
    CONSTRAINT chk_billing_fx_pair CHECK (base_currency <> quote_currency),
    CONSTRAINT chk_billing_fx_override CHECK (status <> 'override' OR (override_reason IS NOT NULL AND CHAR_LENGTH(TRIM(override_reason)) > 0))
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_0900_ai_ci;

CREATE TABLE billing_audit_events (
    id BIGINT UNSIGNED NOT NULL AUTO_INCREMENT,
    workspace_id VARCHAR(64) CHARACTER SET ascii COLLATE ascii_bin NULL,
    actor_id VARCHAR(128) NOT NULL,
    action VARCHAR(96) CHARACTER SET ascii COLLATE ascii_bin NOT NULL,
    resource_type VARCHAR(64) CHARACTER SET ascii COLLATE ascii_bin NOT NULL,
    resource_id VARCHAR(128) CHARACTER SET ascii COLLATE ascii_bin NOT NULL DEFAULT '',
    reason VARCHAR(500) NULL,
    request_correlation_id VARCHAR(128) CHARACTER SET ascii COLLATE ascii_bin NOT NULL,
    result ENUM('success','denied','conflict','failed') NOT NULL,
    metadata_json JSON NOT NULL,
    created_at DATETIME(6) NOT NULL DEFAULT CURRENT_TIMESTAMP(6),
    PRIMARY KEY (id),
    KEY idx_billing_audit_workspace (workspace_id, created_at, id),
    KEY idx_billing_audit_actor (actor_id, created_at, id),
    KEY idx_billing_audit_correlation (request_correlation_id),
    CONSTRAINT fk_billing_audit_workspace FOREIGN KEY (workspace_id) REFERENCES workspaces(id) ON DELETE RESTRICT
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_0900_ai_ci;
