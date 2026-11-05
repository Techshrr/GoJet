<?php
declare(strict_types=1);
namespace GoJet\Installer;
require_once __DIR__.'/UpgradeIO.php';
require_once __DIR__.'/UpgradeSwitch.php';

/** Offline compatibility checks, before creating a plan or stopping writers. */
final class UpgradeCompatibility
{
    public static function versions(string $old,string $new): void
    {
        foreach ([$old,$new] as $version) {
            if (!preg_match('/^10\.0\.0(?:-rc\.(?:0|[1-9][0-9]{0,8}))?$/D',$version)) {
                throw new \RuntimeException('Unsupported version compatibility boundary');
            }
        }
        if (version_compare($new,$old,'<')) { throw new \RuntimeException('Downgrade requires restore'); }
    }
    public static function identity(string $raw,string $service,string $version,string $source,bool $capabilities=false): void
    {
        $r=json_decode($raw,true,8,JSON_THROW_ON_ERROR);
        if (!is_array($r) || count($r)!==($capabilities?4:3)
            || ($r['service']??null)!==$service || ($r['version']??null)!==$version || ($r['commit']??null)!==$source
            || ($capabilities && ($r['risk_projection_restore']??null)!=='gojet.risk-projection-restore.v1')) {
            throw new \RuntimeException('Release executable compatibility mismatch');
        }
    }
    public static function verify(array $release,array $manifest): array
    {
        UpgradeIO::root();
        $account=posix_getpwnam('gojet');
        if ($account===false || $account['uid']<=0 || $account['gid']<=0) { throw new \RuntimeException('Compatibility probe account unavailable'); }
        // No EnvironmentFile or credentials; deny network and filesystem writes.
        $base=['/usr/bin/systemd-run','--quiet','--wait','--pipe','--collect','--service-type=exec'];
        foreach (['User=gojet','Group=gojet','WorkingDirectory=/','UMask=0077','NoNewPrivileges=yes',
            'CapabilityBoundingSet=','AmbientCapabilities=','ProtectSystem=strict','ProtectHome=yes',
            'PrivateNetwork=yes','PrivateTmp=yes','PrivateDevices=yes','ProtectKernelTunables=yes',
            'ProtectKernelModules=yes','ProtectControlGroups=yes','RestrictSUIDSGID=yes',
            'RuntimeMaxSec=5s','TimeoutStopSec=2s','KillMode=control-group'] as $property) {
            $base[]='--property='.$property;
        }
        $source=$release['dependencies']['source']; $version=$manifest['version'];
        $root=$release['dependencies']['release'];
        foreach ([...UpgradeSwitch::SERVICES,'filepreflight'] as $service) {
            self::identity(UpgradeIO::command([...$base,'--',$root.'/bin/'.$service,'--version'],10),$service,$version,$source);
        }
        self::identity(UpgradeIO::command([...$base,'--',$root.'/bin/operationsmonitor','--upgrade-capabilities'],10),
            'operationsmonitor',$version,$source,true);
        return ['source'=>$source,'version'=>$version,'service_identities'=>8,'helper_identities'=>1,
            'risk_projection_restore'=>'gojet.risk-projection-restore.v1'];
    }
}
