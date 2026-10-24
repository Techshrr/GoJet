<?php

declare(strict_types=1);
namespace GoJet\Installer;
require_once __DIR__.'/InstallState.php';
require_once __DIR__.'/PreflightSnapshot.php';
require_once __DIR__.'/ApplyProgress.php';

/** HTTP authentication and bounded apply mailbox; privileged execution stays outside FPM. */
final class HttpAccess
{
    /** Envelope includes CSRF and draft; persisted draft retains its separate 24 KiB limit. */
    public static function bodyLimit(string $uri): int
    {
        return $uri === '/install/configuration' ? 32768 : 4096;
    }

    private const ROUTES = ['/install', '/install/', '/install/environment', '/install/data',
        '/install/configuration', '/install/configuration/approve', '/install/site', '/install/admin', '/install/services', '/install/health', '/install/complete'];

    public static function handle(array $server, array $cookies, string $body, string $directory, string $hash, string $origin, string $completionFile): array
    {
        $headers = ['X-Robots-Tag' => 'noindex, nofollow', 'Cache-Control' => 'no-store',
            'X-Content-Type-Options' => 'nosniff', 'Referrer-Policy' => 'no-referrer',
            'Content-Security-Policy' => "default-src 'none'; style-src 'unsafe-inline'; form-action 'self'; frame-ancestors 'none'; base-uri 'none'",
            'Content-Type' => 'application/json; charset=utf-8'];
        $reply = static fn(int $status, string $error): array => [$status, $headers, ['error' => $error]];
        // Root-owned final authority is checked before authentication or rendering.
        try { $completion = new CompletionLock($completionFile); }
        catch (\Throwable) { return $reply(503, 'Installer unavailable'); }
        if ($completion->exists()) { return $reply(404, 'Not found'); }
        $uri = $server['REQUEST_URI'] ?? '';
        if (!is_string($uri) || !in_array($uri, self::ROUTES, true)) { return $reply(404, 'Not found'); }
        $url = parse_url($origin);
        if (!is_array($url) || ($url['scheme'] ?? '') !== 'https' || empty($url['host'])
            || isset($url['path']) || isset($url['user']) || isset($url['pass']) || isset($url['query']) || isset($url['fragment'])
            || preg_match('/[\r\n]/', $origin)) { return $reply(503, 'Installer unavailable'); }
        // Trust only the web server's TLS variable, never forwarded headers supplied by a client.
        $host = $url['host'].(isset($url['port']) ? ':'.$url['port'] : '');
        if (($server['HTTPS'] ?? '') !== 'on' || ($server['HTTP_HOST'] ?? '') !== $host) {
            return $reply(400, 'Invalid installer origin');
        }
        $method = $server['REQUEST_METHOD'] ?? '';
        if (!in_array($method, ['GET', 'POST'], true)) { return $reply(405, 'Method not allowed'); }
        if ($method === 'POST') {
            if (($server['HTTP_ORIGIN'] ?? '') !== $origin) { return $reply(403, 'Invalid installer origin'); }
            if (strlen($body) > self::bodyLimit($uri) || (int)($server['CONTENT_LENGTH'] ?? 0) > self::bodyLimit($uri)) { return $reply(413, 'Request too large'); }
            if (strtolower(trim(explode(';', $server['CONTENT_TYPE'] ?? '')[0])) !== 'application/json') {
                return $reply(415, 'JSON required');
            }
        }
        try {
            $state = new InstallState($directory, $hash, $completion);
            if ($method === 'POST' && in_array($uri, ['/install', '/install/'], true)) {
                try { $data = json_decode($body, true, 8, JSON_THROW_ON_ERROR); }
                catch (\Throwable) { return $reply(400, 'Invalid request'); }
                if (!is_array($data) || array_keys($data) !== ['bootstrap_token'] || !is_string($data['bootstrap_token'])) {
                    return $reply(400, 'Invalid request');
                }
                $auth = $state->authenticate($data['bootstrap_token']);
                $headers['Set-Cookie'] = '__Secure-gojet_install='.$auth['session'].'; Path=/install; Max-Age=1800; Secure; HttpOnly; SameSite=Strict';
                unset($auth['session']);
                return [200, $headers, $auth];
            }
            $session = $cookies['__Secure-gojet_install'] ?? '';
            if (!is_string($session)) { return $reply(401, 'Installer authentication required'); }
            $current = $state->inspect($session);
            if ($method === 'POST') {
                if ($uri === '/install/configuration') {
                    try { $data = json_decode($body, true, 16, JSON_THROW_ON_ERROR); }
                    catch (\Throwable) { return $reply(400, 'Invalid request'); }
                    if (!is_array($data) || count($data) !== 2 || !isset($data['csrf'], $data['draft'])
                        || !is_string($data['csrf']) || !is_array($data['draft'])) {
                        return $reply(400, 'Invalid request');
                    }
                    return [202, $headers, $state->submitConfiguration($session, $data['csrf'], $data['draft'])];
                }
                if ($uri === '/install/configuration/approve') {
                    try { $data = json_decode($body, true, 8, JSON_THROW_ON_ERROR); }
                    catch (\Throwable) { return $reply(400, 'Invalid request'); }
                    if (!is_array($data) || array_keys($data) !== ['csrf'] || !is_string($data['csrf'])) {
                        return $reply(400, 'Invalid request');
                    }
                    // Authority is fixed by server configuration, never by a client path or approval flag.
                    return [200, $headers, $state->adoptConfiguration($session, $data['csrf'], dirname($completionFile))];
                }
                if ($uri === '/install/services') {
                    try { $data = json_decode($body, true, 8, JSON_THROW_ON_ERROR); }
                    catch (\Throwable) { return $reply(400, 'Invalid request'); }
                    if (!is_array($data) || array_keys($data) !== ['csrf'] || !is_string($data['csrf'])) {
                        return $reply(400, 'Invalid request');
                    }
                    return [202, $headers, $state->submitApply($session, $data['csrf'])];
                }
                // No public route may turn an arbitrary client success flag into an approved check.
                return $reply(503, 'Installer step execution unavailable');
            }
            if ($uri === '/install/services' && $current['step'] === 'services'
                && ($server['HTTP_ACCEPT'] ?? '') === 'application/json') {
                return [200, $headers + ['Vary' => 'Accept'], $current +
                    ['preflight' => PreflightSnapshot::read(dirname($completionFile), $directory),
                     'execution' => $current['status'] === 'checking'
                         ? ApplyProgress::read(dirname($completionFile), $directory) : null]];
            }
            if (!in_array($uri, ['/install', '/install/', '/install/'.$current['step']], true)
                || in_array($uri, ['/install/services', '/install/health', '/install/complete'], true)) {
                return $reply(409, 'Installer step unavailable');
            }
            if (($server['HTTP_ACCEPT'] ?? '') === 'application/json') { return [200, $headers + ['Vary' => 'Accept'], $current]; }
            $headers['Content-Type'] = 'text/html; charset=utf-8';
            return [200, $headers, $current];
        } catch (\RuntimeException $e) {
            return match ($e->getMessage()) {
                'Invalid configuration draft' => $reply(400, 'Invalid configuration draft'),
                'Configuration draft exceeds limit' => $reply(413, 'Request too large'),
                'Invalid installer CSRF token' => $reply(403, 'Invalid installer CSRF token'),
                'Installer transition forbidden' => $reply(409, 'Installer step unavailable'),
                'Installer unavailable' => $reply(404, 'Not found'),
                'Installer authentication failed', 'Installer authentication required' => $reply(401, 'Installer authentication required'),
                'Installer authentication rate limited' => [429, $headers + ['Retry-After' => '60'], ['error' => 'Try again later']],
                default => $reply(503, 'Installer unavailable'),
            };
        } catch (\Throwable) { return $reply(503, 'Installer unavailable'); }
    }
}
