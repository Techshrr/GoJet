<?php
declare(strict_types=1);
require_once dirname(__DIR__).'/src/InstallationApply.php';
try {
    if (PHP_SAPI !== 'cli' || $argc !== 2) { throw new RuntimeException('Invalid invocation'); }
    echo json_encode(GoJet\Installer\InstallationApply::run($argv[1]), JSON_THROW_ON_ERROR)."\n";
} catch (Throwable) {
    fwrite(STDERR, "Installation apply refused; inspect private phase journals before recovery.\n"); exit(1);
}
