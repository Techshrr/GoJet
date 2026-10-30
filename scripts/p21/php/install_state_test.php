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
    // Draft-only state fixture: no native preparation, approval or execution is implied.
    $draft = ['configuration' => ['schema' => 'gojet.native-configuration.v1', 'origin' => 'https://example.test',
        'database' => ['port' => 3306, 'name' => 'test', 'user' => 'test', 'password' => ''],
        'redis' => ['port' => 6379, 'db' => 0, 'password' => ''], 'files' => ['max_upload_bytes' => 1, 'max_files' => 1, 'max_bytes' => 1, 'types' => []],
        'quotas' => ['text' => 100, 'bio' => 100, 'qr' => 100],
        'smtp' => ['host' => 'smtp.example.test', 'port' => 587, 'from' => 'mail@example.test', 'username' => '', 'password' => ''],
        'risk' => ['name' => 'fixture', 'endpoint' => 'https://example.test/check', 'policy_version' => 'v1'],
        'turnstile_secret' => 'private-test-token', 'auth_rate' => ['limit' => 10, 'window_seconds' => 60]],
        'admin' => ['email' => 'admin@example.test', 'display_name' => 'Admin', 'password' => 'private-test-password']];
    require_once dirname(__DIR__, 3).'/installer/src/ConfigurationDraft.php';
    // Reject structural mistakes before a draft can become frozen; never echo input values.
    foreach (['database', 'redis', 'quotas', 'smtp', 'risk', 'auth_rate'] as $section) {
        $bad = $draft; $bad['configuration'][$section]['unexpected'] = 'private-value';
        reject(fn() => GoJet\Installer\ConfigurationDraft::validate($bad), 'Invalid configuration draft');
        $bad = $draft; array_pop($bad['configuration'][$section]);
        reject(fn() => GoJet\Installer\ConfigurationDraft::validate($bad), 'Invalid configuration draft');
    }
    $bad = $draft; $bad['configuration']['schema'] = 'unknown';
    reject(fn() => GoJet\Installer\ConfigurationDraft::validate($bad), 'Invalid configuration draft');
    foreach (['success', 'publication-failure'] as $case) {
        $draftDir = $dir.'/draft-'.$case;
        mkdir($draftDir, 0700);
        $draftState = new InstallState($draftDir, hash('sha256', $bootstrap));
        try {
            $draftState->initialize(); $auth = $draftState->authenticate($bootstrap);
            $draftPath = $draftDir.'/configuration-draft.json';
            reject(fn() => $draftState->submitConfiguration(str_repeat('0', 64), $auth['csrf'], $draft), 'authentication required');
            reject(fn() => $draftState->submitConfiguration($auth['session'], str_repeat('0', 64), $draft), 'CSRF');
            need(!file_exists($draftPath), 'Unauthorized draft published');
            $bad = $draft; $bad['configuration']['database']['command'] = '/bin/sh';
            reject(fn() => $draftState->submitConfiguration($auth['session'], $auth['csrf'], $bad), 'Invalid configuration draft');
            need($draftState->inspect($auth['session'])['status'] === 'ready', 'Invalid draft froze state');
            reject(fn() => $draftState->submitConfiguration($auth['session'], $auth['csrf'], $draft), 'CSRF');
            $auth = $draftState->authenticate($bootstrap);
            $bad = $draft; $bad['admin']['password'] = str_repeat('x', 24576);
            reject(fn() => $draftState->submitConfiguration($auth['session'], $auth['csrf'], $bad), 'exceeds limit');
            need(!file_exists($draftPath), 'Oversized draft published');
            $auth = $draftState->authenticate($bootstrap);
            $lock = fopen($draftDir.'/state.lock', 'r+'); flock($lock, LOCK_EX);
            reject(fn() => $draftState->submitConfiguration($auth['session'], $auth['csrf'], $draft), 'busy');
            flock($lock, LOCK_UN); fclose($lock);
            if ($case === 'publication-failure') {
                mkdir($draftPath, 0700);
                reject(fn() => $draftState->submitConfiguration($auth['session'], $auth['csrf'], $draft), 'already exists');
                rmdir($draftPath); // Remove only the injected obstruction; the durable barrier must remain.
            } else {
                $reply = $draftState->submitConfiguration($auth['session'], $auth['csrf'], $draft);
                need($reply === ['step' => 'environment', 'status' => 'checking'], 'Draft response disclosed data or advanced approval');
                need(json_decode(file_get_contents($draftPath), true) === $draft, 'Draft bytes lost');
                $snapshot = GoJet\Installer\ConfigurationDraft::frozen($draftDir, fileowner($draftDir));
                need($snapshot['draft'] === $draft && $snapshot['sha256'] === hash_file('sha256', $draftPath), 'Frozen snapshot mismatch');
                reject(fn() => GoJet\Installer\ConfigurationDraft::frozen($draftDir, fileowner($draftDir)+1), 'Unsafe draft directory');
                chmod($draftPath, 0644);
                reject(fn() => GoJet\Installer\ConfigurationDraft::frozen($draftDir, fileowner($draftDir)), 'Unsafe configuration draft');
                chmod($draftPath, 0600);
                rename($draftPath, $draftPath.'.saved'); symlink($draftPath.'.saved', $draftPath);
                reject(fn() => GoJet\Installer\ConfigurationDraft::frozen($draftDir, fileowner($draftDir)), 'Unsafe configuration draft');
                unlink($draftPath); rename($draftPath.'.saved', $draftPath);
                link($draftPath, $draftPath.'.link');
                reject(fn() => GoJet\Installer\ConfigurationDraft::frozen($draftDir, fileowner($draftDir)), 'Unsafe configuration draft');
                unlink($draftPath.'.link');
                need(fileowner($draftPath) === fileowner($draftDir) && (fileperms($draftPath) & 0777) === 0600
                    && stat($draftPath)['nlink'] === 1, 'Unsafe draft file');
            }
            need($draftState->inspect($auth['session'])['status'] === 'checking', 'Draft publication lost freeze barrier');
            $before = file_exists($draftPath) ? file_get_contents($draftPath) : null;
            $auth = $draftState->authenticate($bootstrap);
            reject(fn() => $draftState->submitConfiguration($auth['session'], $auth['csrf'], $draft), 'transition forbidden');
            need((file_exists($draftPath) ? file_get_contents($draftPath) : null) === $before, 'Reauthentication replaced draft');
            $auth = $draftState->authenticate($bootstrap);
            reject(fn() => $draftState->submitApply($auth['session'], $auth['csrf']), 'transition forbidden');
            foreach (['private-test-token', 'private-test-password'] as $value) {
                need(!str_contains(file_get_contents($draftDir.'/state.json'), $value), 'Secret leaked into status state');
            }
            need(!file_exists($draftDir.'/apply-request.json'), 'Draft authorized execution');
        } finally {
            foreach (glob($draftDir.'/*') as $file) { is_dir($file) ? rmdir($file) : unlink($file); }
            rmdir($draftDir);
        }
    }
    echo json_encode(['private_durable_state' => true, 'auth_rotation_expiry' => true, 'csrf_replay_rejected' => true,
        'sequential_checks' => true, 'validator_failure_redacted' => true, 'interrupted_check_blocks' => true,
        'concurrent_lock_rejected' => true, 'unsafe_state_rejected' => true, 'installed_lock_blocks' => true,
        'private_configuration_draft' => true, 'draft_auth_csrf_bounds' => true,
        'draft_reauthentication_no_replacement' => true, 'draft_publication_failure_blocks' => true,
        'frozen_snapshot_identity_mode_links' => true,
        'web_adapter_and_privileged_apply_verified' => false], JSON_THROW_ON_ERROR)."\n";
} finally {
    foreach (glob($dir.'/*') as $file) { unlink($file); }
    rmdir($dir);
}
