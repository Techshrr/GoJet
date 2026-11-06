<?php
declare(strict_types=1);
namespace GoJet\Installer;
require_once __DIR__.'/CompletionLock.php';

/** Root execution observation. Never a completion authority or permission to retry. */
final class ApplyProgress
{
    private const PHASES = ['starting', 'data', 'services', 'site', 'health'];
    private const TTL = 120;
    private static function authority(string $directory): void
    {
        if (!function_exists('posix_geteuid') || posix_geteuid() !== 0) { throw new \RuntimeException('Root required'); }
        CompletionLock::verifyProvisioningPath($directory.'/installed.lock');
        if ((new CompletionLock($directory.'/installed.lock'))->exists()) { throw new \RuntimeException('Installer unavailable'); }
    }
    private static function sync(string $directory): void
    {
        $f = fopen($directory, 'r');
        if ($f === false) { throw new \RuntimeException('Snapshot directory unavailable'); }
        try { if (!fsync($f)) { throw new \RuntimeException('Snapshot directory sync failed'); } }
        finally { fclose($f); }
    }
    public static function publish(string $authority, string $state, string $phase, string $status): array
    {
        self::authority($authority);
        $value = ['schema' => 1, 'state_sha256' => hash('sha256', $state), 'updated_at' => time(), 'phase' => $phase, 'status' => $status];
        self::validate($value);
        $stage = $authority.'/.apply-progress-'.bin2hex(random_bytes(16));
        $old = umask(0077);
        try { $f = fopen($stage, 'x+b'); } finally { umask($old); }
        if ($f === false) { throw new \RuntimeException('Snapshot publication failed'); }
        try {
            $raw = json_encode($value, JSON_THROW_ON_ERROR)."\n";
            if (fwrite($f, $raw) !== strlen($raw) || !fflush($f) || !fsync($f) || !chmod($stage, 0644)) {
                throw new \RuntimeException('Snapshot sync failed');
            }
            if (!rename($stage, $authority.'/apply-progress.json')) { throw new \RuntimeException('Snapshot publication failed'); }
        } finally { fclose($f); if (file_exists($stage)) { unlink($stage); } }
        self::sync($authority);
        return self::read($authority, $state);
    }
    private static function validate(array $value): void
    {
        if (array_keys($value) !== ['schema', 'state_sha256', 'updated_at', 'phase', 'status'] || $value['schema'] !== 1
            || !is_string($value['state_sha256']) || !preg_match('/^[a-f0-9]{64}$/D', $value['state_sha256'])
            || !is_int($value['updated_at']) || $value['updated_at'] <= 0
            || !in_array($value['phase'], self::PHASES, true)
            || !in_array($value['status'], ['running', 'failed'], true)) { throw new \RuntimeException('Invalid execution observation'); }
    }
    public static function read(string $authority, string $state): array
    {
        $unavailable = ['status' => 'unavailable', 'phase' => null, 'updated_at' => null, 'installation_complete' => false];
        try {
            if ((new CompletionLock($authority.'/installed.lock'))->exists()) { return $unavailable; }
            $path = $authority.'/apply-progress.json'; clearstatcache(true, $path); $s = @lstat($path);
            if ($s === false || ($s['mode'] & 0177777) !== 0100644 || $s['uid'] !== 0 || $s['nlink'] !== 1 || $s['size'] > 4096) { return $unavailable; }
            $f = @fopen($path, 'rb');
            if ($f === false) { return $unavailable; }
            try {
                $opened = fstat($f);
                if ($opened['dev'] !== $s['dev'] || $opened['ino'] !== $s['ino']) { return $unavailable; }
                $raw = stream_get_contents($f, 4097);
            } finally { fclose($f); }
            if ($raw === false || strlen($raw) > 4096) { return $unavailable; }
            $value = json_decode($raw, true, 8, JSON_THROW_ON_ERROR);
            self::validate($value);
            if (!hash_equals($value['state_sha256'], hash('sha256', $state))) { return $unavailable; }
            $age = time() - $value['updated_at'];
            if ($age < 0) { return $unavailable; }
            if ($value['status'] === 'running' && $age > self::TTL) { return array_replace($unavailable, ['status' => 'stale']); }
            return ['status' => $value['status'], 'phase' => $value['phase'],
                'updated_at' => $value['updated_at'], 'installation_complete' => false];
        } catch (\Throwable) { return $unavailable; }
    }
}
