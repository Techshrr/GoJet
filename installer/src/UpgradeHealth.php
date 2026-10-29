<?php
declare(strict_types=1);
namespace GoJet\Installer;
require_once __DIR__.'/UpgradeIO.php';
require_once __DIR__.'/UpgradeSwitch.php';
require_once __DIR__.'/DependencyPreflight.php';
require_once __DIR__.'/UpgradeMaintenance.php';
require_once __DIR__.'/Migrations.php';

/** Live release, eight daemons, dependency and TLS checks; no synthetic installation records. */
final class UpgradeHealth
{
    public static function verify(array $release, string $pointer): void
    {
        UpgradeIO::root();
        UpgradeSwitch::live($pointer, $release['snapshot'], $release['configuration_hashes']);
        if (DependencyPreflight::check($release['dependencies'])['ready'] !== true) {
            throw new \RuntimeException('Upgrade dependency health failed');
        }
        $root=$release['dependencies']['release'];
        $manifest=json_decode(UpgradeIO::read($root.'/manifest.json',0644,16777216),true,32,JSON_THROW_ON_ERROR);
        $catalog=$manifest['files']['migration-catalog.json']['sha256'];
        $db=UpgradeIO::connection(['host'=>'127.0.0.1']+$release['dependencies']['database']);
        Migrations::inspectUpgrade($db,$root,$catalog,$root,$catalog);
        foreach ($release['environment_hashes'] as $path => $hash) {
            if (!hash_equals($hash, hash('sha256', UpgradeIO::read($path)))) { throw new \RuntimeException('Environment changed'); }
        }
        $account = posix_getpwnam('gojet');
        if ($account === false || $account['uid'] === 0) { throw new \RuntimeException('Service account unavailable'); }
        foreach (UpgradeSwitch::SERVICES as $name) {
            $unit = 'gojet-'.$name.'.service';
            $raw = UpgradeIO::command(['/usr/bin/systemctl','show',$unit,'--property=ActiveState,SubState,MainPID,NRestarts,User,FragmentPath,DropInPaths']);
            $s = [];
            foreach (explode("\n", trim($raw)) as $line) { if (str_contains($line, '=')) { [$key,$value] = explode('=', $line, 2); $s[$key] = $value; } }
            if (($s['ActiveState'] ?? '') !== 'active' || ($s['SubState'] ?? '') !== 'running'
                || ($s['User'] ?? '') !== 'gojet' || ($s['NRestarts'] ?? '') !== '0' || ($s['DropInPaths'] ?? '') !== ''
                || realpath($s['FragmentPath'] ?? '') !== $release['snapshot'].'/'.$unit
                || !ctype_digit($s['MainPID'] ?? '') || (int)$s['MainPID'] < 2
                || realpath('/proc/'.$s['MainPID'].'/exe') !== $release['dependencies']['release'].'/bin/'.$name
                || trim(UpgradeIO::command(['/usr/bin/systemctl','is-enabled',$unit])) !== 'enabled') {
                throw new \RuntimeException('Upgrade daemon identity or readiness failed');
            }
            $status = @file_get_contents('/proc/'.$s['MainPID'].'/status');
            if ($status === false || !preg_match('/^Uid:\s+(\d+)\s+(\d+)\s+(\d+)\s+(\d+)$/m', $status, $ids)) {
                throw new \RuntimeException('Service identity unavailable');
            }
            for ($i=1; $i<=4; ++$i) { if ((int)$ids[$i] !== $account['uid']) { throw new \RuntimeException('Unexpected daemon privilege'); } }
        }
        UpgradeIO::command(UnprivilegedProbe::command($release['dependencies']['release'], 'logreceiver', ['GOJET_LOG_SOCKET'=>'/run/gojet-log/collector.sock']));
        $http_response_header=[];
        $context=stream_context_create(['http'=>['timeout'=>3,'ignore_errors'=>true,'follow_location'=>0]]);
        $probe=@fopen('http://127.0.0.1:8080/healthz','rb',false,$context);
        if ($probe===false) { throw new \RuntimeException('Redirect dependency health unavailable'); }
        fclose($probe);
        if (!preg_match('~^HTTP/\S+ 204(?: |$)~',$http_response_header[0]??'')) {
            throw new \RuntimeException('Redirect database/Redis health failed');
        }
        UpgradeIO::command(['/usr/sbin/nginx','-t']);
        if (trim(UpgradeIO::command(['/usr/bin/systemctl','is-active','nginx.service'])) !== 'active') {
            throw new \RuntimeException('Nginx unavailable');
        }
        $root = $release['dependencies']['release'];
        $paths = ['/' => $root.'/static/site/index.html','/app' => $root.'/static/workspace/index.html','/admin' => $root.'/static/admin/index.html'];
        $docs = glob($root.'/static/docs/*/index.html');
        if (!$docs) { throw new \RuntimeException('Docs entry unavailable'); }
        foreach ($docs as $entry) { $paths['/docs/'.basename(dirname($entry)).'/'] = $entry; }
        foreach ($paths as $path => $file) {
            [$code,$body] = self::request($release, $path);
            if ($code !== 200 || !hash_equals((string)hash_file('sha256',$file), hash('sha256',$body))) {
                throw new \RuntimeException('Upgrade static route health failed');
            }
        }
        [$code,$body] = self::request($release,'/api/public/auth/providers');
        if ($code !== 200 || !is_array(json_decode($body,true,16,JSON_THROW_ON_ERROR))
            || !in_array(self::request($release,'/api/workspaces/1/files')[0],[401,403],true)
            || self::request($release,'/install')[0] !== 404 || self::request($release,'/install/environment')[0] !== 404
            || self::request($release,'/','unrelated.invalid')[0] !== 421) { throw new \RuntimeException('Upgrade HTTP boundary failed'); }
    }
    private static function request(array $release, string $path, ?string $host = null): array
    {
        $url = parse_url($release['origin']);
        if (($url['scheme'] ?? '') !== 'https' || !preg_match('/^[a-z0-9.-]+$/D', $url['host'] ?? '')) {
            throw new \RuntimeException('Invalid sealed origin');
        }
        $context = stream_context_create([
            'ssl'=>['verify_peer'=>true,'verify_peer_name'=>true,'allow_self_signed'=>false,'peer_name'=>$url['host'],
                'SNI_enabled'=>true,'cafile'=>$release['certificate']],
            'http'=>['timeout'=>3,'follow_location'=>0,'ignore_errors'=>true,'protocol_version'=>1.1,
                'header'=>'Host: '.($host ?? $url['host'].(isset($url['port']) ? ':'.$url['port'] : ''))."\r\nX-GoJet-Upgrade-Probe: ".UpgradeMaintenance::token()."\r\nConnection: close\r\n"]]);
        $http_response_header = [];
        $h = @fopen('https://127.0.0.1:'.($url['port'] ?? 443).$path,'rb',false,$context);
        if ($h === false) { throw new \RuntimeException('Upgrade TLS request failed'); }
        try {
            $body = stream_get_contents($h,1048577); $meta = stream_get_meta_data($h);
            if ($body === false || strlen($body)>1048576 || !feof($h) || $meta['timed_out']
                || !preg_match('~^HTTP/\S+ ([0-9]{3})(?: |$)~',$http_response_header[0] ?? '',$m)) {
                throw new \RuntimeException('Upgrade TLS response invalid');
            }
            return [(int)$m[1],$body];
        } finally { fclose($h); }
    }
}
