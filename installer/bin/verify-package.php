<?php
declare(strict_types=1);
require_once dirname(__DIR__).'/src/PackageIntegrity.php';
try {
    if (PHP_SAPI !== 'cli' || $argc !== 4) { throw new RuntimeException('Invalid invocation'); }
    echo json_encode(GoJet\Installer\PackageIntegrity::verify($argv[1], $argv[2], $argv[3]), JSON_THROW_ON_ERROR)."\n";
} catch (Throwable) {
    fwrite(STDERR, "Package integrity verification refused.\n");
    exit(1);
}
