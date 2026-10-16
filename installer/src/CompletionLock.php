<?php

declare(strict_types=1);
namespace GoJet\Installer;

/** Read-only final authority. Only the future root apply worker may commit this lock after health checks. */
final class CompletionLock
{
    public function __construct(private readonly string $path)
    {
        $parent = dirname($path);
        if ($path === '' || $path[0] !== '/' || basename($path) !== 'installed.lock'
            || realpath($parent) !== $parent || !is_dir($parent)
            || fileowner($parent) !== 0 || (fileperms($parent) & 0022) !== 0) {
            throw new \RuntimeException('Unsafe completion lock configuration');
        }
    }

    /** Provisioning check, run outside the restricted FPM worker before installing its configuration. */
    public static function verifyProvisioningPath(string $path): void
    {
        new self($path);
        $parent = dirname($path);
        // No non-root ancestor may rename the authority directory. Root-owned sticky
        // temporary roots are permitted for isolated native CI fixtures.
        for ($ancestor = dirname($parent); ; $ancestor = dirname($ancestor)) {
            if (fileowner($ancestor) !== 0 || ((fileperms($ancestor) & 0022) !== 0
                && (fileperms($ancestor) & 01000) === 0)) {
                throw new \RuntimeException('Unsafe completion lock ancestor');
            }
            if ($ancestor === '/') { break; }
        }
    }

    public function exists(): bool
    {
        clearstatcache(true, $this->path);
        // Any existing node (including a dangling symlink) fails closed. Do not
        // parse a client-controlled completion flag or infer health from this API.
        return file_exists($this->path) || is_link($this->path);
    }
}
