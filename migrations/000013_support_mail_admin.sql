-- GoJet V10 / P14 Support Tickets and Mail
-- Repository-global immutable migration: 000013
-- MySQL 8.x
-- Admin Mail settings deliberately contain no SMTP address, username, password,
-- provider response or other credential material. Transport secrets remain runtime-owned.

CREATE TABLE mail_settings (
    settings_key VARCHAR(32) CHARACTER SET ascii COLLATE ascii_bin NOT NULL,
    enabled TINYINT(1) NOT NULL DEFAULT 1,
    version BIGINT UNSIGNED NOT NULL DEFAULT 1,
    updated_at DATETIME(6) NOT NULL DEFAULT CURRENT_TIMESTAMP(6) ON UPDATE CURRENT_TIMESTAMP(6),
    PRIMARY KEY (settings_key),
    CONSTRAINT chk_mail_settings_key CHECK (settings_key = 'primary'),
    CONSTRAINT chk_mail_settings_enabled CHECK (enabled IN (0,1)),
    CONSTRAINT chk_mail_settings_version CHECK (version >= 1)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_0900_ai_ci;

INSERT INTO mail_settings (settings_key,enabled,version)
VALUES ('primary',1,1)
ON DUPLICATE KEY UPDATE settings_key=settings_key;

INSERT INTO mail_templates
(template_key,locale,version,subject_template,text_template,html_template,variable_allowlist_json,internal_only,enabled)
VALUES
(
  'mail-test','en',1,
  'GoJet mail delivery test',
  'This is a GoJet mail delivery test.',
  '<p>This is a GoJet mail delivery test.</p>',
  JSON_ARRAY(),0,1
)
ON DUPLICATE KEY UPDATE template_key=template_key;
