<?php

declare(strict_types=1);
namespace GoJet\Installer;

/** Internal state authority. HTTP and privileged apply adapters must not accept state from clients. */
final class InstallState
{
    private const STEPS = ['environment', 'data', 'site', 'admin', 'services', 'health', 'complete'];

    public function __construct(private readonly string $directory, private readonly string $bootstrapHash)
    {
        if (!preg_match('/^[a-f0-9]{64}$/D', $bootstrapHash)
            || realpath($directory) !== $directory || !is_dir($directory)
            || (fileperms($directory) & 0777) !== 0700) {
            throw new \RuntimeException('Unsafe installer state configuration');
        }
    }

    /** Explicit provisioning only. Missing/corrupt state during requests must never reopen an install. */
    public function initialize(): void
    {
        $this->locked(function (): void {
            if (file_exists($this->directory.'/state.json') || is_link($this->directory.'/state.json')) {
                throw new \RuntimeException('Installer already initialized');
            }
            $this->save(['schema' => 2, 'step' => 0, 'status' => 'ready', 'session' => null,
                'expires' => 0, 'csrf' => null, 'attempts' => 0, 'retry_at' => 0]);
        });
    }

    /** Called only by a TLS POST adapter; bootstrap token never appears in URLs or durable state. */
    public function authenticate(string $bootstrap): array
    {
        return $this->locked(function () use ($bootstrap): array {
            $s = $this->read();
            if ($s['retry_at'] <= time()) { $s['attempts'] = 0; $s['retry_at'] = time() + 60; }
            if ($s['attempts'] >= 10) { throw new \RuntimeException('Installer authentication rate limited'); }
            $s['attempts']++;
            $this->save($s);
            if (strlen($bootstrap) < 32 || strlen($bootstrap) > 256
                || !hash_equals($this->bootstrapHash, hash('sha256', $bootstrap))) {
                throw new \RuntimeException('Installer authentication failed');
            }
            // Re-authentication rotates authority but cannot erase interrupted/failed progress.
            $session = bin2hex(random_bytes(32));
            $csrf = bin2hex(random_bytes(32));
            $s['session'] = hash('sha256', $session);
            $s['csrf'] = hash('sha256', $csrf);
            $s['expires'] = time() + 1800;
            $this->save($s);
            return ['session' => $session, 'csrf' => $csrf, 'step' => self::STEPS[$s['step']], 'status' => $s['status']];
        });
    }

    public function inspect(string $session): array
    {
        return $this->locked(function () use ($session): array {
            $s = $this->authorized($session);
            return ['step' => self::STEPS[$s['step']], 'status' => $s['status']];
        });
    }

    /** Execute trusted server-side validation, never a request-supplied success flag or callback name. */
    public function check(string $session, string $csrf, string $step, callable $validate): array
    {
        return $this->locked(function () use ($session, $csrf, $step, $validate): array {
            $s = $this->authorized($session);
            if (!preg_match('/^[a-f0-9]{64}$/D', $csrf) || !is_string($s['csrf'])
                || !hash_equals($s['csrf'], hash('sha256', $csrf))) {
                throw new \RuntimeException('Invalid installer CSRF token');
            }
            // Consume durably before validation, including rejected/out-of-order submissions.
            $s['csrf'] = null;
            $this->save($s);
            if ($step !== self::STEPS[$s['step']] || $s['step'] >= 4
                || !in_array($s['status'], ['ready', 'failed'], true)) {
                throw new \RuntimeException('Installer transition forbidden');
            }
            $s['status'] = 'checking';
            $this->save($s); // A terminated validator leaves a durable barrier, never a false pass.
            try {
                $passed = $validate() === true;
            } catch (\Throwable) {
                $passed = false; // Do not persist or expose validator exception secrets.
            }
            $s['status'] = $passed ? 'ready' : 'failed';
            if ($passed) { $s['step']++; }
            $next = bin2hex(random_bytes(32));
            $s['csrf'] = hash('sha256', $next);
            $this->save($s);
            return ['step' => self::STEPS[$s['step']], 'status' => $s['status'], 'csrf' => $next];
        });
    }

    private function authorized(string $session): array
    {
        $s = $this->read();
        if (!preg_match('/^[a-f0-9]{64}$/D', $session) || !is_string($s['session'])
            || !hash_equals($s['session'], hash('sha256', $session)) || $s['expires'] <= time()) {
            throw new \RuntimeException('Installer authentication required');
        }
        return $s;
    }

    private function read(): array
    {
        $path = $this->directory.'/state.json';
        $this->privateFile($path);
        if (filesize($path) > 4096) { throw new \RuntimeException('Invalid installer state'); }
        try { $s = json_decode((string)file_get_contents($path), true, 16, JSON_THROW_ON_ERROR); }
        catch (\Throwable) { throw new \RuntimeException('Invalid installer state'); }
        if (!is_array($s) || array_keys($s) !== ['schema', 'step', 'status', 'session', 'expires', 'csrf', 'attempts', 'retry_at']
            || $s['schema'] !== 2 || !is_int($s['step']) || $s['step'] < 0 || $s['step'] > 6
            || !in_array($s['status'], ['ready', 'checking', 'failed'], true) || !is_int($s['expires'])
            || !is_int($s['attempts']) || $s['attempts'] < 0 || $s['attempts'] > 10 || !is_int($s['retry_at'])
            || !$this->digestOrNull($s['session']) || !$this->digestOrNull($s['csrf'])) {
            throw new \RuntimeException('Invalid installer state');
        }
        return $s;
    }

    private function digestOrNull(mixed $value): bool
    {
        return $value === null || (is_string($value) && preg_match('/^[a-f0-9]{64}$/D', $value) === 1);
    }

    private function privateFile(string $path): void
    {
        clearstatcache(true, $path);
        if (is_link($path) || !is_file($path) || (fileperms($path) & 0777) !== 0600
            || fileowner($path) !== fileowner($this->directory) || stat($path)['nlink'] !== 1) {
            throw new \RuntimeException('Unsafe installer state file');
        }
    }

    private function save(array $s): void
    {
        $path = $this->directory.'/state-'.bin2hex(random_bytes(16)).'.tmp';
        $previous = umask(0077);
        try { $f = fopen($path, 'x+b'); } finally { umask($previous); }
        if ($f === false) { throw new \RuntimeException('Installer state write failed'); }
        try {
            $data = json_encode($s, JSON_THROW_ON_ERROR)."\n";
            if (fwrite($f, $data) !== strlen($data) || !fflush($f) || !fsync($f)) {
                throw new \RuntimeException('Installer state write failed');
            }
            fclose($f); $f = null;
            if (!rename($path, $this->directory.'/state.json')) {
                throw new \RuntimeException('Installer state commit failed');
            }
            // Linux directory sync makes the rename durable across a host crash.
            $dir = fopen($this->directory, 'r');
            if ($dir === false) { throw new \RuntimeException('Installer state sync failed'); }
            try { if (!fsync($dir)) { throw new \RuntimeException('Installer state sync failed'); } }
            finally { fclose($dir); }
        } finally {
            if (is_resource($f)) { fclose($f); }
            if (file_exists($path)) { unlink($path); }
        }
    }

    private function locked(callable $operation): mixed
    {
        // Provisioned by the future root worker outside the web root; never writable by other users.
        if (file_exists($this->directory.'/installed.lock') || is_link($this->directory.'/installed.lock')) {
            throw new \RuntimeException('Installer unavailable');
        }
        $path = $this->directory.'/state.lock';
        if (is_link($path) || (file_exists($path) && !is_file($path))) {
            throw new \RuntimeException('Unsafe installer lock');
        }
        $previous = umask(0077);
        try { $f = fopen($path, 'c+b'); } finally { umask($previous); }
        if ($f === false) { throw new \RuntimeException('Installer lock failed'); }
        try {
            $this->privateFile($path);
            if (!flock($f, LOCK_EX | LOCK_NB)) { throw new \RuntimeException('Installer busy'); }
            if (file_exists($this->directory.'/installed.lock') || is_link($this->directory.'/installed.lock')) {
                throw new \RuntimeException('Installer unavailable');
            }
            return $operation();
        } finally { flock($f, LOCK_UN); fclose($f); }
    }
}
