<?php
declare(strict_types=1);
namespace GoJet\Installer;

/** Bounded mailbox data; never a command, path, configuration or completion authority. */
final class ApplyRequest
{
    public static function read(string $path, int $uid): array
    {
        clearstatcache(true, $path); $s = @lstat($path);
        if ($s === false || ($s['mode'] & 0177777) !== 0100600 || $s['uid'] !== $uid || $s['nlink'] !== 1 || $s['size'] > 4096) {
            throw new \RuntimeException('Unsafe apply mailbox');
        }
        $f = fopen($path, 'rb');
        if ($f === false) { throw new \RuntimeException('Apply mailbox unavailable'); }
        try {
            $opened = fstat($f);
            if ($opened['dev'] !== $s['dev'] || $opened['ino'] !== $s['ino']) { throw new \RuntimeException('Apply mailbox changed'); }
            $raw = stream_get_contents($f, 4097);
            if ($raw === false || strlen($raw) > 4096) { throw new \RuntimeException('Invalid apply mailbox'); }
            $value = json_decode($raw, true, 16, JSON_THROW_ON_ERROR);
            if (!is_array($value)) { throw new \RuntimeException('Invalid apply mailbox'); }
            return $value;
        } finally { fclose($f); }
    }
    public static function write(string $path, array $value): void
    {
        $old = umask(0077);
        try { $f = @fopen($path, 'x+b'); } finally { umask($old); }
        if ($f === false) { throw new \RuntimeException('Apply mailbox already exists'); }
        try {
            $raw = json_encode($value, JSON_THROW_ON_ERROR)."\n";
            if (fwrite($f, $raw) !== strlen($raw) || !fflush($f) || !fsync($f)) { throw new \RuntimeException('Apply mailbox sync failed'); }
        } finally { fclose($f); }
        $dir = fopen(dirname($path), 'r');
        if ($dir === false) { throw new \RuntimeException('Apply mailbox directory unavailable'); }
        try { if (!fsync($dir)) { throw new \RuntimeException('Apply mailbox directory sync failed'); } }
        finally { fclose($dir); }
    }
    public static function enqueue(string $directory): void
    {
        $approval = self::read($directory.'/apply-approval.json', fileowner($directory));
        if (array_keys($approval) !== ['schema', 'seal_sha256'] || $approval['schema'] !== 'gojet.web-apply.v1'
            || !is_string($approval['seal_sha256']) || !preg_match('/^[a-f0-9]{64}$/D', $approval['seal_sha256'])) {
            throw new \RuntimeException('Invalid apply approval');
        }
        self::write($directory.'/apply-request.json', $approval);
    }
}
