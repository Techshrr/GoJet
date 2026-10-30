<?php
declare(strict_types=1);
require_once dirname(__DIR__).'/src/WebInstallationApply.php';
try {
    if (PHP_SAPI !== 'cli' || $argc !== 4) { throw new RuntimeException('Invalid invocation'); }
    echo json_encode(GoJet\Installer\WebInstallationApply::provision($argv[1], $argv[2], $argv[3]), JSON_THROW_ON_ERROR)."\n";
} catch (Throwable) { fwrite(STDERR, "Web apply provisioning refused.\n"); exit(1); }
