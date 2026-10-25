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
    public static function install(): array { return self::installSchedule('preflight', WebInstallationApply::schedule()); }
    /** Separate explicit root authorization to execute the fixed approved mailbox. */
    public static function installApply(): array { return self::installSchedule('apply', WebInstallationApply::applySchedule()); }
    /** Root-selected binding/state become literal unit arguments, never web parameters. */
    public static function installPreparation(string $binding, string $directory, ?string $predecessor = null): array
    {
        $units = WebConfigurationPreparation::schedule($binding, $directory, $predecessor);
        $kind = substr(array_key_first($units), strlen('gojet-installer-'), -strlen('.service'));
        return self::installSchedule($kind, $units);
    }
    private static function installSchedule(string $kind, array $units): array
    {
        $timerName = 'gojet-installer-'.$kind.'.timer';
        CompletionLock::verifyProvisioningPath(self::DIRECTORY.'/installed.lock');
        $receipt = self::AUTHORITY.'/'.$kind.'-schedule-started.json';
        foreach ([$receipt, self::AUTHORITY.'/'.$kind.'-schedule-ready.json'] as $record) {
            if (file_exists($record) || is_link($record)) { throw new \RuntimeException('Scheduler provisioning already attempted'); }
        }
        foreach ($units as $name => $bytes) {
            $path = self::DIRECTORY.'/'.$name;
            if (file_exists($path) || is_link($path)) { throw new \RuntimeException('Scheduler unit already exists'); }
            $existing = self::properties($name);
            if (($existing['LoadState'] ?? '') !== 'not-found' || ($existing['FragmentPath'] ?? '') !== ''
                || ($existing['DropInPaths'] ?? '') !== '' || ($existing['ActiveState'] ?? '') !== 'inactive') {
                throw new \RuntimeException('Existing scheduler configuration');
            }
        }
        // Exclusive durable barrier before publishing anything. Partial attempts require inspection.
        ApplyRequest::write($receipt, ['schema' => 1, 'units' => array_map(fn($bytes) => hash('sha256', $bytes), $units)]);
        foreach ($units as $name => $bytes) {
            $path = self::DIRECTORY.'/'.$name;
            $old = umask(0077);
            try { $f = @fopen($path, 'x+b'); } finally { umask($old); }
            if ($f === false) { throw new \RuntimeException('Unit publication refused'); }
            try {
                if (fwrite($f, $bytes) !== strlen($bytes) || !fflush($f) || !chmod($path, 0644) || !fsync($f)) {
                    throw new \RuntimeException('Unit publication failed');
                }
            } finally { fclose($f); }
            self::sync();
        }
        self::command(['/usr/bin/systemd-analyze', 'verify', ...array_map(fn($name) => self::DIRECTORY.'/'.$name, array_keys($units))]);
        self::command(['/usr/bin/systemctl', 'daemon-reload']);
        foreach ($units as $name => $bytes) {
            $p = self::properties($name);
            if (($p['LoadState'] ?? '') !== 'loaded' || ($p['FragmentPath'] ?? '') !== self::DIRECTORY.'/'.$name
                || ($p['DropInPaths'] ?? '') !== '') { throw new \RuntimeException('Scheduler unit override detected'); }
        }
        if ((new CompletionLock(self::AUTHORITY.'/installed.lock'))->exists()) { throw new \RuntimeException('Installer unavailable'); }
        self::command(['/usr/bin/systemctl', 'enable', '--now', $timerName]);
        $timer = self::properties($timerName);
        if (($timer['ActiveState'] ?? '') !== 'active' || ($timer['UnitFileState'] ?? '') !== 'enabled') {
            throw new \RuntimeException('Scheduler activation failed');
        }
        ApplyRequest::write(self::AUTHORITY.'/'.$kind.'-schedule-ready.json', ['schema' => 1, 'scheduler_enabled' => true]);
        return ['scheduler_enabled' => true, 'installation_complete' => false];
    }
}
