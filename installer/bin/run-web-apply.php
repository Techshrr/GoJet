<?php
declare(strict_types=1);
require_once dirname(__DIR__).'/src/WebInstallationApply.php';
try {
    if (PHP_SAPI !== 'cli' || $argc !== 1) { throw new RuntimeException('Invalid invocation'); }
    echo json_encode(GoJet\Installer\WebInstallationApply::run(), JSON_THROW_ON_ERROR)."\n";
} catch (Throwable $error) {
    fwrite(STDERR, "Web apply refused; inspect root binding and phase journals.\n");
    // Root CLI diagnostics contain only shipped source locations, never messages or stack arguments.
    for ($depth = 0; $error !== null && $depth < 4; $depth++, $error = $error->getPrevious()) {
        if ($error instanceof GoJet\Installer\NativeDependencyFailure) {
            fwrite(STDERR, 'Web apply dependency failures: '.implode(',', $error->failedChecks)."\n");
        }
        $file = $error->getFile();
        if (dirname($file) === dirname(__DIR__).'/src' && preg_match('/^[A-Za-z]+\.php$/D', basename($file))) {
            fwrite(STDERR, 'Web apply failure location: '.basename($file).':'.$error->getLine()."\n");
        }
    }
    exit(1);
}
