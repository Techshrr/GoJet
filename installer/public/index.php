<?php

declare(strict_types=1);

require_once dirname(__DIR__).'/src/Shell.php';

use GoJet\Installer\Shell;

header('X-Robots-Tag: noindex, nofollow');
header('Cache-Control: no-store');
header('Content-Type: text/html; charset=utf-8');

echo Shell::render('session-ready');
