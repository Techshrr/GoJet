<?php
declare(strict_types=1);
namespace GoJet\Installer;
require_once __DIR__.'/UpgradeIO.php';

/** Maintenance probes require both loopback and a private, unguessable capability. */
final class UpgradeMaintenance
{
    public const FILE='/etc/nginx/gojet-upgrade-gate.conf';
    private const TOKEN='/var/lib/gojet-upgrade-authority/probe-token';
    public static function token(): string
    {
        $token=UpgradeIO::read(self::TOKEN);
        if (!preg_match('/^[a-f0-9]{64}$/D',$token)) { throw new \RuntimeException('Invalid maintenance capability'); }
        return $token;
    }
    public static function render(string $token): string
    {
        if (!preg_match('/^[a-f0-9]{64}$/D',$token)) { throw new \RuntimeException('Invalid maintenance capability'); }
        return 'if ($remote_addr !~ "^(127[.]0[.]0[.]1|::1)$") { return 503; }'."\n"
            .'if ($http_x_gojet_upgrade_probe != "'.$token.'") { return 503; }'."\n";
    }
    public static function gate(bool $closed): void
    {
        if (!file_exists(self::TOKEN) && !is_link(self::TOKEN)) { UpgradeIO::write(self::TOKEN,bin2hex(random_bytes(32))); }
        $block=self::render(self::token());
        $exists=file_exists(self::FILE) || is_link(self::FILE);
        if ($exists && !in_array(UpgradeIO::read(self::FILE),["\n",$block],true)) {
            throw new \RuntimeException('Foreign maintenance gate');
        }
        UpgradeIO::write(self::FILE,$closed?$block:"\n",0600,$exists);
    }
    public static function guarded(string $nginx): string
    {
        // All input must come from the sealed renderer, never a substring-based bypass.
        if (str_contains($nginx,self::FILE)) { throw new \RuntimeException('Unexpected existing maintenance include'); }
        $result=str_replace("server {\n","server {\n    include ".self::FILE.";\n",$nginx,$count);
        if ($count<2) { throw new \RuntimeException('Unsupported Nginx server layout'); }
        return $result;
    }
}
