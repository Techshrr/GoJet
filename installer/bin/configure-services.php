<?php
declare(strict_types=1);
require_once dirname(__DIR__).'/src/NativeConfiguration.php';
try {
    if (PHP_SAPI !== 'cli' || $argc !== 2 || !function_exists('posix_geteuid') || posix_geteuid() !== 0) { throw new RuntimeException('Invalid invocation'); }
    $raw = stream_get_contents(STDIN, 16385);
    if (strlen($raw) > 16384) { throw new RuntimeException('Input exceeds limit'); }
    $plan = GoJet\Installer\NativeConfiguration::compile(json_decode($raw, true, 16, JSON_THROW_ON_ERROR));
    $result = GoJet\Installer\ServiceEnvironment::create($argv[1], $plan['services']);
    echo json_encode($result + ['configuration_validated' => true, 'max_body_bytes' => $plan['max_body_bytes']], JSON_THROW_ON_ERROR)."\n";
} catch (Throwable) {
    fwrite(STDERR, "Native service configuration refused.\n");
    exit(1);
}
