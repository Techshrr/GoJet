<?php
declare(strict_types=1);

// Destructive operations are confined to a freshly created random CI database.
$repo = dirname(__DIR__, 2);
require_once $repo.'/installer/src/Migrations.php';
require_once $repo.'/installer/src/DatabaseBackup.php';
require_once $repo.'/installer/src/DatabaseRestore.php';
use GoJet\Installer\Migrations;
use GoJet\Installer\DatabaseBackup;
use GoJet\Installer\DatabaseRestore;
use GoJet\Installer\UpgradeIO;

function need(bool $condition, string $message): void {
    if (!$condition) { throw new RuntimeException($message); }
}
need(getenv('P22_DISPOSABLE_ADMIN_CI') === '1' && posix_geteuid() === 0, 'Disposable root CI required');
$source = getenv('P22_SOURCE');
need(is_string($source) && preg_match('/^[0-9a-f]{40}$/D', $source) === 1, 'Source required');
$password = getenv('P22_MYSQL_ADMIN_PASSWORD');
need(is_string($password), 'Explicit disposable credentials required');
$name = 'gojet_p22_upgrade_'.bin2hex(random_bytes(8));
$root = '/var/lib/gojet-p22-upgrade-'.bin2hex(random_bytes(8));
mkdir($root, 0700);
$connection = ['host'=>'127.0.0.1','port'=>3306,'name'=>$name,'user'=>'root','password'=>$password];
$server = new PDO('mysql:host=127.0.0.1;port=3306', 'root', $password, [PDO::ATTR_ERRMODE=>PDO::ERRMODE_EXCEPTION]);
$oldSource = '32ea29d766b0ac0790964517ce4e220a1208bf0d';
try {
    $server->exec('CREATE DATABASE `'.$name.'` CHARACTER SET utf8mb4 COLLATE utf8mb4_0900_ai_ci');
    $hashes = [];
    foreach (['before'=>31, 'after'=>32] as $which=>$count) {
        $directory = $root.'/'.$which;
        mkdir($directory, 0700); mkdir($directory.'/migrations', 0700);
        $rows = [];
        foreach (glob($repo.'/migrations/*.sql') as $path) {
            $id = (int)substr(basename($path), 0, 6);
            if ($id > $count) { continue; }
            $raw = file_get_contents($path);
            if ($id <= 31) {
                $process = proc_open(['git','-c','safe.directory='.$repo,'show',$oldSource.':migrations/'.basename($path)],
                    [0=>['file','/dev/null','r'],1=>['pipe','w'],2=>['pipe','w']], $pipes, $repo);
                need(is_resource($process), 'Original migration unavailable');
                $original = stream_get_contents($pipes[1]); fclose($pipes[1]);
                $diagnostic = stream_get_contents($pipes[2]); fclose($pipes[2]);
                need(proc_close($process) === 0 && $diagnostic === '' && $original === $raw, 'Original migration changed');
            }
            copy($path, $directory.'/migrations/'.basename($path));
            $rows[] = ['id'=>$id,'path'=>'migrations/'.basename($path),'sha256'=>hash('sha256',$raw),
                'bytes'=>strlen($raw),'rollback'=>'restore-verified-database-backup; no automatic down migration'];
        }
        need(count($rows) === $count, 'Migration count mismatch');
        file_put_contents($directory.'/migration-catalog.json', json_encode(['schema'=>'gojet.migration-catalog.v1','migrations'=>$rows], JSON_THROW_ON_ERROR));
        $hashes[$which] = hash_file('sha256', $directory.'/migration-catalog.json');
    }
    $db = UpgradeIO::connection($connection);
    Migrations::apply($db, $root.'/before', $hashes['before'], $oldSource, '10.0.0-rc.1');
    $db->exec("INSERT INTO admin_administrators(id,email,email_normalized,display_name) VALUES ('adm_p22','upgrade@example.test','upgrade@example.test','Migration fixture')");
    $insert = $db->prepare('INSERT INTO admin_platform_settings(setting_key,value_json,version,updated_by) VALUES (?,CAST(? AS JSON),?,?)');
    $insert->execute(['general',json_encode(['site_name'=>'Existing 站点','public_base_url'=>'https://example.test','support_url'=>'https://example.test/help']),7,'adm_p22']);
    $insert->execute(['brand',json_encode(['logo_path'=>'/assets/existing.svg','favicon_path'=>'/assets/favicon.ico']),4,'adm_p22']);
    $rows = fn(PDO $d) => $d->query("SELECT setting_key,setting_key+0 AS ordinal,value_json,version,updated_by,updated_at FROM admin_platform_settings WHERE setting_key IN ('general','brand') ORDER BY setting_key")->fetchAll(PDO::FETCH_ASSOC);
    $before = $rows($db);
    $type = fn(PDO $d) => $d->query("SELECT COLUMN_TYPE FROM information_schema.COLUMNS WHERE TABLE_SCHEMA=DATABASE() AND TABLE_NAME='admin_platform_settings' AND COLUMN_NAME='setting_key'")->fetchColumn();
    need($type($db) === "enum('general','brand')", 'Unexpected old enum');
    DatabaseBackup::create($connection, $root.'/backup');
    $backupHash = hash_file('sha256', $root.'/backup/complete.json');
    $restore = DatabaseRestore::rehearse($connection, $root.'/backup', $backupHash);
    need($restore['restore_verified'] === true, 'Restore rehearsal failed');
    $inspection = Migrations::inspectUpgrade($db, $root.'/before', $hashes['before'], $root.'/after', $hashes['after']);
    need($inspection['pending_migration_ids'] === [32], 'Wrong migration prefix');
    Migrations::apply($db, $root.'/after', $hashes['after'], $source, '10.0.0-rc.2');
    need($rows($db) === $before, 'Existing setting bytes/version/ordinal changed');
    need($type($db) === "enum('general','brand','google_one_tap')", 'New enum absent');
    $insert->execute(['google_one_tap','{"enabled":"false"}',1,'adm_p22']);
    need($db->query("SELECT value_json FROM admin_platform_settings WHERE setting_key='google_one_tap'")->fetchColumn() !== false, 'Preference insert failed');
    Migrations::apply($db, $root.'/after', $hashes['after'], $source, '10.0.0-rc.2');
    need((int)$db->query("SELECT COUNT(*) FROM gojet_schema_migrations WHERE status='applied'")->fetchColumn() === 32, 'Migration replay changed history');
    $insert = null; $db = null;
    need(DatabaseRestore::restore($connection, $root.'/backup', $backupHash, $restore)['database_restored'] === true, 'Actual restore failed');
    $db = UpgradeIO::connection($connection);
    need($rows($db) === $before && $type($db) === "enum('general','brand')", 'Original settings/schema not restored');
    need((int)$db->query("SELECT COUNT(*) FROM gojet_schema_migrations WHERE status='applied'")->fetchColumn() === 31, 'Original history not restored');
    $report = ['passed'=>true,'source'=>$source,'original_migrations_unchanged'=>31,'pending_migration'=>32,
        'existing_settings_preserved'=>true,'new_preference_writable'=>true,'managed_migration_replay'=>true,
        'backup_rehearsal'=>true,'actual_backup_restore'=>true,'full_native_upgrade_verified'=>false];
    $output = $repo.'/artifacts/v10/P22/admin-repair';
    if (!is_dir($output)) { mkdir($output, 0755, true); }
    file_put_contents($output.'/settings-upgrade.json', json_encode($report, JSON_PRETTY_PRINT|JSON_THROW_ON_ERROR)."\n");
    echo json_encode($report, JSON_THROW_ON_ERROR)."\n";
} finally {
    $server->exec('DROP DATABASE IF EXISTS `'.$name.'`');
    $remove = function(string $path) use (&$remove): void {
        if (is_link($path) || is_file($path)) { unlink($path); return; }
        foreach (scandir($path) as $part) { if ($part !== '.' && $part !== '..') { $remove($path.'/'.$part); } }
        rmdir($path);
    };
    $remove($root);
}
