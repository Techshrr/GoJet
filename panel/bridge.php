<?php
// Fixed container-local paths only. Never invoked by the web process.
declare(strict_types=1);
umask(0077);
require_once '/opt/gojet/native/installer/src/NativeConfiguration.php';
require_once '/opt/gojet/native/installer/src/DataBootstrap.php';
use GoJet\Installer\NativeConfiguration;
use GoJet\Installer\DataBootstrap;
$private='/var/lib/linketic/private';
function savePrivate(string $path, array $value): void {
    $tmp=$path.'.'.bin2hex(random_bytes(8)).'.tmp';
    $f=fopen($tmp,'x+b');
    if ($f===false) throw new RuntimeException('Cannot persist state');
    try { $raw=json_encode($value,JSON_THROW_ON_ERROR); if(fwrite($f,$raw)!==strlen($raw)||!fflush($f)||!fsync($f)) throw new RuntimeException('Cannot sync state'); }
    finally { fclose($f); }
    if(!rename($tmp,$path)) throw new RuntimeException('Cannot commit state');
    $dir=fopen(dirname($path),'r'); try {if(!fsync($dir)) throw new RuntimeException('Cannot sync directory');} finally {fclose($dir);}
}

try {
    if (posix_geteuid()!==0) throw new RuntimeException('Root container worker required');
    $mode=$argv[1]??'';
    $p=json_decode(file_get_contents($private.'/configuration.json'),true,32,JSON_THROW_ON_ERROR);
    $c=NativeConfiguration::compile($p['native']);
    $r='/opt/gojet/native';
    $request=['schema'=>'gojet.root-data-bootstrap.v1','release'=>$r,
      'source'=>'ce305a9207fd2fe46396e7cf433137c48ab5aec1','version'=>'10.0.0-rc.2',
      'catalog_sha256'=>hash_file('sha256',$r.'/migration-catalog.json'),
      'platformapi_sha256'=>hash_file('sha256',$r.'/bin/platformapi'),
      'database'=>['host'=>'127.0.0.1']+$p['native']['database'],
      'redis'=>['host'=>'127.0.0.1']+$p['native']['redis'], 'admin'=>$p['admin'],
      'admin_cipher'=>['id'=>$p['native']['secrets']['key_id'],'key_hex'=>$p['native']['secrets']['admin']], 'origin'=>$p['native']['origin']];
    if ($mode==='validate') { DataBootstrap::validateRequest($request); echo "{}\n"; exit; }
    $root=json_decode(file_get_contents($private.'/mysql.json'),true,8,JSON_THROW_ON_ERROR);
    $db=new PDO('mysql:unix_socket=/run/mysqld/mysqld.sock;charset=utf8mb4','root',$root['password'],[PDO::ATTR_ERRMODE=>PDO::ERRMODE_EXCEPTION]);
    $claim=$private.'/database-claimed.json';
    if ($mode==='recover') {
      $journal=$private.'/data/journal.json';
      $recovery=$private.'/recovery.json';
      if(!is_file($recovery)) {
        if(!is_file($journal)) { echo "{}\n"; exit; }
        $j=json_decode(file_get_contents($journal),true,8,JSON_THROW_ON_ERROR);
        if(($j['phase']??'')==='data-complete') { echo "{}\n"; exit; }
      }
      // Recovery is limited to an owned first-install schema, never an activated site.
      if(!is_file($claim)||is_file($private.'/activation-started.json')||is_file($private.'/installed.json')) throw new RuntimeException('Recovery ownership refused');
      $owner=json_decode(file_get_contents($claim),true,8,JSON_THROW_ON_ERROR);
      $identity=hash_file('sha256',$private.'/configuration.json');
      if(($owner['configuration_sha256']??'')!==$identity) throw new RuntimeException('Recovery identity mismatch');
      if(!is_file($recovery)) {
        $state=['configuration_sha256'=>$identity,'archive'=>'failed-initialization-'.bin2hex(random_bytes(8)),'backed_up'=>false];
        savePrivate($recovery,$state);
      }
      $state=json_decode(file_get_contents($recovery),true,8,JSON_THROW_ON_ERROR);
      if(($state['configuration_sha256']??'')!==$identity||!preg_match('/^failed-initialization-[a-f0-9]{16}$/D',$state['archive']??'')) throw new RuntimeException('Invalid recovery state');
      $backup=$private.'/'.$state['archive'];
      if(!is_dir($backup)&&!mkdir($backup,0700)) throw new RuntimeException('Recovery archive unavailable');
      if(!$state['backed_up']) {
        $process=proc_open(['/usr/bin/mysqldump','--socket=/run/mysqld/mysqld.sock','--user=root','--single-transaction','--routines','--triggers','--set-gtid-purged=OFF','gojet'],[0=>['file','/dev/null','r'],1=>['file',$backup.'/database.sql','w'],2=>['file','/dev/null','w']],$pipes,null,['PATH'=>'/usr/bin:/bin','MYSQL_PWD'=>$root['password']]);
        if(!is_resource($process)||proc_close($process)!==0) throw new RuntimeException('Recovery backup refused');
        $dump=fopen($backup.'/database.sql','r'); try {if(!fsync($dump)) throw new RuntimeException('Recovery backup sync failed');} finally {fclose($dump);}
        savePrivate($backup.'/backup.json',['configuration_sha256'=>$identity]);
        $state['backed_up']=true; savePrivate($recovery,$state);
      }
      // Once the durable backup is sealed, interrupted DROP/rename steps can be retried.
      $db->exec('DROP DATABASE IF EXISTS gojet');
      $db->exec("DROP USER IF EXISTS 'gojet_app'@'127.0.0.1'");
      if(is_dir($private.'/data')) {
        if(is_dir($backup.'/data')||!rename($private.'/data',$backup.'/data')) throw new RuntimeException('Recovery journal preservation failed');
      } elseif(!is_dir($backup.'/data')) { throw new RuntimeException('Recovery journal missing'); }
      savePrivate($backup.'/recovered.json',['recovered'=>true]);
      unlink($recovery);
      $dir=fopen($private,'r'); try {if(!fsync($dir)) throw new RuntimeException('Recovery sync failed');} finally {fclose($dir);}
      echo "{}\n"; exit;
    }
    if ($mode==='database') {
      $exists=$db->query("SELECT COUNT(*) FROM information_schema.SCHEMATA WHERE SCHEMA_NAME='gojet'")->fetchColumn();
      if(!is_file($claim)) {
        if($exists || $db->query("SELECT COUNT(*) FROM mysql.user WHERE User='gojet_app'")->fetchColumn()) throw new RuntimeException('Existing database is not owned by this installer');
        savePrivate($claim,['configuration_sha256'=>hash_file('sha256',$private.'/configuration.json')]);
      }
      $owner=json_decode(file_get_contents($claim),true,8,JSON_THROW_ON_ERROR);
      if(($owner['configuration_sha256']??'')!==hash_file('sha256',$private.'/configuration.json')) throw new RuntimeException('Database owner mismatch');
      $db->exec('CREATE DATABASE IF NOT EXISTS gojet CHARACTER SET utf8mb4');
      $db->exec("CREATE USER IF NOT EXISTS 'gojet_app'@'127.0.0.1' IDENTIFIED BY ".$db->quote($p['native']['database']['password']));
      $db->exec("GRANT ALL ON gojet.* TO 'gojet_app'@'127.0.0.1'");
      if (!is_dir($private.'/data')) mkdir($private.'/data',0700);
      if (!is_file($private.'/data/request.json')) savePrivate($private.'/data/request.json',$request);
      DataBootstrap::run($private.'/data');
      echo "{}\n"; exit;
    }
    if ($mode==='finish') {
      $db->exec('USE gojet');
      $db->beginTransaction();
      if (!$db->query('SELECT COUNT(*) FROM admin_turnstile_config WHERE id=1')->fetchColumn()) {
        $nonce=random_bytes(12); $tag='';
        $cipher=openssl_encrypt($p['native']['turnstile_secret'],'aes-256-gcm',hex2bin($p['native']['secrets']['admin']),OPENSSL_RAW_DATA,$nonce,$tag,'admin-turnstile:singleton');
        if ($cipher===false) throw new RuntimeException('Encryption failed');
        $q=$db->prepare("INSERT INTO admin_turnstile_config(id,site_key,secret_ciphertext,secret_key_id,enabled,provider_state,version,updated_by) SELECT 1,?,?,?,1,'healthy',1,id FROM admin_administrators LIMIT 1");
        $q->execute([$p['site_key'],$nonce.$cipher.$tag,$p['native']['secrets']['key_id']]);
      }
      $db->commit();
      savePrivate($private.'/services.json',$c['services']);
      echo "{}\n"; exit;
    }
    throw new RuntimeException('Invalid operation');
} catch (Throwable $e) { fwrite(STDERR,"Container data step refused: ".($mode??"unknown")." / ".get_class($e)." / ".$e->getCode()."\n"); exit(1); }
