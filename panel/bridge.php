<?php
// Fixed container-local paths only. Never invoked by the web process.
declare(strict_types=1);
umask(0077);
require_once '/opt/gojet/native/installer/src/NativeConfiguration.php';
require_once '/opt/gojet/native/installer/src/DataBootstrap.php';
use GoJet\Installer\NativeConfiguration;
use GoJet\Installer\DataBootstrap;
$private='/var/lib/linketic/private';
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
    if ($mode==='database') {
      // The schema name is fixed and belongs solely to this application's named volume.
      $exists=$db->query("SELECT COUNT(*) FROM information_schema.SCHEMATA WHERE SCHEMA_NAME='gojet'")->fetchColumn();
      if (!$exists) {
        $db->exec('CREATE DATABASE gojet CHARACTER SET utf8mb4');
        $db->exec("CREATE USER 'gojet_app'@'127.0.0.1' IDENTIFIED BY ".$db->quote($p['native']['database']['password']));
        $db->exec("GRANT ALL ON gojet.* TO 'gojet_app'@'127.0.0.1'");
      }
      if (!is_dir($private.'/data')) mkdir($private.'/data',0700);
      if (!is_file($private.'/data/request.json')) file_put_contents($private.'/data/request.json',json_encode($request,JSON_THROW_ON_ERROR));
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
      file_put_contents($private.'/services.json',json_encode($c['services'],JSON_THROW_ON_ERROR));
      echo "{}\n"; exit;
    }
    throw new RuntimeException('Invalid operation');
} catch (Throwable $e) { fwrite(STDERR,"Container configuration/data step failed.\n"); exit(1); }
