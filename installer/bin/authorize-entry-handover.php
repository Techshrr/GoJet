<?php
declare(strict_types=1);
require_once dirname(__DIR__).'/src/EntryHandover.php';
try {
    if (PHP_SAPI !== 'cli' || $argc !== 3) { throw new RuntimeException('Invalid invocation'); }
    echo json_encode(GoJet\Installer\EntryHandover::authorize($argv[1], $argv[2]), JSON_THROW_ON_ERROR)."\n";
} catch (Throwable) {
    fwrite(STDERR, "Entry handover authorization refused; inspect the sealed plan and original entry receipts.\n"); exit(1);
}
