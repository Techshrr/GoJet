<?php
declare(strict_types=1);
require_once dirname(__DIR__).'/src/InstallationCompletion.php';
try {
    if (PHP_SAPI !== 'cli' || $argc !== 2) { throw new RuntimeException('Invalid invocation'); }
    echo json_encode(GoJet\Installer\InstallationCompletion::run($argv[1], false), JSON_THROW_ON_ERROR)."\n";
} catch (Throwable) {
    fwrite(STDERR, "Installation verification refused; completion not acknowledged.\n"); exit(1);
}
