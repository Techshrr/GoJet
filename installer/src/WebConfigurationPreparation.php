<?php
declare(strict_types=1);
namespace GoJet\Installer;
require_once __DIR__.'/InstallationPreparation.php';

/** Root worker primitive. Scheduling and approval remain separate explicit operations. */
final class WebConfigurationPreparation
{
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
