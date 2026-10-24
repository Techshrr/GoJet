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
}
