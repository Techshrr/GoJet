<?php

declare(strict_types=1);
namespace GoJet\Installer;
require_once __DIR__.'/InstallationCompletion.php';

/** Root-only orchestration of a pre-approved sealed plan. No automatic recovery. */
final class InstallationApply
{
    private const AUTHORITY = '/var/lib/gojet-installer-authority';
    private static function lock(string $name)
    {
        $path = self::AUTHORITY.'/'.$name;
        clearstatcache(true, $path);
        if (file_exists($path) || is_link($path)) { self::verifyLock($path); }
        $previous = umask(0077);
        try { $h = fopen($path, 'c+b'); } finally { umask($previous); }
        if ($h === false) { throw new \RuntimeException('Apply lock unavailable'); }
        try {
            self::verifyLock($path);
            if (!flock($h, LOCK_EX | LOCK_NB)) { throw new \RuntimeException('Apply busy'); }
            return $h;
        } catch (\Throwable $e) { fclose($h); throw $e; }
    }
    private static function verifyLock(string $path): void
    {
        clearstatcache(true, $path); $s = @lstat($path);
        if ($s === false || ($s['mode'] & 0177777) !== 0100600 || $s['uid'] !== 0 || $s['nlink'] !== 1 || $s['size'] !== 0) {
            throw new \RuntimeException('Unsafe apply lock');
        }
    }
    public static function run(string $directory, ?callable $observe = null): array
    {
        if (!function_exists('posix_geteuid') || posix_geteuid() !== 0) { throw new \RuntimeException('Root required'); }
        CompletionLock::verifyProvisioningPath(self::AUTHORITY.'/installed.lock');
        $apply = self::lock('installation-apply.lock');
        try {
            if ((new CompletionLock(self::AUTHORITY.'/installed.lock'))->exists()) {
                return InstallationCompletion::run($directory, true);
            }
            // Serialize data against site/service/finalization operations too. Each later
            // phase owns this same lock itself; do not recursively acquire it.
            $phase = self::lock('service-activation.lock');
            try {
                if ((new CompletionLock(self::AUTHORITY.'/installed.lock'))->exists()) { throw new \RuntimeException('Installation complete'); }
                if ($observe !== null) { $observe('data'); }
                InstallationPreparation::runData($directory);
            } finally { flock($phase, LOCK_UN); fclose($phase); }
            // These shipped phase authorities revalidate identity/readiness and refuse
            // failed/interrupted journals. No deletion, compensation or blind retry here.
            if ($observe !== null) { $observe('services'); }
            ServiceActivation::run($directory);
            if ($observe !== null) { $observe('site'); }
            SiteActivation::run($directory);
            if ($observe !== null) { $observe('health'); }
            return InstallationCompletion::run($directory, true);
        } finally { flock($apply, LOCK_UN); fclose($apply); }
    }
}
