<?php
declare(strict_types=1);
namespace GoJet\Installer;
require_once __DIR__.'/NativeWebConfig.php';
require_once __DIR__.'/WebConfigurationRestart.php';

/** Fixed Ubuntu pool; root stdin only. Never changes Nginx, web state or approval. */
final class WebConfigurationSwitch
{
    private const TARGET = '/etc/php/8.3/fpm/pool.d/gojet-installer.conf';
    private const MAIN = '/etc/php/8.3/fpm/php-fpm.conf';
    private const AUTHORITY = '/var/lib/gojet-installer-authority';

    private static function read(string $path, int $mode, int $limit = 65536): string
    {
        clearstatcache(true, $path); $s = @lstat($path);
        if ($s === false || realpath($path) !== $path || ($s['mode'] & 0177777) !== (0100000 | $mode)
            || $s['uid'] !== 0 || $s['nlink'] !== 1 || $s['size'] > $limit) {
            throw new \RuntimeException('Unsafe FPM authority');
        }
        $raw = file_get_contents($path);
        if ($raw === false || strlen($raw) > $limit) { throw new \RuntimeException('FPM authority unreadable'); }
        return $raw;
    }
    private static function absent(string $path): void
    {
        clearstatcache(true, $path);
        if (file_exists($path) || is_link($path)) { throw new \RuntimeException('Switch boundary occupied'); }
    }
    private static function sync(string $path): void
    {
        $f = fopen($path, 'r');
        if ($f === false) { throw new \RuntimeException('Switch sync unavailable'); }
        try { if (!fsync($f)) { throw new \RuntimeException('Switch sync failed'); } } finally { fclose($f); }
    }
    private static function command(array $argv): string
    {
        $p = proc_open($argv, [0 => ['file', '/dev/null', 'r'], 1 => ['pipe', 'w'], 2 => ['pipe', 'w']], $pipes,
            '/', ['PATH' => '/usr/sbin:/usr/bin:/sbin:/bin', 'LC_ALL' => 'C']);
        if (!is_resource($p)) { throw new \RuntimeException('FPM command unavailable'); }
        $out = ''; $err = ''; $deadline = microtime(true)+30;
        stream_set_blocking($pipes[1], false); stream_set_blocking($pipes[2], false);
        try {
            do {
                $out .= stream_get_contents($pipes[1], 65536); $err .= stream_get_contents($pipes[2], 65536);
                $status = proc_get_status($p);
                if (strlen($out)+strlen($err) > 65536 || microtime(true) > $deadline) { throw new \RuntimeException('FPM command exceeded bounds'); }
                if ($status['running']) { usleep(20000); }
            } while ($status['running']);
            $out .= stream_get_contents($pipes[1], 65536); $err .= stream_get_contents($pipes[2], 65536);
            if ($status['exitcode'] !== 0 || strlen($out)+strlen($err) > 65536 || str_contains($err, 'WARNING:')) {
                throw new \RuntimeException('FPM command refused');
            }
            return trim($out);
        } finally {
            if (proc_get_status($p)['running']) { proc_terminate($p, 9); }
            fclose($pipes[1]); fclose($pipes[2]); proc_close($p);
        }
    }
    /** Reject alternative include chains and duplicate pool authority before replacing a file. */
    private static function service(): void
    {
        $main = self::read(self::MAIN, 0644);
        $includes = 0;
        foreach (explode("\n", $main) as $line) {
            $line = trim($line);
            if ($line === '' || $line[0] === ';') { continue; }
            if ($line[0] === '[' && $line !== '[global]') { throw new \RuntimeException('Unexpected main pool'); }
            if (preg_match('/^include\s*=/i', $line)) {
                if (!preg_match('~^include\s*=\s*/etc/php/8\.3/fpm/pool\.d/\*\.conf$~D', $line)) {
                    throw new \RuntimeException('Unsupported FPM include');
                }
                $includes++;
            }
        }
        if ($includes !== 1) { throw new \RuntimeException('Missing pool authority'); }
        foreach (glob(dirname(self::TARGET).'/*.conf') ?: [] as $path) {
            $raw = self::read($path, $path === self::TARGET ? 0600 : 0644);
            if (preg_match('/^\s*include\s*=/mi', $raw)
                || ($path !== self::TARGET && preg_match('/^\s*\[\s*gojet-installer\s*\]/mi', $raw))) {
                throw new \RuntimeException('Conflicting pool authority');
            }
        }
        $properties = [];
        foreach (explode("\n", self::command(['/usr/bin/systemctl', 'show', 'php8.3-fpm.service',
            '--property=LoadState,ActiveState,DropInPaths,ExecStart,MainPID'])) as $line) {
            if (str_contains($line, '=')) { [$key, $value] = explode('=', $line, 2); $properties[$key] = $value; }
        }
        if (($properties['LoadState'] ?? '') !== 'loaded' || ($properties['ActiveState'] ?? '') !== 'active'
            || ($properties['DropInPaths'] ?? '') !== '' || !ctype_digit($properties['MainPID'] ?? '')
            || (int)$properties['MainPID'] <= 1
            || !str_contains($properties['ExecStart'] ?? '', 'argv[]=/usr/sbin/php-fpm8.3 --nodaemonize --fpm-config '.self::MAIN.' ;')) {
            throw new \RuntimeException('Unsupported active FPM service');
        }
        $pid = (int)$properties['MainPID'];
        if (realpath('/proc/'.$pid.'/exe') !== realpath('/usr/sbin/php-fpm8.3')) { throw new \RuntimeException('Unexpected FPM executable'); }
    }

    /** Shared read-only host guard for first provisioning and later pool replacement. */
    public static function verifyHost(): void
    {
        if (!function_exists('posix_geteuid') || posix_geteuid() !== 0) { throw new \RuntimeException('Root required'); }
        foreach ([dirname(self::TARGET), dirname(self::MAIN)] as $parent) {
            CompletionLock::verifyProvisioningPath($parent.'/installed.lock');
        }
        self::service();
    }

    public static function run(array $input): array
    {
        if (!function_exists('posix_geteuid') || posix_geteuid() !== 0) { throw new \RuntimeException('Root required'); }
        foreach ([self::AUTHORITY, dirname(self::TARGET), dirname(self::MAIN)] as $parent) {
            CompletionLock::verifyProvisioningPath($parent.'/installed.lock');
        }
        $bindingPath = $input['binding']; $predecessor = $input['predecessor'];
        $state = dirname($bindingPath).'/state'; $old = $input['previous'];
        WebConfigurationPreparation::schedule($bindingPath, $state, $predecessor);
        $binding = ApplyRequest::read($bindingPath, 0);
        $retired = ApplyRequest::read($predecessor.'.retired.json', 0);
        if (($old['release'] ?? null) !== $binding['release'] || ($old['completion'] ?? null) !== self::AUTHORITY.'/installed.lock'
            || !is_string($old['state'] ?? null) || !hash_equals($retired['state_sha256'], hash('sha256', $old['state']))) {
            throw new \RuntimeException('Previous endpoint identity mismatch');
        }
        $previous = NativeWebConfig::render($old)['fpm'];
        $next = $old; $next['state'] = $state; $next['bootstrap_sha256'] = $input['bootstrap_sha256'];
        $replacement = NativeWebConfig::render($next)['fpm'];
        $base = dirname($bindingPath).'/fpm-switch';
        $identity = ['schema' => 1, 'predecessor_sha256' => hash('sha256', $predecessor),
            'binding_sha256' => hash('sha256', json_encode($binding, JSON_THROW_ON_ERROR)),
            'previous_sha256' => hash('sha256', $previous), 'replacement_sha256' => hash('sha256', $replacement)];
        foreach (['installed.lock', 'web-apply.json', 'configuration-approval.json', 'web-apply-started.json'] as $name) {
            self::absent(self::AUTHORITY.'/'.$name);
        }
        foreach (['apply-approval.json', 'apply-request.json'] as $name) { self::absent($state.'/'.$name); }
        self::absent($bindingPath.'.retired.json');
        // A single fixed destination across all attempt lineages requires a global lock too.
        $lockPath = self::AUTHORITY.'/fpm-switch.lock';
        $mask = umask(0077);
        try { $lock = @fopen($lockPath, 'x+b'); } finally { umask($mask); }
        if ($lock !== false) { fclose($lock); self::sync(self::AUTHORITY); }
        self::read($lockPath, 0600, 0);
        $lock = fopen($lockPath, 'r+b');
        if ($lock === false) { throw new \RuntimeException('Switch lock unavailable'); }
        try {
            if (!flock($lock, LOCK_EX | LOCK_NB)) { throw new \RuntimeException('Switch busy'); }
            self::service();
            $current = self::read(self::TARGET, 0600);
            $started = file_exists($base.'-started.json') || is_link($base.'-started.json');
            $ready = file_exists($base.'-ready.json') || is_link($base.'-ready.json');
            if ($started && ApplyRequest::read($base.'-started.json', 0) !== $identity) { throw new \RuntimeException('Switch request changed'); }
            if ($ready) {
                if (!$started || ApplyRequest::read($base.'-ready.json', 0) !== $identity || $current !== $replacement) {
                    throw new \RuntimeException('Switch completion mismatch');
                }
                return ['fpm_switched' => true, 'already_applied' => true, 'installation_complete' => false];
            }
            if ($current !== $previous && (!$started || $current !== $replacement)) { throw new \RuntimeException('Foreign pool preserved'); }
            // A first switch cannot redirect an already submitted/prepared successor.
            if (!$started) {
                foreach ([$state.'/configuration-draft.json', $binding['destination'], $bindingPath.'.started.json', $bindingPath.'.ready.json'] as $path) {
                    self::absent($path);
                }
            }
            self::command(['/usr/sbin/php-fpm8.3', '-t', '-y', self::MAIN]);
            if (!$started) { ApplyRequest::write($base.'-started.json', $identity); }
            if ($current === $previous) {
                // Same-directory private temporary; never included by the *.conf glob.
                $temporary = dirname(self::TARGET).'/.gojet-switch-'.bin2hex(random_bytes(16)).'.tmp';
                $mask = umask(0077);
                try { $f = fopen($temporary, 'x+b'); } finally { umask($mask); }
                if ($f === false) { throw new \RuntimeException('Pool staging failed'); }
                try {
                    if (fwrite($f, $replacement) !== strlen($replacement) || !fflush($f) || !fsync($f)
                        || self::read(self::TARGET, 0600) !== $previous || !rename($temporary, self::TARGET)) {
                        throw new \RuntimeException('Pool publication failed');
                    }
                    self::sync(dirname(self::TARGET));
                } finally { fclose($f); if (file_exists($temporary)) { unlink($temporary); } }
            }
            self::service();
            self::command(['/usr/sbin/php-fpm8.3', '-t', '-y', self::MAIN]);
            self::command(['/usr/bin/systemctl', 'reload', 'php8.3-fpm.service']);
            self::service();
            if (self::read(self::TARGET, 0600) !== $replacement) { throw new \RuntimeException('Pool changed during reload'); }
            ApplyRequest::write($base.'-ready.json', $identity);
            return ['fpm_switched' => true, 'already_applied' => false, 'installation_complete' => false];
        } finally { flock($lock, LOCK_UN); fclose($lock); }
    }
}
