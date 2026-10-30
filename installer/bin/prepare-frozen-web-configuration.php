<?php
declare(strict_types=1);
require_once dirname(__DIR__).'/src/InstallationPreparation.php';
try {
    if (PHP_SAPI !== 'cli' || $argc !== 3) { throw new RuntimeException('Invalid invocation'); }
    echo json_encode(GoJet\Installer\InstallationPreparation::createFromFrozenWebDraft($argv[1], $argv[2]),
        JSON_THROW_ON_ERROR)."\n";
} catch (Throwable) {
    fwrite(STDERR, "Frozen web configuration preparation refused.\n"); exit(1);
}
