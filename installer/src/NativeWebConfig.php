<?php

declare(strict_types=1);
namespace GoJet\Installer;
require_once __DIR__.'/CompletionLock.php';

/** Render only; root-owned provisioning/apply must validate ownership before installing these files. */
final class NativeWebConfig
{
    public static function render(array $input): array
    {
        $keys = ['origin', 'release', 'state', 'bootstrap_sha256', 'certificate', 'private_key', 'socket', 'completion'];
        if (array_diff($keys, array_keys($input)) || array_diff(array_keys($input), $keys)) {
            throw new \InvalidArgumentException('Invalid native web configuration');
        }
        foreach ($input as $value) {
            if (!is_string($value) || preg_match('/[\x00-\x20\x7f"\'\\\\;$]/', $value)) {
                throw new \InvalidArgumentException('Unsafe native web configuration');
            }
        }
        $url = parse_url($input['origin']);
        if (!is_array($url) || ($url['scheme'] ?? '') !== 'https' || empty($url['host'])
            || preg_match('/^[a-zA-Z0-9.-]+$/D', $url['host']) !== 1
            || isset($url['path']) || isset($url['user']) || isset($url['pass']) || isset($url['query']) || isset($url['fragment'])) {
            throw new \InvalidArgumentException('Invalid native HTTPS origin');
        }
        $port = $url['port'] ?? 443;
        if ($port < 1 || $port > 65535) { throw new \InvalidArgumentException('Invalid HTTPS port'); }
        foreach (['release', 'state', 'certificate', 'private_key', 'socket', 'completion'] as $key) {
            $path = $input[$key];
            if (preg_match('~^/(?:[A-Za-z0-9_.-]+/)*[A-Za-z0-9_.-]+$~D', $path) !== 1
                || in_array('..', explode('/', $path), true) || in_array('.', explode('/', $path), true)) {
                throw new \InvalidArgumentException('Unsafe native path');
            }
        }
        if (preg_match('/^[a-f0-9]{64}$/D', $input['bootstrap_sha256']) !== 1) {
            throw new \InvalidArgumentException('Invalid bootstrap digest');
        }
        if ($input['state'] === $input['release'] || str_starts_with($input['state'], $input['release'].'/')) {
            throw new \InvalidArgumentException('Private state must be outside release');
        }
        if (basename($input['completion']) !== 'installed.lock'
            || str_starts_with($input['completion'], $input['state'].'/')
            || str_starts_with($input['completion'], $input['release'].'/')) {
            throw new \InvalidArgumentException('Completion authority must be outside writable state and release');
        }
        CompletionLock::verifyProvisioningPath($input['completion']);
        $completion = $input['completion']; $completionDirectory = dirname($completion);
        $host = $url['host']; $origin = $input['origin']; $release = $input['release'];
        $state = $input['state']; $hash = $input['bootstrap_sha256'];
        $certificate = $input['certificate']; $privateKey = $input['private_key']; $socket = $input['socket'];
        // Dedicated bootstrap virtual host, not the final composed product site configuration.
        $nginx = <<<NGINX
server {
    listen {$port} ssl;
    server_name {$host};
    ssl_certificate {$certificate};
    ssl_certificate_key {$privateKey};
    ssl_protocols TLSv1.2 TLSv1.3;
    client_max_body_size 4k;
    access_log off;
    add_header X-Robots-Tag "noindex, nofollow" always;
    add_header Cache-Control "no-store" always;
    location ~ ^/install(?:/|/(?:environment|data|site|admin|services|health|complete))?$ {
        fastcgi_pass unix:{$socket};
        fastcgi_pass_request_headers off;
        fastcgi_param SCRIPT_FILENAME {$release}/installer/public/index.php;
        fastcgi_param SCRIPT_NAME /install/index.php;
        fastcgi_param REQUEST_METHOD \$request_method;
        fastcgi_param REQUEST_URI \$request_uri;
        fastcgi_param CONTENT_TYPE \$content_type;
        fastcgi_param CONTENT_LENGTH \$content_length;
        fastcgi_param SERVER_PROTOCOL \$server_protocol;
        fastcgi_param GATEWAY_INTERFACE CGI/1.1;
        fastcgi_param SERVER_NAME \$server_name;
        fastcgi_param SERVER_PORT \$server_port;
        fastcgi_param REMOTE_ADDR \$remote_addr;
        fastcgi_param HTTPS \$https;
        fastcgi_param HTTP_HOST \$http_host;
        fastcgi_param HTTP_ORIGIN \$http_origin;
        fastcgi_param HTTP_COOKIE \$http_cookie;
        fastcgi_connect_timeout 5s;
        fastcgi_read_timeout 30s;
    }
    location / { return 404; }
}
NGINX;
        $fpm = <<<FPM
[gojet-installer]
user = gojet-installer
group = gojet-installer
listen = {$socket}
listen.owner = gojet-installer
listen.group = www-data
listen.mode = 0660
pm = ondemand
pm.max_children = 2
pm.process_idle_timeout = 10s
pm.max_requests = 100
clear_env = yes
security.limit_extensions = .php
request_terminate_timeout = 30s
catch_workers_output = yes
php_admin_flag[display_errors] = off
php_admin_flag[log_errors] = on
php_admin_value[expose_php] = Off
php_admin_value[open_basedir] = {$release}:{$state}:{$completionDirectory}
php_admin_value[upload_max_filesize] = 4K
php_admin_value[post_max_size] = 8K
env[GOJET_INSTALL_STATE_DIR] = {$state}
env[GOJET_INSTALL_BOOTSTRAP_SHA256] = {$hash}
env[GOJET_INSTALL_ORIGIN] = {$origin}
env[GOJET_INSTALL_COMPLETION_FILE] = {$completion}
FPM;
        return ['nginx' => $nginx."\n", 'fpm' => $fpm."\n"];
    }
}
