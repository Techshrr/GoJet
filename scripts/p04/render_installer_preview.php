<?php

declare(strict_types=1);

// CLI-only component preview. Never route requests through this file or package
// it as an Installer entrypoint; authentication/apply are verified by P21.
if (PHP_SAPI !== 'cli' || $argc !== 1) {
    http_response_code(404);
    exit(1);
}
require_once dirname(__DIR__, 2).'/installer/src/Shell.php';
// This component has no icon asset; avoid an unrelated implicit favicon fetch.
echo str_replace('<head>', '<head><link rel="icon" href="data:,">', GoJet\Installer\Shell::render('session-ready'));
