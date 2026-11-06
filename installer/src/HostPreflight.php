<?php
declare(strict_types=1);
namespace GoJet\Installer;

/** Fixed native support boundary; no request/env override of host identity. */
final class HostPreflight
{
    public static function supported(string $release, string $system, string $machine, int $integerBytes): bool
    {
        if ($system !== 'Linux' || $machine !== 'x86_64' || $integerBytes !== 8 || strlen($release) > 16384) { return false; }
        $values = [];
        foreach (preg_split('/\r?\n/', $release) as $line) {
            if (!preg_match('/^(ID|VERSION_ID)=(.*)$/D', $line, $match)) { continue; }
            if (isset($values[$match[1]])) { return false; }
            // os-release is data, never shell input. Only these literal values are supported.
            $values[$match[1]] = $match[2];
        }
        return in_array($values['ID'] ?? '', ['ubuntu', '"ubuntu"', "'ubuntu'"], true)
            && in_array($values['VERSION_ID'] ?? '', ['24.04', '"24.04"', "'24.04'"], true);
    }

    public static function check(string $release, string $storage): bool
    {
        $identity = @file_get_contents('/etc/os-release', false, null, 0, 16385);
        if ($identity === false || !self::supported($identity, php_uname('s'), php_uname('m'), PHP_INT_SIZE)) { return false; }
        // Called only after package verification. Reserve room for one additional
        // extracted payload on its filesystem, rather than invent a hardware quota.
        $manifest = json_decode(file_get_contents($release.'/manifest.json'), true, 32, JSON_THROW_ON_ERROR);
        $required = filesize($release.'/manifest.json') + filesize($release.'/sbom.cdx.json') + filesize($release.'/SHA256SUMS');
        foreach ($manifest['files'] as $file) { $required += $file['bytes']; }
        $free = @disk_free_space($release);
        if ($required <= 0 || $free === false || $free < $required) { return false; }
        // Probe every storage filesystem, including independently mounted partitions.
        // Actual permissions/writes remain checked as the unprivileged service user.
        foreach ([$storage, $storage.'/quarantine', $storage.'/published'] as $path) {
            $available = @disk_free_space($path);
            if ($available === false || $available <= 0) { return false; }
        }
        return true;
    }
}
