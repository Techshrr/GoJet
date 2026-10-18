<?php

declare(strict_types=1);
namespace GoJet\Installer;
require_once __DIR__.'/ServiceEnvironment.php';

/** Typed native installation settings. No process execution, network calls or state transitions. */
final class NativeConfiguration
{
    private static function keys(mixed $value, array $keys): void
    {
        if (!is_array($value) || array_diff($keys, array_keys($value)) || array_diff(array_keys($value), $keys)) {
            throw new \InvalidArgumentException('Invalid configuration fields');
        }
    }
    private static function text(mixed $value, int $min, int $max): string
    {
        if (!is_string($value) || strlen($value) < $min || strlen($value) > $max
            || preg_match('/[\x00-\x1f\x7f]/', $value) || !preg_match('//u', $value)) {
            throw new \InvalidArgumentException('Invalid configuration value');
        }
        return $value;
    }
    private static function number(mixed $value, int $min, int $max): int
    {
        if (!is_int($value) || $value < $min || $value > $max) { throw new \InvalidArgumentException('Invalid numeric setting'); }
        return $value;
    }
    private static function host(mixed $host): string
    {
        self::text($host, 1, 253);
        if (!preg_match('/^[a-z0-9](?:[a-z0-9-]{0,61}[a-z0-9])?(?:\.[a-z0-9](?:[a-z0-9-]{0,61}[a-z0-9])?)*$/D', $host)) {
            throw new \InvalidArgumentException('Invalid hostname');
        }
        return $host;
    }
    private static function path(mixed $path): string
    {
        self::text($path, 1, 256);
        if (!preg_match('~^/(?:[A-Za-z0-9_-][A-Za-z0-9_.-]*/)*[A-Za-z0-9_-][A-Za-z0-9_.-]*$~D', $path)) {
            throw new \InvalidArgumentException('Invalid native path');
        }
        return $path;
    }
    public static function compile(array $p): array
    {
        self::keys($p, ['schema', 'origin', 'database', 'redis', 'secrets', 'files', 'quotas', 'smtp', 'risk', 'turnstile_secret', 'auth_rate']);
        if ($p['schema'] !== 'gojet.native-configuration.v1') { throw new \InvalidArgumentException('Invalid schema'); }
        $origin = self::text($p['origin'], 1, 300);
        $url = parse_url($origin);
        if (!is_array($url) || ($url['scheme'] ?? '') !== 'https'
            || array_diff(array_keys($url), ['scheme', 'host', 'port'])) { throw new \InvalidArgumentException('HTTPS origin required'); }
        $host = self::host($url['host'] ?? '');
        if (filter_var($host, FILTER_VALIDATE_IP) !== false) { throw new \InvalidArgumentException('DNS origin required'); }
        $port = self::number($url['port'] ?? 443, 1, 65535);
        if ($origin !== 'https://'.$host.(isset($url['port']) ? ':'.$port : '')) { throw new \InvalidArgumentException('Canonical origin required'); }
        foreach (['database' => ['port', 'name', 'user', 'password'], 'redis' => ['port', 'db', 'password']] as $name => $fields) {
            self::keys($p[$name], $fields);
            self::number($p[$name]['port'], 1, 65535);
            self::text($p[$name]['password'], 0, 1024);
        }
        foreach (['name', 'user'] as $name) {
            if (!preg_match('/^[A-Za-z0-9_]{1,64}$/D', self::text($p['database'][$name], 1, 64))) { throw new \InvalidArgumentException('Invalid database identity'); }
        }
        self::number($p['redis']['db'], 0, 15);
        if ($p['database']['port'] === $p['redis']['port']) { throw new \InvalidArgumentException('Dependency port conflict'); }
        $secretNames = ['csrf', 'grant', 'oauth', 'admin', 'file_public', 'text_public'];
        self::keys($p['secrets'], ['key_id', ...$secretNames]);
        $keyID = self::text($p['secrets']['key_id'], 1, 64);
        if (!preg_match('/^[A-Za-z0-9_.-]+$/D', $keyID)) { throw new \InvalidArgumentException('Invalid key identity'); }
        $seen = [];
        foreach ($secretNames as $name) {
            $secret = self::text($p['secrets'][$name], 64, 64);
            if (!preg_match('/^[a-f0-9]{64}$/D', $secret) || $secret === str_repeat('0', 64) || in_array($secret, $seen, true)) {
                throw new \InvalidArgumentException('Independent 256-bit keys required');
            }
            $seen[] = $secret;
        }
        self::keys($p['files'], ['root', 'clamav_socket', 'max_upload_bytes', 'max_files', 'max_bytes', 'types']);
        $files = $p['files'];
        self::path($files['root']); self::path($files['clamav_socket']);
        if ($files['root'] === $files['clamav_socket'] || str_starts_with($files['clamav_socket'], $files['root'].'/')) {
            throw new \InvalidArgumentException('Storage and scanner paths conflict');
        }
        self::number($files['max_upload_bytes'], 1, 1072693248); // Leave1MiB for multipart overhead under the1GiB ingress bound.
        self::number($files['max_files'], 1, 1000000);
        self::number($files['max_bytes'], $files['max_upload_bytes'], PHP_INT_MAX);
        if (!is_array($files['types']) || count($files['types']) < 1 || count($files['types']) > 64) { throw new \InvalidArgumentException('File policy required'); }
        $types = [];
        foreach ($files['types'] as $ext => $mimes) {
            if (!is_string($ext) || !preg_match('/^[a-z0-9]{1,16}$/D', $ext) || !is_array($mimes)
                || !array_is_list($mimes) || count($mimes) < 1 || count($mimes) > 8) { throw new \InvalidArgumentException('Invalid file policy'); }
            foreach ($mimes as $mime) {
                if (!is_string($mime) || !preg_match('~^[a-z0-9][a-z0-9.+-]{0,63}/[a-z0-9][a-z0-9.+-]{0,127}$~D', $mime)) { throw new \InvalidArgumentException('Invalid MIME policy'); }
            }
            $types[] = $ext.'='.implode('|', $mimes);
        }
        self::keys($p['quotas'], ['text', 'bio', 'qr']);
        foreach (['text' => 1000000, 'bio' => 1000000, 'qr' => 100000] as $name => $max) { self::number($p['quotas'][$name], 1, $max); }
        self::keys($p['auth_rate'], ['limit', 'window_seconds']);
        self::number($p['auth_rate']['limit'], 1, 1000);
        self::number($p['auth_rate']['window_seconds'], 1, 86400);
        self::keys($p['smtp'], ['host', 'port', 'from', 'username', 'password']);
        $smtp = $p['smtp']; self::host($smtp['host']); self::number($smtp['port'], 1, 65535);
        if (filter_var(self::text($smtp['from'], 3, 254), FILTER_VALIDATE_EMAIL) === false) { throw new \InvalidArgumentException('Invalid sender'); }
        self::text($smtp['username'], 0, 256); self::text($smtp['password'], 0, 1024);
        if (($smtp['username'] === '') !== ($smtp['password'] === '') || trim($smtp['username']) !== $smtp['username']) { throw new \InvalidArgumentException('Incomplete SMTP credentials'); }
        self::keys($p['risk'], ['name', 'endpoint', 'policy_version']);
        $risk = $p['risk'];
        foreach (['name', 'policy_version'] as $name) {
            if (!preg_match('/^[A-Za-z0-9_.-]{1,64}$/D', self::text($risk[$name], 1, 64))) { throw new \InvalidArgumentException('Invalid risk identity'); }
        }
        $endpoint = parse_url(self::text($risk['endpoint'], 1, 2048));
        if (!is_array($endpoint) || ($endpoint['scheme'] ?? '') !== 'https' || !isset($endpoint['host'])
            || array_diff(array_keys($endpoint), ['scheme', 'host', 'port', 'path'])) { throw new \InvalidArgumentException('Invalid risk endpoint'); }
        self::host($endpoint['host']); self::number($endpoint['port'] ?? 443, 1, 65535);
        $turnstile = self::text($p['turnstile_secret'], 1, 1024);
        if (trim($turnstile) !== $turnstile) { throw new \InvalidArgumentException('Invalid verification credential'); }
        $db = $p['database'];
        $database = ['GOJET_MYSQL_DSN' => $db['user'].':'.$db['password'].'@tcp(127.0.0.1:'.$db['port'].')/'.$db['name'].'?parseTime=true'];
        $redis = ['GOJET_REDIS_ADDR' => '127.0.0.1:'.$p['redis']['port'], 'GOJET_REDIS_DB' => (string)$p['redis']['db'], 'GOJET_REDIS_PASSWORD' => $p['redis']['password']];
        $log = ['GOJET_LOG_SOCKET' => '/run/gojet-log/collector.sock'];
        $storage = ['GOJET_FILE_STORAGE_ROOT' => $files['root'], 'GOJET_CLAMAV_NETWORK' => 'unix', 'GOJET_CLAMAV_ADDRESS' => $files['clamav_socket']];
        $riskEnv = ['GOJET_RISK_PROVIDER_NAME' => $risk['name'], 'GOJET_RISK_PROVIDER_ENDPOINT' => $risk['endpoint'], 'GOJET_RISK_POLICY_VERSION' => $risk['policy_version']];
        $api = $database + $redis + $log + $storage + $riskEnv + [
            'GOJET_PLATFORMAPI_ADDR' => '127.0.0.1:8081', 'GOJET_AUTH_ALLOWED_ORIGIN' => $origin, 'GOJET_ADMIN_ALLOWED_ORIGIN' => $origin,
            'GOJET_AUTH_RATE_LIMIT' => (string)$p['auth_rate']['limit'], 'GOJET_AUTH_RATE_WINDOW_SECONDS' => (string)$p['auth_rate']['window_seconds'],
            'GOJET_AUTH_TRUSTED_PROXY_CIDRS' => '127.0.0.1/32,::1/128',
            'GOJET_AUTH_CSRF_KEY_HEX' => $p['secrets']['csrf'], 'GOJET_AUTH_GRANT_KEY_ID' => $keyID, 'GOJET_AUTH_GRANT_KEY_HEX' => $p['secrets']['grant'],
            'GOJET_OAUTH_KEY_ID' => $keyID, 'GOJET_OAUTH_KEY_HEX' => $p['secrets']['oauth'],
            'GOJET_ADMIN_TOTP_KEY_ID' => $keyID, 'GOJET_ADMIN_TOTP_KEY_HEX' => $p['secrets']['admin'],
            'GOJET_FILE_PUBLIC_AUTH_SECRET' => $p['secrets']['file_public'], 'GOJET_TEXT_PUBLIC_AUTH_SECRET' => $p['secrets']['text_public'],
            'GOJET_FILE_MAX_UPLOAD_BYTES' => (string)$files['max_upload_bytes'], 'GOJET_FILE_WORKSPACE_MAX_FILES' => (string)$files['max_files'],
            'GOJET_FILE_WORKSPACE_MAX_BYTES' => (string)$files['max_bytes'], 'GOJET_FILE_TYPE_ALLOWLIST' => implode(';', $types),
            'GOJET_TEXT_WORKSPACE_QUOTA' => (string)$p['quotas']['text'], 'GOJET_BIO_WORKSPACE_QUOTA' => (string)$p['quotas']['bio'],
            'GOJET_QR_WORKSPACE_QUOTA' => (string)$p['quotas']['qr'], 'GOJET_TURNSTILE_SECRET' => $turnstile];
        foreach (['AUTH', 'ACCOUNT', 'ADMIN_ACCESS', 'ADMIN_OAUTH', 'WORKSPACE', 'BILLING', 'FILES', 'TEXT', 'BIO', 'ANALYTICS', 'SUPPORT', 'TRUST', 'TRUST_ADMIN'] as $feature) { $api['GOJET_'.$feature.'_ENABLED'] = '1'; }
        // Payment providers remain opt-in; OAuth/One Tap retain their database-owned Admin switches.
        foreach (['STRIPE', 'PAYPAL', 'ALIPAY', 'WECHAT', 'CRYPTO', 'EPAY'] as $provider) { $api['GOJET_BILLING_'.$provider.'_ENABLED'] = '0'; }
        $api['GOJET_ADMIN_RESTART_ENABLED'] = '0';
        $services = [
            'platformapi' => $api,
            'redirectengine' => $database + $redis + $log + ['GOJET_REDIRECTENGINE_ADDR' => '127.0.0.1:8080', 'GOJET_REDIRECT_TRUST_LOOPBACK_PROXY' => '1', 'GOJET_ANALYTICS_ENABLED' => '1'],
            'analyticsworker' => $database + $redis + $log,
            'analyticsreconciler' => $database + $redis + $log + ['GOJET_ANALYTICS_RECONCILE_REPAIR' => '0', 'GOJET_ANALYTICS_RECONCILER_ONCE' => '0'],
            'mailworker' => $database + $log + ['GOJET_SMTP_ADDR' => $smtp['host'].':'.$smtp['port'], 'GOJET_SMTP_FROM' => $smtp['from'], 'GOJET_SMTP_USERNAME' => $smtp['username'], 'GOJET_SMTP_PASSWORD' => $smtp['password']],
            'fileworker' => $database + $log + $storage,
            'operationsmonitor' => $database + $redis + $log + $riskEnv + ['GOJET_WEBHOOK_DELIVERY_ENABLED' => '0'],
            'logreceiver' => $log];
        ServiceEnvironment::render($services); // Enforce total bounds and safe systemd serialization before returning secrets.
        return ['services' => $services, 'origin' => $origin, 'max_body_bytes' => $files['max_upload_bytes'] + 1048576];
    }
}
