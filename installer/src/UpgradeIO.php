<?php
declare(strict_types=1);
namespace GoJet\Installer;
require_once __DIR__.'/CompletionLock.php';

/** Shared private filesystem/process boundary for the root upgrade transaction. */
final class UpgradeIO
{
    public static function root(): void
    {
        if (PHP_SAPI !== 'cli' || !function_exists('posix_geteuid') || posix_geteuid() !== 0) {
            throw new \RuntimeException('Root upgrade authority required');
        }
    }
    public static function disjoint(array $paths): void
    {
        foreach ($paths as $i=>$path) {
            if (!is_string($path) || !preg_match('~^/(?:[A-Za-z0-9_.-]+/)*[A-Za-z0-9_.-]+$~D',$path)
                || preg_match('~(?:^|/)\.{1,2}(?:/|$)~',$path)) { throw new \RuntimeException('Unsafe upgrade path'); }
            foreach (array_slice($paths,$i+1) as $other) {
                if ($path===$other || str_starts_with($path,$other.'/') || str_starts_with($other,$path.'/')) {
                    throw new \RuntimeException('Upgrade paths overlap');
                }
            }
        }
    }
    public static function directory(string $path, int $mode = 0700): void
    {
        self::root(); CompletionLock::verifyProvisioningPath($path.'/installed.lock');
        if ((fileperms($path) & 07777) !== $mode) { throw new \RuntimeException('Unsafe upgrade directory'); }
    }
    public static function read(string $path, int $mode = 0600, int $limit = 1048576): string
    {
        CompletionLock::verifyProvisioningPath(dirname($path).'/installed.lock');
        clearstatcache(true, $path); $s = @lstat($path);
        if ($s === false || ($s['mode'] & 0177777) !== (0100000 | $mode) || $s['uid'] !== 0
            || $s['nlink'] !== 1 || $s['size'] > $limit) { throw new \RuntimeException('Unsafe upgrade file'); }
        $raw = file_get_contents($path);
        if ($raw === false) { throw new \RuntimeException('Upgrade file unavailable'); }
        return $raw;
    }
    public static function sync(string $path): void
    {
        $h = fopen($path, 'r');
        if ($h === false) { throw new \RuntimeException('Upgrade sync unavailable'); }
        try { if (!fsync($h)) { throw new \RuntimeException('Upgrade sync failed'); } } finally { fclose($h); }
    }
    public static function write(string $path, string $raw, int $mode = 0600, bool $replace = false): void
    {
        self::root(); CompletionLock::verifyProvisioningPath(dirname($path).'/installed.lock');
        if ($replace) { self::read($path, $mode); }
        elseif (file_exists($path) || is_link($path)) { throw new \RuntimeException('Upgrade file already exists'); }
        $tmp = dirname($path).'/.upgrade-'.bin2hex(random_bytes(16));
        $mask = umask(0077);
        try { $h = fopen($tmp, 'xb'); } finally { umask($mask); }
        if ($h === false) { throw new \RuntimeException('Upgrade write unavailable'); }
        try {
            if (fwrite($h, $raw) !== strlen($raw) || !chmod($tmp, $mode) || !fflush($h) || !fsync($h)) {
                throw new \RuntimeException('Upgrade write failed');
            }
            if ($replace) {
                if (!rename($tmp, $path)) { throw new \RuntimeException('Upgrade publication failed'); }
            } else {
                if (!link($tmp, $path) || !unlink($tmp)) { throw new \RuntimeException('Upgrade publication failed'); }
            }
            self::sync(dirname($path));
        } finally { fclose($h); if (is_file($tmp)) { unlink($tmp); } }
    }
    public static function json(string $path): array
    {
        $value = json_decode(self::read($path), true, 32, JSON_THROW_ON_ERROR);
        if (!is_array($value)) { throw new \RuntimeException('Invalid upgrade record'); }
        return $value;
    }
    public static function record(string $path, array $value): void
    {
        self::write($path, json_encode($value, JSON_THROW_ON_ERROR)."\n", 0600, file_exists($path));
    }
    public static function command(array $args, int $seconds = 45, ?string $input = null, array $env = []): string
    {
        $p = proc_open($args, [0 => ['file', $input ?? '/dev/null', 'r'], 1 => ['pipe', 'w'], 2 => ['pipe', 'w']], $pipes,
            '/', $env + ['PATH' => '/usr/sbin:/usr/bin:/sbin:/bin', 'LC_ALL' => 'C']);
        if (!is_resource($p)) { throw new \RuntimeException('Upgrade command unavailable'); }
        $out = ''; $err = ''; $deadline = microtime(true) + $seconds;
        stream_set_blocking($pipes[1], false); stream_set_blocking($pipes[2], false);
        try {
            do {
                $out .= stream_get_contents($pipes[1], 65536); $err .= stream_get_contents($pipes[2], 65536);
                $state = proc_get_status($p);
                if (strlen($out) + strlen($err) > 262144 || microtime(true) >= $deadline) {
                    throw new \RuntimeException('Upgrade command exceeded bound');
                }
                if (!$state['running']) { break; }
                usleep(20000);
            } while (true);
            $out .= stream_get_contents($pipes[1], 65536); $err .= stream_get_contents($pipes[2], 65536);
            if ($state['exitcode'] !== 0 || strlen($out) + strlen($err) > 262144) {
                throw new \RuntimeException('Upgrade command failed');
            }
            return $out;
        } finally {
            if (proc_get_status($p)['running']) { proc_terminate($p, 9); }
            foreach ($pipes as $h) { fclose($h); } proc_close($p);
        }
    }
    public static function connection(array $d, bool $select = true): \PDO
    {
        foreach (['name', 'user'] as $key) {
            if (!is_string($d[$key] ?? null) || !preg_match('/^[A-Za-z0-9_]{1,64}$/D', $d[$key])) {
                throw new \RuntimeException('Invalid upgrade database identity');
            }
        }
        if (($d['host'] ?? '') !== '127.0.0.1' || !is_int($d['port'] ?? null) || $d['port'] < 1 || $d['port'] > 65535
            || !is_string($d['password'] ?? null) || strlen($d['password']) > 1024 || preg_match('/[\x00-\x1f\x7f]/', $d['password'])) {
            throw new \RuntimeException('Invalid upgrade database connection');
        }
        try {
            $db = new \PDO('mysql:host=127.0.0.1;port='.$d['port'].($select ? ';dbname='.$d['name'] : '').';charset=utf8mb4', $d['user'], $d['password'],
                [\PDO::ATTR_ERRMODE => \PDO::ERRMODE_EXCEPTION, \PDO::ATTR_TIMEOUT => 10]);
            if (!preg_match('/^8\./', (string)$db->query('SELECT VERSION()')->fetchColumn())) { throw new \RuntimeException(); }
            return $db;
        } catch (\Throwable) { throw new \RuntimeException('Upgrade database unavailable'); }
    }
}
