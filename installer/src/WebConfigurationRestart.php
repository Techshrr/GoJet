<?php
declare(strict_types=1);
namespace GoJet\Installer;
require_once __DIR__.'/InstallationPreparation.php';
require_once __DIR__.'/InstallState.php';

/** Invoked under the preparation worker lock. Publishes a fresh, isolated attempt. */
final class WebConfigurationRestart
{
    /** Authenticate the root-created replacement before enabling its own preparation timer. */
    public static function scheduleKind(string $bindingPath, string $directory, string $predecessor): string
    {
        if (!function_exists('posix_geteuid') || posix_geteuid() !== 0) { throw new \RuntimeException('Root required'); }
        $successor = dirname($bindingPath);
        foreach ([dirname($predecessor), $successor] as $parent) { CompletionLock::verifyProvisioningPath($parent.'/installed.lock'); }
        if ($bindingPath !== $successor.'/binding.json' || $directory !== $successor.'/state'
            || (fileperms($successor) & 07777) !== 0755) { throw new \RuntimeException('Replacement not activated'); }
        $previous = ApplyRequest::read($predecessor, 0);
        $retired = ApplyRequest::read($predecessor.'.retired.json', 0);
        $ready = ApplyRequest::read($predecessor.'.replacement-ready.json', 0);
        if (array_keys($retired) !== ['schema', 'binding_sha256', 'state_sha256', 'draft_sha256', 'successor_sha256']
            || $retired['schema'] !== 1) { throw new \RuntimeException('Invalid retirement identity'); }
        foreach (['binding_sha256', 'state_sha256', 'draft_sha256', 'successor_sha256'] as $key) {
            if (!is_string($retired[$key]) || !preg_match('/^[a-f0-9]{64}$/D', $retired[$key])) { throw new \RuntimeException('Invalid retirement digest'); }
        }
        $next = ApplyRequest::read($bindingPath, 0);
        if (!hash_equals($retired['binding_sha256'], hash('sha256', json_encode($previous, JSON_THROW_ON_ERROR)))
            || !hash_equals($retired['successor_sha256'], hash('sha256', $successor))) {
            throw new \RuntimeException('Replacement lineage mismatch');
        }
        $expected = $previous; $expected['destination'] = $successor.'/prepared';
        if ($next !== $expected || $ready !== ['schema' => 1, 'successor_sha256' => hash('sha256', $successor),
            'binding_sha256' => hash('sha256', json_encode($next, JSON_THROW_ON_ERROR))]) {
            throw new \RuntimeException('Replacement publication mismatch');
        }
        return 'preparation-'.hash('sha256', $bindingPath);
    }

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
    private static function exists(string $path): bool
    {
        clearstatcache(true, $path); return file_exists($path) || is_link($path);
    }
    private static function publish(string $path, array $value): void
    {
        $temporary = dirname($path).'/.gojet-replacement-'.bin2hex(random_bytes(16)).'.tmp';
        $mask = umask(0077);
        try { $f = fopen($temporary, 'x+b'); } finally { umask($mask); }
        if ($f === false) { throw new \RuntimeException('Replacement staging failed'); }
        try {
            $raw = json_encode($value, JSON_THROW_ON_ERROR)."\n";
            if (fwrite($f, $raw) !== strlen($raw) || !fflush($f) || !fsync($f)
                || !link($temporary, $path) || !unlink($temporary)) { throw new \RuntimeException('Replacement publication failed'); }
            self::sync(dirname($path));
        } finally { fclose($f); if (file_exists($temporary)) { unlink($temporary); } }
    }
    /** Only the root-only unpublished tree may finish initialization; never reset a live state. */
    private static function finish(string $successor, array $next, string $bootstrapHash, array $account, string $readyPath): bool
    {
        $receipt = ['schema' => 1, 'successor_sha256' => hash('sha256', $successor),
            'binding_sha256' => hash('sha256', json_encode($next, JSON_THROW_ON_ERROR))];
        $ready = self::exists($readyPath);
        if ($ready && ApplyRequest::read($readyPath, 0) !== $receipt) { throw new \RuntimeException('Changed replacement receipt'); }
        $present = self::exists($successor); $mode = null;
        if ($present) {
            $s = lstat($successor); $mode = $s['mode'] & 07777;
            if (realpath($successor) !== $successor || !is_dir($successor) || $s['uid'] !== 0
                || !in_array($mode, [0700, 0755], true)
                || array_diff(scandir($successor), ['.', '..', 'binding.json', 'state', 'binding.json.worker.lock'])) {
                throw new \RuntimeException('Replacement advanced or unsafe');
            }
        } elseif ($ready) { throw new \RuntimeException('Missing completed replacement'); }
        $binding = $successor.'/binding.json'; $bindingExists = self::exists($binding);
        if ($bindingExists && ApplyRequest::read($binding, 0) !== $next) { throw new \RuntimeException('Changed successor binding'); }
        $state = $successor.'/state'; $stateExists = self::exists($state);
        $initial = ['schema' => 2, 'step' => 0, 'status' => 'ready', 'session' => null,
            'expires' => 0, 'csrf' => null, 'attempts' => 0, 'retry_at' => 0];
        if (($mode === 0755 || $ready) && (!$bindingExists || !$stateExists)) {
            throw new \RuntimeException('Missing published successor input');
        }
        if ($stateExists) {
            $s = lstat($state);
            if (!$bindingExists || realpath($state) !== $state || ($s['mode'] & 0177777) !== 0040700
                || !in_array($s['uid'], [0, $account['uid']], true) || !in_array($s['gid'], [0, $account['gid']], true)
                || array_diff(scandir($state), ['.', '..', 'state.json', 'state.lock'])) {
                throw new \RuntimeException('Unsafe successor state');
            }
            foreach (['state.json', 'state.lock'] as $name) {
                $path = $state.'/'.$name;
                if (!self::exists($path)) {
                    if ($mode !== 0700 || $ready || $s['uid'] !== 0) { throw new \RuntimeException('Missing initialized state'); }
                    continue;
                }
                $f = lstat($path);
                if (realpath($path) !== $path || ($f['mode'] & 0177777) !== 0100600 || $f['nlink'] !== 1 || $f['size'] > 4096
                    || !in_array($f['uid'], [0, $account['uid']], true) || !in_array($f['gid'], [0, $account['gid']], true)
                    || (($mode === 0755 || $ready) && ($f['uid'] !== $account['uid'] || $f['gid'] !== $account['gid']))) {
                    throw new \RuntimeException('Unsafe successor state file');
                }
                if ($name === 'state.json' && ApplyRequest::read($path, $f['uid']) !== $initial) {
                    throw new \RuntimeException('Successor state already used');
                }
                if ($name === 'state.lock' && $f['size'] !== 0) { throw new \RuntimeException('Changed successor lock'); }
            }
            if (($mode === 0755 || $ready) && ($s['uid'] !== $account['uid'] || $s['gid'] !== $account['gid'])) {
                throw new \RuntimeException('Changed initialized state owner');
            }
            if (!self::exists($state.'/state.json') && self::exists($state.'/state.lock') && fileowner($state.'/state.lock') !== 0) {
                throw new \RuntimeException('Ambiguous state initialization');
            }
        }
        $jsonExists = self::exists($state.'/state.json');
        $stateLockExists = self::exists($state.'/state.lock');
        if (($mode === 0755 || $ready) && (!$jsonExists || !$stateLockExists)) { throw new \RuntimeException('Published state disappeared'); }
        // A successor worker may already exist and be waiting. Serialize without replacing its inode.
        $worker = $successor.'/binding.json.worker.lock'; $lock = null;
        if (self::exists($worker)) {
            $s = lstat($worker);
            if (realpath($worker) !== $worker || ($s['mode'] & 0177777) !== 0100600
                || $s['uid'] !== 0 || $s['nlink'] !== 1 || $s['size'] !== 0) { throw new \RuntimeException('Unsafe successor worker lock'); }
            $lock = fopen($worker, 'r+b');
            if ($lock === false) { throw new \RuntimeException('Successor lock unavailable'); }
        }
        try {
            if ($lock !== null && !flock($lock, LOCK_EX | LOCK_NB)) { throw new \RuntimeException('Successor worker busy'); }
            if (!$present) {
                if (!mkdir($successor, 0700)) { throw new \RuntimeException('Successor publication refused'); }
                self::sync(dirname($successor));
            }
            if (!$bindingExists) { self::publish($binding, $next); }
            if (!$stateExists && !mkdir($state, 0700)) { throw new \RuntimeException('New state publication failed'); }
            if (!$jsonExists) {
                (new InstallState($state, $bootstrapHash))->initialize();
            } elseif (!$stateLockExists) {
                $mask = umask(0077);
                try { $f = fopen($state.'/state.lock', 'x+b'); } finally { umask($mask); }
                if ($f === false) { throw new \RuntimeException('State lock publication failed'); }
                try { if (!fsync($f)) { throw new \RuntimeException('State lock sync failed'); } } finally { fclose($f); }
                self::sync($state);
            }
            if ($mode !== 0755) {
                foreach ([$state.'/state.json', $state.'/state.lock', $state] as $path) {
                    if (!chown($path, $account['uid']) || !chgrp($path, $account['gid'])) { throw new \RuntimeException('New state ownership failed'); }
                    self::sync($path);
                }
                self::sync($successor);
            }
            if (!$ready) { self::publish($readyPath, $receipt); }
            if ($mode !== 0755) {
                if (!chmod($successor, 0755)) { throw new \RuntimeException('New attempt activation failed'); }
                self::sync($successor); self::sync(dirname($successor));
            }
            return $ready && $mode === 0755;
        } finally { if ($lock !== null) { flock($lock, LOCK_UN); fclose($lock); } }
    }
    public static function create(string $bindingPath, string $directory, string $successor, string $draftHash, string $bootstrapHash, bool $recover = false): array
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
        foreach ([$binding['destination'], $bindingPath.'.ready.json', $authority.'/installed.lock', $authority.'/web-apply.json',
            $authority.'/configuration-approval.json', $authority.'/web-apply-started.json',
            $directory.'/apply-approval.json', $directory.'/apply-request.json'] as $path) { self::absent($path); }
        if (!$recover) {
            foreach ([$successor, $bindingPath.'.retired.json', $bindingPath.'.replacement-ready.json'] as $path) { self::absent($path); }
        }
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
        $retirement = $identity + ['draft_sha256' => $draftHash, 'successor_sha256' => hash('sha256', $successor)];
        if ($recover) {
            if (ApplyRequest::read($bindingPath.'.retired.json', 0) !== $retirement) {
                throw new \RuntimeException('Original replacement authority required');
            }
        } else {
            // Retire first, durably, under the shared worker lock. A partial restart stays closed.
            ApplyRequest::write($bindingPath.'.retired.json', $retirement);
        }
        $next = $binding; $next['destination'] = $successor.'/prepared';
        $already = self::finish($successor, $next, $bootstrapHash, $account, $bindingPath.'.replacement-ready.json');
        return ['restarted' => true, 'previous_attempt_retired' => true, 'binding' => $successor.'/binding.json',
            'state' => $successor.'/state', 'prepared' => $successor.'/prepared', 'installation_complete' => false]
            + ($recover ? ['recovered' => true, 'already_applied' => $already] : []);
    }
}
