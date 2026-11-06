<?php

declare(strict_types=1);
namespace GoJet\Installer;
require_once __DIR__.'/CompletionLock.php';

/** Root-only rendering of a product TLS vhost; never installs/reloads or completes installation. */
final class ProductWebConfig
{
    private static function path(string $path): void
    {
        if (!preg_match('~^/(?:[A-Za-z0-9_.-]+/)*[A-Za-z0-9_.-]+$~D', $path)
            || in_array('.', explode('/', $path), true) || in_array('..', explode('/', $path), true)) {
            throw new \InvalidArgumentException('Unsafe path');
        }
    }

    private static function file(string $path, bool $private = false): void
    {
        self::path($path);
        CompletionLock::verifyProvisioningPath(dirname($path).'/installed.lock');
        clearstatcache(true, $path);
        $stat = lstat($path);
        if ($stat === false || ($stat['mode'] & 0170000) !== 0100000 || $stat['uid'] !== 0
            || $stat['nlink'] !== 1 || ($stat['mode'] & ($private ? 0077 : 0022)) !== 0) {
            throw new \RuntimeException('Unsafe configuration file');
        }
    }

    public static function render(array $input): string
    {
        if (!function_exists('posix_geteuid') || posix_geteuid() !== 0) { throw new \RuntimeException('Root required'); }
        $keys = ['origin', 'release', 'certificate', 'private_key', 'max_body_bytes'];
        if (array_diff($keys, array_keys($input)) || array_diff(array_keys($input), [...$keys, 'http_port'])) {
            throw new \InvalidArgumentException('Invalid product configuration');
        }
        foreach (['origin', 'release', 'certificate', 'private_key'] as $key) {
            if (!is_string($input[$key])) { throw new \InvalidArgumentException('Invalid string'); }
        }
        $url = parse_url($input['origin']);
        $host = $url['host'] ?? '';
        if (!is_array($url) || ($url['scheme'] ?? '') !== 'https' || strlen($host) > 253
            || !preg_match('/^(?:[a-z0-9](?:[a-z0-9-]{0,61}[a-z0-9])?)(?:\.[a-z0-9](?:[a-z0-9-]{0,61}[a-z0-9])?)*$/D', $host)
            || filter_var($host, FILTER_VALIDATE_IP) !== false
            || isset($url['user']) || isset($url['pass']) || isset($url['path']) || isset($url['query']) || isset($url['fragment'])) {
            throw new \InvalidArgumentException('Canonical HTTPS DNS origin required');
        }
        $port = $url['port'] ?? 443;
        if ($port < 1 || $port > 65535) { throw new \InvalidArgumentException('Invalid port'); }
        $httpPort = $input['http_port'] ?? null;
        if (array_key_exists('http_port', $input) && (!is_int($httpPort) || $httpPort < 1
            || $httpPort > 65535 || $httpPort === $port)) {
            throw new \InvalidArgumentException('Invalid HTTP port');
        }
        $limit = $input['max_body_bytes'];
        if (!is_int($limit) || $limit < 1 || $limit > 1073741824) { throw new \InvalidArgumentException('Invalid body limit'); }
        $release = $input['release']; self::path($release);
        CompletionLock::verifyProvisioningPath($release.'/installed.lock');
        $cert = $input['certificate']; $key = $input['private_key'];
        self::file($cert); self::file($key, true);
        $fragments = [];
        foreach (['platform-proxy.conf', 'static-apps.conf', 'redirect-proxy.conf'] as $name) {
            $path = $release.'/deploy/nginx/'.$name;
            self::file($path);
            if (filesize($path) > 262144) { throw new \RuntimeException('Configuration exceeds limit'); }
            $value = file_get_contents($path);
            if ($value === false) { throw new \RuntimeException('Configuration unreadable'); }
            $fragments[] = str_replace('/opt/gojet/current/', $release.'/', $value);
        }
        // PackageIntegrity must first bind these trusted root files to approved release evidence.
        $routes = implode("\n", $fragments);
        $bareHost = $port === 443 ? 'if ($http_host = "'.$host.'") { set $gojet_authority_valid 1; }' : '';
        $http = '';
        if ($httpPort !== null) {
            // Never construct a redirect authority from request headers.
            $target = 'https://'.$host.($port === 443 ? '' : ':'.$port);
            $httpBareHost = $httpPort === 80 ? 'if ($http_host = "'.$host.'") { set $gojet_http_authority_valid 1; }' : '';
            $http = <<<NGINX
server {
    listen {$httpPort} default_server;
    server_name _;
    access_log off;
    return 421;
}
server {
    listen {$httpPort};
    server_name {$host};
    access_log off;
    set \$gojet_http_authority_valid 0;
    if (\$http_host = "{$host}:{$httpPort}") { set \$gojet_http_authority_valid 1; }
    {$httpBareHost}
    if (\$gojet_http_authority_valid = 0) { return 421; }
    add_header Cache-Control "no-store" always;
    return 308 {$target}\$request_uri;
}

NGINX;
        }
        return $http.<<<NGINX
server {
    listen {$port} ssl default_server;
    server_name _;
    ssl_protocols TLSv1.2 TLSv1.3;
    ssl_reject_handshake on;
    return 421;
}
server {
    listen {$port} ssl;
    server_name {$host};
    ssl_certificate {$cert};
    ssl_certificate_key {$key};
    ssl_protocols TLSv1.2 TLSv1.3;
    set \$gojet_authority_valid 0;
    if (\$http_host = "{$host}:{$port}") { set \$gojet_authority_valid 1; }
    {$bareHost}
    if (\$gojet_authority_valid = 0) { return 421; }
    if (\$ssl_server_name != "{$host}") { return 421; }
    client_max_body_size {$limit};
    access_log off;
    location = /.well-known/gojet-installation-result {
        if (\$request_method !~ ^(GET|HEAD)$) { return 405; }
        alias /var/lib/gojet-installation-result/result.json;
        default_type application/json;
        add_header Cache-Control "no-store" always;
        add_header X-Content-Type-Options "nosniff" always;
        add_header X-Robots-Tag "noindex, nofollow" always;
        disable_symlinks on;
        open_file_cache off;
        etag off;
        if_modified_since off;
    }
    location = /install { return 404; }
    location ^~ /install/ { return 404; }
{$routes}
}

NGINX;
    }
}
