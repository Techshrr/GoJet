<?php
declare(strict_types=1);
require dirname(__DIR__, 3).'/installer/src/InstallState.php';
use GoJet\Installer\InstallState;
function need(bool $value, string $message): void { if (!$value) { throw new RuntimeException($message); } }
function reject(callable $f, string $message): void {
    try { $f(); } catch (RuntimeException $e) { need(str_contains($e->getMessage(), $message), 'Unexpected rejection: '.$e->getMessage()); return; }
    throw new RuntimeException('Missing rejection: '.$message);
}
$dir = sys_get_temp_dir().'/gojet-state-'.bin2hex(random_bytes(8));
mkdir($dir, 0700);
$bootstrap = bin2hex(random_bytes(32));
$state = new InstallState($dir, hash('sha256', $bootstrap));
try {
    reject(fn() => $state->authenticate($bootstrap), 'Unsafe installer state');
    $state->initialize();
    reject(fn() => $state->initialize(), 'already initialized');
    reject(fn() => $state->authenticate(str_repeat('x', 64)), 'authentication failed');
    $a = $state->authenticate($bootstrap);
    reject(fn() => $state->inspect(str_repeat('0', 64)), 'authentication required');
    $calls = 0;
    reject(fn() => $state->check($a['session'], str_repeat('0', 64), 'environment', function () use (&$calls) { $calls++; return true; }), 'CSRF');
    need($calls === 0, 'Unauthorized validation ran');
    reject(fn() => $state->check($a['session'], $a['csrf'], 'admin', fn() => true), 'transition forbidden');
    reject(fn() => $state->check($a['session'], $a['csrf'], 'environment', fn() => true), 'CSRF');
    $b = $state->authenticate($bootstrap);
    reject(fn() => $state->inspect($a['session']), 'authentication required');
    $secret = 'test-only-password-never-serialize';
    $failed = $state->check($b['session'], $b['csrf'], 'environment', function () use ($secret) { throw new RuntimeException($secret); });
    need($failed['step'] === 'environment' && $failed['status'] === 'failed', 'Failure advanced state');
    $raw = file_get_contents($dir.'/state.json');
    foreach ([$secret, $bootstrap, $b['session'], $failed['csrf']] as $value) { need(!str_contains($raw, $value), 'Secret persisted'); }
    $reloaded = new InstallState($dir, hash('sha256', $bootstrap));
    $result = $failed;
    foreach (['environment', 'data', 'site', 'admin'] as $step) {
        $old = $result['csrf'];
        $result = $reloaded->check($b['session'], $old, $step, fn() => true);
        reject(fn() => $state->check($b['session'], $old, $step, fn() => true), 'CSRF');
    }
    need($result['step'] === 'services', 'Sequential checks not durable');
    reject(fn() => $state->check($b['session'], $result['csrf'], 'services', fn() => true), 'transition forbidden');
    $f = fopen($dir.'/state.lock', 'r+'); flock($f, LOCK_EX);
    reject(fn() => $state->inspect($b['session']), 'busy');
    flock($f, LOCK_UN); fclose($f);
    $s = json_decode(file_get_contents($dir.'/state.json'), true);
    $s['step'] = 0; $s['status'] = 'ready';
    file_put_contents($dir.'/state.json', json_encode($s));
    $c = $state->authenticate($bootstrap);
    $child = 'require '.var_export(dirname(__DIR__, 3).'/installer/src/InstallState.php', true).';'
        .'$s=new GoJet\\Installer\\InstallState('.var_export($dir, true).','.var_export(hash('sha256', $bootstrap), true).');'
        .'$s->check('.var_export($c['session'], true).','.var_export($c['csrf'], true).',"environment",function(){exit(17);});';
    $process = proc_open([PHP_BINARY, '-r', $child], [0 => ['pipe','r'], 1 => ['pipe','w'], 2 => ['pipe','w']], $pipes);
    need(is_resource($process), 'Cannot start interrupted-check probe');
    foreach ($pipes as $pipe) { fclose($pipe); }
    need(proc_close($process) === 17, 'Validator termination probe did not run');
    need($state->inspect($c['session'])['status'] === 'checking', 'Termination lost durable barrier');
    $c = $state->authenticate($bootstrap);
    reject(fn() => $state->check($c['session'], $c['csrf'], 'environment', fn() => true), 'transition forbidden');
    $s['expires'] = time()-1; $s['session'] = hash('sha256', $c['session']);
    file_put_contents($dir.'/state.json', json_encode($s));
    reject(fn() => $state->inspect($c['session']), 'authentication required');
    file_put_contents($dir.'/state.json', '{}');
    reject(fn() => $state->authenticate($bootstrap), 'Invalid installer state');
    unlink($dir.'/state.json'); symlink('/dev/null', $dir.'/state.json');
    reject(fn() => $state->authenticate($bootstrap), 'Unsafe installer state');
    unlink($dir.'/state.json');
    touch($dir.'/installed.lock');
    reject(fn() => $state->initialize(), 'unavailable');
    reject(fn() => $state->authenticate($bootstrap), 'unavailable');
    echo json_encode(['private_durable_state' => true, 'auth_rotation_expiry' => true, 'csrf_replay_rejected' => true,
        'sequential_checks' => true, 'validator_failure_redacted' => true, 'interrupted_check_blocks' => true,
        'concurrent_lock_rejected' => true, 'unsafe_state_rejected' => true, 'installed_lock_blocks' => true,
        'web_adapter_and_privileged_apply_verified' => false], JSON_THROW_ON_ERROR)."\n";
} finally {
    foreach (glob($dir.'/*') as $file) { unlink($file); }
    rmdir($dir);
}
