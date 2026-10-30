<?php
declare(strict_types=1);
namespace GoJet\Installer;
require_once __DIR__.'/UpgradeIO.php';

/** Atomic pointer for versioned unit/vhost snapshots. Caller owns maintenance and service locks. */
final class UpgradeSwitch
{
    public const SERVICES = ['logreceiver','redirectengine','analyticsworker','analyticsreconciler','platformapi','mailworker','fileworker','operationsmonitor'];
    public static function targets(): array
    {
        $paths = ['nginx.conf' => '/etc/nginx/conf.d/gojet.conf'];
        foreach (self::SERVICES as $name) { $paths['gojet-'.$name.'.service'] = '/etc/systemd/system/gojet-'.$name.'.service'; }
        return $paths;
    }
    public static function snapshot(string $directory, array $files): array
    {
        UpgradeIO::directory(dirname($directory));
        if (array_diff(array_keys($files), array_keys(self::targets())) || count($files) !== count(self::targets())) {
            throw new \RuntimeException('Incomplete upgrade configuration');
        }
        if (!@mkdir($directory, 0700)) { throw new \RuntimeException('Snapshot already exists'); }
        $hashes = [];
        foreach ($files as $name => $raw) {
            if (!is_string($raw) || strlen($raw) > 1048576) { throw new \RuntimeException('Invalid upgrade configuration'); }
            UpgradeIO::write($directory.'/'.$name, $raw, 0644); $hashes[$name] = hash('sha256', $raw);
        }
        UpgradeIO::record($directory.'/inventory.json', $hashes); UpgradeIO::sync($directory);
        return $hashes;
    }
    public static function verify(string $directory, array $hashes): void
    {
        UpgradeIO::directory($directory);
        if (count($hashes) !== count(self::targets()) || array_diff(array_keys($hashes), array_keys(self::targets()))) {
            throw new \RuntimeException('Invalid configuration inventory');
        }
        $names = scandir($directory); sort($names);
        $expected = ['.', '..', 'inventory.json', ...array_keys(self::targets())]; sort($expected);
        if ($names !== $expected || UpgradeIO::json($directory.'/inventory.json') !== $hashes) {
            throw new \RuntimeException('Changed configuration inventory');
        }
        foreach ($hashes as $name => $hash) {
            if (!is_string($hash) || !hash_equals($hash, hash('sha256', UpgradeIO::read($directory.'/'.$name, 0644)))) {
                throw new \RuntimeException('Changed upgrade configuration');
            }
        }
    }
    public static function point(string $pointer, string $expected, string $next): void
    {
        UpgradeIO::directory(dirname($pointer));
        clearstatcache(true, $pointer);
        if (!is_link($pointer) || lstat($pointer)['uid'] !== 0 || readlink($pointer) !== $expected) {
            throw new \RuntimeException('Foreign active release pointer');
        }
        UpgradeIO::directory($next);
        $tmp = dirname($pointer).'/.pointer-'.bin2hex(random_bytes(16));
        try {
            if (!symlink($next, $tmp) || !rename($tmp, $pointer)) { throw new \RuntimeException('Active pointer switch failed'); }
            clearstatcache(true);
            UpgradeIO::sync(dirname($pointer));
        } finally { if (is_link($tmp)) { unlink($tmp); } }
    }
    /** Initial adoption is resumable only for exact old bytes or our exact owned links. */
    public static function adopt(string $pointer, string $snapshot, array $hashes, ?array $originalHashes = null): void
    {
        self::verify($snapshot, $hashes); UpgradeIO::directory(dirname($pointer));
        if (file_exists($pointer) || is_link($pointer)) {
            if (!is_link($pointer) || lstat($pointer)['uid'] !== 0 || readlink($pointer) !== $snapshot) {
                throw new \RuntimeException('Foreign active pointer');
            }
        } else {
            if (!symlink($snapshot, $pointer)) { throw new \RuntimeException('Active pointer publication failed'); }
            UpgradeIO::sync(dirname($pointer));
        }
        // Validate every current target BEFORE replacing any of them.
        foreach (self::targets() as $name => $target) {
            if (is_link($target)) {
                if (lstat($target)['uid'] !== 0 || readlink($target) !== $pointer.'/'.$name) { throw new \RuntimeException('Foreign live configuration link'); }
            } elseif (!hash_equals(($originalHashes ?? $hashes)[$name], hash('sha256', UpgradeIO::read($target, 0644)))) {
                throw new \RuntimeException('Live configuration changed');
            }
        }
        foreach (self::targets() as $name => $target) {
            if (is_link($target)) { continue; }
            $tmp = dirname($target).'/.gojet-upgrade-'.bin2hex(random_bytes(16));
            try {
                if (!symlink($pointer.'/'.$name, $tmp) || !rename($tmp, $target)) { throw new \RuntimeException('Live configuration adoption failed'); }
                clearstatcache(true);
                UpgradeIO::sync(dirname($target));
            } finally { if (is_link($tmp)) { unlink($tmp); } }
        }
    }
    public static function live(string $pointer, string $snapshot, array $hashes): void
    {
        self::verify($snapshot, $hashes);
        if (!is_link($pointer) || lstat($pointer)['uid'] !== 0 || readlink($pointer) !== $snapshot) {
            throw new \RuntimeException('Active snapshot mismatch');
        }
        foreach (self::targets() as $name => $target) {
            if (!is_link($target) || lstat($target)['uid'] !== 0 || readlink($target) !== $pointer.'/'.$name
                || realpath($target) !== $snapshot.'/'.$name) { throw new \RuntimeException('Active configuration mismatch'); }
        }
    }
}
