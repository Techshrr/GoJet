-- GoJet V10 / P15 Authentication, OAuth and Account
-- Repository-global immutable migration: 000018
-- MySQL 8.x
-- Seeds the exact frozen OAuth provider registry. Provider credentials remain
-- disabled/unconfigured until an authorized settings.manage mutation stores
-- encrypted server-side configuration.

INSERT INTO oauth_provider_configs
(provider,enabled,client_id,client_secret_ciphertext,secret_key_id,authorization_url,token_url,userinfo_url,redirect_uri,scopes_json,version,updated_by)
VALUES
('google',0,'',NULL,NULL,'','','','',JSON_ARRAY(),1,'system'),
('facebook',0,'',NULL,NULL,'','','','',JSON_ARRAY(),1,'system'),
('github',0,'',NULL,NULL,'','','','',JSON_ARRAY(),1,'system'),
('qq',0,'',NULL,NULL,'','','','',JSON_ARRAY(),1,'system'),
('wechat',0,'',NULL,NULL,'','','','',JSON_ARRAY(),1,'system'),
('rainbow',0,'',NULL,NULL,'','','','',JSON_ARRAY(),1,'system')
ON DUPLICATE KEY UPDATE provider=VALUES(provider);
