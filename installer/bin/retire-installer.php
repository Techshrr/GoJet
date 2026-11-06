<?php
declare(strict_types=1);
require_once dirname(__DIR__).'/src/InstallerRetirement.php';
try {
    if (PHP_SAPI !== 'cli' || $argc !== 3) { throw new RuntimeException('Invalid invocation'); }
    echo json_encode(GoJet\Installer\InstallerRetirement::run($argv[1], $argv[2]), JSON_THROW_ON_ERROR)."\n";
} catch (Throwable) {
    fwrite(STDERR, "Installer retirement refused; preserve completion and retirement receipts and inspect owned resources.\n"); exit(1);
}
