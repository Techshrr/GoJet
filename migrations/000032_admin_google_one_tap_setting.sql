-- GoJet P22: append the existing Google One Tap preference to the setting key domain.
-- Preserve the existing ENUM ordinals and every general/brand row.
-- Recovery uses the verified pre-upgrade database backup; no down migration.
ALTER TABLE admin_platform_settings
    MODIFY COLUMN setting_key ENUM('general','brand','google_one_tap') NOT NULL;
