<?php
declare(strict_types=1);
namespace GoJet\Installer;
require_once __DIR__.'/CompletionLock.php';
require_once __DIR__.'/ApplyRequest.php';

/** Minimal public observation, published only after the matching private completion authority. */
final class InstallationResult
{
    private const DIRECTORY = '/var/lib/gojet-installation-result';
    private const MARKER = '/var/lib/gojet-installer-authority/installed.lock';
    public static function id(string $sealHash): string
    {
        if (!preg_match('/^[a-f0-9]{64}$/D', $sealHash)) { throw new \RuntimeException('Invalid result identity'); }
        return hash('sha256', "gojet.installation-result.v1\n".$sealHash);
    }
    private static function sync(string $directory): void
    {
        $f = fopen($directory, 'r');
        if ($f === false) { throw new \RuntimeException('Result directory unavailable'); }
        try { if (!fsync($f)) { throw new \RuntimeException('Result sync failed'); } } finally { fclose($f); }
    }
    public static function publish(array $completion): void
    {
        if (!function_exists('posix_geteuid') || posix_geteuid() !== 0) { throw new \RuntimeException('Root required'); }
        CompletionLock::verifyProvisioningPath(self::MARKER);
        if (($completion['schema'] ?? null) !== 'gojet.installation-completion.v1'
            || ApplyRequest::read(self::MARKER, 0) !== $completion) { throw new \RuntimeException('Verified completion required'); }
        $value = ['schema' => 'gojet.installation-result.v1', 'result_id' => self::id($completion['seal_sha256']), 'installation_complete' => true];
        $raw = json_encode($value, JSON_THROW_ON_ERROR)."\n";
        CompletionLock::verifyProvisioningPath(dirname(self::DIRECTORY).'/installed.lock');
        clearstatcache();
        if (!file_exists(self::DIRECTORY) && !is_link(self::DIRECTORY)) {
            $mask = umask(0022);
            try { if (!mkdir(self::DIRECTORY, 0755)) { throw new \RuntimeException('Result directory creation failed'); } }
            finally { umask($mask); }
            self::sync(dirname(self::DIRECTORY));
        }
        CompletionLock::verifyProvisioningPath(self::DIRECTORY.'/installed.lock');
        if (realpath(self::DIRECTORY) !== self::DIRECTORY || (fileperms(self::DIRECTORY) & 07777) !== 0755) {
            throw new \RuntimeException('Unsafe result directory');
        }
        $path = self::DIRECTORY.'/result.json'; clearstatcache(true, $path); $s = @lstat($path);
        if ($s !== false) {
            if (($s['mode'] & 0177777) !== 0100644 || $s['uid'] !== 0 || $s['nlink'] !== 1
                || $s['size'] !== strlen($raw) || file_get_contents($path) !== $raw) { throw new \RuntimeException('Foreign result file'); }
            return;
        }
        $temp = self::DIRECTORY.'/.result-'.bin2hex(random_bytes(12));
        $mask = umask(0077);
        try { $f = fopen($temp, 'x+b'); } finally { umask($mask); }
        if ($f === false) { throw new \RuntimeException('Result staging failed'); }
        try {
            if (fwrite($f, $raw) !== strlen($raw) || !fflush($f) || !chmod($temp, 0644) || !fsync($f)
                || !link($temp, $path) || !unlink($temp)) { throw new \RuntimeException('Result publication failed'); }
            self::sync(self::DIRECTORY);
        } finally { fclose($f); if (file_exists($temp)) { unlink($temp); } }
    }
}
