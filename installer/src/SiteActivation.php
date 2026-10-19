<?php

declare(strict_types=1);
namespace GoJet\Installer;
require_once __DIR__.'/ServiceActivation.php';

/** Publish only the sealed product vhost; never final installation authority. */
final class SiteActivation
{
    private const AUTHORITY = '/var/lib/gojet-installer-authority';
    private const TARGET = '/etc/nginx/conf.d/gojet.conf';
    private static function command(array $argv): array
    {
        $p = proc_open($argv, [0 => ['file', '/dev/null', 'r'], 1 => ['pipe', 'w'], 2 => ['pipe', 'w']], $pipes, '/', ['PATH' => '/usr/sbin:/usr/bin:/sbin:/bin']);
        if (!is_resource($p)) { throw new \RuntimeException('Native command unavailable'); }
        $out = ''; $err = ''; $deadline = microtime(true)+30;
        stream_set_blocking($pipes[1], false); stream_set_blocking($pipes[2], false);
        try {
            do {
                $out .= stream_get_contents($pipes[1], 1048576); $err .= stream_get_contents($pipes[2], 65536);
                $status = proc_get_status($p);
                if (strlen($out) > 1048576 || strlen($err) > 65536 || microtime(true) > $deadline) { throw new \RuntimeException('Native command exceeded bound'); }
                if ($status['running']) { usleep(20000); }
            } while ($status['running']);
            $out .= stream_get_contents($pipes[1], 1048576); $err .= stream_get_contents($pipes[2], 65536);
            if ($status['exitcode'] !== 0 || strlen($out) > 1048576 || strlen($err) > 65536) { throw new \RuntimeException('Native command refused'); }
            // Nginx may exit0 while ignoring a duplicate server_name: reject warnings too.
            if (str_contains($err, '[warn]')) { throw new \RuntimeException('Ambiguous Nginx configuration'); }
            return ['out' => $out, 'err' => $err];
        } finally {
            if (proc_get_status($p)['running']) { proc_terminate($p, 9); }
            fclose($pipes[1]); fclose($pipes[2]); proc_close($p);
        }
    }
    private static function privateFile(string $path): string
    {
        clearstatcache(true, $path); $s = @lstat($path);
        if ($s === false || ($s['mode'] & 0177777) !== 0100600 || $s['uid'] !== 0 || $s['nlink'] !== 1 || $s['size'] > 8192) { throw new \RuntimeException('Unsafe site authority'); }
        return (string)file_get_contents($path);
    }
    private static function sync(string $directory): void
    {
        $h = fopen($directory, 'r');
        if ($h === false) { throw new \RuntimeException('Directory unavailable'); }
        try { if (!fsync($h)) { throw new \RuntimeException('Directory sync failed'); } } finally { fclose($h); }
    }
    private static function persist(string $directory, array $state): void
    {
        $path = $directory.'/site-'.bin2hex(random_bytes(12)).'.tmp';
        $old = umask(0077);
        try { $h = fopen($path, 'x+b'); } finally { umask($old); }
        if ($h === false) { throw new \RuntimeException('Site journal unavailable'); }
        try {
            $raw = json_encode($state, JSON_THROW_ON_ERROR)."\n";
            if (fwrite($h, $raw) !== strlen($raw) || !fflush($h) || !fsync($h) || !rename($path, $directory.'/site.json')) { throw new \RuntimeException('Site journal commit failed'); }
            self::sync($directory);
        } finally { fclose($h); if (file_exists($path)) { unlink($path); } }
    }
    private static function owned(string $hash): bool
    {
        clearstatcache(true, self::TARGET); $s = @lstat(self::TARGET);
        return $s !== false && ($s['mode'] & 0177777) === 0100644 && $s['uid'] === 0 && $s['nlink'] === 1
            && hash_equals($hash, (string)hash_file('sha256', self::TARGET));
    }
    /** Parse directive tokens rather than line-oriented regex (inline/quoted directives are legal). */
    private static function listenPorts(string $config): array
    {
        preg_match_all('~\s+|\#[^\n]*|"(?:\\\\.|[^"\\\\])*"|\'(?:\\\\.|[^\'\\\\])*\'|[{};]|[^\s{};\#]+~', $config, $matches);
        $directive = []; $ports = [];
        foreach ($matches[0] as $token) {
            if (trim($token) === '' || $token[0] === '#') { continue; }
            if (in_array($token, ['{', '}', ';'], true)) {
                if ($token === ';' && ($directive[0] ?? '') === 'listen') {
                    $address = $directive[1] ?? '';
                    if (!str_starts_with($address, 'unix:')) {
                        if (ctype_digit($address)) { $port = (int)$address; }
                        elseif (preg_match('/:(\d+)$/D', $address, $m)) { $port = (int)$m[1]; }
                        elseif (preg_match('/^[A-Za-z0-9.*_-]+$|^\[[a-fA-F0-9:]+\]$/D', $address)) { $port = 80; }
                        else { throw new \RuntimeException('Unsupported listen address'); }
                        if ($port < 1 || $port > 65535) { throw new \RuntimeException('Invalid listen port'); }
                        $ports[$port] = true;
                    }
                }
                $directive = [];
            } else {
                if ($token[0] === '"' || $token[0] === "'") { $token = substr($token, 1, -1); }
                // Backslash-expanded configuration is unsupported for authority selection.
                if (str_contains($token, '\\') && ($directive[0] ?? '') === 'listen') { throw new \RuntimeException('Unsupported listen token'); }
                $directive[] = $token;
            }
        }
        return array_keys($ports);
    }
    public static function run(string $directory): array { return self::execute($directory, false); }
    public static function recover(string $directory): array { return self::execute($directory, true); }
    private static function execute(string $directory, bool $recover): array
    {
        if (!function_exists('posix_geteuid') || posix_geteuid() !== 0) { throw new \RuntimeException('Root required'); }
        CompletionLock::verifyProvisioningPath(self::AUTHORITY.'/installed.lock');
        CompletionLock::verifyProvisioningPath(dirname(self::TARGET).'/installed.lock');
        $lockPath = self::AUTHORITY.'/service-activation.lock';
        self::privateFile($lockPath); // Service activation must already have provisioned this authority.
        $h = fopen($lockPath, 'r+b');
        if ($h === false) { throw new \RuntimeException('Site lock unavailable'); }
        try {
            if (!flock($h, LOCK_EX | LOCK_NB)) { throw new \RuntimeException('Activation busy'); }
            if ((new CompletionLock(self::AUTHORITY.'/installed.lock'))->exists()) { throw new \RuntimeException('Installation complete'); }
            $seal = ServiceActivation::verifyReady($directory);
            $hash = $seal['files']['nginx.conf']; $sealHash = hash_file('sha256', $directory.'/seal.json');
            $recoveryCount = 0;
            if (file_exists($directory.'/site.json') || is_link($directory.'/site.json')) {
                $state = json_decode(self::privateFile($directory.'/site.json'), true, 16, JSON_THROW_ON_ERROR);
                if (($state['schema'] ?? '') !== 1 || ($state['source'] ?? '') !== $seal['source'] || ($state['seal_sha256'] ?? '') !== $sealHash) {
                    throw new \RuntimeException('Verified site recovery required');
                }
                $recoveryCount = $state['recovery_count'] ?? 0;
                if (!is_int($recoveryCount) || $recoveryCount < 0 || $recoveryCount >= 1000) { throw new \RuntimeException('Invalid recovery history'); }
                if ($recover) {
                    if (!in_array($state['phase'] ?? '', ['failed', 'publishing', 'reloading', 'recovering', 'recovered'], true)) {
                        throw new \RuntimeException('Site phase is not recoverable');
                    }
                    $exists = file_exists(self::TARGET) || is_link(self::TARGET);
                    if ($exists && (!self::owned($hash) || $state['phase'] === 'recovered')) { throw new \RuntimeException('Changed site requires operator review'); }
                    $already = $state['phase'] === 'recovered';
                    if (!$already && $state['phase'] !== 'recovering') {
                        $state['recovered_from'] = $state['phase']; $state['phase'] = 'recovering';
                        $state['cleanup_complete'] = false; self::persist($directory, $state);
                    }
                    if ($exists) {
                        if (!unlink(self::TARGET)) { throw new \RuntimeException('Site removal failed'); }
                        self::sync(dirname(self::TARGET));
                    }
                    // Always restore the loaded baseline too: the killed worker may have reloaded it.
                    self::command(['/usr/sbin/nginx', '-t']);
                    self::command(['/usr/bin/systemctl', 'reload', 'nginx.service']);
                    self::command(['/usr/bin/systemctl', 'is-active', 'nginx.service']);
                    if (!$already) {
                        $state['phase'] = 'recovered'; $state['cleanup_complete'] = true;
                        $state['recovery_count'] = $recoveryCount+1; self::persist($directory, $state);
                    }
                    return ['site_recovered' => true, 'already_applied' => $already, 'installation_complete' => false];
                }
                if (($state['phase'] ?? '') === 'site-configured' && self::owned($hash)) {
                    self::command(['/usr/sbin/nginx', '-t']);
                    return ['site_configured' => true, 'already_applied' => true, 'installation_complete' => false];
                }
                if (($state['phase'] ?? '') !== 'recovered' || ($state['cleanup_complete'] ?? false) !== true || $recoveryCount < 1) {
                    throw new \RuntimeException('Verified site recovery required');
                }
            } elseif ($recover) { throw new \RuntimeException('Site journal required'); }
            if (file_exists(self::TARGET) || is_link(self::TARGET)) { throw new \RuntimeException('Existing site configuration'); }
            $baseline = self::command(['/usr/sbin/nginx', '-T'])['out'];
            $raw = (string)file_get_contents($directory.'/nginx.conf');
            $ports = self::listenPorts($raw);
            if ($ports === [] || array_intersect($ports, self::listenPorts($baseline))) { throw new \RuntimeException('Existing Nginx listener'); }
            // Require this fixed conf.d destination to be included by the native Ubuntu configuration.
            if (!preg_match('~\binclude\s+/etc/nginx/conf\.d/\*\.conf\s*;~', preg_replace('/#[^\n]*/', '', $baseline))) { throw new \RuntimeException('Unsupported Nginx include authority'); }
            $state = ['schema' => 1, 'source' => $seal['source'], 'seal_sha256' => $sealHash, 'phase' => 'publishing', 'cleanup_complete' => false, 'recovery_count' => $recoveryCount];
            self::persist($directory, $state); $created = false;
            try {
                $old = umask(0022);
                try { $f = fopen(self::TARGET, 'x+b'); } finally { umask($old); }
                if ($f === false) { throw new \RuntimeException('Site publication failed'); }
                $created = true;
                try { if (fwrite($f, $raw) !== strlen($raw) || !fflush($f) || !fsync($f)) { throw new \RuntimeException('Site sync failed'); } }
                finally { fclose($f); }
                self::sync(dirname(self::TARGET));
                self::command(['/usr/sbin/nginx', '-t']);
                $state['phase'] = 'reloading'; self::persist($directory, $state);
                self::command(['/usr/bin/systemctl', 'reload', 'nginx.service']);
                self::command(['/usr/bin/systemctl', 'is-active', 'nginx.service']);
                $state['phase'] = 'site-configured'; self::persist($directory, $state);
                return ['site_configured' => true, 'already_applied' => false, 'installation_complete' => false];
            } catch (\Throwable) {
                $clean = false;
                try {
                    if ($created && self::owned($hash)) {
                        unlink(self::TARGET); self::sync(dirname(self::TARGET));
                        self::command(['/usr/sbin/nginx', '-t']);
                        if ($state['phase'] === 'reloading') { self::command(['/usr/bin/systemctl', 'reload', 'nginx.service']); }
                        $clean = true;
                    }
                } catch (\Throwable) { /* Preserve failure barrier; never remove foreign configuration. */ }
                $state['phase'] = 'failed'; $state['cleanup_complete'] = $clean; self::persist($directory, $state);
                throw new \RuntimeException('Site activation failed; verify recovery');
            }
        } finally { flock($h, LOCK_UN); fclose($h); }
    }
}
