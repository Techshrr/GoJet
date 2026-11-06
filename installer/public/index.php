<?php

declare(strict_types=1);

require_once dirname(__DIR__).'/src/Shell.php';
require_once dirname(__DIR__).'/src/HttpAccess.php';

use GoJet\Installer\HttpAccess;
use GoJet\Installer\Shell;

header('X-Robots-Tag: noindex, nofollow');
header('Cache-Control: no-store');
// Read at most one byte beyond the limit; never load an unbounded secret-bearing request.
$input = fopen('php://input', 'rb');
$body = $input === false ? '' : (string)stream_get_contents($input, HttpAccess::bodyLimit((string)($_SERVER['REQUEST_URI'] ?? '')) + 1);
if (is_resource($input)) { fclose($input); }
[$status, $headers, $result] = HttpAccess::handle($_SERVER, $_COOKIE, $body,
    (string)getenv('GOJET_INSTALL_STATE_DIR'), (string)getenv('GOJET_INSTALL_BOOTSTRAP_SHA256'),
    (string)getenv('GOJET_INSTALL_ORIGIN'), (string)getenv('GOJET_INSTALL_COMPLETION_FILE'));
// HTML navigation receives the login surface; API callers keep the original JSON boundary.
$loginPage = $status === 401 && ($_SERVER['REQUEST_METHOD'] ?? '') === 'GET'
    && in_array($_SERVER['REQUEST_URI'] ?? '', ['/install', '/install/'], true)
    && str_contains($_SERVER['HTTP_ACCEPT'] ?? '', 'text/html');
$htmlPage = $loginPage || ($status === 200 && str_starts_with($headers['Content-Type'], 'text/html'));
if ($htmlPage) {
    $headers['Content-Type'] = 'text/html; charset=utf-8';
    $headers['Content-Security-Policy'] .= '; '.Shell::scriptPolicy();
    $headers['Vary'] = 'Accept';
}
http_response_code($status);
foreach ($headers as $name => $value) { header($name.': '.$value); }
if ($htmlPage) { echo Shell::interactive(); }
else { echo json_encode($result, JSON_THROW_ON_ERROR); }
