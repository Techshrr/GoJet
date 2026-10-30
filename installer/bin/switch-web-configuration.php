<?php
declare(strict_types=1);
require_once dirname(__DIR__).'/src/WebConfigurationPreparation.php';
try {
    if (PHP_SAPI !== 'cli' || $argc !== 1 || !function_exists('posix_geteuid') || posix_geteuid() !== 0) {
        throw new RuntimeException('Invalid invocation');
    }
    $raw = stream_get_contents(STDIN, 8193);
    if ($raw === false || strlen($raw) > 8192) { throw new RuntimeException('Oversized request'); }
    $input = json_decode($raw, true, 16, JSON_THROW_ON_ERROR);
    if (!is_array($input)) { throw new RuntimeException('Invalid request'); }
    echo json_encode(GoJet\Installer\WebConfigurationPreparation::switchWeb($input), JSON_THROW_ON_ERROR)."\n";
} catch (Throwable) {
    fwrite(STDERR, "Installer FPM switch refused; preserve switch receipts and inspect the active pool.\n"); exit(1);
}
