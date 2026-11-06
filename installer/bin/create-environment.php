<?php
declare(strict_types=1);
require_once dirname(__DIR__).'/src/ServiceEnvironment.php';
try {
    if (PHP_SAPI !== 'cli' || $argc !== 2) { throw new RuntimeException('Invalid invocation'); }
    $raw = stream_get_contents(STDIN, 131073);
    if (strlen($raw) > 131072) { throw new RuntimeException('Input exceeds limit'); }
    echo json_encode(GoJet\Installer\ServiceEnvironment::create($argv[1], json_decode($raw, true, 16, JSON_THROW_ON_ERROR)), JSON_THROW_ON_ERROR)."\n";
} catch (Throwable) {
    fwrite(STDERR, "Service environment creation refused.\n");
    exit(1);
}
