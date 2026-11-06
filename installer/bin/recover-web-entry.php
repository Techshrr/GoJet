<?php
declare(strict_types=1);
require_once dirname(__DIR__).'/src/WebEntryProvisioning.php';
try {
    if (PHP_SAPI !== 'cli' || $argc !== 2 || !function_exists('posix_geteuid') || posix_geteuid() !== 0) {
        throw new RuntimeException('Invalid invocation');
    }
    $raw = stream_get_contents(STDIN, 8193);
    if ($raw === false || strlen($raw) > 8192) { throw new RuntimeException('Oversized request'); }
    $input = json_decode($raw, true, 16, JSON_THROW_ON_ERROR);
    if (!is_array($input)) { throw new RuntimeException('Invalid request'); }
    echo json_encode(GoJet\Installer\WebEntryProvisioning::run($argv[1], $input, true), JSON_THROW_ON_ERROR)."\n";
} catch (Throwable) {
    fwrite(STDERR, "Installer entry recovery refused; preserve root receipts and inspect owned web configuration.\n"); exit(1);
}
