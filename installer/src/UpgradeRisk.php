<?php
declare(strict_types=1);
namespace GoJet\Installer;
require_once __DIR__.'/UpgradeIO.php';

/** Rebuild durable destination-risk authority while writers remain stopped. */
final class UpgradeRisk
{
    private const UNIT='gojet-risk-projection-restore.service';
    public static function stop(): void
    {
        UpgradeIO::root();
        // The upgrade client may have died while PID1 still owns the worker.
        // Quiesce it before restoring MySQL or invalidating another generation.
        $loaded=trim(UpgradeIO::command(['/usr/bin/systemctl','list-units','--all','--full','--no-legend','--plain',self::UNIT]));
        if ($loaded!=='') { UpgradeIO::command(['/usr/bin/systemctl','stop',self::UNIT],10); }
    }
    public static function rebuild(array $release): array
    {
        UpgradeIO::root();
        $files=array_values(array_filter(array_keys($release['environment_hashes']),fn($p)=>basename($p)==='operationsmonitor.env'));
        if (count($files)!==1 || !hash_equals($release['environment_hashes'][$files[0]],hash('sha256',UpgradeIO::read($files[0])))) {
            throw new \RuntimeException('Risk projection environment changed');
        }
        $account=posix_getpwnam('gojet');
        if ($account===false || $account['uid']<=0 || $account['gid']<=0) { throw new \RuntimeException('Risk projection account unavailable'); }
        // PID1 reads the private EnvironmentFile; credentials never enter argv.
        $args=['/usr/bin/systemd-run','--unit='.self::UNIT,'--quiet','--wait','--pipe','--collect','--service-type=exec'];
        foreach (['User=gojet','Group=gojet','WorkingDirectory=/','UMask=0077','NoNewPrivileges=yes',
            'CapabilityBoundingSet=','AmbientCapabilities=','ProtectSystem=strict','ProtectHome=yes',
            'PrivateTmp=yes','PrivateDevices=yes','ProtectKernelTunables=yes','ProtectKernelModules=yes',
            'ProtectControlGroups=yes','RestrictSUIDSGID=yes','RestrictAddressFamilies=AF_UNIX AF_INET AF_INET6',
            'RuntimeMaxSec=75s','TimeoutStopSec=2s','KillMode=control-group','EnvironmentFile='.$files[0]] as $property) {
            $args[]='--property='.$property;
        }
        $args[]='--'; $args[]=$release['dependencies']['release'].'/bin/operationsmonitor'; $args[]='--restore-risk-projections';
        $result=json_decode(UpgradeIO::command($args,80),true,8,JSON_THROW_ON_ERROR);
        if (!is_array($result) || array_keys($result)!==['complete','visited','projected','withheld'] || $result['complete']!==true) {
            throw new \RuntimeException('Risk projection restore incomplete');
        }
        foreach (['visited','projected','withheld'] as $key) {
            if (!is_int($result[$key]) || $result[$key]<0 || $result[$key]>100000) { throw new \RuntimeException('Invalid projection restore count'); }
        }
        if ($result['visited']!==$result['projected']+$result['withheld']) { throw new \RuntimeException('Projection restore inventory mismatch'); }
        return $result;
    }
}
