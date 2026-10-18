<?php

declare(strict_types=1);
namespace GoJet\Installer;
require_once __DIR__.'/Migrations.php';
require_once __DIR__.'/CompletionLock.php';

/** Root-only data phase. Does not start services, switch releases, or commit installation completion. */
final class DataBootstrap
{
    private static function privateFile(string $path): void
    {
        clearstatcache(true, $path);
        if (is_link($path) || !is_file($path) || fileowner($path) !== 0
            || (fileperms($path) & 0777) !== 0600 || stat($path)['nlink'] !== 1) {
            throw new \RuntimeException('Untrusted root job file');
        }
    }

    private static function immutableFile(string $path, string $hash): void
    {
        if (realpath($path) !== $path || !is_file($path) || fileowner($path) !== 0
            || (fileperms($path) & 0022) !== 0 || stat($path)['nlink'] !== 1
            || !hash_equals($hash, (string)hash_file('sha256', $path))) {
            throw new \RuntimeException('Untrusted release input');
        }
    }

    private static function keys(array $value, array $keys): void
    {
        if (array_diff($keys, array_keys($value)) || array_diff(array_keys($value), $keys)) {
            throw new \RuntimeException('Invalid data request');
        }
    }

    private static function plan(string $raw): array
    {
        $p = json_decode($raw, true, 16, JSON_THROW_ON_ERROR);
        self::keys($p, ['schema', 'release', 'source', 'version', 'catalog_sha256', 'platformapi_sha256',
            'database', 'redis', 'admin', 'admin_cipher', 'origin']);
        if ($p['schema'] !== 'gojet.root-data-bootstrap.v1') { throw new \RuntimeException('Invalid request schema'); }
        foreach (['source' => 40, 'catalog_sha256' => 64, 'platformapi_sha256' => 64] as $key => $length) {
            if (!is_string($p[$key]) || !preg_match('/^[a-f0-9]{'.$length.'}$/D', $p[$key])) { throw new \RuntimeException('Invalid release identity'); }
        }
        if (!is_string($p['version']) || !preg_match('/^[0-9A-Za-z][0-9A-Za-z.+-]{0,63}$/D', $p['version'])
            || !is_string($p['origin']) || !preg_match('~^https://[A-Za-z0-9.-]+(?::[0-9]{1,5})?$~D', $p['origin'])) {
            throw new \RuntimeException('Invalid release configuration');
        }
        if (parse_url($p['origin']) === false || (parse_url($p['origin'], PHP_URL_PORT) ?? 443) < 1) { throw new \RuntimeException('Invalid origin port'); }
        self::keys($p['database'], ['host', 'port', 'name', 'user', 'password']);
        self::keys($p['redis'], ['host', 'port', 'db', 'password']);
        self::keys($p['admin'], ['email', 'display_name', 'password']);
        self::keys($p['admin_cipher'], ['id', 'key_hex']);
        foreach (['database', 'redis'] as $service) {
            if ($p[$service]['host'] !== '127.0.0.1' || !is_int($p[$service]['port']) || $p[$service]['port'] < 1 || $p[$service]['port'] > 65535
                || !is_string($p[$service]['password']) || strlen($p[$service]['password']) > 1024 || str_contains($p[$service]['password'], "\0")) {
                throw new \RuntimeException('Only bounded local native dependency configuration is supported');
            }
        }
        foreach (['name', 'user'] as $key) {
            if (!is_string($p['database'][$key]) || !preg_match('/^[A-Za-z0-9_]{1,64}$/D', $p['database'][$key])) { throw new \RuntimeException('Invalid database identity'); }
        }
        if (!is_int($p['redis']['db']) || $p['redis']['db'] < 0 || $p['redis']['db'] > 15
            || !is_string($p['admin_cipher']['id']) || !preg_match('/^[A-Za-z0-9_.-]{1,64}$/D', $p['admin_cipher']['id'])
            || !is_string($p['admin_cipher']['key_hex']) || !preg_match('/^[a-f0-9]{64}$/D', $p['admin_cipher']['key_hex'])) {
            throw new \RuntimeException('Invalid native security configuration');
        }
        foreach (['email' => 320, 'display_name' => 128, 'password' => 1024] as $key => $limit) {
            if (!is_string($p['admin'][$key]) || trim($p['admin'][$key]) === '' || strlen($p['admin'][$key]) > $limit || str_contains($p['admin'][$key], "\0")) {
                throw new \RuntimeException('Invalid initial administrator');
            }
        }
        if (strpbrk($p['admin']['display_name'], "\r\n") !== false
            || strlen(json_encode($p['admin'] + ['correlation_id' => 'p21-root-'.substr($p['source'], 0, 32)], JSON_THROW_ON_ERROR)) > 4096) {
            throw new \RuntimeException('Initial administrator request exceeds bounds');
        }
        $root = $p['release'];
        if (!is_string($root) || realpath($root) !== $root || !is_dir($root) || fileowner($root) !== 0 || (fileperms($root) & 0022) !== 0) {
            throw new \RuntimeException('Untrusted release directory');
        }
        // Reuse the root hierarchy check; it does not require an installed marker.
        CompletionLock::verifyProvisioningPath($root.'/installed.lock');
        foreach (['bin', 'migrations'] as $directory) {
            $path = $root.'/'.$directory;
            if (realpath($path) !== $path || !is_dir($path) || fileowner($path) !== 0 || (fileperms($path) & 0022) !== 0) { throw new \RuntimeException('Untrusted release directory'); }
        }
        self::immutableFile($root.'/bin/platformapi', $p['platformapi_sha256']);
        if (!is_executable($root.'/bin/platformapi')) { throw new \RuntimeException('Missing native executable'); }
        self::immutableFile($root.'/migration-catalog.json', $p['catalog_sha256']);
        foreach (Migrations::load($root, $p['catalog_sha256']) as $row) { self::immutableFile($root.'/'.$row['path'], $row['sha256']); }
        $identity = json_decode(self::native($root.'/bin/platformapi', '--version', '', ['PATH' => '/usr/bin:/bin']), true, 8, JSON_THROW_ON_ERROR);
        if (!is_array($identity) || count($identity) !== 3 || ($identity['service'] ?? '') !== 'platformapi'
            || ($identity['commit'] ?? '') !== $p['source'] || ($identity['version'] ?? '') !== $p['version']) {
            throw new \RuntimeException('Native release identity mismatch');
        }
        return $p;
    }

    private static function persist(string $directory, array $journal): void
    {
        $tmp = $directory.'/journal-'.bin2hex(random_bytes(12)).'.tmp';
        $old = umask(0077);
        try { $f = fopen($tmp, 'x+b'); } finally { umask($old); }
        if ($f === false) { throw new \RuntimeException('Cannot persist root journal'); }
        try {
            $raw = json_encode($journal, JSON_THROW_ON_ERROR)."\n";
            if (fwrite($f, $raw) !== strlen($raw) || !fflush($f) || !fsync($f)) { throw new \RuntimeException('Cannot persist root journal'); }
            fclose($f); $f = null;
            if (!rename($tmp, $directory.'/journal.json')) { throw new \RuntimeException('Cannot commit root journal'); }
            $dir = fopen($directory, 'r');
            if ($dir === false) { throw new \RuntimeException('Cannot sync root journal'); }
            try { if (!fsync($dir)) { throw new \RuntimeException('Cannot sync root journal'); } }
            finally { fclose($dir); }
        } finally { if (is_resource($f)) { fclose($f); } if (file_exists($tmp)) { unlink($tmp); } }
    }

    private static function administrator(array $p): void
    {
        $d = $p['database']; $r = $p['redis'];
        $env = ['PATH' => '/usr/sbin:/usr/bin:/sbin:/bin',
            'GOJET_MYSQL_DSN' => $d['user'].':'.$d['password'].'@tcp(127.0.0.1:'.$d['port'].')/'.$d['name'].'?parseTime=true&charset=utf8mb4',
            'GOJET_REDIS_ADDR' => '127.0.0.1:'.$r['port'], 'GOJET_REDIS_PASSWORD' => $r['password'], 'GOJET_REDIS_DB' => (string)$r['db'],
            'GOJET_ADMIN_ACCESS_ENABLED' => '1', 'GOJET_ADMIN_TOTP_KEY_ID' => $p['admin_cipher']['id'],
            'GOJET_ADMIN_TOTP_KEY_HEX' => $p['admin_cipher']['key_hex'], 'GOJET_ADMIN_ALLOWED_ORIGIN' => $p['origin']];
        $input = json_encode($p['admin'] + ['correlation_id' => 'p21-root-'.substr($p['source'], 0, 32)], JSON_THROW_ON_ERROR);
        if (self::native($p['release'].'/bin/platformapi', '--bootstrap-admin', $input, $env) !== "{\"created\":true}\n") {
            throw new \RuntimeException('Native administrator creation refused');
        }
    }

    private static function native(string $binary, string $argument, string $input, array $env): string
    {
        $process = proc_open([$binary, $argument], [0 => ['pipe', 'r'], 1 => ['pipe', 'w'], 2 => ['pipe', 'w']], $pipes, null, $env);
        if (!is_resource($process)) { throw new \RuntimeException('Native process unavailable'); }
        $stdout = ''; $stderr = ''; $deadline = microtime(true) + 35;
        try {
            if (strlen($input) > 4096 || fwrite($pipes[0], $input) !== strlen($input)) { throw new \RuntimeException('Native request failed'); }
            fclose($pipes[0]); unset($pipes[0]);
            stream_set_blocking($pipes[1], false); stream_set_blocking($pipes[2], false);
            do {
                $stdout .= stream_get_contents($pipes[1], 8192);
                $stderr .= stream_get_contents($pipes[2], 8192);
                $status = proc_get_status($process);
                if (strlen($stdout) + strlen($stderr) > 8192 || microtime(true) > $deadline) { throw new \RuntimeException('Native request exceeded bounds'); }
                if ($status['running']) { usleep(20000); }
            } while ($status['running']);
            $stdout .= stream_get_contents($pipes[1], 8192); $stderr .= stream_get_contents($pipes[2], 8192);
            if (strlen($stdout) + strlen($stderr) > 8192 || $status['exitcode'] !== 0 || $stderr !== '') { throw new \RuntimeException('Native process refused'); }
            return $stdout;
        } finally {
            if (proc_get_status($process)['running']) { proc_terminate($process, 9); }
            foreach ($pipes as $pipe) { if (is_resource($pipe)) { fclose($pipe); } }
            proc_close($process);
        }
    }

    /** Validate a prepared request before publishing it; does not connect to the database. */
    public static function validateRequest(array $request): void
    {
        if (!function_exists('posix_geteuid') || posix_geteuid() !== 0) { throw new \RuntimeException('Root required'); }
        $raw = json_encode($request, JSON_THROW_ON_ERROR);
        if (strlen($raw) > 16384) { throw new \RuntimeException('Root request exceeds bound'); }
        self::plan($raw);
    }

    public static function run(string $directory): array
    {
        if (!function_exists('posix_geteuid') || posix_geteuid() !== 0) { throw new \RuntimeException('Root data worker required'); }
        if (realpath($directory) !== $directory || !is_dir($directory) || fileowner($directory) !== 0 || (fileperms($directory) & 0777) !== 0700) { throw new \RuntimeException('Untrusted root job directory'); }
        CompletionLock::verifyProvisioningPath($directory.'/installed.lock');
        $request = $directory.'/request.json'; self::privateFile($request);
        if (filesize($request) > 16384) { throw new \RuntimeException('Root request exceeds bound'); }
        $raw = (string)file_get_contents($request); $digest = hash('sha256', $raw);
        $p = self::plan($raw); // Validate all immutable inputs before any mutation.
        $lock = $directory.'/worker.lock';
        if (file_exists($lock) || is_link($lock)) { self::privateFile($lock); }
        $old = umask(0077);
        try { $handle = fopen($lock, 'c+b'); } finally { umask($old); }
        if ($handle === false) { throw new \RuntimeException('Root lock unavailable'); }
        try {
            self::privateFile($lock);
            if (!flock($handle, LOCK_EX | LOCK_NB)) { throw new \RuntimeException('Root job busy'); }
            $journalPath = $directory.'/journal.json';
            if (file_exists($journalPath) || is_link($journalPath)) {
                self::privateFile($journalPath);
                if (filesize($journalPath) > 4096) { throw new \RuntimeException('Invalid root journal'); }
                $journal = json_decode((string)file_get_contents($journalPath), true, 8, JSON_THROW_ON_ERROR);
                if (($journal['schema'] ?? null) !== 1 || ($journal['source'] ?? '') !== $p['source'] || ($journal['request_sha256'] ?? null) !== $digest || ($journal['phase'] ?? '') !== 'data-complete') { throw new \RuntimeException('Root job requires verified recovery'); }
                return ['data_complete' => true, 'already_applied' => true, 'installation_complete' => false];
            }
            $journal = ['schema' => 1, 'request_sha256' => $digest, 'source' => $p['source'], 'phase' => 'migrating'];
            self::persist($directory, $journal);
            try {
                $d = $p['database'];
                $db = new \PDO('mysql:host=127.0.0.1;port='.$d['port'].';dbname='.$d['name'].';charset=utf8mb4', $d['user'], $d['password'], [\PDO::ATTR_ERRMODE => \PDO::ERRMODE_EXCEPTION, \PDO::ATTR_TIMEOUT => 5]);
                $db->exec('SET SESSION lock_wait_timeout=10, innodb_lock_wait_timeout=10');
                Migrations::apply($db, $p['release'], $p['catalog_sha256'], $p['source'], $p['version']);
                $journal['phase'] = 'admin-started'; self::persist($directory, $journal);
                self::administrator($p);
                $journal['phase'] = 'data-complete'; self::persist($directory, $journal);
                return ['data_complete' => true, 'already_applied' => false, 'installation_complete' => false];
            } catch (\Throwable) {
                $journal['failed_at'] = $journal['phase']; $journal['phase'] = 'failed'; self::persist($directory, $journal);
                throw new \RuntimeException('Data bootstrap failed; verified recovery required');
            }
        } finally { flock($handle, LOCK_UN); fclose($handle); }
    }
}
