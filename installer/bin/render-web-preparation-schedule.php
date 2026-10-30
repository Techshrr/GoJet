<?php
declare(strict_types=1);
require_once dirname(__DIR__).'/src/WebConfigurationPreparation.php';
try {
    if (PHP_SAPI !== 'cli' || !in_array($argc, [3, 4], true)) { throw new RuntimeException('Invalid invocation'); }
    echo json_encode(GoJet\Installer\WebConfigurationPreparation::schedule($argv[1], $argv[2], $argv[3] ?? null), JSON_THROW_ON_ERROR)."\n";
} catch (Throwable) { fwrite(STDERR, "Preparation schedule rendering refused.\n"); exit(1); }
