<?php
declare(strict_types=1);
namespace GoJet\Installer;
require_once __DIR__.'/WebInstallationApply.php';
require_once __DIR__.'/WebConfigurationPreparation.php';

/** Shared publication guards for separately authorized observation and execution schedulers. */
final class PreflightScheduleInstallation
{
    private const AUTHORITY = '/var/lib/gojet-installer-authority';
    private const DIRECTORY = '/etc/systemd/system';
    private static function command(array $argv): string
    {
        $p = proc_open($argv, [0 => ['file', '/dev/null', 'r'], 1 => ['pipe', 'w'], 2 => ['pipe', 'w']], $pipes,
            '/', ['PATH' => '/usr/sbin:/usr/bin:/sbin:/bin', 'LC_ALL' => 'C']);
        if (!is_resource($p)) { throw new \RuntimeException('Scheduler command unavailable'); }
        $out = ''; $err = ''; $deadline = microtime(true) + 45;
        stream_set_blocking($pipes[1], false); stream_set_blocking($pipes[2], false);
        try {
            do {
                $out .= stream_get_contents($pipes[1], 65536); $err .= stream_get_contents($pipes[2], 65536);
                $status = proc_get_status($p);
                if (strlen($out) + strlen($err) > 65536 || microtime(true) > $deadline) { throw new \RuntimeException('Scheduler command exceeded bound'); }
                if ($status['running']) { usleep(20000); }
            } while ($status['running']);
            $out .= stream_get_contents($pipes[1], 65536); $err .= stream_get_contents($pipes[2], 65536);
            if ($status['exitcode'] !== 0 || strlen($out) + strlen($err) > 65536) { throw new \RuntimeException('Scheduler command refused'); }
            return trim($out);
        } finally {
            if (proc_get_status($p)['running']) { proc_terminate($p, 9); }
            fclose($pipes[1]); fclose($pipes[2]); proc_close($p);
        }
    }
    private static function properties(string $name): array
    {
        $out = self::command(['/usr/bin/systemctl', 'show', $name, '--property=LoadState,FragmentPath,DropInPaths,ActiveState,UnitFileState']);
        $value = [];
        foreach (explode("\n", $out) as $line) {
            if (str_contains($line, '=')) { [$key, $item] = explode('=', $line, 2); $value[$key] = $item; }
        }
        return $value;
    }
    private static function sync(): void
    {
        $f = fopen(self::DIRECTORY, 'r');
        if ($f === false) { throw new \RuntimeException('Unit directory unavailable'); }
        try { if (!fsync($f)) { throw new \RuntimeException('Unit directory sync failed'); } }
        finally { fclose($f); }
    }
    /** Observation-only entry point; existing callers never enable execution. */
    public static function install(): array { return self::installSchedule('preflight', WebInstallationApply::schedule(), self::inputHash(self::AUTHORITY.'/web-apply.json')); }
    /** Separate explicit root authorization to execute the fixed approved mailbox. */
    public static function installApply(): array { return self::installSchedule('apply', WebInstallationApply::applySchedule(), self::inputHash(self::AUTHORITY.'/web-apply.json')); }
    /** Root-selected binding/state become literal unit arguments, never web parameters. */
    public static function installPreparation(string $binding, string $directory, ?string $predecessor = null): array
    {
        $units = WebConfigurationPreparation::schedule($binding, $directory, $predecessor);
        $kind = substr(array_key_first($units), strlen('gojet-installer-'), -strlen('.service'));
        return self::installSchedule($kind, $units, self::inputHash($binding));
    }
    /** Explicit recovery uses the original, re-rendered authority; never clears worker receipts. */
    public static function recover(string $kind, ?string $binding = null, ?string $directory = null, ?string $predecessor = null): array
    {
        if ($kind === 'preparation' && $binding !== null && $directory !== null) {
            $units = WebConfigurationPreparation::schedule($binding, $directory, $predecessor);
            $kind = substr(array_key_first($units), strlen('gojet-installer-'), -strlen('.service'));
        } elseif ($kind === 'preflight' && $binding === null) { $units = WebInstallationApply::schedule(); }
        elseif ($kind === 'apply' && $binding === null) { $units = WebInstallationApply::applySchedule(); }
        else { throw new \RuntimeException('Invalid scheduler recovery'); }
        return self::installSchedule($kind, $units, self::inputHash($binding ?? self::AUTHORITY.'/web-apply.json'), true);
    }
    private static function inputHash(string $path): string
    {
        return hash('sha256', self::read($path, 0600, 4096));
    }
    private static function exists(string $path): bool
    {
        clearstatcache(true, $path); return file_exists($path) || is_link($path);
    }
    private static function read(string $path, int $mode, int $limit = 16384): string
    {
        clearstatcache(true, $path); $s = @lstat($path);
        if ($s === false || realpath($path) !== $path || ($s['mode'] & 0177777) !== (0100000 | $mode)
            || $s['uid'] !== 0 || $s['nlink'] !== 1 || $s['size'] > $limit) { throw new \RuntimeException('Unsafe scheduler file'); }
        $raw = file_get_contents($path);
        if ($raw === false || strlen($raw) > $limit) { throw new \RuntimeException('Scheduler read failed'); }
        return $raw;
    }
    private static function publish(string $path, string $raw): void
    {
        // The final unit is absent or complete; interrupted staging is never loaded by systemd.
        $temporary = self::DIRECTORY.'/.gojet-schedule-'.bin2hex(random_bytes(16)).'.tmp';
        $mask = umask(0077);
        try { $f = fopen($temporary, 'x+b'); } finally { umask($mask); }
        if ($f === false) { throw new \RuntimeException('Scheduler staging failed'); }
        try {
            if (fwrite($f, $raw) !== strlen($raw) || !fflush($f) || !chmod($temporary, 0644) || !fsync($f)
                || !link($temporary, $path) || !unlink($temporary)) { throw new \RuntimeException('Scheduler publication refused'); }
            self::sync();
        } finally { fclose($f); if (file_exists($temporary)) { unlink($temporary); } }
    }
    private static function installSchedule(string $kind, array $units, string $inputHash, bool $recover = false): array
    {
        CompletionLock::verifyProvisioningPath(self::DIRECTORY.'/installed.lock');
        CompletionLock::verifyProvisioningPath(self::AUTHORITY.'/installed.lock');
        $lockPath = self::AUTHORITY.'/scheduler-publication.lock';
        $mask = umask(0077);
        try { $created = @fopen($lockPath, 'x+b'); } finally { umask($mask); }
        if ($created !== false) { fclose($created); }
        self::read($lockPath, 0600, 0);
        $lock = fopen($lockPath, 'r+b');
        if ($lock === false) { throw new \RuntimeException('Scheduler lock unavailable'); }
        try {
            if (!flock($lock, LOCK_EX | LOCK_NB)) { throw new \RuntimeException('Scheduler publication busy'); }
            if ((new CompletionLock(self::AUTHORITY.'/installed.lock'))->exists()) { throw new \RuntimeException('Installer unavailable'); }
            return self::publishSchedule($kind, $units, $inputHash, $recover);
        } finally { flock($lock, LOCK_UN); fclose($lock); }
    }
    private static function publishSchedule(string $kind, array $units, string $inputHash, bool $recover): array
    {
        $timerName = 'gojet-installer-'.$kind.'.timer';
        $receipt = self::AUTHORITY.'/'.$kind.'-schedule-started.json';
        $ready = self::AUTHORITY.'/'.$kind.'-schedule-ready.json';
        $identity = ['schema' => 2, 'input_sha256' => $inputHash, 'units' => array_map(fn($bytes) => hash('sha256', $bytes), $units)];
        $completed = ['schema' => 1, 'scheduler_enabled' => true];
        if ($recover) {
            if (ApplyRequest::read($receipt, 0) !== $identity) { throw new \RuntimeException('Scheduler recovery identity changed'); }
            if (self::exists($ready) && ApplyRequest::read($ready, 0) !== $completed) { throw new \RuntimeException('Invalid scheduler receipt'); }
        } elseif (self::exists($receipt) || self::exists($ready)) { throw new \RuntimeException('Scheduler provisioning already attempted'); }
        $missing = [];
        foreach ($units as $name => $bytes) {
            $path = self::DIRECTORY.'/'.$name;
            if (self::exists($path)) {
                if (!$recover || self::read($path, 0644) !== $bytes) { throw new \RuntimeException('Existing or changed scheduler unit'); }
            } else { $missing[$path] = $bytes; }
            $existing = self::properties($name);
            $empty = ($existing['LoadState'] ?? '') === 'not-found' && ($existing['FragmentPath'] ?? '') === '';
            $owned = $recover && ($existing['LoadState'] ?? '') === 'loaded' && ($existing['FragmentPath'] ?? '') === $path;
            if ((!$empty && !$owned) || ($existing['DropInPaths'] ?? '') !== ''
                || !in_array($existing['ActiveState'] ?? '', str_ends_with($name, '.timer') && $owned ? ['inactive', 'active'] : ['inactive'], true)) {
                throw new \RuntimeException('Existing scheduler configuration or busy worker');
            }
        }
        $timer = self::properties($timerName);
        $active = ($timer['ActiveState'] ?? '') === 'active';
        if ($recover && (self::exists($ready) || $active) && $missing !== []) { throw new \RuntimeException('Incomplete active scheduler'); }
        if ($recover && self::exists($ready)) {
            // A ready receipt does not authorize restarting a deliberately disabled timer.
            if (!$active || ($timer['UnitFileState'] ?? '') !== 'enabled') { throw new \RuntimeException('Completed scheduler changed'); }
            return ['scheduler_enabled' => true, 'already_applied' => true, 'installation_complete' => false];
        }
        if (!$recover) { ApplyRequest::write($receipt, $identity); }
        foreach ($missing as $path => $bytes) { self::publish($path, $bytes); }
        self::command(['/usr/bin/systemd-analyze', 'verify', ...array_map(fn($name) => self::DIRECTORY.'/'.$name, array_keys($units))]);
        self::command(['/usr/bin/systemctl', 'daemon-reload']);
        foreach ($units as $name => $bytes) {
            $p = self::properties($name);
            if (self::read(self::DIRECTORY.'/'.$name, 0644) !== $bytes || ($p['LoadState'] ?? '') !== 'loaded'
                || ($p['FragmentPath'] ?? '') !== self::DIRECTORY.'/'.$name || ($p['DropInPaths'] ?? '') !== '') {
                throw new \RuntimeException('Scheduler unit override detected');
            }
        }
        if ((new CompletionLock(self::AUTHORITY.'/installed.lock'))->exists()) { throw new \RuntimeException('Installer unavailable'); }
        if (!$active) { self::command(['/usr/bin/systemctl', 'enable', '--now', $timerName]); }
        $timer = self::properties($timerName);
        if (($timer['ActiveState'] ?? '') !== 'active' || ($timer['UnitFileState'] ?? '') !== 'enabled') {
            throw new \RuntimeException('Scheduler activation failed');
        }
        ApplyRequest::write($ready, $completed);
        return ['scheduler_enabled' => true] + ($recover ? ['already_applied' => false] : []) + ['installation_complete' => false];
    }
}
