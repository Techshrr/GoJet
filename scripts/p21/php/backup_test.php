<?php
declare(strict_types=1);
require dirname(__DIR__, 3).'/installer/src/DatabaseBackup.php';
require dirname(__DIR__, 3).'/installer/src/Migrations.php';
use GoJet\Installer\DatabaseBackup;
use GoJet\Installer\Migrations;
function check(bool $condition, string $reason): void { if (!$condition) { throw new RuntimeException($reason); } }
function deny(callable $action): void {
    try { $action(); } catch (Throwable) { return; }
    throw new RuntimeException('Expected backup rejection');
}
$connection = ['host'=>'127.0.0.1','port'=>3306,'name'=>'gojet_p21_backup','user'=>'root','password'=>getenv('P21_BACKUP_PASSWORD')];
$db = new PDO('mysql:host=127.0.0.1;dbname=gojet_p21_backup;charset=utf8mb4', 'root', $connection['password'], [PDO::ATTR_ERRMODE=>PDO::ERRMODE_EXCEPTION]);
$root = getenv('P21_MIGRATION_ROOT');
Migrations::apply($db, $root, hash_file('sha256', $root.'/migration-catalog.json'), getenv('P21_SOURCE'), '10.0.0-rc.1');
$db->exec('CREATE TABLE backup_probe(id INT PRIMARY KEY, payload LONGBLOB, label VARCHAR(128), nullable_value TEXT NULL) ENGINE=InnoDB');
$stmt = $db->prepare('INSERT INTO backup_probe VALUES(?,?,?,?)');
$stmt->execute([1, "\0\x01\xff'\\\n", '恢复验证', null]);
$temporary = sys_get_temp_dir().'/gojet-backup-'.bin2hex(random_bytes(8));
mkdir($temporary, 0700);
$receipt = DatabaseBackup::create($connection, $temporary.'/snapshot');
check($receipt['restore_verified'] === false && $receipt['upgrade_authorized'] === false, 'Backup claimed restore/upgrade');
check(hash_file('sha256', $temporary.'/snapshot/database.sql') === $receipt['sha256'], 'Dump digest mismatch');
check(!file_exists($temporary.'/snapshot/client.cnf'), 'Credential retained');
foreach (['database.sql', 'complete.json'] as $name) {
    check((fileperms($temporary.'/snapshot/'.$name) & 0777) === 0600, 'Public backup file');
}
$original = file_get_contents($temporary.'/snapshot/complete.json');
deny(fn() => DatabaseBackup::create($connection, $temporary.'/snapshot'));
check(file_get_contents($temporary.'/snapshot/complete.json') === $original, 'Existing backup changed');
symlink($temporary.'/snapshot', $temporary.'/linked');
deny(fn() => DatabaseBackup::create($connection, $temporary.'/linked'));
$bad = $connection; $bad['password'] = 'wrong-backup-credential';
deny(fn() => DatabaseBackup::create($bad, $temporary.'/bad-auth'));
check(!file_exists($temporary.'/bad-auth/complete.json'), 'Failed auth claimed success');
// A real account can connect and SELECT but cannot acquire the backup read lock.
$limited = $connection; $limited['user'] = 'gojet_backup_limited'; $limited['password'] = 'p21-limited-only';
deny(fn() => DatabaseBackup::create($limited, $temporary.'/denied-lock'));
check(!file_exists($temporary.'/denied-lock/complete.json') && !file_exists($temporary.'/denied-lock/client.cnf'), 'Failed export retained authority or credentials');
// Restore only into a pre-created disposable CI database, never the source.
$restoreFile = $temporary.'/restore.cnf';
file_put_contents($restoreFile, "[client]\nhost=127.0.0.1\nprotocol=TCP\nuser=root\npassword=\"".str_replace(['\\','"'],['\\\\','\\"'],$connection['password'])."\"\n"); chmod($restoreFile,0600);
$process = proc_open(['/usr/bin/mysql','--defaults-file='.$restoreFile,'gojet_p21_restore'],
    [0=>['file',$temporary.'/snapshot/database.sql','r'],1=>['pipe','w'],2=>['pipe','w']],$pipes,null,['PATH'=>'/usr/bin:/bin','HOME'=>$temporary]);
check(is_resource($process), 'Restore not started');
$out=stream_get_contents($pipes[1]); $err=stream_get_contents($pipes[2]); fclose($pipes[1]); fclose($pipes[2]);
check(proc_close($process)===0 && $out==='' && $err==='', 'Native restore failed'); unlink($restoreFile);
$restored = new PDO('mysql:host=127.0.0.1;dbname=gojet_p21_restore;charset=utf8mb4','root',$connection['password'],[PDO::ATTR_ERRMODE=>PDO::ERRMODE_EXCEPTION]);
$tables=$db->query('SHOW TABLES')->fetchAll(PDO::FETCH_COLUMN);
check($tables===$restored->query('SHOW TABLES')->fetchAll(PDO::FETCH_COLUMN),'Restored table inventory differs');
foreach ($tables as $table) {
    check((bool)preg_match('/^[a-zA-Z0-9_]+$/D',$table),'Unsafe fixture table');
    $sql='CHECKSUM TABLE `'.$table.'` EXTENDED';
    $a=$db->query($sql)->fetch(PDO::FETCH_ASSOC)['Checksum'];
    $b=$restored->query($sql)->fetch(PDO::FETCH_ASSOC)['Checksum'];
    check($a!==null && $a===$b,'Restored table checksum differs');
}
check($db->query('SELECT * FROM backup_probe')->fetchAll(PDO::FETCH_ASSOC)===$restored->query('SELECT * FROM backup_probe')->fetchAll(PDO::FETCH_ASSOC),'Binary/null/Unicode roundtrip failed');
$triggerSQL='SELECT TRIGGER_NAME,ACTION_TIMING,EVENT_MANIPULATION,ACTION_STATEMENT FROM information_schema.TRIGGERS WHERE TRIGGER_SCHEMA=DATABASE() ORDER BY TRIGGER_NAME';
$triggers=$db->query($triggerSQL)->fetchAll(PDO::FETCH_ASSOC);
check(count($triggers)>0 && $triggers===$restored->query($triggerSQL)->fetchAll(PDO::FETCH_ASSOC),'Restored triggers differ');
check(Migrations::apply($restored,$root,hash_file('sha256',$root.'/migration-catalog.json'),getenv('P21_SOURCE'),'10.0.0-rc.1')['applied_now']===0,'Restored migration journal unusable');
echo json_encode(['native_mysql'=>true,'consistent_backup'=>true,'restore_drill'=>true,'tables_verified'=>count($tables),'triggers_verified'=>count($triggers),'binary_null_unicode_roundtrip'=>true,'private_files'=>true,'existing_backup_preserved'=>true,'failed_export_has_no_receipt'=>true,'production_restore_implemented'=>false,'full_t014_acceptance'=>false],JSON_THROW_ON_ERROR)."\n";
