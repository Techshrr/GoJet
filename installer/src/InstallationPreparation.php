<?php

declare(strict_types=1);
namespace GoJet\Installer;
require_once __DIR__.'/PackageIntegrity.php';
require_once __DIR__.'/NativeConfiguration.php';
require_once __DIR__.'/ProductWebConfig.php';
require_once __DIR__.'/DataBootstrap.php';
require_once __DIR__.'/DependencyPreflight.php';

/** Root-private preparation and the existing data phase, not service apply or final completion. */
final class InstallationPreparation
{
    private const SERVICES = ['redirectengine', 'analyticsworker', 'analyticsreconciler', 'platformapi', 'mailworker', 'fileworker', 'operationsmonitor', 'logreceiver'];
    private static function root(): void
    {
        if (!function_exists('posix_geteuid') || posix_geteuid() !== 0) { throw new \RuntimeException('Root required'); }
    }
    private static function keys(array $p, array $keys): void
    {
        if (array_diff($keys, array_keys($p)) || array_diff(array_keys($p), $keys)) { throw new \RuntimeException('Invalid preparation input'); }
    }
    private static function sync(string $path): void
    {
        $f = fopen($path, 'r');
        if ($f === false) { throw new \RuntimeException('Directory unavailable'); }
        try { if (!fsync($f)) { throw new \RuntimeException('Directory sync failed'); } } finally { fclose($f); }
    }
    private static function write(string $path, string $value): void
    {
        $old = umask(0077);
        try { $f = fopen($path, 'x+b'); } finally { umask($old); }
        if ($f === false) { throw new \RuntimeException('Private write failed'); }
        try {
            if (fwrite($f, $value) !== strlen($value) || !fflush($f) || !fsync($f)) { throw new \RuntimeException('Private sync failed'); }
        } finally { fclose($f); }
    }
    private static function privateFile(string $path): string
    {
        clearstatcache(true, $path); $s = @lstat($path);
        if ($s === false || ($s['mode'] & 0177777) !== 0100600 || $s['uid'] !== 0 || $s['nlink'] !== 1 || $s['size'] > 262144) {
            throw new \RuntimeException('Unsafe prepared file');
        }
        $raw = file_get_contents($path);
        if ($raw === false) { throw new \RuntimeException('Prepared file unavailable'); }
        return $raw;
    }
    private static function privateDirectory(string $path): void
    {
        CompletionLock::verifyProvisioningPath($path.'/installed.lock');
        if ((fileperms($path) & 07777) !== 0700) { throw new \RuntimeException('Unsafe prepared directory'); }
    }
    private static function overlaps(string $a, string $b): bool
    {
        return $a === $b || str_starts_with($a, $b.'/') || str_starts_with($b, $a.'/');
    }
    /** Bind packaged hardening to immutable release, private environment and configured storage. */
    private static function units(string $release, string $destination, string $storage): array
    {
        foreach ([$release, $destination, $storage] as $path) {
            if (!preg_match('~^/(?:[A-Za-z0-9_.-]+/)*[A-Za-z0-9_.-]+$~D', $path)
                || in_array('.', explode('/', $path), true) || in_array('..', explode('/', $path), true)) {
                throw new \RuntimeException('Unsafe service path');
            }
        }
        $units = [];
        foreach (self::SERVICES as $name) {
            $template = file_get_contents($release.'/deploy/systemd/gojet-'.$name.'.service');
            if ($template === false || strlen($template) > 65536) { throw new \RuntimeException('Invalid service template'); }
            $replace = ['WorkingDirectory=/opt/gojet/current' => 'WorkingDirectory='.$release,
                'EnvironmentFile=/etc/gojet/'.$name.'.env' => 'EnvironmentFile='.$destination.'/environment/'.$name.'.env',
                'ExecStart=/opt/gojet/current/bin/'.$name => 'ExecStart='.$release.'/bin/'.$name];
            if (in_array($name, ['platformapi', 'fileworker'], true)) {
                $replace['ReadWritePaths=/var/lib/gojet/files'] = 'ReadWritePaths='.$storage;
            }
            foreach ($replace as $from => $to) {
                $template = str_replace("\n".$from."\n", "\n".$to."\n", $template, $count);
                if ($count !== 1) { throw new \RuntimeException('Service template boundary changed'); }
            }
            $units['units/gojet-'.$name.'.service'] = $template;
        }
        return $units;
    }
    public static function create(string $destination, string $release, string $manifestHash, string $source, array $input): array
    {
        self::root();
        self::keys($input, ['configuration', 'admin', 'tls']);
        $parent = dirname($destination);
        CompletionLock::verifyProvisioningPath($parent.'/installed.lock');
        if (!preg_match('/^[A-Za-z0-9][A-Za-z0-9_.-]{0,63}$/D', basename($destination))
            || $destination !== $parent.'/'.basename($destination)) { throw new \RuntimeException('Invalid job destination'); }
        PackageIntegrity::verify($release, $manifestHash, $source);
        $manifest = json_decode(file_get_contents($release.'/manifest.json'), true, 32, JSON_THROW_ON_ERROR);
        $config = NativeConfiguration::compile($input['configuration']);
        $p = $input['configuration'];
        self::keys($input['tls'], ['certificate', 'private_key', 'http_port']);
        $web = ['origin' => $config['origin'], 'release' => $release, 'max_body_bytes' => $config['max_body_bytes']] + $input['tls'];
        foreach ([$destination, $release] as $protected) {
            if (self::overlaps($p['files']['root'], $protected)) { throw new \RuntimeException('Storage conflicts with immutable state'); }
        }
        if (self::overlaps($destination, $release)) { throw new \RuntimeException('Job conflicts with release'); }
        if ($input['tls']['certificate'] === $input['tls']['private_key']) { throw new \RuntimeException('Distinct TLS inputs required'); }
        $nginx = ProductWebConfig::render($web);
        foreach (['certificate', 'private_key'] as $key) {
            if (self::overlaps($input['tls'][$key], $destination) || self::overlaps($input['tls'][$key], $p['files']['root'])
                || filesize($input['tls'][$key]) > 1048576) { throw new \RuntimeException('TLS path conflicts or exceeds bound'); }
        }
        $data = ['schema' => 'gojet.root-data-bootstrap.v1', 'release' => $release, 'source' => $source,
            'version' => $manifest['version'], 'catalog_sha256' => hash_file('sha256', $release.'/migration-catalog.json'),
            'platformapi_sha256' => hash_file('sha256', $release.'/bin/platformapi'),
            'database' => ['host' => '127.0.0.1'] + $p['database'], 'redis' => ['host' => '127.0.0.1'] + $p['redis'],
            'admin' => $input['admin'], 'admin_cipher' => ['id' => $p['secrets']['key_id'], 'key_hex' => $p['secrets']['admin']],
            'origin' => $config['origin']];
        DataBootstrap::validateRequest($data);
        // Derive live dependency probes from the same validated settings, never a second client profile.
        $dependencies = ['release' => $release, 'manifest_sha256' => $manifestHash, 'source' => $source,
            'database' => $p['database'], 'redis' => $p['redis'], 'storage' => $p['files']['root'],
            'clamav_socket' => $p['files']['clamav_socket']];
        $files = ['dependencies.json' => json_encode($dependencies, JSON_THROW_ON_ERROR)."\n", 'data/request.json' => json_encode($data, JSON_THROW_ON_ERROR)."\n", 'nginx.conf' => $nginx];
        foreach (ServiceEnvironment::render($config['services']) as $name => $value) { $files['environment/'.$name] = $value; }
        $files += self::units($release, $destination, $p['files']['root']);
        $external = [];
        foreach (['certificate', 'private_key'] as $key) {
            $path = $input['tls'][$key]; $external[$path] = ['sha256' => hash_file('sha256', $path), 'mode' => fileperms($path) & 07777, 'private' => $key === 'private_key'];
        }
        // Stable parent lock serializes preparation publication. Existing jobs are never replaced.
        $lockPath = $parent.'/.installation-preparation.lock';
        if (is_link($lockPath) || (file_exists($lockPath) && !is_file($lockPath))) { throw new \RuntimeException('Unsafe preparation lock'); }
        $old = umask(0077);
        try { $lock = fopen($lockPath, 'c+b'); } finally { umask($old); }
        if ($lock === false) { throw new \RuntimeException('Preparation lock unavailable'); }
        $stage = null;
        try {
            self::privateFile($lockPath);
            if (!flock($lock, LOCK_EX | LOCK_NB)) { throw new \RuntimeException('Preparation busy'); }
            clearstatcache();
            if (file_exists($destination) || is_link($destination)) { throw new \RuntimeException('Prepared job already exists'); }
            $stage = $parent.'/.installation-'.bin2hex(random_bytes(16));
            if (!mkdir($stage, 0700) || !mkdir($stage.'/data', 0700) || !mkdir($stage.'/environment', 0700) || !mkdir($stage.'/units', 0700)) { throw new \RuntimeException('Preparation staging failed'); }
            $hashes = [];
            foreach ($files as $name => $value) { self::write($stage.'/'.$name, $value); $hashes[$name] = hash('sha256', $value); }
            self::write($stage.'/seal.json', json_encode(['schema' => 3, 'source' => $source, 'release' => $release,
                'manifest_sha256' => $manifestHash, 'files' => $hashes, 'tls_files' => $external], JSON_THROW_ON_ERROR)."\n");
            self::sync($stage.'/data'); self::sync($stage.'/environment'); self::sync($stage.'/units'); self::sync($stage);
            if (!rename($stage, $destination)) { throw new \RuntimeException('Preparation publication failed'); }
            $stage = null; self::sync($parent);
            return ['prepared' => true, 'source' => $source, 'manifest_sha256' => $manifestHash,
                'environment_files' => 8, 'data_executed' => false, 'installation_complete' => false];
        } finally {
            if ($stage !== null && is_dir($stage)) {
                foreach (array_merge(array_keys($files), ['seal.json']) as $name) { if (is_file($stage.'/'.$name)) { unlink($stage.'/'.$name); } }
                foreach (['data', 'environment', 'units'] as $name) { if (is_dir($stage.'/'.$name)) { rmdir($stage.'/'.$name); } }
                rmdir($stage);
            }
            flock($lock, LOCK_UN); fclose($lock);
        }
    }
    private static function inspect(string $directory): array
    {
        self::root(); self::privateDirectory($directory);
        $seal = json_decode(self::privateFile($directory.'/seal.json'), true, 16, JSON_THROW_ON_ERROR);
        self::keys($seal, ['schema', 'source', 'release', 'manifest_sha256', 'files', 'tls_files']);
        if ($seal['schema'] !== 3) { throw new \RuntimeException('Invalid preparation seal'); }
        $names = ['dependencies.json', 'data/request.json', 'nginx.conf'];
        foreach (self::SERVICES as $service) { $names[] = 'environment/'.$service.'.env'; $names[] = 'units/gojet-'.$service.'.service'; }
        self::keys($seal['files'], $names);
        foreach (['data', 'environment', 'units'] as $name) { self::privateDirectory($directory.'/'.$name); }
        foreach ($names as $name) {
            if (!is_string($seal['files'][$name]) || !hash_equals($seal['files'][$name], hash('sha256', self::privateFile($directory.'/'.$name)))) {
                throw new \RuntimeException('Prepared input changed');
            }
        }
        foreach (['' => ['data', 'dependencies.json', 'environment', 'nginx.conf', 'seal.json', 'units', 'services.json', 'site.json'], 'units/' => array_map(fn($s) => 'gojet-'.$s.'.service', self::SERVICES), 'environment/' => array_map(fn($s) => $s.'.env', self::SERVICES), 'data/' => ['request.json', 'journal.json', 'worker.lock']] as $prefix => $allowed) {
            foreach (scandir($directory.'/'.$prefix) as $name) {
                if ($name === '.' || $name === '..') { continue; }
                if (!in_array($name, $allowed, true)) { throw new \RuntimeException('Unexpected prepared input'); }
                if (($prefix === 'data/' && $name !== 'request.json') || ($prefix === '' && in_array($name, ['services.json', 'site.json'], true))) { self::privateFile($directory.'/'.$prefix.$name); }
            }
        }
        if (!is_array($seal['tls_files']) || count($seal['tls_files']) !== 2) { throw new \RuntimeException('Invalid TLS binding'); }
        foreach ($seal['tls_files'] as $path => $binding) {
            self::keys($binding, ['sha256', 'mode', 'private']);
            if (!is_int($binding['mode']) || !is_bool($binding['private'])) { throw new \RuntimeException('Invalid TLS metadata'); }
            $hash = $binding['sha256'];
            clearstatcache(true, $path);
            CompletionLock::verifyProvisioningPath(dirname($path).'/installed.lock');
            $stat = @lstat($path);
            if ($stat === false || ($stat['mode'] & 0170000) !== 0100000 || $stat['uid'] !== 0 || $stat['nlink'] !== 1
                || ($stat['mode'] & 07777) !== $binding['mode'] || ($stat['mode'] & 0022) !== 0
                || ($binding['private'] && ($stat['mode'] & 0077) !== 0) || $stat['size'] > 1048576 || !is_string($hash) || !hash_equals($hash, hash_file('sha256', $path))) {
                throw new \RuntimeException('TLS input changed');
            }
        }
        PackageIntegrity::verify($seal['release'], $seal['manifest_sha256'], $seal['source']);
        $request = json_decode(self::privateFile($directory.'/data/request.json'), true, 16, JSON_THROW_ON_ERROR);
        if ($request['release'] !== $seal['release'] || $request['source'] !== $seal['source']) { throw new \RuntimeException('Prepared source mismatch'); }
        // Readiness must be current at execution; preparation is not a health authority.
        $dependencies = json_decode(self::privateFile($directory.'/dependencies.json'), true, 16, JSON_THROW_ON_ERROR);
        if ($dependencies['release'] !== $seal['release'] || $dependencies['source'] !== $seal['source']
            || $dependencies['manifest_sha256'] !== $seal['manifest_sha256']) { throw new \RuntimeException('Prepared dependency identity mismatch'); }
        $readiness = DependencyPreflight::check($dependencies);
        return ['seal' => $seal, 'readiness' => $readiness];
    }
    public static function preflight(string $directory): array
    {
        return self::inspect($directory)['readiness'];
    }
    public static function verify(string $directory): array
    {
        $result = self::inspect($directory);
        $readiness = $result['readiness'];
        if ($readiness['ready'] !== true) { throw new \RuntimeException('Native dependencies not ready'); }
        return $result['seal'];
    }
    public static function runData(string $directory): array
    {
        self::verify($directory);
        return DataBootstrap::run($directory.'/data');
    }
}
