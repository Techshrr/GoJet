<?php
declare(strict_types=1);
namespace GoJet\Installer;
require_once __DIR__.'/WebConfigurationSwitch.php';
require_once __DIR__.'/SiteActivation.php';

/** First endpoint only. Existing configuration, web state and approval are never reset. */
final class WebEntryProvisioning
{
    private const AUTHORITY = '/var/lib/gojet-installer-authority';
    private const POOL = '/etc/php/8.3/fpm/pool.d/gojet-installer.conf';
    private const SITE = '/etc/nginx/conf.d/gojet-installer.conf';

    private static function absent(string $path): void
    {
        clearstatcache(true, $path);
        if (file_exists($path) || is_link($path)) { throw new \RuntimeException('Entry boundary occupied'); }
    }
    private static function path(string $path): void
    {
        if (strlen($path) > 512 || !preg_match('~^/(?:[A-Za-z0-9_.-]+/)*[A-Za-z0-9_.-]+$~D', $path)
            || in_array('.', explode('/', $path), true) || in_array('..', explode('/', $path), true)) {
            throw new \RuntimeException('Unsafe entry path');
        }
    }
    private static function read(string $path, int $mode, int $limit = 262144): string
    {
        clearstatcache(true, $path); $s = @lstat($path);
        if ($s === false || realpath($path) !== $path || ($s['mode'] & 0177777) !== (0100000 | $mode)
            || $s['uid'] !== 0 || $s['nlink'] !== 1 || $s['size'] > $limit) { throw new \RuntimeException('Unsafe entry file'); }
        $raw = file_get_contents($path);
        if ($raw === false || strlen($raw) > $limit) { throw new \RuntimeException('Entry read failed'); }
        return $raw;
    }
    private static function sync(string $path): void
    {
        $f = fopen($path, 'r');
        if ($f === false) { throw new \RuntimeException('Entry sync unavailable'); }
        try { if (!fsync($f)) { throw new \RuntimeException('Entry sync failed'); } } finally { fclose($f); }
    }
    private static function publish(string $path, string $raw, int $mode): void
    {
        $temporary = dirname($path).'/.gojet-entry-'.bin2hex(random_bytes(16)).'.tmp';
        $mask = umask(0077);
        try { $f = fopen($temporary, 'x+b'); } finally { umask($mask); }
        if ($f === false) { throw new \RuntimeException('Entry staging failed'); }
        try {
            if (fwrite($f, $raw) !== strlen($raw) || !fflush($f) || !chmod($temporary, $mode) || !fsync($f)
                || !link($temporary, $path) || !unlink($temporary)) { throw new \RuntimeException('Entry publication refused'); }
            self::sync(dirname($path));
        } finally { fclose($f); if (file_exists($temporary)) { unlink($temporary); } }
    }
    private static function command(array $argv): string
    {
        $p = proc_open($argv, [0 => ['file', '/dev/null', 'r'], 1 => ['pipe', 'w'], 2 => ['pipe', 'w']], $pipes,
            '/', ['PATH' => '/usr/sbin:/usr/bin:/sbin:/bin', 'LC_ALL' => 'C']);
        if (!is_resource($p)) { throw new \RuntimeException('Entry command unavailable'); }
        $out = ''; $err = ''; $deadline = microtime(true)+30;
        stream_set_blocking($pipes[1], false); stream_set_blocking($pipes[2], false);
        try {
            do {
                $out .= stream_get_contents($pipes[1], 1048576); $err .= stream_get_contents($pipes[2], 65536);
                $status = proc_get_status($p);
                if (strlen($out) > 1048576 || strlen($err) > 65536 || microtime(true) > $deadline) { throw new \RuntimeException('Entry command exceeded bounds'); }
                if ($status['running']) { usleep(20000); }
            } while ($status['running']);
            $out .= stream_get_contents($pipes[1], 1048576); $err .= stream_get_contents($pipes[2], 65536);
            if ($status['exitcode'] !== 0 || strlen($out) > 1048576 || strlen($err) > 65536
                || str_contains($err, '[warn]') || str_contains($err, 'WARNING:')) { throw new \RuntimeException('Entry command refused'); }
            return $out;
        } finally {
            if (proc_get_status($p)['running']) { proc_terminate($p, 9); }
            fclose($pipes[1]); fclose($pipes[2]); proc_close($p);
        }
    }
    private static function nginx(): string
    {
        $properties = [];
        foreach (explode("\n", self::command(['/usr/bin/systemctl', 'show', 'nginx.service',
            '--property=LoadState,ActiveState,DropInPaths,MainPID,ExecStart'])) as $line) {
            if (str_contains($line, '=')) { [$key, $value] = explode('=', $line, 2); $properties[$key] = $value; }
        }
        if (($properties['LoadState'] ?? '') !== 'loaded' || ($properties['ActiveState'] ?? '') !== 'active'
            || ($properties['DropInPaths'] ?? '') !== '' || !ctype_digit($properties['MainPID'] ?? '')
            || (int)$properties['MainPID'] <= 1
            || !str_contains($properties['ExecStart'] ?? '', 'argv[]=/usr/sbin/nginx -g daemon on; master_process on; ;')) {
            throw new \RuntimeException('Unsupported Nginx service');
        }
        if (realpath('/proc/'.(int)$properties['MainPID'].'/exe') !== realpath('/usr/sbin/nginx')) {
            throw new \RuntimeException('Unexpected Nginx executable');
        }
        $config = self::command(['/usr/sbin/nginx', '-T']);
        if (!preg_match('~\binclude\s+/etc/nginx/conf\.d/\*\.conf\s*;~', preg_replace('/#[^\n]*/', '', $config))) {
            throw new \RuntimeException('Unsupported Nginx include authority');
        }
        return $config;
    }
    private static function health(array $web): void
    {
        $url = parse_url($web['origin']); $port = $url['port'] ?? 443;
        $host = $url['host'].(isset($url['port']) ? ':'.$port : '');
        $context = stream_context_create([
            'ssl' => ['verify_peer' => true, 'verify_peer_name' => true, 'allow_self_signed' => false,
                'peer_name' => $url['host'], 'SNI_enabled' => true, 'cafile' => $web['certificate']],
            'http' => ['method' => 'GET', 'header' => 'Host: '.$host."\r\nAccept: application/json\r\nConnection: close\r\n",
                'timeout' => 2, 'ignore_errors' => true, 'follow_location' => 0, 'max_redirects' => 0],
        ]);
        // Reload is asynchronous. Observe only; never submit credentials or mutate web state.
        for ($attempt = 0; $attempt < 20; $attempt++) {
            $http_response_header = [];
            $body = @file_get_contents('https://127.0.0.1:'.$port.'/install/', false, $context, 0, 4097);
            if (is_string($body) && strlen($body) <= 4096
                && preg_match('~^HTTP/\S+ 401(?: |$)~', $http_response_header[0] ?? '')
                && json_decode($body, true) === ['error' => 'Installer authentication required']) { return; }
            usleep(100000);
        }
        throw new \RuntimeException('Entry HTTPS health failed');
    }
    private static function barrier(): void
    {
        foreach (['installed.lock', 'web-apply.json', 'configuration-approval.json', 'web-apply-started.json'] as $name) {
            self::absent(self::AUTHORITY.'/'.$name);
        }
    }
    private static function exists(string $path): bool
    {
        clearstatcache(true, $path); return file_exists($path) || is_link($path);
    }
    /** Resume only an unpublished prefix. Live/missing state and changed files are never rebuilt. */
    private static function recoverPublication(string $directory, array $input, array $private, array $rendered, array $account, string $baseline): void
    {
        // A published vhost requires the full original publication, verified by run() below.
        if (self::exists(self::SITE)) { return; }
        self::absent(self::SITE.'.handover'); self::absent(self::POOL.'.retired');
        $present = self::exists($directory); $mode = null;
        if ($present) {
            $stat = lstat($directory); $mode = $stat['mode'] & 07777;
            if (realpath($directory) !== $directory || !is_dir($directory) || $stat['uid'] !== 0
                || !in_array($mode, [0700, 0755], true)) { throw new \RuntimeException('Unsafe partial entry'); }
            $names = array_diff(scandir($directory), ['.', '..']);
            if (array_diff($names, ['binding.json', 'web.json', 'state', 'fpm.sock'])) {
                throw new \RuntimeException('Entry has advanced or contains unknown files');
            }
        }
        $missing = [];
        foreach ($private as $name => $raw) {
            $path = $directory.'/'.$name;
            if (self::exists($path)) {
                if (self::read($path, 0600) !== $raw) { throw new \RuntimeException('Changed partial entry input'); }
            } else { $missing[$path] = $raw; }
        }
        if (self::exists($directory.'/web.json') && !self::exists($directory.'/binding.json')) {
            throw new \RuntimeException('Ambiguous entry publication order');
        }
        $state = $directory.'/state'; $stateExists = self::exists($state);
        $initial = ['schema' => 2, 'step' => 0, 'status' => 'ready', 'session' => null,
            'expires' => 0, 'csrf' => null, 'attempts' => 0, 'retry_at' => 0];
        if ($stateExists) {
            $stat = lstat($state);
            if (realpath($state) !== $state || ($stat['mode'] & 0177777) !== 0040700
                || !in_array($stat['uid'], [0, $account['uid']], true)
                || !in_array($stat['gid'], [0, $account['gid']], true) || $missing !== []
                || array_diff(scandir($state), ['.', '..', 'state.json', 'state.lock'])) {
                throw new \RuntimeException('Unsafe partial state');
            }
            foreach (['state.json', 'state.lock'] as $name) {
                $path = $state.'/'.$name;
                if (!self::exists($path)) {
                    if ($mode !== 0700 || $stat['uid'] !== 0) { throw new \RuntimeException('Missing published state'); }
                    continue;
                }
                $file = lstat($path);
                if (realpath($path) !== $path || ($file['mode'] & 0177777) !== 0100600 || $file['nlink'] !== 1
                    || !in_array($file['uid'], [0, $account['uid']], true)
                    || !in_array($file['gid'], [0, $account['gid']], true) || $file['size'] > 4096
                    || ($mode === 0755 && ($file['uid'] !== $account['uid'] || $file['gid'] !== $account['gid']))) {
                    throw new \RuntimeException('Changed partial state ownership');
                }
                if ($name === 'state.json' && ApplyRequest::read($path, $file['uid']) !== $initial) {
                    throw new \RuntimeException('Partial entry state already used');
                }
                if ($name === 'state.lock' && $file['size'] !== 0) { throw new \RuntimeException('Changed state lock'); }
            }
            if ($mode === 0755 && ($stat['uid'] !== $account['uid'] || $stat['gid'] !== $account['gid'])) {
                throw new \RuntimeException('Changed published state ownership');
            }
            if (!self::exists($state.'/state.json') && self::exists($state.'/state.lock') && fileowner($state.'/state.lock') !== 0) {
                throw new \RuntimeException('Ambiguous partial initialization');
            }
        }
        if ($mode === 0755 && (!$stateExists || $missing !== [])) { throw new \RuntimeException('Missing live entry input'); }
        $poolExists = self::exists(self::POOL);
        if ($poolExists && ($mode !== 0755 || self::read(self::POOL, 0600) !== $rendered['fpm'])) {
            throw new \RuntimeException('Changed or out-of-order pool');
        }
        if (self::exists($directory.'/fpm.sock') && !$poolExists) { throw new \RuntimeException('Ambiguous entry socket'); }
        $ports = SiteActivation::listenPorts($rendered['nginx']);
        if ($ports === [] || array_intersect($ports, SiteActivation::listenPorts($baseline))) { throw new \RuntimeException('Existing entry listener'); }
        $probe = @stream_socket_server('tcp://0.0.0.0:'.$ports[0], $errno, $error);
        if ($probe === false) { throw new \RuntimeException('Occupied recovery port'); } fclose($probe);
        // All existing nodes are checked before the first mutation. No existing file is replaced.
        if (!$present) {
            if (!mkdir($directory, 0700)) { throw new \RuntimeException('Entry recovery mkdir failed'); }
            self::sync(dirname($directory));
        }
        foreach ($missing as $path => $raw) { self::publish($path, $raw, 0600); }
        if (!$stateExists && !mkdir($state, 0700)) { throw new \RuntimeException('State recovery mkdir failed'); }
        if ($mode !== 0755) {
            if (!self::exists($state.'/state.json')) {
                (new InstallState($state, $input['bootstrap_sha256']))->initialize();
            } elseif (!self::exists($state.'/state.lock')) { self::publish($state.'/state.lock', '', 0600); }
            foreach ([$state.'/state.json', $state.'/state.lock', $state] as $path) {
                if (!chown($path, $account['uid']) || !chgrp($path, $account['gid'])) { throw new \RuntimeException('State recovery ownership failed'); }
                self::sync($path);
            }
            if (!chmod($directory, 0755)) { throw new \RuntimeException('Entry recovery traversal failed'); }
            self::sync($directory); self::sync(dirname($directory));
        }
        if (!$poolExists) { self::publish(self::POOL, $rendered['fpm'], 0600); }
        self::publish(self::SITE, $rendered['nginx'], 0644);
        clearstatcache(true, $directory);
    }
    public static function run(string $directory, array $input, bool $recover = false): array
    {
        if (!function_exists('posix_geteuid') || posix_geteuid() !== 0) { throw new \RuntimeException('Root required'); }
        $keys = ['schema', 'origin', 'release', 'manifest_sha256', 'source', 'bootstrap_sha256', 'tls', 'files'];
        if (array_diff($keys, array_keys($input)) || array_diff(array_keys($input), $keys)
            || $input['schema'] !== 'gojet.web-entry.v1' || !is_array($input['tls']) || !is_array($input['files'])
            || array_diff(['certificate', 'private_key', 'http_port'], array_keys($input['tls']))
            || array_diff(array_keys($input['tls']), ['certificate', 'private_key', 'http_port'])
            || array_diff(['root', 'clamav_socket'], array_keys($input['files']))
            || array_diff(array_keys($input['files']), ['root', 'clamav_socket'])) { throw new \RuntimeException('Invalid entry request'); }
        self::path($directory);
        if (strlen($directory.'/fpm.sock') > 100) { throw new \RuntimeException('Entry socket path too long'); }
        foreach ([$input['release'], $input['tls']['certificate'], $input['tls']['private_key'], ...array_values($input['files'])] as $path) {
            if (!is_string($path)) { throw new \RuntimeException('Invalid entry path'); } self::path($path);
            if ($path === $directory || str_starts_with($path, $directory.'/') || str_starts_with($directory, $path.'/')) {
                throw new \RuntimeException('Entry overlaps protected input');
            }
        }
        foreach ([self::AUTHORITY, dirname(self::POOL), dirname(self::SITE), dirname($directory)] as $parent) {
            CompletionLock::verifyProvisioningPath($parent.'/installed.lock');
        }
        foreach ([self::AUTHORITY, '/etc/php', '/etc/nginx'] as $protected) {
            if ($directory === $protected || str_starts_with($directory, $protected.'/') || str_starts_with($protected, $directory.'/')) {
                throw new \RuntimeException('Entry overlaps host authority');
            }
        }
        PackageIntegrity::verify($input['release'], $input['manifest_sha256'], $input['source']);
        $account = posix_getpwnam('gojet-installer');
        if ($account === false || $account['uid'] <= 0 || $account['gid'] <= 0) { throw new \RuntimeException('Installer account required'); }
        $web = ['origin' => $input['origin'], 'release' => $input['release'], 'state' => $directory.'/state',
            'bootstrap_sha256' => $input['bootstrap_sha256'], 'certificate' => $input['tls']['certificate'],
            'private_key' => $input['tls']['private_key'], 'socket' => $directory.'/fpm.sock', 'completion' => self::AUTHORITY.'/installed.lock'];
        $rendered = NativeWebConfig::render($web);
        // Validate the same TLS/path/HTTP-port constraints required by later product preparation.
        ProductWebConfig::render(['origin' => $input['origin'], 'release' => $input['release'], 'max_body_bytes' => 1048576] + $input['tls']);
        $cert = self::read($web['certificate'], 0644); $key = self::read($web['private_key'], 0600);
        $parsed = openssl_x509_parse($cert);
        if ($web['certificate'] === $web['private_key'] || !openssl_x509_check_private_key($cert, $key) || $parsed === false
            || ($parsed['validFrom_time_t'] ?? PHP_INT_MAX) > time() || ($parsed['validTo_time_t'] ?? 0) <= time()) {
            throw new \RuntimeException('Invalid entry TLS');
        }
        $binding = ['schema' => 'gojet.web-configuration-binding.v1', 'destination' => $directory.'/prepared',
            'release' => $input['release'], 'manifest_sha256' => $input['manifest_sha256'], 'source' => $input['source'],
            'tls' => $input['tls'], 'files' => $input['files']];
        $private = ['binding.json' => json_encode($binding, JSON_THROW_ON_ERROR)."\n",
            'web.json' => json_encode($web, JSON_THROW_ON_ERROR)."\n"];
        foreach ($private as $raw) { if (strlen($raw) > 4096) { throw new \RuntimeException('Entry binding exceeds bound'); } }
        $identity = ['schema' => 1, 'directory_sha256' => hash('sha256', $directory),
            'private' => array_map(fn($raw) => hash('sha256', $raw), $private),
            'pool_sha256' => hash('sha256', $rendered['fpm']), 'site_sha256' => hash('sha256', $rendered['nginx']),
            'certificate_sha256' => hash('sha256', $cert), 'key_sha256' => hash('sha256', $key)];
        self::barrier();
        $locks = [];
        try {
            // Match the product activation authority as well as the replacement pool authority.
            foreach (['service-activation.lock', 'fpm-switch.lock'] as $name) {
                $lockPath = self::AUTHORITY.'/'.$name;
                $mask = umask(0077);
                try { $created = @fopen($lockPath, 'x+b'); } finally { umask($mask); }
                if ($created !== false) { fclose($created); self::sync(self::AUTHORITY); }
                self::read($lockPath, 0600, 0); $lock = fopen($lockPath, 'r+b');
                if ($lock === false) { throw new \RuntimeException('Entry lock unavailable'); }
                $locks[] = $lock;
                if (!flock($lock, LOCK_EX | LOCK_NB)) { throw new \RuntimeException('Entry busy'); }
            }
            self::barrier(); WebConfigurationSwitch::verifyHost();
            $baseline = self::nginx();
            $started = self::AUTHORITY.'/web-entry-started.json'; $ready = self::AUTHORITY.'/web-entry-ready.json';
            $exists = file_exists($started) || is_link($started);
            if ($recover && !$exists) { throw new \RuntimeException('Original entry receipt required'); }
            if ($exists) {
                if (ApplyRequest::read($started, 0) !== $identity) { throw new \RuntimeException('Entry identity changed'); }
                if ($recover && !self::exists($ready)) {
                    self::recoverPublication($directory, $input, $private, $rendered, $account, $baseline);
                }
                CompletionLock::verifyProvisioningPath($directory.'/installed.lock');
                if ((fileperms($directory) & 07777) !== 0755) { throw new \RuntimeException('Incomplete entry publication'); }
                foreach ($private as $name => $raw) { if (self::read($directory.'/'.$name, 0600) !== $raw) { throw new \RuntimeException('Changed entry input'); } }
                foreach ([self::POOL => [$rendered['fpm'], 0600], self::SITE => [$rendered['nginx'], 0644]] as $path => [$raw, $mode]) {
                    if (self::read($path, $mode) !== $raw) { throw new \RuntimeException('Incomplete or changed entry configuration'); }
                }
                self::absent($directory.'/binding.json.retired.json');
                if (file_exists($ready) || is_link($ready)) {
                    if (ApplyRequest::read($ready, 0) !== $identity) { throw new \RuntimeException('Entry receipt changed'); }
                    return ['entry_ready' => true, 'already_applied' => true, 'installation_complete' => false];
                }
                // Explicit same-request recovery only after every immutable publication is complete.
                $state = $directory.'/state'; clearstatcache(true, $state); $s = @lstat($state);
                if ($s === false || realpath($state) !== $state || ($s['mode'] & 0177777) !== 0040700 || $s['uid'] !== $account['uid']) {
                    throw new \RuntimeException('Invalid published state');
                }
                $value = ApplyRequest::read($state.'/state.json', $account['uid']);
                if (($value['schema'] ?? null) !== 2 || ($value['step'] ?? null) !== 0) { throw new \RuntimeException('Entry already advanced'); }
            } else {
                foreach ([$directory, $ready, self::POOL, self::SITE] as $path) { self::absent($path); }
                $ports = SiteActivation::listenPorts($rendered['nginx']);
                if ($ports === [] || array_intersect($ports, SiteActivation::listenPorts($baseline))) { throw new \RuntimeException('Existing entry listener'); }
                $probe = @stream_socket_server('tcp://0.0.0.0:'.$ports[0], $errno, $error);
                if ($probe === false) { throw new \RuntimeException('Occupied entry port'); } fclose($probe);
                self::command(['/usr/sbin/php-fpm8.3', '-t']);
                ApplyRequest::write($started, $identity);
                if (!mkdir($directory, 0700)) { throw new \RuntimeException('Entry directory publication failed'); }
                self::sync(dirname($directory));
                foreach ($private as $name => $raw) { self::publish($directory.'/'.$name, $raw, 0600); }
                $state = $directory.'/state';
                if (!mkdir($state, 0700)) { throw new \RuntimeException('Entry state creation failed'); }
                (new InstallState($state, $input['bootstrap_sha256']))->initialize();
                foreach ([$state.'/state.json', $state.'/state.lock', $state] as $path) {
                    if (!chown($path, $account['uid']) || !chgrp($path, $account['gid'])) { throw new \RuntimeException('Entry ownership failed'); }
                    self::sync($path);
                }
                if (!chmod($directory, 0755)) { throw new \RuntimeException('Entry traversal activation failed'); }
                self::sync($directory); self::sync(dirname($directory));
                self::publish(self::POOL, $rendered['fpm'], 0600);
                self::publish(self::SITE, $rendered['nginx'], 0644);
            }
            self::barrier(); WebConfigurationSwitch::verifyHost();
            self::command(['/usr/sbin/php-fpm8.3', '-t']); self::nginx();
            self::command(['/usr/bin/systemctl', 'reload', 'php8.3-fpm.service']);
            self::command(['/usr/bin/systemctl', 'reload', 'nginx.service']);
            self::health($web); self::barrier();
            ApplyRequest::write($ready, $identity);
            return ['entry_ready' => true, 'already_applied' => false, 'installation_complete' => false];
        } finally { foreach (array_reverse($locks) as $held) { flock($held, LOCK_UN); fclose($held); } }
    }
}
