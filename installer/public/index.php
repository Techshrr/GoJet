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
$body = $input === false ? '' : (string)stream_get_contents($input, 4097);
if (is_resource($input)) { fclose($input); }
[$status, $headers, $result] = HttpAccess::handle($_SERVER, $_COOKIE, $body,
    (string)getenv('GOJET_INSTALL_STATE_DIR'), (string)getenv('GOJET_INSTALL_BOOTSTRAP_SHA256'),
    (string)getenv('GOJET_INSTALL_ORIGIN'));
http_response_code($status);
foreach ($headers as $name => $value) { header($name.': '.$value); }
if ($status === 200 && str_starts_with($headers['Content-Type'], 'text/html')) {
    echo Shell::render(match ($result['status']) {
        'failed' => 'retryable-failure', 'checking' => 'step-checking', default => 'session-ready',
    });
} else {
    echo json_encode($result, JSON_THROW_ON_ERROR);
}
