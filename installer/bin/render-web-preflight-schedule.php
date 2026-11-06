<?php
declare(strict_types=1);
require_once dirname(__DIR__).'/src/WebInstallationApply.php';
try {
    if (PHP_SAPI !== 'cli' || $argc !== 1) { throw new RuntimeException('Invalid invocation'); }
    echo json_encode(GoJet\Installer\WebInstallationApply::schedule(), JSON_THROW_ON_ERROR)."\n";
} catch (Throwable) { fwrite(STDERR, "Preflight schedule rendering refused.\n"); exit(1); }
