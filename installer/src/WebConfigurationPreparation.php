<?php
declare(strict_types=1);
namespace GoJet\Installer;
require_once __DIR__.'/InstallationPreparation.php';

/** Root preparation worker and scheduler; approval and execution remain separate operations. */
final class WebConfigurationPreparation
{
    /** Validate the root-selected inputs before publishing a fixed preparation timer. */
    public static function schedule(string $bindingPath, string $directory): array
    {
        if (!function_exists('posix_geteuid') || posix_geteuid() !== 0) { throw new \RuntimeException('Root required'); }
        $authority = '/var/lib/gojet-installer-authority';
        CompletionLock::verifyProvisioningPath($authority.'/installed.lock');
        if ((new CompletionLock($authority.'/installed.lock'))->exists()) { throw new \RuntimeException('Installer unavailable'); }
        CompletionLock::verifyProvisioningPath(dirname($bindingPath).'/installed.lock');
        CompletionLock::verifyProvisioningPath(dirname($directory).'/installed.lock');
        $binding = ApplyRequest::read($bindingPath, 0);
        $fields = ['schema', 'destination', 'release', 'manifest_sha256', 'source', 'tls', 'files'];
        if (array_diff($fields, array_keys($binding)) || array_diff(array_keys($binding), $fields)
            || $binding['schema'] !== 'gojet.web-configuration-binding.v1') { throw new \RuntimeException('Invalid preparation binding'); }
        // Literal systemd arguments: reject specifiers, whitespace, escapes and ambiguous paths.
        foreach ([$bindingPath, $directory, $binding['release'], $binding['destination']] as $path) {
            if (!is_string($path) || !preg_match('~^/(?:[A-Za-z0-9_.-]+/)*[A-Za-z0-9_.-]+$~D', $path)
                || in_array('.', explode('/', $path), true) || in_array('..', explode('/', $path), true)) {
                throw new \RuntimeException('Unsafe scheduler path');
            }
        }
        CompletionLock::verifyProvisioningPath(dirname($binding['destination']).'/installed.lock');
        $account = posix_getpwnam('gojet-installer');
        clearstatcache(); $dir = @lstat($directory);
        if ($account === false || $account['uid'] <= 0 || $dir === false || realpath($directory) !== $directory
            || ($dir['mode'] & 0177777) !== 0040700 || $dir['uid'] !== $account['uid']) {
            throw new \RuntimeException('Unsafe configuration directory');
        }
        $state = ApplyRequest::read($directory.'/state.json', $account['uid']);
        if (($state['schema'] ?? null) !== 2 || ($state['step'] ?? null) !== 0
            || !in_array($state['status'] ?? null, ['ready', 'checking'], true)) { throw new \RuntimeException('Invalid preparation state'); }
        $release = $binding['release'];
        PackageIntegrity::verify($release, $binding['manifest_sha256'], $binding['source']);
        return [
            'gojet-installer-preparation.service' => <<<UNIT
[Unit]
Description=Prepare the submitted GoJet configuration for root approval
After=network.target
ConditionPathExists={$bindingPath}
ConditionPathExists={$directory}/configuration-draft.json
ConditionPathExists=!{$bindingPath}.started.json
ConditionPathExists=!{$bindingPath}.ready.json
ConditionPathExists=!{$authority}/installed.lock

[Service]
Type=oneshot
User=root
Group=root
ExecStart=/usr/bin/php8.3 {$release}/installer/bin/run-web-preparation.php {$bindingPath} {$directory}
TimeoutStartSec=300
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
            'gojet-installer-preparation.timer' => <<<UNIT
[Unit]
Description=Observe submitted GoJet configuration
ConditionPathExists={$bindingPath}
ConditionPathExists=!{$authority}/installed.lock

[Timer]
OnActiveSec=1s
OnCalendar=*-*-* *:*:00/10
AccuracySec=1s
Unit=gojet-installer-preparation.service

[Install]
WantedBy=timers.target

UNIT,
        ];
    }

    public static function run(string $bindingPath, string $directory): array
    {
        if (!function_exists('posix_geteuid') || posix_geteuid() !== 0) { throw new \RuntimeException('Root required'); }
        CompletionLock::verifyProvisioningPath(dirname($bindingPath).'/installed.lock');
        CompletionLock::verifyProvisioningPath(dirname($directory).'/installed.lock');
        if ((new CompletionLock('/var/lib/gojet-installer-authority/installed.lock'))->exists()) {
            throw new \RuntimeException('Installer unavailable');
        }
        // Operator binding fits the private mailbox bound; never accept paths from web input.
        $binding = ApplyRequest::read($bindingPath, 0);
        $fields = ['schema', 'destination', 'release', 'manifest_sha256', 'source', 'tls', 'files'];
        if (array_diff($fields, array_keys($binding)) || array_diff(array_keys($binding), $fields)
            || $binding['schema'] !== 'gojet.web-configuration-binding.v1' || !is_string($binding['destination'])) {
            throw new \RuntimeException('Invalid preparation binding');
        }
        $identity = ['schema' => 1, 'binding_sha256' => hash('sha256', json_encode($binding, JSON_THROW_ON_ERROR)),
            'state_sha256' => hash('sha256', $directory)];
        $started = $bindingPath.'.started.json'; $ready = $bindingPath.'.ready.json';
        if (file_exists($ready) || is_link($ready)) {
            $receipt = ApplyRequest::read($ready, 0);
            $attempt = ApplyRequest::read($started, 0);
            if ($attempt !== $identity || array_keys($receipt) !== [...array_keys($identity), 'seal_sha256']
                || array_intersect_key($receipt, $identity) !== $identity) {
                throw new \RuntimeException('Preparation receipt mismatch');
            }
            InstallationPreparation::verify($binding['destination']);
            if (!is_string($receipt['seal_sha256']) || !hash_equals($receipt['seal_sha256'], hash_file('sha256', $binding['destination'].'/seal.json'))) {
                throw new \RuntimeException('Prepared plan changed');
            }
            return ['prepared' => true, 'already_prepared' => true, 'installation_complete' => false];
        }
        if (file_exists($started) || is_link($started)) { throw new \RuntimeException('Preparation interrupted'); }
        $account = posix_getpwnam('gojet-installer');
        clearstatcache(); $dir = @lstat($directory);
        if ($account === false || $account['uid'] <= 0 || $dir === false || realpath($directory) !== $directory
            || ($dir['mode'] & 0177777) !== 0040700 || $dir['uid'] !== $account['uid']) {
            throw new \RuntimeException('Unsafe configuration directory');
        }
        $state = ApplyRequest::read($directory.'/state.json', $account['uid']);
        if (($state['schema'] ?? null) !== 2 || ($state['step'] ?? null) !== 0) { throw new \RuntimeException('Invalid preparation state'); }
        if (($state['status'] ?? null) === 'ready') {
            return ['prepared' => false, 'waiting_for_configuration' => true, 'installation_complete' => false];
        }
        ConfigurationDraft::frozen($directory, $account['uid']);
        // Exclusive durable barrier serializes concurrent workers and survives process termination.
        ApplyRequest::write($started, $identity);
        InstallationPreparation::createFromFrozenWebDraft($bindingPath, $directory);
        if (ApplyRequest::read($bindingPath, 0) !== $binding) { throw new \RuntimeException('Preparation binding changed'); }
        InstallationPreparation::verify($binding['destination']);
        ApplyRequest::write($ready, $identity + ['seal_sha256' => hash_file('sha256', $binding['destination'].'/seal.json')]);
        return ['prepared' => true, 'already_prepared' => false, 'installation_complete' => false];
    }
}
