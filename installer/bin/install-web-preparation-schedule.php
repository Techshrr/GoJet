<?php
declare(strict_types=1);
require_once dirname(__DIR__).'/src/PreflightScheduleInstallation.php';
try {
    if (PHP_SAPI !== 'cli' || $argc !== 3) { throw new RuntimeException('Invalid invocation'); }
    echo json_encode(GoJet\Installer\PreflightScheduleInstallation::installPreparation($argv[1], $argv[2]), JSON_THROW_ON_ERROR)."\n";
} catch (Throwable) { fwrite(STDERR, "Preparation schedule provisioning refused; inspect root receipts and units before recovery.\n"); exit(1); }
