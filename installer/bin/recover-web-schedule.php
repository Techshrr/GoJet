<?php
declare(strict_types=1);
require_once dirname(__DIR__).'/src/PreflightScheduleInstallation.php';
try {
    if (PHP_SAPI !== 'cli' || !function_exists('posix_geteuid') || posix_geteuid() !== 0
        || !(($argc === 2 && in_array($argv[1], ['preflight', 'apply'], true))
            || (in_array($argc, [4, 5], true) && $argv[1] === 'preparation'))) {
        throw new RuntimeException('Invalid invocation');
    }
    echo json_encode(GoJet\Installer\PreflightScheduleInstallation::recover($argv[1], $argv[2] ?? null,
        $argv[3] ?? null, $argv[4] ?? null), JSON_THROW_ON_ERROR)."\n";
} catch (Throwable) {
    fwrite(STDERR, "Scheduler recovery refused; preserve original receipts and inspect unit identity and worker status.\n"); exit(1);
}
