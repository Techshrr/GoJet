<?php
declare(strict_types=1);
require_once dirname(__DIR__).'/src/PreflightScheduleInstallation.php';
try {
    if (PHP_SAPI !== 'cli' || !in_array($argc, [3, 4], true)) { throw new RuntimeException('Invalid invocation'); }
    echo json_encode(GoJet\Installer\PreflightScheduleInstallation::installPreparation($argv[1], $argv[2], $argv[3] ?? null), JSON_THROW_ON_ERROR)."\n";
} catch (Throwable) { fwrite(STDERR, "Preparation schedule provisioning refused; inspect root receipts and units before recovery.\n"); exit(1); }
