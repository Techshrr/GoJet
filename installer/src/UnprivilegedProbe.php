<?php

declare(strict_types=1);
namespace GoJet\Installer;

/** Fixed health probes: PID1 drops identity before applying no-new-privileges. */
final class UnprivilegedProbe
{
    public static function command(string $release, string $probe, array $environment): array
    {
        $expected = match ($probe) {
            'filepreflight' => ['GOJET_FILE_STORAGE_ROOT', 'GOJET_CLAMAV_NETWORK', 'GOJET_CLAMAV_ADDRESS'],
            'logreceiver' => ['GOJET_LOG_SOCKET'],
            default => throw new \RuntimeException('Unsupported health probe'),
        };
        $account = posix_getpwnam('gojet');
        if (posix_geteuid() !== 0 || $account === false || $account['uid'] <= 0 || $account['gid'] <= 0
            || !preg_match('~^/(?:[A-Za-z0-9_.-]+/)*[A-Za-z0-9_.-]+$~D', $release)
            || realpath($release) !== $release || array_keys($environment) !== $expected) {
            throw new \RuntimeException('Invalid health probe authority');
        }
        $argv = ['/usr/bin/systemd-run', '--quiet', '--wait', '--pipe', '--collect', '--service-type=exec'];
        foreach (['User=gojet', 'Group=gojet', 'WorkingDirectory=/', 'UMask=0077',
            'NoNewPrivileges=yes', 'CapabilityBoundingSet=', 'AmbientCapabilities=',
            'ProtectSystem=full', 'ProtectHome=yes', 'ProtectKernelTunables=yes',
            'ProtectKernelModules=yes', 'ProtectControlGroups=yes', 'RestrictSUIDSGID=yes',
            'RuntimeMaxSec=12s', 'TimeoutStopSec=2s', 'KillMode=control-group'] as $property) {
            $argv[] = '--property='.$property;
        }
        $argv[] = '--setenv=PATH=/usr/sbin:/usr/bin:/sbin:/bin';
        foreach ($environment as $name => $value) {
            if (!is_string($value) || strlen($value) > 4096 || str_contains($value, "\0")
                || str_contains($value, "\n") || str_contains($value, "\r")) {
                throw new \RuntimeException('Invalid health probe environment');
            }
            $argv[] = '--setenv='.$name.'='.$value;
        }
        $argv[] = '--';
        $argv[] = $release.'/bin/'.$probe;
        if ($probe === 'logreceiver') { $argv[] = '--check'; }
        return $argv;
    }
}
