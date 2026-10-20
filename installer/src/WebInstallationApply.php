<?php
declare(strict_types=1);
namespace GoJet\Installer;
require_once __DIR__.'/InstallationApply.php';
require_once __DIR__.'/InstallState.php';
require_once __DIR__.'/ApplyRequest.php';

/** One root-approved plan, one unprivileged request, one explicit root invocation. */
final class WebInstallationApply
{
    private const AUTHORITY = '/var/lib/gojet-installer-authority';
    private static function root(): void
    {
        if (!function_exists('posix_geteuid') || posix_geteuid() !== 0) { throw new \RuntimeException('Root required'); }
        CompletionLock::verifyProvisioningPath(self::AUTHORITY.'/installed.lock');
        if ((new CompletionLock(self::AUTHORITY.'/installed.lock'))->exists()) { throw new \RuntimeException('Installer unavailable'); }
    }
    private static function stateParent(string $directory): void
    {
        if ($directory === '' || $directory[0] !== '/' || basename($directory) === '.' || basename($directory) === '..'
            || realpath(dirname($directory)) !== dirname($directory)) { throw new \RuntimeException('Unsafe web state path'); }
        for ($p = dirname($directory); ; $p = dirname($p)) {
            if (fileowner($p) !== 0 || (fileperms($p) & 0022) !== 0) { throw new \RuntimeException('Unsafe web state ancestor'); }
            if ($p === '/') { break; }
        }
    }
    public static function provision(string $prepared, string $directory, string $bootstrapHash): array
    {
        self::root(); self::stateParent($directory);
        $seal = InstallationPreparation::verify($prepared);
        $account = posix_getpwnam('gojet-installer');
        if ($account === false || $account['uid'] === 0) { throw new \RuntimeException('Installer account required'); }
        if (!preg_match('/^[a-f0-9]{64}$/D', $bootstrapHash)) { throw new \RuntimeException('Invalid bootstrap digest'); }
        $request = json_decode((string)file_get_contents($prepared.'/dependencies.json'), true, 32, JSON_THROW_ON_ERROR);
        foreach ([$prepared, $seal['release'], $request['storage']] as $protected) {
            if ($protected !== '' && ($directory === $protected || str_starts_with($directory, $protected.'/') || str_starts_with($protected, $directory.'/'))) {
                throw new \RuntimeException('Web state overlaps installation');
            }
        }
        // Exclusive fixed root binding is also the provisioning barrier. Never overwrite it.
        $approval = ['schema' => 'gojet.web-apply.v1', 'seal_sha256' => hash_file('sha256', $prepared.'/seal.json')];
        ApplyRequest::write(self::AUTHORITY.'/web-apply.json', ['schema' => 1, 'prepared' => $prepared,
            'directory' => $directory, 'uid' => $account['uid'], 'approval' => $approval]);
        if (file_exists($directory) || is_link($directory) || !mkdir($directory, 0700)) { throw new \RuntimeException('Web state already exists'); }
        $state = new InstallState($directory, $bootstrapHash);
        $state->initializePreparedApply();
        ApplyRequest::write($directory.'/apply-approval.json', $approval);
        foreach (['state.json', 'state.lock', 'apply-approval.json'] as $name) {
            if (!chown($directory.'/'.$name, $account['uid']) || !chgrp($directory.'/'.$name, $account['gid'])) { throw new \RuntimeException('Web state ownership failed'); }
        }
        if (!chown($directory, $account['uid']) || !chgrp($directory, $account['gid'])) { throw new \RuntimeException('Web state ownership failed'); }
        return ['source' => $seal['source'], 'web_apply_provisioned' => true, 'installation_complete' => false];
    }
    public static function run(): array
    {
        self::root();
        $binding = ApplyRequest::read(self::AUTHORITY.'/web-apply.json', 0);
        if (array_keys($binding) !== ['schema', 'prepared', 'directory', 'uid', 'approval'] || $binding['schema'] !== 1
            || !is_string($binding['prepared']) || !is_string($binding['directory']) || !is_int($binding['uid']) || $binding['uid'] <= 0) {
            throw new \RuntimeException('Invalid root apply binding');
        }
        $directory = $binding['directory']; self::stateParent($directory);
        clearstatcache(true, $directory);
        if (realpath($directory) !== $directory || !is_dir($directory) || fileowner($directory) !== $binding['uid']
            || (fileperms($directory) & 0777) !== 0700) { throw new \RuntimeException('Unsafe web state'); }
        $request = ApplyRequest::read($directory.'/apply-request.json', $binding['uid']);
        if ($request !== $binding['approval'] || !hash_equals($binding['approval']['seal_sha256'], (string)hash_file('sha256', $binding['prepared'].'/seal.json'))) {
            throw new \RuntimeException('Apply request does not match root approval');
        }
        InstallationPreparation::verify($binding['prepared']);
        // Exclusive durable receipt before any database/service mutation. A failed or killed
        // invocation requires operator inspection; even root cannot blindly replay this mailbox.
        ApplyRequest::write(self::AUTHORITY.'/web-apply-started.json', $binding['approval']);
        return InstallationApply::run($binding['prepared']);
    }
}
