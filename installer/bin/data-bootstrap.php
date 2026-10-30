<?php
declare(strict_types=1);
require_once dirname(__DIR__).'/src/DataBootstrap.php';
try {
    if (PHP_SAPI !== 'cli' || $argc !== 2) { throw new RuntimeException('Invalid invocation'); }
    echo json_encode(GoJet\Installer\DataBootstrap::run($argv[1]), JSON_THROW_ON_ERROR)."\n";
} catch (Throwable) {
    fwrite(STDERR, "Root data bootstrap refused; inspect private journal and verify recovery.\n");
    exit(1);
}
