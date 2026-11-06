<?php
declare(strict_types=1);
require_once dirname(__DIR__).'/src/WebConfigurationPreparation.php';
try {
    if (PHP_SAPI !== 'cli' || $argc !== 4) { throw new RuntimeException('Invalid invocation'); }
    echo json_encode(GoJet\Installer\WebConfigurationPreparation::recover($argv[1], $argv[2], $argv[3]), JSON_THROW_ON_ERROR)."\n";
} catch (Throwable) { fwrite(STDERR, "Preparation recovery refused; preserve private records and inspect the published plan.\n"); exit(1); }
