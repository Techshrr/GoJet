<?php
declare(strict_types=1);
require_once dirname(__DIR__).'/src/WebConfigurationPreparation.php';
try {
    if (PHP_SAPI !== 'cli' || $argc !== 6) { throw new RuntimeException('Invalid invocation'); }
    echo json_encode(GoJet\Installer\WebConfigurationPreparation::recoverRestart($argv[1], $argv[2], $argv[3], $argv[4], $argv[5]), JSON_THROW_ON_ERROR)."\n";
} catch (Throwable) { fwrite(STDERR, "Configuration restart recovery refused; preserve old attempts and inspect private restart records.\n"); exit(1); }
