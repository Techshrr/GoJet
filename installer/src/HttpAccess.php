<?php

declare(strict_types=1);
namespace GoJet\Installer;
require_once __DIR__.'/InstallState.php';

/** HTTP boundary only. Privileged apply and step validators are not exposed here yet. */
final class HttpAccess
{
    private const ROUTES = ['/install', '/install/', '/install/environment', '/install/data',
        '/install/site', '/install/admin', '/install/services', '/install/health', '/install/complete'];

    public static function handle(array $server, array $cookies, string $body, string $directory, string $hash, string $origin): array
    {
        $headers = ['X-Robots-Tag' => 'noindex, nofollow', 'Cache-Control' => 'no-store',
            'X-Content-Type-Options' => 'nosniff', 'Referrer-Policy' => 'no-referrer',
            'Content-Security-Policy' => "default-src 'none'; style-src 'unsafe-inline'; form-action 'self'; frame-ancestors 'none'; base-uri 'none'",
            'Content-Type' => 'application/json; charset=utf-8'];
        $reply = static fn(int $status, string $error): array => [$status, $headers, ['error' => $error]];
        // Test the final lock before session creation, request parsing, or any shell output.
        if ($directory !== '' && (file_exists($directory.'/installed.lock') || is_link($directory.'/installed.lock'))) {
            return $reply(404, 'Not found');
        }
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
            if (strlen($body) > 4096 || (int)($server['CONTENT_LENGTH'] ?? 0) > 4096) { return $reply(413, 'Request too large'); }
            if (strtolower(trim(explode(';', $server['CONTENT_TYPE'] ?? '')[0])) !== 'application/json') {
                return $reply(415, 'JSON required');
            }
        }
        try {
            $state = new InstallState($directory, $hash);
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
                // No public route may turn an arbitrary client success flag into an approved check.
                return $reply(503, 'Installer step execution unavailable');
            }
            if (!in_array($uri, ['/install', '/install/', '/install/'.$current['step']], true)
                || in_array($uri, ['/install/services', '/install/health', '/install/complete'], true)) {
                return $reply(409, 'Installer step unavailable');
            }
            $headers['Content-Type'] = 'text/html; charset=utf-8';
            return [200, $headers, $current];
        } catch (\RuntimeException $e) {
            return match ($e->getMessage()) {
                'Installer unavailable' => $reply(404, 'Not found'),
                'Installer authentication failed', 'Installer authentication required' => $reply(401, 'Installer authentication required'),
                'Installer authentication rate limited' => [429, $headers + ['Retry-After' => '60'], ['error' => 'Try again later']],
                default => $reply(503, 'Installer unavailable'),
            };
        } catch (\Throwable) { return $reply(503, 'Installer unavailable'); }
    }
}
