<?php
declare(strict_types=1);
require_once dirname(__DIR__).'/src/InstallationPreparation.php';
try {
    if (PHP_SAPI !== 'cli' || $argc !== 2) { throw new RuntimeException('Invalid invocation'); }
    echo json_encode(GoJet\Installer\InstallationPreparation::runData($argv[1]), JSON_THROW_ON_ERROR)."\n";
} catch (Throwable) {
    fwrite(STDERR, "Prepared data execution refused; verify private inputs and recovery.\n"); exit(1);
}
