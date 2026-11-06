<?php
declare(strict_types=1);
namespace GoJet\Installer;
require_once __DIR__.'/CompletionLock.php';

/** Root-only consistent logical backup. Does not authorize upgrades or restore production. */
final class DatabaseBackup
{
    public static function create(array $connection, string $directory): array
    {
        if (PHP_SAPI !== 'cli' || !function_exists('posix_geteuid') || posix_geteuid() !== 0) {
            throw new \RuntimeException('Root backup authority required');
        }
        if (array_diff(array_keys($connection), ['host', 'port', 'name', 'user', 'password'])
            || count($connection) !== 5 || ($connection['host'] ?? null) !== '127.0.0.1'
            || !is_int($connection['port'] ?? null) || $connection['port'] < 1 || $connection['port'] > 65535) {
            throw new \RuntimeException('Invalid backup connection');
        }
        foreach (['name', 'user'] as $key) {
            if (!is_string($connection[$key] ?? null) || !preg_match('/^[A-Za-z0-9_]{1,64}$/D', $connection[$key])) {
                throw new \RuntimeException('Invalid backup identity');
            }
        }
        if (!is_string($connection['password'] ?? null) || strlen($connection['password']) > 1024
            || preg_match('/[\x00-\x1f\x7f]/', $connection['password'])) { throw new \RuntimeException('Invalid backup credential'); }
        $parent = dirname($directory);
        CompletionLock::verifyProvisioningPath($parent.'/installed.lock');
        if (($directory !== $parent.'/'.basename($directory)) || !preg_match('/^[A-Za-z0-9][A-Za-z0-9_-]{0,63}$/D', basename($directory))
            || (fileperms($parent) & 07777) !== 0700) { throw new \RuntimeException('Private backup parent required'); }
        $binary = '/usr/bin/mysqldump';
        $stat = @lstat($binary);
        if ($stat === false || ($stat['mode'] & 0170000) !== 0100000 || $stat['uid'] !== 0
            || ($stat['mode'] & 0022) !== 0 || !is_executable($binary)) { throw new \RuntimeException('Trusted mysqldump required'); }
        // Validate the actual server before creating any backup output.
        try {
            $db = new \PDO('mysql:host=127.0.0.1;port='.$connection['port'].';dbname='.$connection['name'].';charset=utf8mb4',
                $connection['user'], $connection['password'], [\PDO::ATTR_ERRMODE => \PDO::ERRMODE_EXCEPTION, \PDO::ATTR_TIMEOUT => 10]);
            if (!preg_match('/^8\./', (string)$db->query('SELECT VERSION()')->fetchColumn())) { throw new \RuntimeException('Native MySQL8 required'); }
            $server = (string)$db->query('SELECT @@server_uuid')->fetchColumn();
            $query = $db->prepare('SELECT DEFAULT_CHARACTER_SET_NAME,DEFAULT_COLLATION_NAME FROM information_schema.SCHEMATA WHERE SCHEMA_NAME=?');
            $query->execute([$connection['name']]); $encoding = $query->fetch(\PDO::FETCH_NUM);
            if ($encoding !== ['utf8mb4', 'utf8mb4_0900_ai_ci']) { throw new \RuntimeException('Unsupported backup encoding'); }
        } catch (\Throwable) {
            throw new \RuntimeException('Backup database connection or server validation failed');
        }
        $old = umask(0077);
        try {
            // Never replace or resume an existing backup, including a partial one.
            if (!@mkdir($directory, 0700)) { throw new \RuntimeException('Backup destination exists or unavailable'); }
        } finally { umask($old); }
        $credentials = $directory.'/client.cnf';
        $dump = $directory.'/database.sql';
        $process = null; $pipes = []; $output = null;
        try {
            $password = str_replace(['\\', '"'], ['\\\\', '\\"'], $connection['password']);
            self::writePrivate($credentials, "[client]\nhost=127.0.0.1\nprotocol=TCP\nport=".$connection['port']."\nuser=".$connection['user']."\npassword=\"".$password."\"\n");
            $old = umask(0077);
            try { $output = fopen($dump, 'x+b'); } finally { umask($old); }
            if ($output === false) { throw new \RuntimeException('Backup output unavailable'); }
            // lock-all-tables gives one global read-locked snapshot, including nontransactional tables.
            // No --databases: the dump does not select/create the source database on restore.
            $process = proc_open([$binary, '--defaults-file='.$credentials, '--lock-all-tables',
                '--routines', '--events', '--triggers', '--hex-blob', '--set-gtid-purged=OFF',
                '--no-tablespaces', '--default-character-set=utf8mb4', $connection['name']],
                [0 => ['file', '/dev/null', 'r'], 1 => $output, 2 => ['pipe', 'w']], $pipes,
                $directory, ['PATH' => '/usr/bin:/bin', 'HOME' => $directory, 'LC_ALL' => 'C']);
            if (!is_resource($process)) { throw new \RuntimeException('Backup process unavailable'); }
            stream_set_blocking($pipes[2], false);
            $deadline = microtime(true) + 300; $diagnostic = false;
            do {
                if (stream_get_contents($pipes[2], 8192) !== '') { $diagnostic = true; }
                $state = proc_get_status($process);
                if (!$state['running']) { break; }
                if (microtime(true) >= $deadline) { throw new \RuntimeException('Backup deadline exceeded'); }
                usleep(20000);
            } while (true);
            if (stream_get_contents($pipes[2], 8192) !== '') { $diagnostic = true; }
            fclose($pipes[2]); $pipes = [];
            proc_close($process); $process = null;
            if ($state['exitcode'] !== 0 || $diagnostic || !fflush($output) || !fsync($output)
                || fstat($output)['size'] === 0) { throw new \RuntimeException('Consistent database export failed'); }
            fclose($output); $output = null;
            // Credentials must be removed before any success receipt is published.
            if (!unlink($credentials)) { throw new \RuntimeException('Backup credential cleanup failed'); }
            $result = ['schema' => 'gojet.database-backup.v1', 'database' => $connection['name'],
                'server_uuid' => $server, 'encoding' => $encoding, 'file' => 'database.sql', 'bytes' => filesize($dump),
                'sha256' => hash_file('sha256', $dump), 'consistency' => 'mysqldump-lock-all-tables',
                'restore_verified' => false, 'upgrade_authorized' => false];
            self::writePrivate($directory.'/complete.json', json_encode($result, JSON_THROW_ON_ERROR)."\n");
            $handle = fopen($directory, 'r');
            try { if ($handle === false || !fsync($handle)) { throw new \RuntimeException('Backup directory sync failed'); } }
            finally { if (is_resource($handle)) { fclose($handle); } }
            return $result;
        } catch (\Throwable) {
            // Preserve partial SQL for diagnosis; it has no usable success receipt.
            if (is_file($directory.'/complete.json')) { unlink($directory.'/complete.json'); }
            throw new \RuntimeException('Database backup failed; preserve private output and retry in a new directory');
        } finally {
            if (is_resource($process)) { proc_terminate($process, 9); proc_close($process); }
            foreach ($pipes as $pipe) { if (is_resource($pipe)) { fclose($pipe); } }
            if (is_resource($output)) { fclose($output); }
            if (is_file($credentials)) { unlink($credentials); }
        }
    }

    private static function writePrivate(string $path, string $bytes): void
    {
        $old = umask(0077);
        try { $file = fopen($path, 'xb'); } finally { umask($old); }
        if ($file === false) { throw new \RuntimeException('Private backup file unavailable'); }
        try {
            if (fwrite($file, $bytes) !== strlen($bytes) || !fflush($file) || !fsync($file)) {
                throw new \RuntimeException('Private backup write failed');
            }
        } finally { fclose($file); }
    }
}
