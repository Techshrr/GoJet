-- GoJet V10 / P20-D016 server-authoritative payment-provider correlation
-- Repository-global immutable migration: 000030
-- MySQL 8.x
-- Rollback: not mechanically safe after payment intent data exists; restore from tested backup.
-- This table stores provider binding/quote evidence only. Billing order/invoice Money remains ISO-3 minor-unit authority.
-- Raw callback bodies, signatures, secrets, payer PII and private keys are prohibited here.

CREATE TABLE billing_provider_intents (
    id VARCHAR(64) CHARACTER SET ascii COLLATE ascii_bin NOT NULL,
    workspace_id VARCHAR(64) CHARACTER SET ascii COLLATE ascii_bin NOT NULL,
    order_id VARCHAR(64) CHARACTER SET ascii COLLATE ascii_bin NOT NULL,
    provider ENUM('alipay','wechat','epay','paypal','stripe','crypto') NOT NULL,
    merchant_reference VARCHAR(64) CHARACTER SET ascii COLLATE ascii_bin NOT NULL,
    provider_reference VARCHAR(191) CHARACTER SET ascii COLLATE ascii_bin NULL,
    settlement_asset_kind ENUM('fiat','token') NOT NULL,
    settlement_asset VARCHAR(16) CHARACTER SET ascii COLLATE ascii_bin NOT NULL,
    settlement_amount_units BIGINT UNSIGNED NOT NULL,
    settlement_scale TINYINT UNSIGNED NULL,
    status ENUM('active','canceled') NOT NULL DEFAULT 'active',
    expires_at DATETIME(6) NULL,
    canceled_at DATETIME(6) NULL,
    created_at DATETIME(6) NOT NULL DEFAULT CURRENT_TIMESTAMP(6),
    updated_at DATETIME(6) NOT NULL DEFAULT CURRENT_TIMESTAMP(6) ON UPDATE CURRENT_TIMESTAMP(6),
    PRIMARY KEY (id),
    UNIQUE KEY uq_billing_provider_intent_merchant (provider, merchant_reference),
    UNIQUE KEY uq_billing_provider_intent_provider_ref (provider, provider_reference),
    KEY idx_billing_provider_intent_order (order_id, status, created_at, id),
    KEY idx_billing_provider_intent_workspace (workspace_id, status, created_at, id),
    CONSTRAINT fk_billing_provider_intent_workspace FOREIGN KEY (workspace_id) REFERENCES workspaces(id) ON DELETE RESTRICT,
    CONSTRAINT fk_billing_provider_intent_order FOREIGN KEY (order_id) REFERENCES billing_orders(id) ON DELETE RESTRICT,
    CONSTRAINT chk_billing_provider_intent_merchant_nonempty CHECK (CHAR_LENGTH(TRIM(merchant_reference)) > 0),
    CONSTRAINT chk_billing_provider_intent_provider_ref_nonempty CHECK (provider_reference IS NULL OR CHAR_LENGTH(TRIM(provider_reference)) > 0),
    CONSTRAINT chk_billing_provider_intent_amount CHECK (settlement_amount_units > 0),
    CONSTRAINT chk_billing_provider_intent_asset CHECK (
        (settlement_asset_kind = 'fiat' AND settlement_asset REGEXP '^[A-Z]{3}$' AND settlement_scale IS NULL)
        OR
        (settlement_asset_kind = 'token' AND settlement_asset REGEXP '^[A-Z0-9]{3,16}$' AND settlement_scale BETWEEN 1 AND 18)
    ),
    CONSTRAINT chk_billing_provider_intent_cancel CHECK (
        (status = 'active' AND canceled_at IS NULL)
        OR
        (status = 'canceled' AND canceled_at IS NOT NULL)
    ),
    CONSTRAINT chk_billing_provider_intent_expiry CHECK (expires_at IS NULL OR expires_at > created_at)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_0900_ai_ci;
