<?php
declare(strict_types=1);
require_once dirname(__DIR__).'/src/WebConfigurationPreparation.php';
try {
    if (PHP_SAPI !== 'cli' || $argc !== 3) { throw new RuntimeException('Invalid invocation'); }
    echo json_encode(GoJet\Installer\WebConfigurationPreparation::run($argv[1], $argv[2]), JSON_THROW_ON_ERROR)."\n";
} catch (Throwable) {
    fwrite(STDERR, "Web preparation refused; inspect private preparation records before recovery.\n"); exit(1);
}
