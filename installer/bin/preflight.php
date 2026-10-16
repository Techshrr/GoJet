<?php
declare(strict_types=1);
require_once dirname(__DIR__).'/src/DependencyPreflight.php';
try {
    if (PHP_SAPI !== 'cli' || $argc !== 1) { throw new RuntimeException('Invalid invocation'); }
    $raw = stream_get_contents(STDIN, 16385);
    if (strlen($raw) > 16384) { throw new RuntimeException('Request too large'); }
    $result = GoJet\Installer\DependencyPreflight::check(json_decode($raw, true, 16, JSON_THROW_ON_ERROR));
    echo json_encode($result, JSON_THROW_ON_ERROR)."\n";
    exit($result['ready'] ? 0 : 2);
} catch (Throwable) {
    fwrite(STDERR, "Native dependency preflight refused.\n");
    exit(1);
}
