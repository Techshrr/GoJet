<?php
declare(strict_types=1);
namespace GoJet\Installer;
require_once __DIR__.'/ApplyRequest.php';

/** Untrusted, bounded configuration data. A stored draft is never root approval. */
final class ConfigurationDraft
{
    private static function keys(mixed $value, array $keys): void
    {
        if (!is_array($value) || array_diff($keys, array_keys($value)) || array_diff(array_keys($value), $keys)) {
            throw new \RuntimeException('Invalid configuration draft');
        }
    }

    public static function validate(array $draft): void
    {
        self::keys($draft, ['configuration', 'admin']);
        self::keys($draft['configuration'], ['schema', 'origin', 'database', 'redis', 'files',
            'quotas', 'smtp', 'risk', 'turnstile_secret', 'auth_rate']);
        self::keys($draft['configuration']['files'], ['max_upload_bytes', 'max_files', 'max_bytes', 'types']);
        self::keys($draft['admin'], ['email', 'display_name', 'password']);
        try { $raw = json_encode($draft, JSON_THROW_ON_ERROR, 16); }
        catch (\Throwable) { throw new \RuntimeException('Invalid configuration draft'); }
        if (strlen($raw) + 1 > 24576) { throw new \RuntimeException('Configuration draft exceeds limit'); }
        // Semantic/native validation remains mandatory in root preparation.
    }

    /** Called under InstallState's lock after the durable checking barrier is committed. */
    public static function publish(string $directory, array $draft): void
    {
        self::validate($draft);
        ApplyRequest::write($directory.'/configuration-draft.json', $draft);
    }

    /** Read one bounded snapshot; callers supply the provisioned account, never a request UID. */
    public static function frozen(string $directory, int $uid): array
    {
        clearstatcache();
        $dir = @lstat($directory);
        if ($dir === false || realpath($directory) !== $directory || ($dir['mode'] & 0177777) !== 0040700
            || $dir['uid'] !== $uid) { throw new \RuntimeException('Unsafe draft directory'); }
        $state = ApplyRequest::read($directory.'/state.json', $uid);
        if (($state['schema'] ?? null) !== 2 || ($state['step'] ?? null) !== 0
            || ($state['status'] ?? null) !== 'checking') { throw new \RuntimeException('Draft is not frozen'); }
        $path = $directory.'/configuration-draft.json';
        $s = @lstat($path);
        if ($s === false || ($s['mode'] & 0177777) !== 0100600 || $s['uid'] !== $uid
            || $s['nlink'] !== 1 || $s['size'] > 24576) { throw new \RuntimeException('Unsafe configuration draft'); }
        $f = @fopen($path, 'rb');
        if ($f === false) { throw new \RuntimeException('Configuration draft unavailable'); }
        try {
            $opened = fstat($f);
            if ($opened['dev'] !== $s['dev'] || $opened['ino'] !== $s['ino']) {
                throw new \RuntimeException('Configuration draft changed');
            }
            $raw = stream_get_contents($f, 24577);
            if ($raw === false || strlen($raw) > 24576) { throw new \RuntimeException('Configuration draft exceeds limit'); }
        } finally { fclose($f); }
        $draft = json_decode($raw, true, 16, JSON_THROW_ON_ERROR);
        self::validate($draft);
        clearstatcache();
        $after = @lstat($directory);
        if ($after === false || $after['dev'] !== $dir['dev'] || $after['ino'] !== $dir['ino']) {
            throw new \RuntimeException('Draft directory changed');
        }
        return ['draft' => $draft, 'sha256' => hash('sha256', $raw)];
    }
}
