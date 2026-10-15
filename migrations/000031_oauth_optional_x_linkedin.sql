-- P20 user-requested optional direct X and LinkedIn login providers.
-- Append enum values to preserve existing ordinal meanings and all identities.
-- Rollback is NOT mechanically safe after new-provider identities/states exist;
-- disable the providers, or use the release backup/restore boundary.
ALTER TABLE oauth_identities MODIFY provider ENUM('google','facebook','github','qq','wechat','rainbow','x','linkedin') NOT NULL;
ALTER TABLE oauth_states MODIFY provider ENUM('google','facebook','github','qq','wechat','rainbow','x','linkedin') NOT NULL;
ALTER TABLE oauth_handoffs MODIFY provider ENUM('google','facebook','github','qq','wechat','rainbow','x','linkedin') NOT NULL;
ALTER TABLE oauth_provider_configs MODIFY provider ENUM('google','facebook','github','qq','wechat','rainbow','x','linkedin') NOT NULL;

INSERT INTO oauth_provider_configs
(provider,enabled,client_id,client_secret_ciphertext,secret_key_id,authorization_url,token_url,userinfo_url,redirect_uri,scopes_json,version,updated_by)
VALUES
('x',0,'',NULL,NULL,'https://x.com/i/oauth2/authorize','https://api.x.com/2/oauth2/token','https://api.x.com/2/users/me','',JSON_ARRAY('tweet.read','users.read'),1,'system'),
('linkedin',0,'',NULL,NULL,'https://www.linkedin.com/oauth/v2/authorization','https://www.linkedin.com/oauth/v2/accessToken','https://api.linkedin.com/v2/userinfo','',JSON_ARRAY('openid','profile','email'),1,'system');
