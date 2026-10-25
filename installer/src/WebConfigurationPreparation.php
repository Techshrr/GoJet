<?php
declare(strict_types=1);
namespace GoJet\Installer;
require_once __DIR__.'/InstallationPreparation.php';

/** Root preparation worker and scheduler; approval and execution remain separate operations. */
final class WebConfigurationPreparation
{
    /** Validate the root-selected inputs before publishing a fixed preparation timer. */
    public static function schedule(string $bindingPath, string $directory, ?string $predecessor = null): array
    {
        if (!function_exists('posix_geteuid') || posix_geteuid() !== 0) { throw new \RuntimeException('Root required'); }
        $authority = '/var/lib/gojet-installer-authority';
        CompletionLock::verifyProvisioningPath($authority.'/installed.lock');
        if ((new CompletionLock($authority.'/installed.lock'))->exists()) { throw new \RuntimeException('Installer unavailable'); }
        self::notRetired($bindingPath);
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
        $kind = 'preparation';
        if ($predecessor !== null) {
            require_once __DIR__.'/WebConfigurationRestart.php';
            $kind = WebConfigurationRestart::scheduleKind($bindingPath, $directory, $predecessor);
        }
        $service = 'gojet-installer-'.$kind.'.service';
        $timer = 'gojet-installer-'.$kind.'.timer';
        return [
            $service => <<<UNIT
[Unit]
Description=Prepare the submitted GoJet configuration for root approval
After=network.target
ConditionPathExists={$bindingPath}
ConditionPathExists={$directory}/configuration-draft.json
ConditionPathExists=!{$bindingPath}.started.json
ConditionPathExists=!{$bindingPath}.ready.json
ConditionPathExists=!{$bindingPath}.retired.json
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
            $timer => <<<UNIT
[Unit]
Description=Observe submitted GoJet configuration
ConditionPathExists={$bindingPath}
ConditionPathExists=!{$authority}/installed.lock

[Timer]
OnActiveSec=1s
OnCalendar=*-*-* *:*:00/10
AccuracySec=1s
Unit={$service}

[Install]
WantedBy=timers.target

UNIT,
        ];
    }

    public static function run(string $bindingPath, string $directory): array
    {
        return self::execute($bindingPath, $directory, null);
    }

    /** Explicit recovery of an already published plan; never rebuild or remove a barrier. */
    public static function recover(string $bindingPath, string $directory, string $sealHash): array
    {
        if (!preg_match('/^[a-f0-9]{64}$/D', $sealHash)) { throw new \RuntimeException('Expected seal digest required'); }
        return self::execute($bindingPath, $directory, $sealHash);
    }

    private static function execute(string $bindingPath, string $directory, ?string $sealHash): array
    {
        return self::withWorkerLock($bindingPath, fn() => self::runLocked($bindingPath, $directory, $sealHash));
    }

    /** A replacement is a new attempt, not a reset of the frozen state or failed receipts. */
    public static function restart(string $bindingPath, string $directory, string $successor, string $draftHash, string $bootstrapHash): array
    {
        require_once __DIR__.'/WebConfigurationRestart.php';
        return self::withWorkerLock($bindingPath, fn() => WebConfigurationRestart::create($bindingPath, $directory, $successor, $draftHash, $bootstrapHash));
    }

    private static function notRetired(string $bindingPath): void
    {
        if (file_exists($bindingPath.'.retired.json') || is_link($bindingPath.'.retired.json')) {
            throw new \RuntimeException('Preparation attempt retired');
        }
    }

    /** Serialize endpoint switching with both retirement and successor preparation. */
    public static function switchWeb(array $input): array
    {
        $fields = ['previous', 'predecessor', 'binding', 'bootstrap_sha256'];
        if (array_diff($fields, array_keys($input)) || array_diff(array_keys($input), $fields)
            || !is_array($input['previous']) || !is_string($input['predecessor']) || !is_string($input['binding'])
            || !is_string($input['bootstrap_sha256']) || $input['predecessor'] === $input['binding']) {
            throw new \RuntimeException('Invalid endpoint switch');
        }
        require_once __DIR__.'/WebConfigurationSwitch.php';
        return self::withWorkerLock($input['predecessor'], fn() => self::withWorkerLock($input['binding'],
            fn() => WebConfigurationSwitch::run($input)));
    }

    private static function withWorkerLock(string $bindingPath, callable $operation): array
    {
        if (!function_exists('posix_geteuid') || posix_geteuid() !== 0) { throw new \RuntimeException('Root required'); }
        CompletionLock::verifyProvisioningPath(dirname($bindingPath).'/installed.lock');
        // Both the normal worker and recovery hold this stable root-only inode throughout.
        $path = $bindingPath.'.worker.lock';
        clearstatcache();
        if (is_link($path) || (file_exists($path) && !is_file($path))) { throw new \RuntimeException('Unsafe worker lock'); }
        $old = umask(0077);
        try { $lock = @fopen($path, 'c+b'); } finally { umask($old); }
        if ($lock === false) { throw new \RuntimeException('Worker lock unavailable'); }
        try {
            clearstatcache(); $s = lstat($path); $opened = fstat($lock);
            if ($s === false || $opened === false || $s['dev'] !== $opened['dev'] || $s['ino'] !== $opened['ino']
                || ($s['mode'] & 0177777) !== 0100600 || $s['uid'] !== 0 || $s['nlink'] !== 1 || $s['size'] !== 0
                || !flock($lock, LOCK_EX | LOCK_NB)) { throw new \RuntimeException('Worker busy or unsafe'); }
            return $operation();
        } finally { flock($lock, LOCK_UN); fclose($lock); }
    }

    private static function runLocked(string $bindingPath, string $directory, ?string $sealHash): array
    {
        if (!function_exists('posix_geteuid') || posix_geteuid() !== 0) { throw new \RuntimeException('Root required'); }
        CompletionLock::verifyProvisioningPath(dirname($bindingPath).'/installed.lock');
        CompletionLock::verifyProvisioningPath(dirname($directory).'/installed.lock');
        self::notRetired($bindingPath);
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
        if ($sealHash !== null) {
            return self::recoverPublished($bindingPath, $directory, $binding, $identity, $sealHash);
        }
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

    private static function recoverPublished(string $bindingPath, string $directory, array $binding, array $identity, string $sealHash): array
    {
        $destination = $binding['destination'];
        if (ApplyRequest::read($bindingPath.'.started.json', 0) !== $identity) { throw new \RuntimeException('Preparation attempt mismatch'); }
        // Recovery is confined to preparation. Never adopt an approved or executed installation.
        foreach (['/var/lib/gojet-installer-authority/web-apply.json',
            '/var/lib/gojet-installer-authority/configuration-approval.json',
            '/var/lib/gojet-installer-authority/web-apply-started.json',
            $directory.'/apply-approval.json', $directory.'/apply-request.json',
            $destination.'/data/journal.json', $destination.'/services.json', $destination.'/site.json'] as $path) {
            if (file_exists($path) || is_link($path)) { throw new \RuntimeException('Preparation already advanced'); }
        }
        $account = posix_getpwnam('gojet-installer');
        if ($account === false || $account['uid'] <= 0) { throw new \RuntimeException('Installer account required'); }
        $snapshot = ConfigurationDraft::frozen($directory, $account['uid']);
        $seal = InstallationPreparation::verify($destination);
        foreach (['release', 'source', 'manifest_sha256'] as $key) {
            if ($seal[$key] !== $binding[$key]) { throw new \RuntimeException('Prepared binding mismatch'); }
        }
        if (!isset($seal['files']['web-draft.json'])
            || !hash_equals($sealHash, hash_file('sha256', $destination.'/seal.json'))
            || ApplyRequest::read($destination.'/web-draft.json', 0) !== ['schema' => 1,
                'state_sha256' => $identity['state_sha256'], 'draft_sha256' => $snapshot['sha256'], 'uid' => $account['uid']]) {
            throw new \RuntimeException('Published preparation mismatch');
        }
        $after = ConfigurationDraft::frozen($directory, $account['uid']);
        if (!hash_equals($snapshot['sha256'], $after['sha256']) || ApplyRequest::read($bindingPath, 0) !== $binding) {
            throw new \RuntimeException('Preparation input changed');
        }
        $receipt = $identity + ['seal_sha256' => $sealHash];
        $path = $bindingPath.'.ready.json';
        $already = file_exists($path) || is_link($path);
        if ($already) {
            if (ApplyRequest::read($path, 0) !== $receipt) { throw new \RuntimeException('Existing receipt mismatch'); }
        } else {
            ApplyRequest::write($path, $receipt);
        }
        return ['prepared' => true, 'recovered' => true, 'already_prepared' => $already, 'installation_complete' => false];
    }
}
