<?php

declare(strict_types=1);
namespace GoJet\Installer;
require_once __DIR__.'/CompletionLock.php';

/** Render transport-safe systemd EnvironmentFiles; business configuration validation is separate. */
final class ServiceEnvironment
{
    private const SERVICES = ['redirectengine', 'analyticsworker', 'analyticsreconciler', 'platformapi',
        'mailworker', 'fileworker', 'operationsmonitor', 'logreceiver'];

    public static function render(array $services): array
    {
        if (array_diff(self::SERVICES, array_keys($services)) || array_diff(array_keys($services), self::SERVICES)) {
            throw new \RuntimeException('Exactly eight services required');
        }
        $files = []; $total = 0;
        foreach (self::SERVICES as $service) {
            $variables = $services[$service];
            if (!is_array($variables) || count($variables) < 1 || count($variables) > 256) { throw new \RuntimeException('Invalid service variables'); }
            ksort($variables, SORT_STRING); $content = '';
            foreach ($variables as $key => $value) {
                if (!is_string($key) || !preg_match('/^GOJET_[A-Z][A-Z0-9_]{0,119}$/D', $key)
                    || preg_match('/^GOJET_(?:TEST(?:_|$)|P[0-9]+(?:_|$))/', $key)
                    || !is_string($value) || strlen($value) > 8192 || preg_match('/[\x00-\x1f\x7f]/', $value)
                    || !preg_match('//u', $value)) { throw new \RuntimeException('Unsafe service environment'); }
                // EnvironmentFile double quotes: escape only backslash and quote.
                // Dollar, backtick and percent are literal; this is never a shell script.
                $content .= $key.'="'.str_replace(['\\', '"'], ['\\\\', '\\"'], $value)."\"\n";
            }
            $total += strlen($content);
            if ($total > 131072) { throw new \RuntimeException('Environment exceeds limit'); }
            $files[$service.'.env'] = $content;
        }
        return $files;
    }

    private static function syncDirectory(string $path): void
    {
        $f = fopen($path, 'r');
        if ($f === false) { throw new \RuntimeException('Directory unavailable'); }
        try { if (!fsync($f)) { throw new \RuntimeException('Directory sync failed'); } } finally { fclose($f); }
    }

    public static function create(string $destination, array $services): array
    {
        if (!function_exists('posix_geteuid') || posix_geteuid() !== 0) { throw new \RuntimeException('Root required'); }
        $files = self::render($services); // Validate everything before creating output.
        $parent = dirname($destination);
        if (!preg_match('/^[A-Za-z0-9][A-Za-z0-9_.-]{0,63}$/D', basename($destination))
            || $destination !== $parent.'/'.basename($destination)) { throw new \RuntimeException('Unsafe destination'); }
        CompletionLock::verifyProvisioningPath($parent.'/installed.lock');
        $lockPath = $parent.'/.environment.lock';
        if (is_link($lockPath)) { throw new \RuntimeException('Unsafe lock'); }
        $old = umask(0077);
        try { $lock = fopen($lockPath, 'c+b'); } finally { umask($old); }
        if ($lock === false) { throw new \RuntimeException('Lock unavailable'); }
        $stage = null;
        try {
            $stat = fstat($lock);
            if (is_link($lockPath) || $stat['uid'] !== 0 || ($stat['mode'] & 0177777) !== 0100600 || $stat['nlink'] !== 1
                || !flock($lock, LOCK_EX | LOCK_NB)) { throw new \RuntimeException('Unsafe or busy lock'); }
            clearstatcache();
            if (file_exists($destination) || is_link($destination)) { throw new \RuntimeException('Destination already exists'); }
            $stage = $parent.'/.environment-'.bin2hex(random_bytes(16));
            if (!mkdir($stage, 0700)) { throw new \RuntimeException('Staging failed'); }
            foreach ($files as $name => $content) {
                $old = umask(0077);
                try { $f = fopen($stage.'/'.$name, 'x+b'); } finally { umask($old); }
                if ($f === false) { throw new \RuntimeException('Environment write failed'); }
                try {
                    if (fwrite($f, $content) !== strlen($content) || !fflush($f) || !fsync($f)) { throw new \RuntimeException('Environment sync failed'); }
                } finally { fclose($f); }
            }
            self::syncDirectory($stage);
            if (!rename($stage, $destination)) { throw new \RuntimeException('Environment publish failed'); }
            $stage = null;
            self::syncDirectory($parent);
            return ['created' => true, 'files' => count($files), 'installation_complete' => false];
        } finally {
            if ($stage !== null && is_dir($stage)) {
                foreach (array_keys($files) as $name) { if (is_file($stage.'/'.$name)) { unlink($stage.'/'.$name); } }
                rmdir($stage);
            }
            flock($lock, LOCK_UN); fclose($lock);
        }
    }
}
