<?php
declare(strict_types=1);
namespace GoJet\Installer;
require_once __DIR__.'/CompletionLock.php';
require_once __DIR__.'/ApplyRequest.php';
require_once __DIR__.'/InstallationResult.php';

/** Root-owned approval only; FPM reads it without receiving private plan paths or credentials. */
final class ConfigurationApproval
{
    private static function validate(array $value): void
    {
        if (array_keys($value) !== ['schema', 'state_sha256', 'draft_sha256', 'seal_sha256'] || $value['schema'] !== 1) {
            throw new \RuntimeException('Invalid configuration approval');
        }
        foreach (['state_sha256', 'draft_sha256', 'seal_sha256'] as $key) {
            if (!is_string($value[$key]) || !preg_match('/^[a-f0-9]{64}$/D', $value[$key])) {
                throw new \RuntimeException('Invalid configuration approval');
            }
        }
    }
    public static function publish(string $authority, string $state, string $draftHash, string $sealHash, int $gid): void
    {
        if (!function_exists('posix_geteuid') || posix_geteuid() !== 0 || $gid <= 0) { throw new \RuntimeException('Root required'); }
        CompletionLock::verifyProvisioningPath($authority.'/installed.lock');
        if ((new CompletionLock($authority.'/installed.lock'))->exists()) { throw new \RuntimeException('Installer unavailable'); }
        $value = ['schema' => 1, 'state_sha256' => hash('sha256', $state), 'draft_sha256' => $draftHash, 'seal_sha256' => $sealHash];
        self::validate($value);
        $path = $authority.'/configuration-approval.json';
        ApplyRequest::write($path, $value); // Exclusive durable root-only barrier, never overwrite.
        if (!chgrp($path, $gid) || !chmod($path, 0640)) { throw new \RuntimeException('Approval permissions failed'); }
        $f = fopen($path, 'rb');
        if ($f === false) { throw new \RuntimeException('Approval unavailable'); }
        try { if (!fsync($f)) { throw new \RuntimeException('Approval sync failed'); } } finally { fclose($f); }
    }
    /** Authenticated observation only; no draft approval or completion authority. */
    public static function resultId(string $authority, string $state): ?string
    {
        try {
            $path = $authority.'/configuration-approval.json'; clearstatcache(true, $path); $s = @lstat($path);
            if ($s === false || ($s['mode'] & 0177777) !== 0100640 || $s['uid'] !== 0
                || $s['gid'] !== filegroup($state) || $s['nlink'] !== 1 || $s['size'] > 4096) { return null; }
            $raw = file_get_contents($path);
            if ($raw === false || strlen($raw) > 4096) { return null; }
            $value = json_decode($raw, true, 8, JSON_THROW_ON_ERROR); self::validate($value);
            if (!hash_equals($value['state_sha256'], hash('sha256', $state))) { return null; }
            return InstallationResult::id($value['seal_sha256']);
        } catch (\Throwable) { return null; }
    }
    public static function read(string $authority, string $state, string $draftHash): array
    {
        if ((new CompletionLock($authority.'/installed.lock'))->exists()) { throw new \RuntimeException('Installer unavailable'); }
        $path = $authority.'/configuration-approval.json'; clearstatcache(); $s = @lstat($path);
        if ($s === false || ($s['mode'] & 0177777) !== 0100640 || $s['uid'] !== 0
            || $s['gid'] !== filegroup($state) || $s['nlink'] !== 1 || $s['size'] > 4096) {
            throw new \RuntimeException('Unsafe configuration approval');
        }
        $f = @fopen($path, 'rb');
        if ($f === false) { throw new \RuntimeException('Approval unavailable'); }
        try {
            $opened = fstat($f);
            if ($opened['dev'] !== $s['dev'] || $opened['ino'] !== $s['ino']) { throw new \RuntimeException('Approval changed'); }
            $raw = stream_get_contents($f, 4097);
        } finally { fclose($f); }
        if ($raw === false || strlen($raw) > 4096) { throw new \RuntimeException('Invalid configuration approval'); }
        $value = json_decode($raw, true, 8, JSON_THROW_ON_ERROR); self::validate($value);
        if (!hash_equals($value['state_sha256'], hash('sha256', $state)) || !hash_equals($value['draft_sha256'], $draftHash)) {
            throw new \RuntimeException('Configuration approval mismatch');
        }
        return ['schema' => 'gojet.web-apply.v1', 'seal_sha256' => $value['seal_sha256']];
    }
}
