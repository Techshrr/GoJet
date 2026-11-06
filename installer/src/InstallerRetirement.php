<?php
declare(strict_types=1);
namespace GoJet\Installer;
require_once __DIR__.'/InstallationCompletion.php';
require_once __DIR__.'/WebConfigurationSwitch.php';

/** Explicit post-completion retirement; preserves private state, receipts and unrelated pools. */
final class InstallerRetirement
{
    private const AUTHORITY = '/var/lib/gojet-installer-authority';
    private const POOL = '/etc/php/8.3/fpm/pool.d/gojet-installer.conf';
    private static function exists(string $path): bool { clearstatcache(true, $path); return file_exists($path) || is_link($path); }
    private static function read(string $path, int $mode): string
    {
        CompletionLock::verifyProvisioningPath(dirname($path).'/installed.lock');
        clearstatcache(true, $path); $s = @lstat($path);
        if ($s === false || realpath($path) !== $path || ($s['mode'] & 0177777) !== (0100000 | $mode)
            || $s['uid'] !== 0 || $s['nlink'] !== 1 || $s['size'] > 16384) { throw new \RuntimeException('Unsafe retirement authority'); }
        $raw = file_get_contents($path);
        if ($raw === false || strlen($raw) > 16384) { throw new \RuntimeException('Retirement read failed'); }
        return $raw;
    }
    private static function command(array $argv): string
    {
        $p = proc_open($argv, [0 => ['file', '/dev/null', 'r'], 1 => ['pipe', 'w'], 2 => ['pipe', 'w']], $pipes,
            '/', ['PATH' => '/usr/sbin:/usr/bin:/sbin:/bin', 'LC_ALL' => 'C']);
        if (!is_resource($p)) { throw new \RuntimeException('Retirement command unavailable'); }
        $out = ''; $err = ''; $deadline = microtime(true)+45;
        stream_set_blocking($pipes[1], false); stream_set_blocking($pipes[2], false);
        try {
            do {
                $out .= stream_get_contents($pipes[1], 65536); $err .= stream_get_contents($pipes[2], 65536);
                $status = proc_get_status($p);
                if (strlen($out)+strlen($err) > 65536 || microtime(true) > $deadline) { throw new \RuntimeException('Retirement command exceeded bound'); }
                if ($status['running']) { usleep(20000); }
            } while ($status['running']);
            $out .= stream_get_contents($pipes[1], 65536); $err .= stream_get_contents($pipes[2], 65536);
            if ($status['exitcode'] !== 0 || strlen($out)+strlen($err) > 65536 || str_contains($err, 'WARNING:')) { throw new \RuntimeException('Retirement command refused'); }
            return trim($out);
        } finally {
            if (proc_get_status($p)['running']) { proc_terminate($p, 9); }
            fclose($pipes[1]); fclose($pipes[2]); proc_close($p);
        }
    }
    private static function properties(string $name): array
    {
        $result = [];
        foreach (explode("\n", self::command(['/usr/bin/systemctl', 'show', $name, '--property=LoadState,FragmentPath,DropInPaths,ActiveState,UnitFileState'])) as $line) {
            if (str_contains($line, '=')) { [$key, $value] = explode('=', $line, 2); $result[$key] = $value; }
        }
        return $result;
    }
    private static function schedulers(): array
    {
        $units = [];
        foreach (glob(self::AUTHORITY.'/*-schedule-started.json') ?: [] as $path) {
            $kind = substr(basename($path), 0, -strlen('-schedule-started.json'));
            if (!preg_match('/^(?:apply|preflight|preparation(?:-[a-f0-9]{16,64})?)$/D', $kind)) { throw new \RuntimeException('Unknown scheduler authority'); }
            $started = ApplyRequest::read($path, 0);
            $ready = ApplyRequest::read(self::AUTHORITY.'/'.$kind.'-schedule-ready.json', 0);
            $names = ['gojet-installer-'.$kind.'.service', 'gojet-installer-'.$kind.'.timer'];
            $legacy = array_keys($started) === ['schema', 'units'] && $started['schema'] === 1;
            $bound = array_keys($started) === ['schema', 'input_sha256', 'units'] && $started['schema'] === 2
                && is_string($started['input_sha256']) && preg_match('/^[a-f0-9]{64}$/D', $started['input_sha256']);
            if ((!$legacy && !$bound) || !is_array($started['units'] ?? null)
                || array_keys($started['units']) !== $names || $ready !== ['schema' => 1, 'scheduler_enabled' => true]) {
                throw new \RuntimeException('Incomplete scheduler publication');
            }
            foreach ($started['units'] as $name => $hash) {
                $file = '/etc/systemd/system/'.$name;
                if (!is_string($hash) || hash('sha256', self::read($file, 0644)) !== $hash) { throw new \RuntimeException('Changed scheduler unit'); }
                $p = self::properties($name);
                if (($p['LoadState'] ?? '') !== 'loaded' || ($p['FragmentPath'] ?? '') !== $file || ($p['DropInPaths'] ?? '') !== ''
                    || (str_ends_with($name, '.service') && ($p['ActiveState'] ?? '') !== 'inactive')) { throw new \RuntimeException('Scheduler busy or overridden'); }
                if (str_ends_with($name, '.timer') && !in_array($p['UnitFileState'] ?? '', ['enabled', 'disabled'], true)) { throw new \RuntimeException('Unsupported timer state'); }
                $units[$name] = $hash;
            }
        }
        ksort($units); return $units;
    }
    public static function run(string $prepared, string $webPath): array
    {
        if (!function_exists('posix_geteuid') || posix_geteuid() !== 0) { throw new \RuntimeException('Root required'); }
        // Never turn a retirement request into installation or initial completion.
        $completion = ApplyRequest::read(self::AUTHORITY.'/installed.lock', 0);
        InstallationCompletion::run($prepared, true);
        $locks = [];
        try {
            foreach (['installation-apply.lock', 'service-activation.lock', 'fpm-switch.lock', 'scheduler-publication.lock'] as $name) {
                $path = self::AUTHORITY.'/'.$name;
                if (!self::exists($path)) { $mask = umask(0077); try { $f = fopen($path, 'x+b'); } finally { umask($mask); } if ($f === false) { throw new \RuntimeException('Retirement lock unavailable'); } fclose($f); }
                if (self::read($path, 0600) !== '') { throw new \RuntimeException('Invalid lock'); }
                $lock = fopen($path, 'r+b'); if ($lock === false) { throw new \RuntimeException('Retirement lock unavailable'); }
                $locks[] = $lock;
                if (!flock($lock, LOCK_EX | LOCK_NB)) { throw new \RuntimeException('Installer busy'); }
            }
            if (ApplyRequest::read(self::AUTHORITY.'/installed.lock', 0) !== $completion) { throw new \RuntimeException('Completion changed'); }
            SiteActivation::verifyReady($prepared);
            $handover = EntryHandover::load($prepared);
            if ($handover === null || !EntryHandover::parked($handover)) { throw new \RuntimeException('Completed entry handover required'); }
            $original = json_decode(self::read($handover['entry'].'/web.json', 0600), true, 16, JSON_THROW_ON_ERROR);
            $webRaw = self::read($webPath, 0600); $web = json_decode($webRaw, true, 16, JSON_THROW_ON_ERROR);
            foreach (['origin', 'release', 'certificate', 'private_key', 'socket', 'completion'] as $key) {
                if (($web[$key] ?? null) !== $original[$key]) { throw new \RuntimeException('Different Installer endpoint'); }
            }
            $pool = NativeWebConfig::render($web)['fpm']; WebConfigurationSwitch::verifyHost();
            $units = self::schedulers();
            $identity = ['schema' => 1, 'seal_sha256' => $completion['seal_sha256'], 'web_sha256' => hash('sha256', $webRaw),
                'pool_sha256' => hash('sha256', $pool), 'units' => $units];
            if (strlen(json_encode($identity, JSON_THROW_ON_ERROR)) > 4095) { throw new \RuntimeException('Retirement identity exceeds bound'); }
            $started = self::AUTHORITY.'/retirement-started.json'; $ready = self::AUTHORITY.'/retirement-ready.json';
            $saved = self::POOL.'.retired'; $active = self::exists(self::POOL); $parked = self::exists($saved);
            if ($active === $parked || self::read($active ? self::POOL : $saved, 0600) !== $pool) { throw new \RuntimeException('Foreign or missing Installer pool'); }
            $attempted = self::exists($started); $done = self::exists($ready);
            if ($attempted) { if (ApplyRequest::read($started, 0) !== $identity) { throw new \RuntimeException('Retirement identity changed'); } }
            elseif ($parked || $done) { throw new \RuntimeException('Unbound retirement state'); }
            if ($done && (ApplyRequest::read($ready, 0) !== $identity || !$parked)) { throw new \RuntimeException('Invalid retirement receipt'); }
            if (!$attempted) { ApplyRequest::write($started, $identity); }
            foreach ($units as $name => $hash) {
                if (!str_ends_with($name, '.timer')) { continue; }
                if (!$done) { self::command(['/usr/bin/systemctl', 'disable', '--now', $name]); }
                $p = self::properties($name);
                if (($p['ActiveState'] ?? '') !== 'inactive' || ($p['UnitFileState'] ?? '') !== 'disabled') { throw new \RuntimeException('Timer retirement incomplete'); }
            }
            if ($active) {
                if (!rename(self::POOL, $saved)) { throw new \RuntimeException('Pool retirement failed'); }
                $dir = fopen(dirname(self::POOL), 'r');
                if ($dir === false) { throw new \RuntimeException('Pool directory unavailable'); }
                try { if (!fsync($dir)) { throw new \RuntimeException('Pool retirement sync failed'); } } finally { fclose($dir); }
            }
            if (!$done) {
                self::command(['/usr/sbin/php-fpm8.3', '-t']);
                self::command(['/usr/bin/systemctl', 'reload', 'php8.3-fpm.service']);
            }
            $closed = false;
            for ($i = 0; $i < 100; $i++) {
                $socket = @stream_socket_client('unix://'.$web['socket'], $errno, $error, .2);
                if ($socket === false) { $closed = true; break; }
                fclose($socket); usleep(100000);
            }
            if (!$closed) { throw new \RuntimeException('Installer socket still active'); }
            WebConfigurationSwitch::verifyHost();
            if (!$done) { ApplyRequest::write($ready, $identity); }
            return ['installer_retired' => true, 'already_applied' => $done, 'installation_complete' => true];
        } finally { foreach (array_reverse($locks) as $lock) { flock($lock, LOCK_UN); fclose($lock); } }
    }
}
