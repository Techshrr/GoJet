<?php

declare(strict_types=1);
namespace GoJet\Installer;
require_once __DIR__.'/InstallationPreparation.php';

/** Root service phase only: fixed systemd authority, never HTTP input or final installation completion. */
final class ServiceActivation
{
    private const NAMES = ['logreceiver', 'redirectengine', 'analyticsworker', 'analyticsreconciler', 'platformapi', 'mailworker', 'fileworker', 'operationsmonitor'];
    private const AUTHORITY = '/var/lib/gojet-installer-authority';
    private const UNIT_DIRECTORY = '/etc/systemd/system';
    private static function command(array $argv, array $env = []): array
    {
        $p = proc_open($argv, [0 => ['file', '/dev/null', 'r'], 1 => ['pipe', 'w'], 2 => ['pipe', 'w']], $pipes, '/', $env + ['PATH' => '/usr/sbin:/usr/bin:/sbin:/bin']);
        if (!is_resource($p)) { throw new \RuntimeException('Native command unavailable'); }
        $out = ''; $err = ''; $deadline = microtime(true)+45;
        stream_set_blocking($pipes[1], false); stream_set_blocking($pipes[2], false);
        try {
            do {
                $out .= stream_get_contents($pipes[1], 65536); $err .= stream_get_contents($pipes[2], 65536);
                $status = proc_get_status($p);
                if (strlen($out)+strlen($err) > 65536 || microtime(true) > $deadline) { throw new \RuntimeException('Native command exceeded bound'); }
                if ($status['running']) { usleep(20000); }
            } while ($status['running']);
            $out .= stream_get_contents($pipes[1], 65536); $err .= stream_get_contents($pipes[2], 65536);
            if (strlen($out)+strlen($err) > 65536) { throw new \RuntimeException('Native output exceeded bound'); }
            return ['code' => $status['exitcode'], 'out' => $out];
        } finally {
            if (proc_get_status($p)['running']) { proc_terminate($p, 9); }
            fclose($pipes[1]); fclose($pipes[2]); proc_close($p);
        }
    }
    private static function systemctl(array $args, bool $required = true): array
    {
        $r = self::command(['/usr/bin/systemctl', ...$args]);
        if ($required && $r['code'] !== 0) { throw new \RuntimeException('Service command refused'); }
        return $r;
    }
    private static function sync(string $directory): void
    {
        $h = fopen($directory, 'r');
        if ($h === false) { throw new \RuntimeException('Directory unavailable'); }
        try { if (!fsync($h)) { throw new \RuntimeException('Directory sync failed'); } } finally { fclose($h); }
    }
    private static function privateFile(string $path): string
    {
        clearstatcache(true, $path); $s = @lstat($path);
        if ($s === false || ($s['mode'] & 0177777) !== 0100600 || $s['uid'] !== 0 || $s['nlink'] !== 1 || $s['size'] > 8192) {
            throw new \RuntimeException('Unsafe service authority file');
        }
        return (string)file_get_contents($path);
    }
    private static function persist(string $directory, array $state): void
    {
        $path = $directory.'/services-'.bin2hex(random_bytes(12)).'.tmp';
        $old = umask(0077);
        try { $h = fopen($path, 'x+b'); } finally { umask($old); }
        if ($h === false) { throw new \RuntimeException('Service journal unavailable'); }
        try {
            $raw = json_encode($state, JSON_THROW_ON_ERROR)."\n";
            if (fwrite($h, $raw) !== strlen($raw) || !fflush($h) || !fsync($h)) { throw new \RuntimeException('Service journal sync failed'); }
            if (!rename($path, $directory.'/services.json')) { throw new \RuntimeException('Service journal commit failed'); }
            self::sync($directory);
        } finally { fclose($h); if (file_exists($path)) { unlink($path); } }
    }
    private static function owned(string $path, string $hash): bool
    {
        clearstatcache(true, $path); $s = @lstat($path);
        return $s !== false && ($s['mode'] & 0177777) === 0100644 && $s['uid'] === 0 && $s['nlink'] === 1
            && hash_equals($hash, (string)hash_file('sha256', $path));
    }
    private static function healthy(array $seal): bool
    {
        $account = posix_getpwnam('gojet');
        if ($account === false) { return false; }
        foreach (self::NAMES as $name) {
            $unit = 'gojet-'.$name.'.service';
            if (!self::owned(self::UNIT_DIRECTORY.'/'.$unit, $seal['files']['units/'.$unit])) { return false; }
            $row = self::systemctl(['show', $unit, '--property=ActiveState,SubState,MainPID,NRestarts,User,FragmentPath'], false);
            $state = [];
            foreach (explode("\n", trim($row['out'])) as $line) { if (str_contains($line, '=')) { [$k, $v] = explode('=', $line, 2); $state[$k] = $v; } }
            if (($state['ActiveState'] ?? '') !== 'active' || ($state['SubState'] ?? '') !== 'running'
                || ($state['NRestarts'] ?? '') !== '0' || ($state['User'] ?? '') !== 'gojet'
                || ($state['FragmentPath'] ?? '') !== self::UNIT_DIRECTORY.'/'.$unit) { return false; }
            $pid = $state['MainPID'] ?? '';
            if (!ctype_digit($pid) || (int)$pid < 2 || realpath('/proc/'.$pid.'/exe') !== $seal['release'].'/bin/'.$name) { return false; }
            $status = @file_get_contents('/proc/'.$pid.'/status');
            if ($status === false || !preg_match('/^Uid:\s+(\d+)\s+(\d+)\s+(\d+)\s+(\d+)$/m', $status, $uids)) { return false; }
            for ($i = 1; $i <= 4; $i++) { if ((int)$uids[$i] !== $account['uid']) { return false; } }
            if (trim(self::systemctl(['is-enabled', $unit], false)['out']) !== 'enabled') { return false; }
        }
        foreach (['http://127.0.0.1:8081/api/public/auth/providers' => 200, 'http://127.0.0.1:8080/healthz' => 204] as $url => $expected) {
            $http_response_header = [];
            $context = stream_context_create(['http' => ['timeout' => 2, 'follow_location' => 0, 'ignore_errors' => true]]);
            $f = @fopen($url, 'rb', false, $context);
            if ($f === false) { return false; }
            fclose($f);
            if (!preg_match('~^HTTP/\S+ '.$expected.'(?: |$)~', $http_response_header[0] ?? '')) { return false; }
        }
        return self::command(['/usr/sbin/runuser', '-u', 'gojet', '--', $seal['release'].'/bin/logreceiver', '--check'], ['GOJET_LOG_SOCKET' => '/run/gojet-log/collector.sock'])['code'] === 0;
    }
    private static function cleanup(array $published, array $seal): bool
    {
        try {
            foreach ($published as $unit) { if (!self::owned(self::UNIT_DIRECTORY.'/'.$unit, $seal['files']['units/'.$unit])) { return false; } }
            if ($published !== []) {
                self::systemctl(['stop', ...$published]);
                foreach ($published as $unit) {
                    $state = trim(self::systemctl(['show', $unit, '--property=ActiveState', '--value'])['out']);
                    if (!in_array($state, ['inactive', 'failed'], true)) { return false; }
                }
                self::systemctl(['disable', ...$published]);
                foreach ($published as $unit) { if (!unlink(self::UNIT_DIRECTORY.'/'.$unit)) { return false; } }
                self::sync(self::UNIT_DIRECTORY); self::systemctl(['daemon-reload']);
                self::systemctl(['reset-failed', ...$published], false);
            }
            return true;
        } catch (\Throwable) { return false; }
    }
    /** Read-only readiness for another root phase holding the shared activation lock. */
    public static function verifyReady(string $directory): array
    {
        $seal = InstallationPreparation::verify($directory);
        $data = json_decode(self::privateFile($directory.'/data/journal.json'), true, 16, JSON_THROW_ON_ERROR);
        $state = json_decode(self::privateFile($directory.'/services.json'), true, 16, JSON_THROW_ON_ERROR);
        if (($data['phase'] ?? '') !== 'data-complete' || ($data['source'] ?? '') !== $seal['source']
            || ($data['request_sha256'] ?? '') !== hash_file('sha256', $directory.'/data/request.json')
            || ($state['schema'] ?? '') !== 1 || ($state['source'] ?? '') !== $seal['source']
            || ($state['seal_sha256'] ?? '') !== hash_file('sha256', $directory.'/seal.json')
            || ($state['phase'] ?? '') !== 'services-ready' || !self::healthy($seal)) {
            throw new \RuntimeException('Verified service readiness required');
        }
        return $seal;
    }
    public static function run(string $directory): array
    {
        if (!function_exists('posix_geteuid') || posix_geteuid() !== 0) { throw new \RuntimeException('Root required'); }
        CompletionLock::verifyProvisioningPath(self::AUTHORITY.'/installed.lock');
        if ((new CompletionLock(self::AUTHORITY.'/installed.lock'))->exists()) { throw new \RuntimeException('Installation already complete'); }
        CompletionLock::verifyProvisioningPath(self::UNIT_DIRECTORY.'/installed.lock');
        $lockPath = self::AUTHORITY.'/service-activation.lock';
        if (file_exists($lockPath) || is_link($lockPath)) { self::privateFile($lockPath); }
        $old = umask(0077);
        try { $lock = fopen($lockPath, 'c+b'); } finally { umask($old); }
        if ($lock === false) { throw new \RuntimeException('Activation lock unavailable'); }
        try {
            self::privateFile($lockPath);
            if (!flock($lock, LOCK_EX | LOCK_NB)) { throw new \RuntimeException('Activation busy'); }
            if ((new CompletionLock(self::AUTHORITY.'/installed.lock'))->exists()) { throw new \RuntimeException('Installation already complete'); }
            $seal = InstallationPreparation::verify($directory);
            $sealHash = hash_file('sha256', $directory.'/seal.json');
            // Activation cannot initialize data or interpret a client completion flag.
            $data = json_decode(self::privateFile($directory.'/data/journal.json'), true, 16, JSON_THROW_ON_ERROR);
            if (($data['phase'] ?? '') !== 'data-complete' || ($data['source'] ?? '') !== $seal['source']
                || ($data['request_sha256'] ?? '') !== hash_file('sha256', $directory.'/data/request.json')) {
                throw new \RuntimeException('Verified data completion required');
            }
            $journalPath = $directory.'/services.json';
            if (file_exists($journalPath) || is_link($journalPath)) {
                $state = json_decode(self::privateFile($journalPath), true, 16, JSON_THROW_ON_ERROR);
                if (($state['schema'] ?? '') !== 1 || ($state['source'] ?? '') !== $seal['source'] || ($state['seal_sha256'] ?? '') !== $sealHash
                    || ($state['phase'] ?? '') !== 'services-ready' || !self::healthy($seal)) { throw new \RuntimeException('Verified service recovery required'); }
                return ['services_ready' => true, 'already_applied' => true, 'installation_complete' => false];
            }
            $units = array_map(fn($n) => 'gojet-'.$n.'.service', self::NAMES);
            self::systemctl(['daemon-reload']);
            foreach ($units as $unit) {
                $r = self::systemctl(['show', $unit, '--property=LoadState', '--value'], false);
                if (trim($r['out']) !== 'not-found') { throw new \RuntimeException('Existing service configuration'); }
                foreach (['/etc/systemd/system', '/run/systemd/system', '/usr/lib/systemd/system'] as $base) {
                    foreach ([$base.'/'.$unit, $base.'/'.$unit.'.d', $base.'/multi-user.target.wants/'.$unit] as $path) {
                        if (file_exists($path) || is_link($path)) { throw new \RuntimeException('Existing service authority'); }
                    }
                }
            }
            $state = ['schema' => 1, 'source' => $seal['source'], 'seal_sha256' => $sealHash, 'phase' => 'publishing', 'cleanup_complete' => false];
            self::persist($directory, $state); $published = [];
            try {
                foreach ($units as $unit) {
                    $path = self::UNIT_DIRECTORY.'/'.$unit;
                    $raw = file_get_contents($directory.'/units/'.$unit);
                    $old = umask(0022);
                    try { $h = fopen($path, 'x+b'); } finally { umask($old); }
                    if ($h === false) { throw new \RuntimeException('Unit publication failed'); }
                    $published[] = $unit;
                    try {
                        if (fwrite($h, $raw) !== strlen($raw) || !fflush($h) || !fsync($h)) { throw new \RuntimeException('Unit sync failed'); }
                    } finally { fclose($h); }
                }
                self::sync(self::UNIT_DIRECTORY); self::systemctl(['daemon-reload']);
                foreach ($units as $unit) {
                    if (trim(self::systemctl(['show', $unit, '--property=DropInPaths', '--value'])['out']) !== '') { throw new \RuntimeException('Unapproved service override'); }
                }
                self::systemctl(['enable', ...$units]);
                $state['phase'] = 'starting'; self::persist($directory, $state);
                self::systemctl(['start', $units[0]]);
                $deadline = microtime(true)+10;
                do {
                    $probe = self::command(['/usr/sbin/runuser', '-u', 'gojet', '--', $seal['release'].'/bin/logreceiver', '--check'], ['GOJET_LOG_SOCKET' => '/run/gojet-log/collector.sock']);
                    if ($probe['code'] === 0) { break; }
                    if (microtime(true) > $deadline) { throw new \RuntimeException('Collector unavailable'); }
                    usleep(100000);
                } while (true);
                self::systemctl(['start', ...array_slice($units, 1)]);
                $deadline = microtime(true)+15;
                while (!self::healthy($seal)) {
                    if (microtime(true) > $deadline) { throw new \RuntimeException('Services unhealthy'); }
                    usleep(200000);
                }
                usleep(1000000);
                if (!self::healthy($seal)) { throw new \RuntimeException('Unstable services'); }
                $state['phase'] = 'services-ready'; self::persist($directory, $state);
                return ['services_ready' => true, 'already_applied' => false, 'installation_complete' => false];
            } catch (\Throwable) {
                $state['phase'] = 'failed'; $state['cleanup_complete'] = self::cleanup($published, $seal);
                self::persist($directory, $state);
                throw new \RuntimeException('Service activation failed; verify recovery');
            }
        } finally { flock($lock, LOCK_UN); fclose($lock); }
    }
}
