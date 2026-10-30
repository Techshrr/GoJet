<?php
declare(strict_types=1);
namespace GoJet\Installer;
require_once __DIR__.'/UpgradeIO.php';

/** Destructive restore requires the sealed receipt digest and exact original database/server. */
final class DatabaseRestore
{
    public static function verify(string $directory, string $receiptHash): array
    {
        UpgradeIO::directory($directory);
        if (!preg_match('/^[a-f0-9]{64}$/D', $receiptHash)) { throw new \RuntimeException('Invalid restore binding'); }
        $raw = UpgradeIO::read($directory.'/complete.json');
        if (!hash_equals($receiptHash, hash('sha256', $raw))) { throw new \RuntimeException('Changed backup receipt'); }
        $r = json_decode($raw, true, 16, JSON_THROW_ON_ERROR);
        if (($r['schema'] ?? '') !== 'gojet.database-backup.v1' || ($r['file'] ?? '') !== 'database.sql'
            || ($r['encoding'] ?? null) !== ['utf8mb4', 'utf8mb4_0900_ai_ci']
            || ($r['consistency'] ?? '') !== 'mysqldump-lock-all-tables' || !is_int($r['bytes'] ?? null) || $r['bytes'] < 1
            || !preg_match('/^[a-f0-9]{64}$/D', $r['sha256'] ?? '')
            || !preg_match('/^[A-Za-z0-9_]{1,64}$/D', $r['database'] ?? '')
            || !preg_match('/^[a-f0-9-]{36}$/D', $r['server_uuid'] ?? '')) { throw new \RuntimeException('Invalid backup receipt'); }
        $path = $directory.'/database.sql'; clearstatcache(true, $path); $s = @lstat($path);
        if ($s === false || ($s['mode'] & 0177777) !== 0100600 || $s['uid'] !== 0 || $s['nlink'] !== 1
            || $s['size'] !== $r['bytes'] || !hash_equals($r['sha256'], (string)hash_file('sha256', $path))) {
            throw new \RuntimeException('Backup SQL integrity failure');
        }
        return $r;
    }
    private static function fingerprint(\PDO $db): array
    {
        $tables=$db->query('SHOW TABLES')->fetchAll(\PDO::FETCH_COLUMN); sort($tables,SORT_STRING);
        $rows=[];
        foreach ($tables as $table) {
            if (!preg_match('/^[A-Za-z0-9_]+$/D',$table)) { throw new \RuntimeException('Unsupported table name'); }
            $checksum=$db->query('CHECKSUM TABLE `'.$table.'` EXTENDED')->fetch(\PDO::FETCH_ASSOC)['Checksum'];
            if ($checksum===null) { throw new \RuntimeException('Table checksum unavailable'); }
            $schema=$db->query('SHOW CREATE TABLE `'.$table.'`')->fetch(\PDO::FETCH_NUM)[1];
            $rows[$table]=['data'=>(string)$checksum,'schema'=>hash('sha256',$schema)];
        }
        $triggers=$db->query('SELECT TRIGGER_NAME,ACTION_TIMING,EVENT_MANIPULATION,ACTION_STATEMENT FROM information_schema.TRIGGERS WHERE TRIGGER_SCHEMA=DATABASE() ORDER BY TRIGGER_NAME')->fetchAll(\PDO::FETCH_ASSOC);
        return ['tables'=>$rows,'triggers'=>hash('sha256',json_encode($triggers,JSON_THROW_ON_ERROR))];
    }
    /** Real restore into a new disposable database; original database is never the import target. */
    public static function rehearse(array $connection, string $directory, string $receiptHash): array
    {
        $r=self::verify($directory,$receiptHash); $db=UpgradeIO::connection($connection);
        if ($connection['name']!==$r['database'] || $db->query('SELECT @@server_uuid')->fetchColumn()!==$r['server_uuid']) {
            throw new \RuntimeException('Rehearsal source mismatch');
        }
        // The supported GoJet schema has base tables and triggers. Do not execute/import
        // custom events, views or routines with cross-database semantics under root.
        foreach (['SELECT COUNT(*) FROM information_schema.EVENTS WHERE EVENT_SCHEMA=DATABASE()',
            'SELECT COUNT(*) FROM information_schema.ROUTINES WHERE ROUTINE_SCHEMA=DATABASE()',
            "SELECT COUNT(*) FROM information_schema.TABLES WHERE TABLE_SCHEMA=DATABASE() AND TABLE_TYPE<>'BASE TABLE'"] as $sql) {
            if ((int)$db->query($sql)->fetchColumn()!==0) { throw new \RuntimeException('Unsupported custom database objects'); }
        }
        $name='gojet_restore_probe_'.bin2hex(random_bytes(12));
        $credentials=$directory.'/rehearse-'.bin2hex(random_bytes(12)).'.cnf'; $created=false;
        try {
            $password=str_replace(['\\','"'],['\\\\','\\"'],$connection['password']);
            UpgradeIO::write($credentials,"[client]\nhost=127.0.0.1\nprotocol=TCP\nport=".$connection['port']."\nuser=".$connection['user']."\npassword=\"".$password."\"\n");
            $db->exec('CREATE DATABASE `'.$name.'` CHARACTER SET utf8mb4 COLLATE utf8mb4_0900_ai_ci'); $created=true;
            UpgradeIO::command(['/usr/bin/mysql','--defaults-file='.$credentials,'--binary-mode',$name],300,
                $directory.'/database.sql',['HOME'=>$directory]);
            $probe=$connection; $probe['name']=$name; $copy=UpgradeIO::connection($probe);
            $before=self::fingerprint($db); $after=self::fingerprint($copy);
            if ($before!==$after) { throw new \RuntimeException('Restored schema or data differs; source must be quiescent'); }
            return ['restore_verified'=>true,'backup_sha256'=>$r['sha256'],'fingerprint'=>$after];
        } finally {
            if (is_file($credentials)) { unlink($credentials); }
            if ($created) { $db->exec('DROP DATABASE `'.$name.'`'); }
        }
    }
    /** Caller must stop all writers, hold the upgrade lock, and persist restoring BEFORE this call. */
    public static function restore(array $connection, string $directory, string $receiptHash, array $verification): array
    {
        UpgradeIO::root();
        $r = self::verify($directory, $receiptHash);
        if (($verification['restore_verified']??false)!==true || ($verification['backup_sha256']??'')!==$r['sha256']
            || !is_array($verification['fingerprint']['tables']??null)) { throw new \RuntimeException('Verified restore rehearsal required'); }
        $db = UpgradeIO::connection($connection, false);
        if ($connection['name'] !== $r['database'] || $db->query('SELECT @@server_uuid')->fetchColumn() !== $r['server_uuid']) {
            throw new \RuntimeException('Restore database/server mismatch');
        }
        // The current supported schema is created with this fixed encoding by the native installer.
        $s = $db->prepare('SELECT DEFAULT_CHARACTER_SET_NAME,DEFAULT_COLLATION_NAME FROM information_schema.SCHEMATA WHERE SCHEMA_NAME=?');
        $s->execute([$connection['name']]); $encoding = $s->fetch(\PDO::FETCH_NUM);
        if ($encoding !== false && $encoding !== $r['encoding']) { throw new \RuntimeException('Unsupported restore database encoding'); }
        $binary = '/usr/bin/mysql'; $stat = @lstat($binary);
        if ($stat === false || ($stat['mode'] & 0170000) !== 0100000 || $stat['uid'] !== 0 || ($stat['mode'] & 0022) !== 0) {
            throw new \RuntimeException('Trusted restore client required');
        }
        $lockName = 'gojet-migrate-'.substr(hash('sha256', $connection['name']), 0, 40);
        $s = $db->prepare('SELECT GET_LOCK(?,0)'); $s->execute([$lockName]);
        if ((int)$s->fetchColumn() !== 1) { throw new \RuntimeException('Restore migration lock busy'); }
        $credentials = $directory.'/restore-'.bin2hex(random_bytes(16)).'.cnf';
        try {
            $password = str_replace(['\\', '"'], ['\\\\', '\\"'], $connection['password']);
            UpgradeIO::write($credentials, "[client]\nhost=127.0.0.1\nprotocol=TCP\nport=".$connection['port']."\nuser=".$connection['user']."\npassword=\"".$password."\"\n");
            // Dropping the complete schema also removes candidate-only tables and triggers.
            // Partial failure remains in the caller's restoring barrier and must never restart old code.
            $name = $connection['name'];
            $db->exec('DROP DATABASE IF EXISTS `'.$name.'`');
            $db->exec('CREATE DATABASE `'.$name.'` CHARACTER SET utf8mb4 COLLATE utf8mb4_0900_ai_ci');
            UpgradeIO::command([$binary, '--defaults-file='.$credentials, '--binary-mode', $name], 300,
                $directory.'/database.sql', ['HOME' => $directory]);
            if (self::fingerprint(UpgradeIO::connection($connection))!==$verification['fingerprint']) {
                throw new \RuntimeException('Restored database differs from verified backup');
            }
            return ['database_restored' => true, 'backup_sha256' => $r['sha256'], 'healthy' => false];
        } catch (\Throwable) {
            throw new \RuntimeException('Database restore incomplete; keep services stopped and preserve recovery state');
        } finally {
            if (is_file($credentials)) { unlink($credentials); }
            $s = $db->prepare('SELECT RELEASE_LOCK(?)'); $s->execute([$lockName]);
        }
    }
}
