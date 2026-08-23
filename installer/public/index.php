<?php

declare(strict_types=1);

require_once dirname(__DIR__).'/src/Shell.php';
require_once dirname(__DIR__).'/src/FilePreflight.php';

use GoJet\Installer\FilePreflight;
use GoJet\Installer\Shell;

header('X-Robots-Tag: noindex, nofollow');
header('Cache-Control: no-store');
header('Content-Type: text/html; charset=utf-8');

$path = parse_url((string)($_SERVER['REQUEST_URI'] ?? '/install'), PHP_URL_PATH) ?: '/install';
$state = 'session-ready';
if (in_array($path, ['/install/environment', '/install/services', '/install/health'], true)) {
    $result = FilePreflight::check();
    $state = $result['state'];
    if ($state === 'hard-failure') {
        http_response_code(503);
    }
}

echo Shell::render($state);
