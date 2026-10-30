<?php
declare(strict_types=1);
require_once dirname(__DIR__).'/src/NativeUpgrade.php';
try {
    if (PHP_SAPI !== 'cli' || $argc !== 3) { throw new RuntimeException('Invalid invocation'); }
    echo json_encode(GoJet\Installer\NativeUpgrade::execute($argv[1],$argv[2],false),JSON_THROW_ON_ERROR)."\n";
} catch (Throwable) {
    fwrite(STDERR,"Upgrade operation refused; preserve the private plan and recovery state.\n");
    exit(1);
}
