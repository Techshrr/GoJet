<?php
declare(strict_types=1);
namespace GoJet\Installer;
require_once __DIR__.'/InstallationPreparation.php';
require_once __DIR__.'/NativeWebConfig.php';
require_once __DIR__.'/ApplyRequest.php';

/** Root authorizes one exact bootstrap vhost for one sealed product plan. */
final class EntryHandover
{
    private const AUTHORITY = '/var/lib/gojet-installer-authority';
    private const SITE = '/etc/nginx/conf.d/gojet-installer.conf';
    private const SAVED = '/etc/nginx/conf.d/gojet-installer.conf.handover';

    private static function exists(string $path): bool
    {
        clearstatcache(true, $path);
        return file_exists($path) || is_link($path);
    }
    private static function read(string $path, int $mode): string
    {
        CompletionLock::verifyProvisioningPath(dirname($path).'/installed.lock');
        clearstatcache(true, $path); $s = @lstat($path);
        if ($s === false || realpath($path) !== $path || ($s['mode'] & 0177777) !== (0100000 | $mode)
            || $s['uid'] !== 0 || $s['nlink'] !== 1 || $s['size'] > 8192) { throw new \RuntimeException('Unsafe entry handover file'); }
        $raw = file_get_contents($path);
        if ($raw === false || strlen($raw) > 8192) { throw new \RuntimeException('Entry handover read failed'); }
        return $raw;
    }
    private static function sync(): void
    {
        $f = fopen(dirname(self::SITE), 'r');
        if ($f === false) { throw new \RuntimeException('Handover directory unavailable'); }
        try { if (!fsync($f)) { throw new \RuntimeException('Handover sync failed'); } } finally { fclose($f); }
    }
    private static function identity(string $prepared, string $entry): array
    {
        if (!preg_match('~^/(?:[A-Za-z0-9_.-]+/)*[A-Za-z0-9_.-]+$~D', $entry)
            || realpath($entry) !== $entry) { throw new \RuntimeException('Invalid entry authority'); }
        $seal = InstallationPreparation::verify($prepared);
        $startedRaw = self::read(self::AUTHORITY.'/web-entry-started.json', 0600);
        $readyRaw = self::read(self::AUTHORITY.'/web-entry-ready.json', 0600);
        $receipt = json_decode($readyRaw, true, 16, JSON_THROW_ON_ERROR);
        if ($readyRaw !== $startedRaw || ($receipt['schema'] ?? null) !== 1
            || ($receipt['directory_sha256'] ?? null) !== hash('sha256', $entry)) { throw new \RuntimeException('Entry receipt mismatch'); }
        $webRaw = self::read($entry.'/web.json', 0600); $bindingRaw = self::read($entry.'/binding.json', 0600);
        if (($receipt['private']['web.json'] ?? null) !== hash('sha256', $webRaw)
            || ($receipt['private']['binding.json'] ?? null) !== hash('sha256', $bindingRaw)) { throw new \RuntimeException('Entry input changed'); }
        $web = json_decode($webRaw, true, 16, JSON_THROW_ON_ERROR);
        $binding = json_decode($bindingRaw, true, 16, JSON_THROW_ON_ERROR);
        $request = json_decode(self::read($prepared.'/data/request.json', 0600), true, 16, JSON_THROW_ON_ERROR);
        foreach (['release', 'source', 'manifest_sha256'] as $key) {
            if (($binding[$key] ?? null) !== $seal[$key]) { throw new \RuntimeException('Different entry release'); }
        }
        if (($web['origin'] ?? null) !== $request['origin'] || ($web['release'] ?? null) !== $seal['release']
            || ($web['completion'] ?? null) !== self::AUTHORITY.'/installed.lock') { throw new \RuntimeException('Different entry destination'); }
        foreach (['certificate' => 'certificate_sha256', 'private_key' => 'key_sha256'] as $key => $digest) {
            $path = $web[$key] ?? '';
            if (($seal['tls_files'][$path]['sha256'] ?? null) !== ($receipt[$digest] ?? null)
                || !isset($receipt[$digest])) { throw new \RuntimeException('Different entry TLS'); }
        }
        $nginx = NativeWebConfig::render($web)['nginx'];
        if (($receipt['site_sha256'] ?? null) !== hash('sha256', $nginx)) { throw new \RuntimeException('Invalid entry render'); }
        return ['schema' => 1, 'entry' => $entry, 'seal_sha256' => hash_file('sha256', $prepared.'/seal.json'),
            'receipt_sha256' => hash('sha256', $readyRaw), 'site_sha256' => hash('sha256', $nginx)];
    }
    /** Explicit root preparation. Does not approve or execute the installation. */
    public static function authorize(string $prepared, string $entry): array
    {
        if (!function_exists('posix_geteuid') || posix_geteuid() !== 0) { throw new \RuntimeException('Root required'); }
        self::read(self::AUTHORITY.'/service-activation.lock', 0600);
        $lock = fopen(self::AUTHORITY.'/service-activation.lock', 'r+b');
        if ($lock === false) { throw new \RuntimeException('Handover lock unavailable'); }
        try {
            if (!flock($lock, LOCK_EX | LOCK_NB)) { throw new \RuntimeException('Activation busy'); }
            foreach ([self::AUTHORITY.'/installed.lock', self::AUTHORITY.'/web-apply-started.json', $prepared.'/site.json', self::SAVED] as $path) {
                if (self::exists($path)) { throw new \RuntimeException('Handover already started'); }
            }
            $identity = self::identity($prepared, $entry);
            if (hash('sha256', self::read(self::SITE, 0644)) !== $identity['site_sha256']) { throw new \RuntimeException('Foreign entry configuration'); }
            $path = $prepared.'/entry-handover.json';
            $already = self::exists($path);
            if ($already) {
                if (ApplyRequest::read($path, 0) !== $identity) { throw new \RuntimeException('Different handover approval'); }
            } else { ApplyRequest::write($path, $identity); }
            return ['entry_handover_authorized' => true, 'already_applied' => $already, 'installation_complete' => false];
        } finally { flock($lock, LOCK_UN); fclose($lock); }
    }
    /** Caller holds service-activation.lock. Keep original FPM/state for explicit recovery. */
    public static function load(string $prepared): ?array
    {
        $path = $prepared.'/entry-handover.json';
        if (!self::exists($path)) { return null; }
        $binding = ApplyRequest::read($path, 0);
        if (!is_string($binding['entry'] ?? null) || $binding !== self::identity($prepared, $binding['entry'])) {
            throw new \RuntimeException('Handover authority changed');
        }
        return $binding;
    }
    public static function fingerprint(?array $binding): ?string
    {
        return $binding === null ? null : hash('sha256', json_encode($binding, JSON_THROW_ON_ERROR));
    }
    /** Exactly one original inode must exist, either active or parked outside *.conf. */
    public static function parked(array $binding): bool
    {
        $active = self::exists(self::SITE); $saved = self::exists(self::SAVED);
        if ($active === $saved) { throw new \RuntimeException('Ambiguous entry handover state'); }
        $raw = self::read($saved ? self::SAVED : self::SITE, 0644);
        if (!hash_equals($binding['site_sha256'], hash('sha256', $raw))) { throw new \RuntimeException('Changed entry configuration'); }
        return $saved;
    }
    public static function park(array $binding): void
    {
        if (self::parked($binding)) { throw new \RuntimeException('Entry already parked'); }
        // Both paths are under a protected root directory and the shared activation lock.
        if (!rename(self::SITE, self::SAVED)) { throw new \RuntimeException('Entry retirement failed'); }
        self::sync();
    }
    public static function restore(array $binding): void
    {
        if (self::parked($binding)) {
            if (!rename(self::SAVED, self::SITE)) { throw new \RuntimeException('Entry restoration failed'); }
            self::sync();
        }
    }
}
