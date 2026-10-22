<?php
declare(strict_types=1);
namespace GoJet\Installer;
require_once __DIR__.'/InstallationApply.php';
require_once __DIR__.'/InstallState.php';
require_once __DIR__.'/ApplyRequest.php';
require_once __DIR__.'/PreflightSnapshot.php';
require_once __DIR__.'/ApplyProgress.php';

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
    /** Render pinned scheduler units for explicit root provisioning. */
    public static function schedule(): array
    {
        self::root();
        $binding = ApplyRequest::read(self::AUTHORITY.'/web-apply.json', 0);
        if (($binding['schema'] ?? null) !== 1 || !is_string($binding['prepared'] ?? null)
            || !is_string($binding['approval']['seal_sha256'] ?? null)
            || !hash_equals($binding['approval']['seal_sha256'], (string)@hash_file('sha256', $binding['prepared'].'/seal.json'))) {
            throw new \RuntimeException('Invalid root apply binding');
        }
        $seal = InstallationPreparation::verify($binding['prepared']);
        $release = $seal['release'];
        if (!preg_match('~^/(?:[A-Za-z0-9_.-]+/)*[A-Za-z0-9_.-]+$~D', $release)
            || in_array('.', explode('/', $release), true) || in_array('..', explode('/', $release), true)
            || realpath($release) !== $release) { throw new \RuntimeException('Unsafe scheduler release'); }
        $authority = self::AUTHORITY;
        return [
            'gojet-installer-preflight.service' => <<<UNIT
[Unit]
Description=GoJet installer dependency observation
After=network.target
ConditionPathExists={$authority}/web-apply.json
ConditionPathExists=!{$authority}/installed.lock

[Service]
Type=oneshot
User=root
Group=root
ExecStart=/usr/bin/php8.3 {$release}/installer/bin/refresh-web-preflight.php
TimeoutStartSec=110
TimeoutStopSec=5
KillMode=control-group
UMask=0077
NoNewPrivileges=yes
ProtectSystem=full
ProtectHome=yes
ProtectKernelTunables=yes
ProtectKernelModules=yes
ProtectControlGroups=yes
RestrictSUIDSGID=yes
StandardOutput=null
StandardError=journal

UNIT,
            'gojet-installer-preflight.timer' => <<<UNIT
[Unit]
Description=Refresh GoJet installer dependency observations
ConditionPathExists={$authority}/web-apply.json
ConditionPathExists=!{$authority}/installed.lock

[Timer]
OnActiveSec=1s
OnUnitInactiveSec=30s
AccuracySec=1s
Unit=gojet-installer-preflight.service

[Install]
WantedBy=timers.target

UNIT,
        ];
    }

    /** Explicitly authorized root executor; never accepts a web-selected command or path. */
    public static function applySchedule(): array
    {
        // Reuse the sealed-release validation used by observation scheduling.
        self::schedule();
        $binding = ApplyRequest::read(self::AUTHORITY.'/web-apply.json', 0);
        $seal = InstallationPreparation::verify($binding['prepared']);
        $release = $seal['release'];
        $directory = $binding['directory'] ?? null;
        if (!is_string($directory) || !preg_match('~^/(?:[A-Za-z0-9_.-]+/)*[A-Za-z0-9_.-]+$~D', $directory)
            || in_array('.', explode('/', $directory), true) || in_array('..', explode('/', $directory), true)
            || realpath($directory) !== $directory) { throw new \RuntimeException('Unsafe executor state path'); }
        self::stateParent($directory);
        $authority = self::AUTHORITY;
        return [
            'gojet-installer-apply.service' => <<<UNIT
[Unit]
Description=Execute the root-approved GoJet installation request
After=network.target
ConditionPathExists={$authority}/web-apply.json
ConditionPathExists={$directory}/apply-request.json
ConditionPathExists=!{$authority}/web-apply-started.json
ConditionPathExists=!{$authority}/installed.lock

[Service]
Type=oneshot
User=root
Group=root
ExecStart=/usr/bin/php8.3 {$release}/installer/bin/run-web-apply.php
TimeoutStartSec=900
TimeoutStopSec=5
KillMode=control-group
UMask=0077
NoNewPrivileges=yes
ProtectHome=yes
ProtectKernelTunables=yes
ProtectKernelModules=yes
ProtectControlGroups=yes
RestrictSUIDSGID=yes
StandardOutput=null
StandardError=journal

UNIT,
            'gojet-installer-apply.timer' => <<<UNIT
[Unit]
Description=Observe requests for the root-approved GoJet installation
ConditionPathExists={$authority}/web-apply.json
ConditionPathExists=!{$authority}/installed.lock

[Timer]
OnActiveSec=1s
OnCalendar=*-*-* *:*:00/10
AccuracySec=1s
Unit=gojet-installer-apply.service

[Install]
WantedBy=timers.target

UNIT,
        ];
    }

    /** Explicit root refresh; FPM never executes a probe or chooses a prepared path. */
    public static function publishPreflight(): array
    {
        self::root();
        $binding = ApplyRequest::read(self::AUTHORITY.'/web-apply.json', 0);
        if (array_keys($binding) !== ['schema', 'prepared', 'directory', 'uid', 'approval'] || $binding['schema'] !== 1
            || !is_string($binding['prepared']) || !is_string($binding['directory']) || !is_int($binding['uid']) || $binding['uid'] <= 0
            || !is_array($binding['approval']) || array_keys($binding['approval']) !== ['schema', 'seal_sha256']
            || $binding['approval']['schema'] !== 'gojet.web-apply.v1'
            || !is_string($binding['approval']['seal_sha256']) || !preg_match('/^[a-f0-9]{64}$/D', $binding['approval']['seal_sha256'])) {
            throw new \RuntimeException('Invalid root apply binding');
        }
        // Serialize refreshes using the immutable root-owned binding inode.
        $lock = fopen(self::AUTHORITY.'/web-apply.json', 'rb');
        if ($lock === false) { throw new \RuntimeException('Preflight refresh unavailable'); }
        try {
            if (!flock($lock, LOCK_EX | LOCK_NB)) { throw new \RuntimeException('Preflight refresh busy'); }
            self::stateParent($binding['directory']);
            // A killed refresh leaves no old success behind.
            PreflightSnapshot::invalidate(self::AUTHORITY);
            $started = time(); $checks = null;
            try {
                if (!hash_equals($binding['approval']['seal_sha256'], (string)@hash_file('sha256', $binding['prepared'].'/seal.json'))) {
                    throw new \RuntimeException('Prepared seal changed');
                }
                $checks = InstallationPreparation::preflight($binding['prepared'])['checks'];
            } catch (\Throwable) { /* Publish only unavailable, never private diagnostics. */ }
            return PreflightSnapshot::publish(self::AUTHORITY, $binding['directory'], $started, $checks);
        } finally { flock($lock, LOCK_UN); fclose($lock); }
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
        $phase = 'starting';
        try {
            return InstallationApply::run($binding['prepared'], static function (string $next) use ($directory, &$phase): void {
                $phase = $next;
                ApplyProgress::publish(self::AUTHORITY, $directory, $phase, 'running');
            });
        } catch (\Throwable $error) {
            // Retain the original failure and all replay barriers even if observation publication fails.
            try { ApplyProgress::publish(self::AUTHORITY, $directory, $phase, 'failed'); } catch (\Throwable) {}
            throw $error;
        }
    }
}
