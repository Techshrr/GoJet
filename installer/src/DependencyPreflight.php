<?php

declare(strict_types=1);
namespace GoJet\Installer;
require_once __DIR__.'/PackageIntegrity.php';
require_once __DIR__.'/UnprivilegedProbe.php';

/** Native Ubuntu 24.04 dependency probes; fixed argv only, no apply or final lock. */
final class DependencyPreflight
{
    private static function process(array $argv, array $env = []): array
    {
        $process = @proc_open($argv, [0 => ['file', '/dev/null', 'r'], 1 => ['pipe', 'w'], 2 => ['pipe', 'w']], $pipes, '/',
            $env + ['PATH' => '/usr/sbin:/usr/bin:/sbin:/bin']);
        if (!is_resource($process)) { throw new \RuntimeException('Probe unavailable'); }
        $out = ''; $err = ''; $deadline = microtime(true) + 15;
        stream_set_blocking($pipes[1], false); stream_set_blocking($pipes[2], false);
        try {
            do {
                $out .= stream_get_contents($pipes[1], 65536); $err .= stream_get_contents($pipes[2], 65536);
                $status = proc_get_status($process);
                if (strlen($out) + strlen($err) > 65536 || microtime(true) > $deadline) { throw new \RuntimeException('Probe exceeded bounds'); }
                if ($status['running']) { usleep(20000); }
            } while ($status['running']);
            $out .= stream_get_contents($pipes[1], 65536); $err .= stream_get_contents($pipes[2], 65536);
            if (strlen($out) + strlen($err) > 65536) { throw new \RuntimeException('Probe exceeded bounds'); }
            return ['code' => $status['exitcode'], 'out' => $out, 'err' => $err];
        } finally {
            if (proc_get_status($process)['running']) { proc_terminate($process, 9); }
            fclose($pipes[1]); fclose($pipes[2]); proc_close($process);
        }
    }

    private static function active(string $unit): bool
    {
        $r = self::process(['/usr/bin/systemctl', 'is-active', $unit]);
        return $r['code'] === 0 && trim($r['out']) === 'active';
    }

    private static function probe(callable $test): bool
    {
        try { return $test() === true; } catch (\Throwable) { return false; }
    }

    private static function redis(array $r): bool
    {
        $socket = @stream_socket_client('tcp://127.0.0.1:'.$r['port'], $errno, $error, 3);
        if ($socket === false) { return false; }
        stream_set_timeout($socket, 3);
        try {
            $send = function (array $args) use ($socket): string {
                $raw = '*'.count($args)."\r\n";
                foreach ($args as $arg) { $raw .= '$'.strlen($arg)."\r\n".$arg."\r\n"; }
                if (fwrite($socket, $raw) !== strlen($raw)) { throw new \RuntimeException('Redis write failed'); }
                $line = fgets($socket, 1024);
                if ($line === false || !str_ends_with($line, "\r\n")) { throw new \RuntimeException('Redis reply failed'); }
                return $line;
            };
            if ($r['password'] !== '' && $send(['AUTH', $r['password']]) !== "+OK\r\n") { return false; }
            if ($send(['SELECT', (string)$r['db']]) !== "+OK\r\n" || $send(['PING']) !== "+PONG\r\n") { return false; }
            // Connectivity alone accepts read-only replicas and users without data access.
            // Never touch application keys; NX prevents overwrites and EX bounds residue
            // even when the connection dies or the account cannot delete its own probe.
            $key = 'gojet:installer:preflight:'.bin2hex(random_bytes(16));
            $value = bin2hex(random_bytes(32)); $owned = false;
            try {
                if ($send(['SET', $key, $value, 'NX', 'EX', '15']) !== "+OK\r\n") { return false; }
                $owned = true;
                if ($send(['GET', $key]) !== '$'.strlen($value)."\r\n") { return false; }
                $body = '';
                while (strlen($body) < strlen($value) + 2) {
                    $chunk = fread($socket, strlen($value) + 2 - strlen($body));
                    if ($chunk === false || $chunk === '') { return false; }
                    $body .= $chunk;
                }
                if (!hash_equals($value."\r\n", $body) || $send(['DEL', $key]) !== ":1\r\n") { return false; }
                $owned = false;
                return true;
            } finally {
                if ($owned) {
                    try { $send(['DEL', $key]); } catch (\Throwable) { /* The probe expires independently. */ }
                }
            }
        } finally { fclose($socket); }
    }

    public static function check(array $p): array
    {
        if (!function_exists('posix_geteuid') || posix_geteuid() !== 0) { throw new \RuntimeException('Root preflight required'); }
        $keys = ['release', 'manifest_sha256', 'source', 'database', 'redis', 'storage', 'clamav_socket'];
        if (array_diff($keys, array_keys($p)) || array_diff(array_keys($p), $keys)) { throw new \RuntimeException('Invalid preflight request'); }
        foreach (['release', 'manifest_sha256', 'source', 'storage', 'clamav_socket'] as $key) {
            if (!is_string($p[$key]) || strlen($p[$key]) > 4096 || str_contains($p[$key], "\0")) { throw new \RuntimeException('Invalid preflight input'); }
        }
        foreach (['storage', 'clamav_socket'] as $key) { if (!str_starts_with($p[$key], '/')) { throw new \RuntimeException('Local absolute paths required'); } }
        foreach (['database' => ['port', 'name', 'user', 'password'], 'redis' => ['port', 'db', 'password']] as $name => $fields) {
            $v = $p[$name];
            if (!is_array($v) || array_diff($fields, array_keys($v)) || array_diff(array_keys($v), $fields)
                || !is_int($v['port']) || $v['port'] < 1 || $v['port'] > 65535 || !is_string($v['password'])
                || strlen($v['password']) > 1024 || str_contains($v['password'], "\0")) { throw new \RuntimeException('Invalid dependency configuration'); }
        }
        foreach (['name', 'user'] as $field) { if (!is_string($p['database'][$field]) || !preg_match('/^[A-Za-z0-9_]{1,64}$/D', $p['database'][$field])) { throw new \RuntimeException('Invalid database identity'); } }
        if (!is_int($p['redis']['db']) || $p['redis']['db'] < 0 || $p['redis']['db'] > 15) { throw new \RuntimeException('Invalid Redis database'); }
        $checks = [];
        $checks['package'] = self::probe(fn() => PackageIntegrity::verify($p['release'], $p['manifest_sha256'], $p['source'])['integrity_verified']);
        $checks['systemd'] = self::probe(function (): bool {
            $r = self::process(['/usr/bin/systemctl', 'show', '--property=Version', '--value']);
            return $r['code'] === 0 && preg_match('/^[0-9]+/', trim($r['out'])) === 1 && is_dir('/run/systemd/system');
        });
        $checks['nginx'] = self::probe(function (): bool {
            $version = self::process(['/usr/sbin/nginx', '-v']);
            return $version['code'] === 0 && str_contains($version['err'], 'nginx/')
                && self::process(['/usr/sbin/nginx', '-t'])['code'] === 0 && self::active('nginx.service');
        });
        $checks['php'] = self::probe(function (): bool {
            $r = self::process(['/usr/sbin/php-fpm8.3', '-v']);
            return PHP_MAJOR_VERSION === 8 && PHP_MINOR_VERSION === 3 && extension_loaded('pdo_mysql')
                && extension_loaded('openssl') && $r['code'] === 0 && preg_match('/^PHP 8\.3\./', $r['out']) === 1
                && self::process(['/usr/sbin/php-fpm8.3', '-t'])['code'] === 0 && self::active('php8.3-fpm.service');
        });
        $checks['mysql'] = self::probe(function () use ($p): bool {
            $d = $p['database'];
            $db = new \PDO('mysql:host=127.0.0.1;port='.$d['port'].';dbname='.$d['name'].';charset=utf8mb4', $d['user'], $d['password'],
                [\PDO::ATTR_ERRMODE => \PDO::ERRMODE_EXCEPTION, \PDO::ATTR_TIMEOUT => 3]);
            $row = $db->query('SELECT VERSION(), @@autocommit, @@log_bin, @@log_bin_trust_function_creators')->fetch(\PDO::FETCH_NUM);
            return preg_match('/^8\./', $row[0]) === 1 && (int)$row[1] === 1 && ((int)$row[2] === 0 || (int)$row[3] === 1);
        });
        $checks['redis'] = self::probe(fn() => self::redis($p['redis']));
        $checks['storage'] = false; $checks['clamav'] = false;
        if ($checks['package']) {
            try {
                $r = self::process(UnprivilegedProbe::command($p['release'], 'filepreflight',
                    ['GOJET_FILE_STORAGE_ROOT' => $p['storage'], 'GOJET_CLAMAV_NETWORK' => 'unix', 'GOJET_CLAMAV_ADDRESS' => $p['clamav_socket']]));
                $report = json_decode($r['out'], true, 16, JSON_THROW_ON_ERROR);
                // The inherited helper reports storage and scanner failures independently.
                if (in_array($r['code'], [0, 2], true) && $r['err'] === '') {
                    $checks['storage'] = ($report['storage']['state'] ?? '') === 'healthy' && ($report['storage']['writable'] ?? false) === true;
                    $checks['clamav'] = ($report['clamav']['state'] ?? '') === 'healthy'
                        && !empty($report['clamav']['engine_version']) && !empty($report['clamav']['signature_version']);
                }
            } catch (\Throwable) { /* Fail closed without exposing paths, credentials or raw service output. */ }
        }
        return ['source' => $p['source'], 'checks' => $checks, 'ready' => !in_array(false, $checks, true), 'installation_complete' => false];
    }
}
