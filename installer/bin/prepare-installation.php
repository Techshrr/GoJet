<?php
declare(strict_types=1);
require_once dirname(__DIR__).'/src/InstallationPreparation.php';
try {
    if (PHP_SAPI !== 'cli' || $argc !== 5) { throw new RuntimeException('Invalid invocation'); }
    $raw = stream_get_contents(STDIN, 24577);
    if (strlen($raw) > 24576) { throw new RuntimeException('Input exceeds limit'); }
    echo json_encode(GoJet\Installer\InstallationPreparation::create($argv[1], $argv[2], $argv[3], $argv[4],
        json_decode($raw, true, 16, JSON_THROW_ON_ERROR)), JSON_THROW_ON_ERROR)."\n";
} catch (Throwable) {
    fwrite(STDERR, "Installation preparation refused.\n"); exit(1);
}
