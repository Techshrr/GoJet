<?php
declare(strict_types=1);
require dirname(__DIR__,3).'/installer/src/UpgradeStorage.php';
require dirname(__DIR__,3).'/installer/src/UpgradeSwitch.php';
require dirname(__DIR__,3).'/installer/src/UpgradeMaintenance.php';
use GoJet\Installer\UpgradeIO;
use GoJet\Installer\UpgradeStorage;
use GoJet\Installer\UpgradeSwitch;
function need(bool $ok,string $message): void { if (!$ok) { throw new RuntimeException($message); } }
function rejected(callable $fn): void { try { $fn(); } catch (Throwable) { return; } throw new RuntimeException('Expected rejection'); }
$root=sys_get_temp_dir().'/gojet-upgrade-files-'.bin2hex(random_bytes(8));
mkdir($root,0700);
try {
    rejected(fn()=>UpgradeIO::disjoint([$root.'/storage',$root.'/storage/backup']));
    rejected(fn()=>UpgradeIO::disjoint([$root.'/storage',$root.'/storage']));
    rejected(fn()=>UpgradeIO::disjoint([$root.'/../storage',$root.'/backup']));
    UpgradeIO::disjoint([$root.'/storage',$root.'/storage-copy']);
    UpgradeIO::write($root.'/record','old');
    rejected(fn()=>UpgradeIO::write($root.'/record','overwrite'));
    UpgradeIO::write($root.'/record','new',0600,true);
    need(UpgradeIO::read($root.'/record')==='new','Atomic record replacement failed');
    symlink($root.'/record',$root.'/foreign');
    rejected(fn()=>UpgradeIO::write($root.'/foreign','unsafe',0600,true));
    need(UpgradeIO::read($root.'/record')==='new','Link target modified');
    $files=array_fill_keys(array_keys(UpgradeSwitch::targets()),"old configuration\n");
    $before=UpgradeSwitch::snapshot($root.'/before',$files);
    $files=array_fill_keys(array_keys(UpgradeSwitch::targets()),"new configuration\n");
    $after=UpgradeSwitch::snapshot($root.'/after',$files);
    UpgradeSwitch::verify($root.'/before',$before); UpgradeSwitch::verify($root.'/after',$after);
    symlink($root.'/before',$root.'/active');
    UpgradeSwitch::point($root.'/active',$root.'/before',$root.'/after');
    need(file_get_contents($root.'/active/nginx.conf')==="new configuration\n",'Pointer not switched');
    rejected(fn()=>UpgradeSwitch::point($root.'/active',$root.'/before',$root.'/before'));
    need(readlink($root.'/active')===$root.'/after','Stale switch modified pointer');
    UpgradeSwitch::point($root.'/active',$root.'/after',$root.'/before');
    need(file_get_contents($root.'/active/nginx.conf')==="old configuration\n",'Pointer recovery failed');
    file_put_contents($root.'/after/nginx.conf','tampered');
    rejected(fn()=>UpgradeSwitch::verify($root.'/after',$after));
    mkdir($root.'/storage',0700); mkdir($root.'/storage/quarantine',0700); mkdir($root.'/storage/published',0700);
    file_put_contents($root.'/storage/published/object',"\0binary\xff\n中文"); chmod($root.'/storage/published/object',0640);
    mkdir($root.'/backup',0700);
    $hash=UpgradeStorage::backup($root.'/storage',$root.'/backup');
    file_put_contents($root.'/storage/published/object','changed');
    file_put_contents($root.'/storage/quarantine/new-object','candidate-created');
    UpgradeStorage::restore($root.'/backup',$hash);
    need(file_get_contents($root.'/storage/published/object')==="\0binary\xff\n中文",'Storage content not restored');
    need((fileperms($root.'/storage/published/object') & 0777)===0640,'Storage mode not restored');
    need(!file_exists($root.'/storage/quarantine/new-object'),'Candidate-only file survived restore');
    need(count(glob($root.'/.gojet-preserved-*'))===1,'Failed storage not preserved');
    // Repeating restoration after interruption must be safe and preserve the displaced tree.
    UpgradeStorage::restore($root.'/backup',$hash);
    need(count(glob($root.'/.gojet-preserved-*'))===2,'Repeated restore lost previous tree');
    file_put_contents($root.'/backup/files/published/object','corrupted');
    rejected(fn()=>UpgradeStorage::restore($root.'/backup',$hash));
    need(file_get_contents($root.'/storage/published/object')==="\0binary\xff\n中文",'Corrupt backup modified live tree');
    symlink($root.'/record',$root.'/storage/quarantine/link'); mkdir($root.'/unsafe-backup',0700);
    rejected(fn()=>UpgradeStorage::backup($root.'/storage',$root.'/unsafe-backup'));
    echo json_encode(['private_atomic_records'=>true,'foreign_links_rejected'=>true,'atomic_pointer_and_restore'=>true,
        'stale_pointer_rejected'=>true,'snapshot_tamper_rejected'=>true,'storage_data_modes_restored'=>true,
        'candidate_files_removed_from_active_tree'=>true,'failed_trees_preserved'=>true,'repeat_restore'=>true,
        'corrupt_backup_rejected_before_live_mutation'=>true,'overlapping_paths_refused'=>true,'native_services_tested'=>false],JSON_THROW_ON_ERROR)."\n";
} finally {
    $remove=function(string $p) use (&$remove): void {
        if (is_link($p) || is_file($p)) { unlink($p); return; }
        foreach (scandir($p) as $n) { if ($n!=='.' && $n!=='..') { $remove($p.'/'.$n); } } rmdir($p);
    };
    $remove($root);
}
