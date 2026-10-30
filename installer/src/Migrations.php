<?php

declare(strict_types=1);
namespace GoJet\Installer;

use PDO;
use RuntimeException;
use Throwable;

/** Native migration engine. Caller must first authenticate the release manifest. */
final class Migrations
{
    public static function statements(string $sql): array
    {
        $out = []; $buffer = ''; $quote = null; $delimiter = ';'; $length = strlen($sql);
        for ($i = 0; $i < $length;) {
            $c = $sql[$i];
            if ($quote !== null) {
                $buffer .= $c; ++$i;
                if ($c === '\\' && $i < $length) { $buffer .= $sql[$i++]; continue; }
                if ($c === $quote) {
                    if ($i < $length && $sql[$i] === $quote) { $buffer .= $sql[$i++]; }
                    else { $quote = null; }
                }
                continue;
            }
            if (($i === 0 || $sql[$i - 1] === "\n") && preg_match('/\G[ \t]*DELIMITER[ \t]+(\S+)[ \t]*(?:\r?\n|$)/Ai', $sql, $m, 0, $i)) {
                if (trim($buffer) !== '' || !in_array($m[1], [';', '$$'], true)) { throw new RuntimeException('unsupported delimiter boundary'); }
                $delimiter = $m[1]; $i += strlen($m[0]); continue;
            }
            if ($c === '#' || (substr($sql, $i, 2) === '--' && ($i + 2 === $length || ctype_space($sql[$i + 2])))) {
                $end = strpos($sql, "\n", $i); $i = $end === false ? $length : $end; $buffer .= "\n"; continue;
            }
            if (substr($sql, $i, 2) === '/*') {
                // Versioned comments may execute SQL; refuse unsupported syntax.
                if (substr($sql, $i, 3) === '/*!') { throw new RuntimeException('unsupported executable comment'); }
                $end = strpos($sql, '*/', $i + 2);
                if ($end === false) { throw new RuntimeException('unterminated SQL comment'); }
                $i = $end + 2; $buffer .= ' '; continue;
            }
            if (substr($sql, $i, strlen($delimiter)) === $delimiter) {
                if (trim($buffer) !== '') { $out[] = trim($buffer); }
                $buffer = ''; $i += strlen($delimiter); continue;
            }
            if (in_array($c, ["'", '"', '`'], true)) { $quote = $c; }
            $buffer .= $c; ++$i;
        }
        if ($quote !== null || trim($buffer) !== '' || $delimiter !== ';') { throw new RuntimeException('incomplete SQL statement'); }
        if ($out === []) { throw new RuntimeException('empty migration'); }
        return $out;
    }

    public static function load(string $root, string $catalogHash): array
    {
        if (!preg_match('/^[a-f0-9]{64}$/D', $catalogHash) || is_link($root) || is_link($root.'/migrations') || is_link($root.'/migration-catalog.json')) { throw new RuntimeException('untrusted migration paths'); }
        $raw = file_get_contents($root.'/migration-catalog.json');
        if ($raw === false || !hash_equals($catalogHash, hash('sha256', $raw))) { throw new RuntimeException('catalog integrity mismatch'); }
        $catalog = json_decode($raw, true, 32, JSON_THROW_ON_ERROR);
        if (($catalog['schema'] ?? '') !== 'gojet.migration-catalog.v1' || empty($catalog['migrations']) || !array_is_list($catalog['migrations'])) { throw new RuntimeException('invalid migration catalog'); }
        $rows = []; $names = [];
        foreach ($catalog['migrations'] as $index => $row) {
            $path = $row['path'] ?? '';
            if (!preg_match('#^migrations/([0-9]{6})_([a-z0-9_]+)\.sql$#D', $path, $match) || ($row['id'] ?? null) !== $index + 1 || (int)$match[1] !== $index + 1 || isset($names[$match[2]])) { throw new RuntimeException('migration catalog ordering'); }
            $names[$match[2]] = true;
            if (is_link($root.'/'.$path) || !is_file($root.'/'.$path)) { throw new RuntimeException('invalid migration file'); }
            $sql = file_get_contents($root.'/'.$path);
            if ($sql === false || strlen($sql) !== ($row['bytes'] ?? null) || hash('sha256', $sql) !== ($row['sha256'] ?? null) || ($row['rollback'] ?? '') !== 'restore-verified-database-backup; no automatic down migration') { throw new RuntimeException('migration integrity mismatch'); }
            $row['statements'] = self::statements($sql); $rows[] = $row;
        }
        $actual = glob($root.'/migrations/*.sql'); sort($actual);
        $expected = array_map(fn($r) => $root.'/'.$r['path'], $rows); sort($expected);
        if ($actual !== $expected) { throw new RuntimeException('unaccounted migration file'); }
        return $rows;
    }

    /** Read-only upgrade history check. Both catalogs must be bound to verified packages by the caller.
     * This is not upgrade authorization: backup/restore, version compatibility and
     * service health remain separate requirements. Apply must recheck under its lock.
     */
    public static function inspectUpgrade(PDO $db, string $currentRoot, string $currentHash, string $candidateRoot, string $candidateHash): array
    {
        $current = self::load($currentRoot, $currentHash);
        $candidate = self::load($candidateRoot, $candidateHash);
        if (count($candidate) < count($current)) { throw new RuntimeException('upgrade removes migration history'); }
        foreach ($current as $index => $row) {
            foreach (['id', 'path', 'bytes', 'sha256', 'rollback'] as $key) {
                if ($row[$key] !== $candidate[$index][$key]) { throw new RuntimeException('upgrade rewrites migration history'); }
            }
        }
        $db->setAttribute(PDO::ATTR_ERRMODE, PDO::ERRMODE_EXCEPTION);
        if ($db->inTransaction() || !preg_match('/^8\./', (string)$db->query('SELECT VERSION()')->fetchColumn())
            || (int)$db->query('SELECT @@autocommit')->fetchColumn() !== 1) {
            throw new RuntimeException('native MySQL8 autocommit required');
        }
        $database = $db->query('SELECT DATABASE()')->fetchColumn();
        if (!is_string($database) || $database === '') { throw new RuntimeException('database selection required'); }
        $lock = 'gojet-migrate-'.substr(hash('sha256', $database), 0, 40);
        $stmt = $db->prepare('SELECT GET_LOCK(?, 0)'); $stmt->execute([$lock]);
        if ((int)$stmt->fetchColumn() !== 1) { throw new RuntimeException('migration lock busy'); }
        try {
            $tables = $db->query('SHOW TABLES')->fetchAll(PDO::FETCH_COLUMN);
            if (!in_array('gojet_schema_migrations', $tables, true)) { throw new RuntimeException('upgrade requires managed migration history'); }
            $history = $db->query('SELECT id, checksum, status FROM gojet_schema_migrations ORDER BY id')->fetchAll(PDO::FETCH_ASSOC);
            if (count($history) !== count($current)) { throw new RuntimeException('upgrade current history incomplete or foreign'); }
            foreach ($history as $index => $record) {
                if ((int)$record['id'] !== $current[$index]['id'] || $record['checksum'] !== $current[$index]['sha256']
                    || $record['status'] !== 'applied') { throw new RuntimeException('upgrade history requires verified recovery'); }
            }
            return ['schema' => 'gojet.upgrade-migration-inspection.v1',
                'current_catalog_sha256' => $currentHash, 'candidate_catalog_sha256' => $candidateHash,
                'applied_migrations' => count($current),
                'pending_migration_ids' => array_column(array_slice($candidate, count($current)), 'id'),
                'migration_history_compatible' => true, 'database_mutated' => false,
                'upgrade_authorized' => false, 'backup_restore_verified' => false];
        } finally {
            $stmt = $db->prepare('SELECT RELEASE_LOCK(?)'); $stmt->execute([$lock]);
        }
    }

    public static function apply(PDO $db, string $root, string $catalogHash, string $source, string $version): array
    {
        if (!preg_match('/^[a-f0-9]{40}$/D', $source) || !preg_match('/^[0-9A-Za-z][0-9A-Za-z.+-]{0,63}$/D', $version)) { throw new RuntimeException('invalid release identity'); }
        $rows = self::load($root, $catalogHash); // Snapshot and parse ALL files before any database mutation.
        $db->setAttribute(PDO::ATTR_ERRMODE, PDO::ERRMODE_EXCEPTION);
        if (!preg_match('/^8\./', (string)$db->query('SELECT VERSION()')->fetchColumn()) || $db->inTransaction()) { throw new RuntimeException('native MySQL8 autocommit required'); }
        $settings = $db->query('SELECT @@autocommit, @@log_bin, @@log_bin_trust_function_creators')->fetch(PDO::FETCH_NUM);
        if ((int)$settings[0] !== 1) { throw new RuntimeException('autocommit required for durable migration journal'); }
        if ((int)$settings[1] === 1 && (int)$settings[2] !== 1) { throw new RuntimeException('database administrator must authorize trigger creation before migrations'); }
        $database = $db->query('SELECT DATABASE()')->fetchColumn();
        if (!is_string($database) || $database === '') { throw new RuntimeException('database selection required'); }
        $lock = 'gojet-migrate-'.substr(hash('sha256', $database), 0, 40);
        $stmt = $db->prepare('SELECT GET_LOCK(?, 0)'); $stmt->execute([$lock]);
        if ((int)$stmt->fetchColumn() !== 1) { throw new RuntimeException('migration lock busy'); }
        try {
            $tables = $db->query('SHOW TABLES')->fetchAll(PDO::FETCH_COLUMN);
            if (!in_array('gojet_schema_migrations', $tables, true) && $tables !== []) { throw new RuntimeException('unmanaged nonempty database'); }
            $db->exec("CREATE TABLE IF NOT EXISTS gojet_schema_migrations (id INT UNSIGNED PRIMARY KEY, checksum CHAR(64) NOT NULL, source CHAR(40) NOT NULL, release_version VARCHAR(64) NOT NULL, status ENUM('started','applied','failed') NOT NULL, started_at DATETIME(6) NOT NULL, completed_at DATETIME(6) NULL) ENGINE=InnoDB");
            $applied = $db->query('SELECT id, checksum, status FROM gojet_schema_migrations ORDER BY id')->fetchAll(PDO::FETCH_ASSOC);
            foreach ($applied as $index => $record) {
                if ((int)$record['id'] !== $index + 1 || !isset($rows[$index]) || $record['checksum'] !== $rows[$index]['sha256'] || $record['status'] !== 'applied') { throw new RuntimeException('migration history requires verified recovery'); }
            }
            if ($applied === [] && array_diff($tables, ['gojet_schema_migrations']) !== []) { throw new RuntimeException('unmanaged schema without migration history'); }
            $count = 0;
            foreach (array_slice($rows, count($applied)) as $row) {
                $stmt = $db->prepare("INSERT INTO gojet_schema_migrations(id,checksum,source,release_version,status,started_at) VALUES(?,?,?,?,'started',UTC_TIMESTAMP(6))");
                $stmt->execute([$row['id'], $row['sha256'], $source, $version]);
                try {
                    foreach ($row['statements'] as $sql) { $db->exec($sql); }
                    $stmt = $db->prepare("UPDATE gojet_schema_migrations SET status='applied',completed_at=UTC_TIMESTAMP(6) WHERE id=? AND status='started'"); $stmt->execute([$row['id']]);
                    if ($stmt->rowCount() !== 1) { throw new RuntimeException('journal state mismatch'); }
                    ++$count;
                } catch (Throwable $error) {
                    try { $stmt = $db->prepare("UPDATE gojet_schema_migrations SET status='failed' WHERE id=? AND status='started'"); $stmt->execute([$row['id']]); } catch (Throwable $ignored) { /* Durable started is also a recovery barrier. */ }
                    $state = $error instanceof \PDOException ? (string)($error->errorInfo[0] ?? 'unknown') : 'internal';
                    $driver = $error instanceof \PDOException ? (int)($error->errorInfo[1] ?? 0) : 0;
                    throw new RuntimeException('migration '.$row['id'].' failed; verified database restore/recovery required; SQLSTATE='.$state.' driver='.$driver);
                }
            }
            return ['applied_now' => $count, 'total' => count($rows), 'source' => $source];
        } finally {
            $stmt = $db->prepare('SELECT RELEASE_LOCK(?)'); $stmt->execute([$lock]);
        }
    }
}
