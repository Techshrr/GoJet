<?php

declare(strict_types=1);
namespace GoJet\Installer;
require_once __DIR__.'/SiteActivation.php';

/** Fixed root completion authority, never a client-supplied success flag. */
final class InstallationCompletion
{
    private const AUTHORITY = '/var/lib/gojet-installer-authority';
    private static function read(string $path): string
    {
        clearstatcache(true, $path); $s = @lstat($path);
        if ($s === false || ($s['mode'] & 0177777) !== 0100600 || $s['uid'] !== 0 || $s['nlink'] !== 1 || $s['size'] > 8192) {
            throw new \RuntimeException('Unsafe completion authority');
        }
        return (string)file_get_contents($path);
    }
    private static function sync(): void
    {
        $h = fopen(self::AUTHORITY, 'r');
        if ($h === false) { throw new \RuntimeException('Authority unavailable'); }
        try { if (!fsync($h)) { throw new \RuntimeException('Authority sync failed'); } } finally { fclose($h); }
    }
    private static function request(array $url, string $certificate, string $path, ?string $host = null): array
    {
        $authority = $url['host'].(isset($url['port']) ? ':'.$url['port'] : '');
        $context = stream_context_create([
            'ssl' => ['verify_peer' => true, 'verify_peer_name' => true, 'allow_self_signed' => false,
                'peer_name' => $url['host'], 'SNI_enabled' => true, 'cafile' => $certificate],
            'http' => ['method' => 'GET', 'timeout' => 3, 'follow_location' => 0, 'ignore_errors' => true,
                'header' => 'Host: '.($host ?? $authority)."\r\nConnection: close\r\n", 'protocol_version' => 1.1]]);
        // No DNS resolution, remote host, proxy, arbitrary path or redirected request.
        $http_response_header = [];
        $h = @fopen('https://127.0.0.1:'.($url['port'] ?? 443).$path, 'rb', false, $context);
        if ($h === false) { throw new \RuntimeException('TLS health unavailable'); }
        try {
            $body = stream_get_contents($h, 1048577);
            $meta = stream_get_meta_data($h);
            if ($body === false || strlen($body) > 1048576 || $meta['timed_out'] || !feof($h)) { throw new \RuntimeException('HTTP health exceeded bounds'); }
        } finally { fclose($h); }
        if (!preg_match('~^HTTP/\S+ ([0-9]{3})(?: |$)~', $http_response_header[0] ?? '', $m)) { throw new \RuntimeException('Invalid HTTP health'); }
        return [(int)$m[1], $body];
    }
    private static function health(string $directory): array
    {
        $seal = SiteActivation::verifyReady($directory); // Package, native dependencies, data and all eight daemons.
        $request = json_decode((string)file_get_contents($directory.'/data/request.json'), true, 32, JSON_THROW_ON_ERROR);
        $url = parse_url($request['origin']);
        if (!is_array($url) || ($url['scheme'] ?? '') !== 'https' || !preg_match('/^[a-z0-9.-]+$/D', $url['host'] ?? '')) {
            throw new \RuntimeException('Invalid sealed origin');
        }
        $certificate = null;
        foreach ($seal['tls_files'] as $path => $binding) { if (!$binding['private']) { $certificate = $path; } }
        if ($certificate === null) { throw new \RuntimeException('TLS trust unavailable'); }
        foreach (['/' => 'site', '/app' => 'workspace', '/admin' => 'admin'] as $path => $app) {
            [$code, $body] = self::request($url, $certificate, $path);
            if ($code !== 200 || !hash_equals(hash_file('sha256', $seal['release'].'/static/'.$app.'/index.html'), hash('sha256', $body))) {
                throw new \RuntimeException('Static health failed');
            }
        }
        $docs = glob($seal['release'].'/static/docs/*/index.html');
        if ($docs === false || $docs === []) { throw new \RuntimeException('Docs entry unavailable'); }
        $language = basename(dirname($docs[0]));
        if (!preg_match('/^[a-zA-Z0-9-]+$/D', $language)) { throw new \RuntimeException('Invalid docs entry'); }
        [$code, $body] = self::request($url, $certificate, '/docs/'.$language.'/');
        if ($code !== 200 || !hash_equals(hash_file('sha256', $docs[0]), hash('sha256', $body))) { throw new \RuntimeException('Docs health failed'); }
        [$code, $body] = self::request($url, $certificate, '/api/public/auth/providers');
        if ($code !== 200 || !is_array(json_decode($body, true, 16, JSON_THROW_ON_ERROR))) { throw new \RuntimeException('API health failed'); }
        if (!in_array(self::request($url, $certificate, '/api/workspaces/1/files')[0], [401, 403], true)
            || self::request($url, $certificate, '/install')[0] !== 404
            || self::request($url, $certificate, '/install/environment')[0] !== 404
            || self::request($url, $certificate, '/', 'unrelated.invalid')[0] !== 421) {
            throw new \RuntimeException('HTTP authorization boundary failed');
        }
        // Recheck the native authority after HTTP probes before committing completion.
        SiteActivation::verifyReady($directory);
        return $seal;
    }
    public static function run(string $directory, bool $complete): array
    {
        if (!function_exists('posix_geteuid') || posix_geteuid() !== 0) { throw new \RuntimeException('Root required'); }
        $marker = self::AUTHORITY.'/installed.lock';
        CompletionLock::verifyProvisioningPath($marker);
        $lockPath = self::AUTHORITY.'/service-activation.lock'; self::read($lockPath);
        $lock = fopen($lockPath, 'r+b');
        if ($lock === false) { throw new \RuntimeException('Completion lock unavailable'); }
        try {
            if (!flock($lock, LOCK_EX | LOCK_NB)) { throw new \RuntimeException('Activation busy'); }
            // Never replace an existing authority. Identical completion is an acknowledgement, not a new health claim.
            if ((new CompletionLock($marker))->exists()) {
                $state = json_decode(self::read($marker), true, 16, JSON_THROW_ON_ERROR);
                $seal = InstallationPreparation::verify($directory);
                if (!$complete || ($state['schema'] ?? '') !== 'gojet.installation-completion.v1'
                    || ($state['source'] ?? '') !== $seal['source'] || ($state['manifest_sha256'] ?? '') !== $seal['manifest_sha256']
                    || ($state['seal_sha256'] ?? '') !== hash_file('sha256', $directory.'/seal.json')) {
                    throw new \RuntimeException('Existing completion authority');
                }
                return ['source' => $seal['source'], 'installation_complete' => true, 'already_applied' => true];
            }
            $seal = self::health($directory);
            if (!$complete) { return ['source' => $seal['source'], 'healthy' => true, 'installation_complete' => false]; }
            $state = ['schema' => 'gojet.installation-completion.v1', 'source' => $seal['source'],
                'manifest_sha256' => $seal['manifest_sha256'], 'seal_sha256' => hash_file('sha256', $directory.'/seal.json')];
            $tmp = self::AUTHORITY.'/completion-'.bin2hex(random_bytes(12)).'.tmp';
            $old = umask(0077);
            try { $h = fopen($tmp, 'x+b'); } finally { umask($old); }
            if ($h === false) { throw new \RuntimeException('Completion staging failed'); }
            try {
                $raw = json_encode($state, JSON_THROW_ON_ERROR)."\n";
                if (fwrite($h, $raw) !== strlen($raw) || !fflush($h) || !fsync($h)) { throw new \RuntimeException('Completion sync failed'); }
                // Atomic no-replace publication: an existing node, including a symlink, wins.
                if (!link($tmp, $marker)) { throw new \RuntimeException('Completion already published'); }
                if (!unlink($tmp)) { throw new \RuntimeException('Completion staging cleanup failed'); }
                self::sync();
            } finally { fclose($h); if (file_exists($tmp)) { unlink($tmp); } }
            return ['source' => $seal['source'], 'installation_complete' => true, 'already_applied' => false];
        } finally { flock($lock, LOCK_UN); fclose($lock); }
    }
}
