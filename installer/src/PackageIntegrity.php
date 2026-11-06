<?php

declare(strict_types=1);
namespace GoJet\Installer;
require_once __DIR__.'/CompletionLock.php';

/** Read-only root release verification. A valid payload is not an installable release. */
final class PackageIntegrity
{
    private static function file(string $path, int $mode, int $limit): array
    {
        clearstatcache(true, $path);
        $s = @lstat($path);
        if ($s === false || ($s['mode'] & 0170000) !== 0100000 || $s['uid'] !== 0
            || ($s['mode'] & 07777) !== $mode || $s['nlink'] !== 1 || $s['size'] > $limit) {
            throw new \RuntimeException('Unsafe package file');
        }
        return $s;
    }

    public static function verify(string $root, string $manifestHash, string $source): array
    {
        if (!function_exists('posix_geteuid') || posix_geteuid() !== 0
            || !preg_match('/^[a-f0-9]{64}$/D', $manifestHash) || !preg_match('/^[a-f0-9]{40}$/D', $source)) {
            throw new \RuntimeException('Invalid package verification authority');
        }
        CompletionLock::verifyProvisioningPath($root.'/installed.lock');
        self::file($root.'/manifest.json', 0644, 16 * 1024 * 1024);
        $raw = file_get_contents($root.'/manifest.json');
        if (!hash_equals($manifestHash, hash('sha256', $raw))) { throw new \RuntimeException('Unapproved manifest'); }
        $m = json_decode($raw, true, 32, JSON_THROW_ON_ERROR);
        if (($m['schema'] ?? '') !== 'gojet.native-payload.v1' || ($m['source'] ?? '') !== $source
            || ($m['phase'] ?? '') !== 'payload-only-not-installable' || ($m['installable'] ?? null) !== false
            || !is_array($m['files'] ?? null) || count($m['files']) < 1 || count($m['files']) > 100000
            || !is_string($m['version'] ?? null) || !preg_match('/^[A-Za-z0-9][A-Za-z0-9.+-]{0,63}$/D', $m['version'])
            || !is_string($m['sbom_sha256'] ?? null) || !preg_match('/^[a-f0-9]{64}$/D', $m['sbom_sha256'])) {
            throw new \RuntimeException('Invalid package manifest');
        }
        $expected = ['manifest.json' => true, 'sbom.cdx.json' => true, 'SHA256SUMS' => true];
        foreach ($m['files'] as $name => $row) {
            if (!is_string($name) || ($name === '' || $name[0] === '/' || str_contains($name, '\\') || preg_match('/[\x00-\x1f\x7f]/', $name))
                || str_contains($name, '//') || array_filter(explode('/', $name), fn($p) => $p === '' || $p[0] === '.')
                || isset($expected[$name]) || !is_array($row) || count($row) !== 3
                || !is_int($row['bytes'] ?? null) || $row['bytes'] < 0 || $row['bytes'] > 128 * 1024 * 1024
                || ($row['mode'] ?? null) !== (str_starts_with($name, 'bin/') ? 0755 : 0644)
                || !is_string($row['sha256'] ?? null) || !preg_match('/^[a-f0-9]{64}$/D', $row['sha256'])) {
                throw new \RuntimeException('Invalid package member');
            }
            $expected[$name] = true;
        }
        // Verify the entire extracted tree, including unexpected files and directories.
        $seen = []; $total = 0;
        $walk = function (string $directory, string $prefix) use (&$walk, &$seen, &$total, $expected, $m): void {
            $entries = scandir($directory);
            if ($entries === false) { throw new \RuntimeException('Unreadable package directory'); }
            foreach ($entries as $entry) {
                if ($entry === '.' || $entry === '..') { continue; }
                $name = $prefix.$entry; $path = $directory.'/'.$entry;
                $s = @lstat($path);
                if ($s === false) { throw new \RuntimeException('Missing package member'); }
                if (($s['mode'] & 0170000) === 0040000) {
                    if ($s['uid'] !== 0 || ($s['mode'] & 07777) !== 0755 || strlen($name) > 1024
                        || !array_filter(array_keys($expected), fn($p) => str_starts_with($p, $name.'/'))) {
                        throw new \RuntimeException('Unsafe package directory');
                    }
                    $walk($path, $name.'/');
                    continue;
                }
                if (!isset($expected[$name])) { throw new \RuntimeException('Unexpected package member'); }
                $row = $m['files'][$name] ?? null;
                $s = self::file($path, $row['mode'] ?? 0644, 128 * 1024 * 1024);
                $total += $s['size'];
                if ($total > 1024 * 1024 * 1024) { throw new \RuntimeException('Package exceeds limit'); }
                $hash = hash_file('sha256', $path);
                if ($row !== null && ($s['size'] !== $row['bytes'] || !hash_equals($row['sha256'], $hash))) {
                    throw new \RuntimeException('Package integrity failure');
                }
                $seen[$name] = $hash;
            }
        };
        $walk($root, '');
        if (count($seen) !== count($expected) || !hash_equals($manifestHash, $seen['manifest.json'] ?? '')
            || !hash_equals($m['sbom_sha256'], $seen['sbom.cdx.json'] ?? '')) { throw new \RuntimeException('Incomplete package'); }
        ksort($seen, SORT_STRING); $sums = '';
        foreach ($seen as $name => $hash) { if ($name !== 'SHA256SUMS') { $sums .= $hash.'  '.$name."\n"; } }
        if (!hash_equals($sums, file_get_contents($root.'/SHA256SUMS'))) { throw new \RuntimeException('Invalid package checksum inventory'); }
        return ['source' => $source, 'manifest_sha256' => $manifestHash, 'members' => count($seen),
            'integrity_verified' => true, 'installable' => false];
    }
}
