<?php
declare(strict_types=1);
require dirname(__DIR__,3).'/installer/src/DatabaseBackup.php';
require dirname(__DIR__,3).'/installer/src/DatabaseRestore.php';
use GoJet\Installer\DatabaseBackup;
use GoJet\Installer\DatabaseRestore;
use GoJet\Installer\UpgradeIO;
function need(bool $ok,string $why): void { if (!$ok) { throw new RuntimeException($why); } }
function rejected(callable $fn): void { try { $fn(); } catch (Throwable) { return; } throw new RuntimeException('Expected rejection'); }
$port=(int)(getenv('P21_UPGRADE_TEST_PORT')?:3306);
$password=getenv('P21_UPGRADE_TEST_PASSWORD');
if ($password===false) { throw new RuntimeException('Explicit disposable database credentials required'); }
$db=new PDO('mysql:host=127.0.0.1;port='.$port,'root',$password,[PDO::ATTR_ERRMODE=>PDO::ERRMODE_EXCEPTION]);
$name='gojet_upgrade_test_'.bin2hex(random_bytes(8));
$root=sys_get_temp_dir().'/gojet-upgrade-db-'.bin2hex(random_bytes(8)); mkdir($root,0700);
$c=['host'=>'127.0.0.1','port'=>$port,'name'=>$name,'user'=>'root','password'=>$password];
try {
    $db->exec('CREATE DATABASE `'.$name.'` CHARACTER SET utf8mb4 COLLATE utf8mb4_0900_ai_ci');
    $live=UpgradeIO::connection($c);
    $live->exec('CREATE TABLE original_data(id INT PRIMARY KEY, value LONGBLOB) ENGINE=InnoDB');
    $s=$live->prepare('INSERT INTO original_data VALUES(?,?)'); $s->execute([1,"\0\xff恢复数据"]);
    DatabaseBackup::create($c,$root.'/backup');
    $hash=hash_file('sha256',$root.'/backup/complete.json');
    $verified=DatabaseRestore::rehearse($c,$root.'/backup',$hash);
    need($verified['restore_verified']===true,'Rehearsal failed');
    $live->exec('CREATE TABLE candidate_only(id INT PRIMARY KEY)');
    $live->exec('DELETE FROM original_data');
    rejected(fn()=>DatabaseRestore::restore($c,$root.'/backup',str_repeat('0',64),$verified));
    need($live->query("SHOW TABLES LIKE 'candidate_only'")->fetchColumn()==='candidate_only','Rejected restore mutated database');
    $other=$c; $other['name']='mysql';
    rejected(fn()=>DatabaseRestore::restore($other,$root.'/backup',$hash,$verified));
    $live=null;
    need(DatabaseRestore::restore($c,$root.'/backup',$hash,$verified)['database_restored']===true,'Restore failed');
    $live=UpgradeIO::connection($c);
    need($live->query('SHOW TABLES')->fetchAll(PDO::FETCH_COLUMN)===['original_data'],'Candidate table survived');
    need($live->query('SELECT value FROM original_data WHERE id=1')->fetchColumn()==="\0\xff恢复数据",'Restored bytes differ');
    $live=null;
    // A crash after DROP DATABASE must remain recoverable from the same sealed backup.
    $db->exec('DROP DATABASE `'.$name.'`');
    need(DatabaseRestore::restore($c,$root.'/backup',$hash,$verified)['database_restored']===true,'Missing-schema recovery failed');
    $live=UpgradeIO::connection($c);
    need($live->query('SELECT value FROM original_data WHERE id=1')->fetchColumn()==="\0\xff恢复数据",'Repeated recovery differs');
    file_put_contents($root.'/backup/database.sql',"\n-- tampered\n",FILE_APPEND);
    rejected(fn()=>DatabaseRestore::restore($c,$root.'/backup',$hash,$verified));
    need((int)$live->query('SELECT COUNT(*) FROM original_data')->fetchColumn()===1,'Tampered backup changed live schema');
    echo json_encode(['native_mysql'=>true,'real_rehearsal'=>true,'real_restore'=>true,'candidate_tables_removed'=>true,
        'binary_unicode_preserved'=>true,'wrong_binding_rejected'=>true,'wrong_database_rejected'=>true,
        'missing_schema_recovery'=>true,'tamper_rejected_before_mutation'=>true,'full_upgrade_verified'=>false],JSON_THROW_ON_ERROR)."\n";
} finally {
    $db->exec('DROP DATABASE IF EXISTS `'.$name.'`');
    $remove=function(string $p) use (&$remove):void {
        if (is_link($p)||is_file($p)) { unlink($p); return; }
        foreach (scandir($p) as $n) { if ($n!=='.'&&$n!=='..') { $remove($p.'/'.$n); } } rmdir($p);
    }; $remove($root);
}
