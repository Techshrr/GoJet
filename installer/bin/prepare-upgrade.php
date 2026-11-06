<?php
declare(strict_types=1);
require_once dirname(__DIR__).'/src/NativeUpgrade.php';
try {
    if (PHP_SAPI !== 'cli' || $argc !== 6) { throw new RuntimeException('Invalid invocation'); }
    GoJet\Installer\UpgradeIO::root();
    $raw=stream_get_contents(STDIN,8193);
    if ($raw===false || strlen($raw)>8192) { throw new RuntimeException('Invalid administrator input'); }
    $administrator=json_decode($raw,true,16,JSON_THROW_ON_ERROR);
    echo json_encode(GoJet\Installer\NativeUpgrade::prepare($argv[1],$argv[2],$argv[3],$argv[4],$argv[5],$administrator),JSON_THROW_ON_ERROR)."\n";
} catch (Throwable) {
    fwrite(STDERR,"Upgrade preparation refused; no migration or service switch was authorized.\n");
    exit(1);
}
