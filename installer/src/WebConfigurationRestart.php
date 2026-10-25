<?php
declare(strict_types=1);
namespace GoJet\Installer;
require_once __DIR__.'/InstallationPreparation.php';
require_once __DIR__.'/InstallState.php';

/** Invoked under the preparation worker lock. Publishes a fresh, isolated attempt. */
final class WebConfigurationRestart
{
    private static function absent(string $path): void
    {
        clearstatcache(true, $path);
        if (file_exists($path) || is_link($path)) { throw new \RuntimeException('Restart boundary already occupied'); }
    }
    private static function sync(string $path): void
    {
        $f = fopen($path, 'r');
        if ($f === false) { throw new \RuntimeException('Restart sync unavailable'); }
        try { if (!fsync($f)) { throw new \RuntimeException('Restart sync failed'); } }
        finally { fclose($f); }
    }
    public static function create(string $bindingPath, string $directory, string $successor, string $draftHash, string $bootstrapHash): array
    {
        if (!function_exists('posix_geteuid') || posix_geteuid() !== 0
            || !preg_match('/^[a-f0-9]{64}$/D', $draftHash) || !preg_match('/^[a-f0-9]{64}$/D', $bootstrapHash)) {
            throw new \RuntimeException('Invalid restart authority');
        }
        $authority = '/var/lib/gojet-installer-authority';
        foreach ([dirname($bindingPath), dirname($directory), dirname($successor), $authority] as $parent) {
            CompletionLock::verifyProvisioningPath($parent.'/installed.lock');
        }
        foreach ([$bindingPath, $directory, $successor] as $path) {
            if (!preg_match('~^/(?:[A-Za-z0-9_.-]+/)*[A-Za-z0-9_.-]+$~D', $path)
                || in_array('.', explode('/', $path), true) || in_array('..', explode('/', $path), true)) {
                throw new \RuntimeException('Unsafe restart path');
            }
        }
        $binding = ApplyRequest::read($bindingPath, 0);
        $fields = ['schema', 'destination', 'release', 'manifest_sha256', 'source', 'tls', 'files'];
        if (array_diff($fields, array_keys($binding)) || array_diff(array_keys($binding), $fields)
            || $binding['schema'] !== 'gojet.web-configuration-binding.v1' || !is_string($binding['destination'])) {
            throw new \RuntimeException('Invalid preparation binding');
        }
        CompletionLock::verifyProvisioningPath(dirname($binding['destination']).'/installed.lock');
        // A published plan has generated keys and a different recovery procedure.
        foreach ([$successor, $binding['destination'], $bindingPath.'.ready.json', $bindingPath.'.retired.json',
            $bindingPath.'.replacement-ready.json', $authority.'/installed.lock', $authority.'/web-apply.json',
            $authority.'/configuration-approval.json', $authority.'/web-apply-started.json',
            $directory.'/apply-approval.json', $directory.'/apply-request.json'] as $path) { self::absent($path); }
        foreach ([$directory, $binding['release'], $binding['destination'], $authority, $bindingPath] as $protected) {
            if ($successor === $protected || str_starts_with($successor, $protected.'/') || str_starts_with($protected, $successor.'/')) {
                throw new \RuntimeException('Restart path overlaps protected state');
            }
        }
        PackageIntegrity::verify($binding['release'], $binding['manifest_sha256'], $binding['source']);
        $account = posix_getpwnam('gojet-installer');
        if ($account === false || $account['uid'] <= 0 || $account['gid'] <= 0) { throw new \RuntimeException('Installer account required'); }
        $snapshot = ConfigurationDraft::frozen($directory, $account['uid']);
        if (!hash_equals($draftHash, $snapshot['sha256'])) { throw new \RuntimeException('Frozen draft mismatch'); }
        $identity = ['schema' => 1, 'binding_sha256' => hash('sha256', json_encode($binding, JSON_THROW_ON_ERROR)),
            'state_sha256' => hash('sha256', $directory)];
        $started = $bindingPath.'.started.json';
        if ((file_exists($started) || is_link($started)) && ApplyRequest::read($started, 0) !== $identity) {
            throw new \RuntimeException('Interrupted attempt identity mismatch');
        }
        // Retire first, durably, under the shared worker lock. A partial restart stays closed.
        ApplyRequest::write($bindingPath.'.retired.json', $identity + ['draft_sha256' => $draftHash,
            'successor_sha256' => hash('sha256', $successor)]);
        if (!mkdir($successor, 0700)) { throw new \RuntimeException('Successor publication refused'); }
        self::sync(dirname($successor));
        $next = $binding; $next['destination'] = $successor.'/prepared';
        ApplyRequest::write($successor.'/binding.json', $next);
        if (!mkdir($successor.'/state', 0700)) { throw new \RuntimeException('New state publication failed'); }
        // Initialize while the entire new tree is still root0700; no existing FPM file is opened for writing.
        (new InstallState($successor.'/state', $bootstrapHash))->initialize();
        foreach (['state.json', 'state.lock'] as $name) {
            $path = $successor.'/state/'.$name;
            if (!chown($path, $account['uid']) || !chgrp($path, $account['gid'])) { throw new \RuntimeException('New state ownership failed'); }
            self::sync($path);
        }
        if (!chown($successor.'/state', $account['uid']) || !chgrp($successor.'/state', $account['gid'])) {
            throw new \RuntimeException('New directory ownership failed');
        }
        self::sync($successor.'/state'); self::sync($successor);
        ApplyRequest::write($bindingPath.'.replacement-ready.json', ['schema' => 1,
            'successor_sha256' => hash('sha256', $successor),
            'binding_sha256' => hash('sha256', json_encode($next, JSON_THROW_ON_ERROR))]);
        // Read/traverse only: binding remains root0600 and state installer0700.
        if (!chmod($successor, 0755)) { throw new \RuntimeException('New attempt activation failed'); }
        self::sync($successor); self::sync(dirname($successor));
        return ['restarted' => true, 'previous_attempt_retired' => true, 'binding' => $successor.'/binding.json',
            'state' => $successor.'/state', 'prepared' => $successor.'/prepared', 'installation_complete' => false];
    }
}
