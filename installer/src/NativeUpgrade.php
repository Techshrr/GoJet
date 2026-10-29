<?php
declare(strict_types=1);
namespace GoJet\Installer;
require_once __DIR__.'/UpgradeHealth.php';
require_once __DIR__.'/UpgradeStorage.php';
require_once __DIR__.'/UpgradeMaintenance.php';
require_once __DIR__.'/UpgradeRedis.php';
require_once __DIR__.'/DatabaseBackup.php';
require_once __DIR__.'/DatabaseRestore.php';
require_once __DIR__.'/InstallationPreparation.php';
require_once __DIR__.'/ServiceActivation.php';

/** Root-only upgrade transaction. First-install completion remains immutable. */
final class NativeUpgrade
{
    private const AUTHORITY='/var/lib/gojet-upgrade-authority';
    private const POINTER=self::AUTHORITY.'/active';
    private static function lock()
    {
        UpgradeIO::root();
        CompletionLock::verifyProvisioningPath(dirname(self::AUTHORITY).'/installed.lock');
        if (!file_exists(self::AUTHORITY) && !is_link(self::AUTHORITY)) { mkdir(self::AUTHORITY,0700); }
        UpgradeIO::directory(self::AUTHORITY);
        $path=self::AUTHORITY.'/upgrade.lock';
        if (!file_exists($path) && !is_link($path)) { UpgradeIO::write($path,''); }
        if (UpgradeIO::read($path)!=='') { throw new \RuntimeException('Invalid upgrade lock'); }
        $h=fopen($path,'r+b');
        if ($h===false || !flock($h,LOCK_EX|LOCK_NB)) { if (is_resource($h)) { fclose($h); } throw new \RuntimeException('Upgrade busy'); }
        return $h;
    }
    private static function unlock($h): void { flock($h,LOCK_UN); fclose($h); }
    private static function checkRelease(array $r): void
    {
        PackageIntegrity::verify($r['dependencies']['release'],$r['dependencies']['manifest_sha256'],$r['dependencies']['source']);
        UpgradeSwitch::verify($r['snapshot'],$r['configuration_hashes']);
        foreach ($r['environment_hashes'] as $path=>$hash) {
            if (!hash_equals($hash,hash('sha256',UpgradeIO::read($path)))) { throw new \RuntimeException('Environment changed'); }
        }
        foreach ($r['tls_files'] as $path=>$row) {
            if (!hash_equals($row['sha256'],hash('sha256',UpgradeIO::read($path,$row['mode'])))) { throw new \RuntimeException('TLS changed'); }
        }
    }
    private static function manifest(array $r): array
    {
        return json_decode(UpgradeIO::read($r['dependencies']['release'].'/manifest.json',0644,16777216),true,32,JSON_THROW_ON_ERROR);
    }
    public static function prepare(string $currentPrepared,string $candidate,string $manifestHash,string $source,string $job,array $administrator): array
    {
        $lock=self::lock();
        try {
            UpgradeIO::directory(dirname($job));
            if (!preg_match('/^[A-Za-z0-9][A-Za-z0-9_-]{0,63}$/D',basename($job)) || $job!==dirname($job).'/'.basename($job)
                || file_exists($job) || is_link($job)) { throw new \RuntimeException('New private upgrade job required'); }
            if (file_exists(self::AUTHORITY.'/pending.json')) { throw new \RuntimeException('Existing upgrade requires recovery'); }
            PackageIntegrity::verify($candidate,$manifestHash,$source);
            $activePath=self::AUTHORITY.'/release.json';
            $activeHash=null; $originalHashes=null;
            if (file_exists($activePath)) {
                $raw=UpgradeIO::read($activePath); $activeHash=hash('sha256',$raw); $old=json_decode($raw,true,32,JSON_THROW_ON_ERROR)['release'];
                self::checkRelease($old); UpgradeHealth::verify($old,self::POINTER);
            } else {
                $seal=InstallationPreparation::verify($currentPrepared);
                ServiceActivation::verifyReady($currentPrepared);
                $installed=UpgradeIO::json('/var/lib/gojet-installer-authority/installed.lock');
                if (($installed['schema']??'')!=='gojet.installation-completion.v1' || $installed['source']!==$seal['source']
                    || $installed['manifest_sha256']!==$seal['manifest_sha256'] || $installed['seal_sha256']!==hash_file('sha256',$currentPrepared.'/seal.json')) {
                    throw new \RuntimeException('Installed release authority mismatch');
                }
                $request=UpgradeIO::json($currentPrepared.'/data/request.json');
                $environment=[];
                foreach (UpgradeSwitch::SERVICES as $name) { $environment[$currentPrepared.'/environment/'.$name.'.env']=$seal['files']['environment/'.$name.'.env']; }
                $certificate=null;
                foreach ($seal['tls_files'] as $path=>$row) { if (!$row['private']) { $certificate=$path; } }
                $old=['dependencies'=>UpgradeIO::json($currentPrepared.'/dependencies.json'),'origin'=>$request['origin'],
                    'certificate'=>$certificate,'tls_files'=>$seal['tls_files'],'environment_hashes'=>$environment,
                    'snapshot'=>$job.'/before','configuration_hashes'=>[]];
            }
            UpgradeIO::disjoint([$job,$candidate,$old['dependencies']['release'],$old['dependencies']['storage'],self::AUTHORITY,$currentPrepared]);
            $new=$old; $new['dependencies']['release']=$candidate; $new['dependencies']['manifest_sha256']=$manifestHash; $new['dependencies']['source']=$source;
            $oldManifest=self::manifest($old); $newManifest=self::manifest($new);
            // This release family has one reviewed compatibility boundary. A new minor
            // or major line must explicitly extend it together with its migration tests.
            foreach ([$oldManifest['version'],$newManifest['version']] as $version) {
                if (!preg_match('/^10\.0\.0(?:-rc\.[0-9]+)?$/D',$version)) { throw new \RuntimeException('Unsupported version compatibility boundary'); }
            }
            if (version_compare($newManifest['version'],$oldManifest['version'],'<')) { throw new \RuntimeException('Downgrade requires restore'); }
            $database=$old['dependencies']['database'];
            if (($administrator['name']??'')!==$database['name'] || ($administrator['port']??null)!==$database['port']) { throw new \RuntimeException('Backup administrator database mismatch'); }
            $db=UpgradeIO::connection($administrator);
            $oldCatalog=$oldManifest['files']['migration-catalog.json']['sha256']; $newCatalog=$newManifest['files']['migration-catalog.json']['sha256'];
            Migrations::inspectUpgrade($db,$old['dependencies']['release'],$oldCatalog,$candidate,$newCatalog);
            if (!mkdir($job,0700)) { throw new \RuntimeException('Upgrade job unavailable'); }
            if ($activeHash===null) {
                $files=[];
                foreach (UpgradeSwitch::targets() as $name=>$path) {
                    $expected=$name==='nginx.conf'?$seal['files']['nginx.conf']:$seal['files']['units/'.$name];
                    $raw=UpgradeIO::read($path,0644);
                    if (!hash_equals($expected,hash('sha256',$raw))) { throw new \RuntimeException('Installed configuration changed'); }
                    $files[$name]=$raw;
                }
                $originalHashes=array_map(fn($raw)=>hash('sha256',$raw),$files);
                $files['nginx.conf']=UpgradeMaintenance::guarded($files['nginx.conf']);
                $old['configuration_hashes']=UpgradeSwitch::snapshot($old['snapshot'],$files);
            }
            mkdir($job.'/environment',0700); $new['environment_hashes']=[];
            foreach (UpgradeSwitch::SERVICES as $name) {
                $oldPath=array_values(array_filter(array_keys($old['environment_hashes']),fn($p)=>basename($p)===$name.'.env'));
                if (count($oldPath)!==1) { throw new \RuntimeException('Service environment missing'); }
                $raw=UpgradeIO::read($oldPath[0]); UpgradeIO::write($job.'/environment/'.$name.'.env',$raw);
                $new['environment_hashes'][$job.'/environment/'.$name.'.env']=hash('sha256',$raw);
            }
            $files=[];
            foreach (UpgradeSwitch::SERVICES as $name) {
                $raw=UpgradeIO::read($candidate.'/deploy/systemd/gojet-'.$name.'.service',0644);
                $replace=['WorkingDirectory=/opt/gojet/current'=>'WorkingDirectory='.$candidate,
                    'ExecStart=/opt/gojet/current/bin/'.$name=>'ExecStart='.$candidate.'/bin/'.$name,
                    'EnvironmentFile=/etc/gojet/'.$name.'.env'=>'EnvironmentFile='.$job.'/environment/'.$name.'.env'];
                if (in_array($name,['platformapi','fileworker'],true)) { $replace['ReadWritePaths=/var/lib/gojet/files']='ReadWritePaths='.$old['dependencies']['storage']; }
                foreach ($replace as $from=>$to) {
                    $raw=str_replace("\n".$from."\n","\n".$to."\n",$raw,$count);
                    if ($count!==1) { throw new \RuntimeException('Candidate unit contract changed'); }
                }
                $files['gojet-'.$name.'.service']=$raw;
            }
            $oldNginx=UpgradeIO::read($old['snapshot'].'/nginx.conf',0644);
            if (preg_match_all('/client_max_body_size ([0-9]+);/',$oldNginx,$limits)!==1) { throw new \RuntimeException('Body limit ambiguous'); }
            $private=array_values(array_filter(array_keys($old['tls_files']),fn($p)=>$old['tls_files'][$p]['private']));
            if (count($private)!==1) { throw new \RuntimeException('TLS key ambiguous'); }
            $web=['origin'=>$old['origin'],'release'=>$candidate,'certificate'=>$old['certificate'],'private_key'=>$private[0],'max_body_bytes'=>(int)$limits[1][0]];
            preg_match_all('/\blisten ([0-9]+)(?: default_server)?;/',$oldNginx,$ports); $ports=array_unique($ports[1]);
            if (count($ports)>1) { throw new \RuntimeException('HTTP binding ambiguous'); }
            if ($ports) { $web['http_port']=(int)reset($ports); }
            $files['nginx.conf']=UpgradeMaintenance::guarded(ProductWebConfig::render($web)); $new['snapshot']=$job.'/after';
            $new['configuration_hashes']=UpgradeSwitch::snapshot($new['snapshot'],$files);
            $plan=['schema'=>'gojet.native-upgrade.v1','old'=>$old,'new'=>$new,'administrator'=>$administrator,
                'old_catalog'=>$oldCatalog,'new_catalog'=>$newCatalog,'version'=>$newManifest['version'],'active_sha256'=>$activeHash,'original_hashes'=>$originalHashes];
            UpgradeIO::record($job.'/plan.json',$plan);
            UpgradeIO::record($job.'/state.json',['phase'=>'prepared','plan_sha256'=>hash_file('sha256',$job.'/plan.json')]);
            return ['prepared'=>true,'plan_sha256'=>hash_file('sha256',$job.'/plan.json'),'upgrade_complete'=>false];
        } finally { self::unlock($lock); }
    }
    private static function services(bool $start): void
    {
        $units=array_map(fn($n)=>'gojet-'.$n.'.service',UpgradeSwitch::SERVICES);
        if (!$start) {
            UpgradeIO::command(['/usr/bin/systemctl','stop',...array_reverse($units)]);
            foreach ($units as $unit) {
                $s=trim(UpgradeIO::command(['/usr/bin/systemctl','show',$unit,'--property=MainPID','--value']));
                if ($s!=='0') { throw new \RuntimeException('Writer service still running'); }
            }
        } else {
            UpgradeIO::command(['/usr/bin/systemctl','daemon-reload']);
            UpgradeIO::command(['/usr/bin/systemctl','reset-failed',...$units]);
            UpgradeIO::command(['/usr/bin/systemctl','start',$units[0]]);
            UpgradeIO::command(['/usr/bin/systemctl','start',...array_slice($units,1)]);
        }
    }
    private static function phase(string $job,array &$state,string $phase): void
    {
        $state['phase']=$phase;
        $state['events'][]=['phase'=>$phase,'at'=>gmdate('Y-m-d\TH:i:s\Z')];
        if (count($state['events'])>512) { throw new \RuntimeException('Upgrade recovery attempt bound exceeded'); }
        UpgradeIO::record($job.'/state.json',$state);
    }
    private static function waitHealthy(array $r): void
    {
        $deadline=microtime(true)+60; $stable=null;
        do {
            try {
                UpgradeHealth::verify($r,self::POINTER);
                if ($stable!==null && microtime(true)-$stable>=1) { return; }
                $stable ??= microtime(true); usleep(200000);
            } catch (\Throwable) {
                $stable=null;
                if (microtime(true)>=$deadline) { throw new \RuntimeException('Upgrade health deadline exceeded'); }
                usleep(200000);
            }
        } while (true);
    }
    public static function execute(string $job,string $expectedPlan,bool $recover=false): array
    {
        $lock=self::lock();
        try {
            UpgradeIO::directory($job); $raw=UpgradeIO::read($job.'/plan.json');
            if (!preg_match('/^[a-f0-9]{64}$/D',$expectedPlan) || !hash_equals($expectedPlan,hash('sha256',$raw))) { throw new \RuntimeException('Upgrade plan binding mismatch'); }
            $p=json_decode($raw,true,32,JSON_THROW_ON_ERROR); $state=UpgradeIO::json($job.'/state.json');
            if (($p['schema']??'')!=='gojet.native-upgrade.v1' || ($state['plan_sha256']??'')!==$expectedPlan) { throw new \RuntimeException('Invalid upgrade state'); }
            $phases=['prepared','stopping','adopting','backing-up','backed-up','rehearsing','migrating','switching','starting','verifying','restoring','committed','recovery-committed','complete','recovered'];
            if (!in_array($state['phase']??null,$phases,true)) { throw new \RuntimeException('Unknown upgrade phase'); }
            self::checkRelease($p['old']);
            if (!$recover || in_array($state['phase'],['committed','complete'],true)) { self::checkRelease($p['new']); }
            $pending=self::AUTHORITY.'/pending.json';
            $hasPending=file_exists($pending) || is_link($pending);
            if ($hasPending && UpgradeIO::json($pending)!==['job'=>$job,'plan_sha256'=>$expectedPlan]) {
                throw new \RuntimeException('Another upgrade is pending');
            }
            if (in_array($state['phase'],['committed','recovery-committed','complete','recovered'],true)) {
                $new=in_array($state['phase'],['committed','complete'],true); $release=$p[$new?'new':'old'];
                if ((UpgradeIO::json(self::AUTHORITY.'/release.json')['release']??null)!==$release) {
                    throw new \RuntimeException('Committed release authority changed');
                }
                self::waitHealthy($release); UpgradeMaintenance::gate(false);
                UpgradeIO::command(['/usr/sbin/nginx','-t']);
                UpgradeIO::command(['/usr/bin/systemctl','reload','nginx.service']);
                self::phase($job,$state,$new?'complete':'recovered');
                if ($hasPending && !unlink($pending)) { throw new \RuntimeException('Completion cleanup failed'); }
                UpgradeIO::sync(self::AUTHORITY);
                return ['upgrade_complete'=>$new,'recovered'=>!$new];
            }
            if (!$hasPending && $state['phase']!=='prepared') { throw new \RuntimeException('Missing recovery barrier'); }
            if (!$recover && $state['phase']!=='prepared') { throw new \RuntimeException('Interrupted upgrade requires explicit recovery'); }
            if ($recover && !$hasPending) { throw new \RuntimeException('No incomplete upgrade to recover'); }
            if (!$recover) {
                $active=self::AUTHORITY.'/release.json';
                if ($p['active_sha256']===null ? file_exists($active) : !hash_equals($p['active_sha256'],hash('sha256',UpgradeIO::read($active)))) {
                    throw new \RuntimeException('Current release changed since preparation');
                }
                UpgradeIO::record($pending,['job'=>$job,'plan_sha256'=>$expectedPlan]);
            }
            try {
                if ($recover) { return self::recover($job,$p,$state); }
                self::phase($job,$state,'stopping'); UpgradeMaintenance::gate(true); self::services(false);
                // Nginx remains alive, but all application writers are stopped. Static reads are harmless.
                self::phase($job,$state,'adopting');
                UpgradeSwitch::adopt(self::POINTER,$p['old']['snapshot'],$p['old']['configuration_hashes'],$p['original_hashes']);
                UpgradeIO::command(['/usr/sbin/nginx','-t']);
                UpgradeIO::command(['/usr/bin/systemctl','reload','nginx.service']);
                self::phase($job,$state,'backing-up');
                $backup=DatabaseBackup::create($p['administrator'],$job.'/database');
                $state['database_receipt_sha256']=hash_file('sha256',$job.'/database/complete.json');
                $state['storage_receipt_sha256']=UpgradeStorage::backup($p['old']['dependencies']['storage'],$job);
                self::phase($job,$state,'backed-up');
                self::phase($job,$state,'rehearsing');
                $state['rehearsal']=DatabaseRestore::rehearse($p['administrator'],$job.'/database',$state['database_receipt_sha256']);
                $db=UpgradeIO::connection($p['administrator']);
                Migrations::inspectUpgrade($db,$p['old']['dependencies']['release'],$p['old_catalog'],$p['new']['dependencies']['release'],$p['new_catalog']);
                self::phase($job,$state,'migrating');
                Migrations::apply($db,$p['new']['dependencies']['release'],$p['new_catalog'],$p['new']['dependencies']['source'],$p['version']);
                $state['invalidated_risk_keys']=UpgradeRedis::invalidate($p['new']['dependencies']['redis']);
                self::phase($job,$state,'switching');
                UpgradeSwitch::point(self::POINTER,$p['old']['snapshot'],$p['new']['snapshot']);
                UpgradeIO::command(['/usr/sbin/nginx','-t']);
                UpgradeIO::command(['/usr/bin/systemctl','reload','nginx.service']);
                self::phase($job,$state,'starting'); self::services(true);
                self::phase($job,$state,'verifying'); self::waitHealthy($p['new']);
                UpgradeIO::record(self::AUTHORITY.'/release.json',['release'=>$p['new'],'upgrade_job'=>$job]);
                self::phase($job,$state,'committed');
                UpgradeMaintenance::gate(false); UpgradeIO::command(['/usr/sbin/nginx','-t']);
                UpgradeIO::command(['/usr/bin/systemctl','reload','nginx.service']);
                self::phase($job,$state,'complete');
                if (!unlink($pending)) { throw new \RuntimeException('Upgrade barrier cleanup failed'); }
                UpgradeIO::sync(self::AUTHORITY);
                return ['upgrade_complete'=>true,'source'=>$p['new']['dependencies']['source'],'recovered'=>false];
            } catch (\Throwable) {
                $state['failure']=['phase'=>$state['phase'],'at'=>gmdate('Y-m-d\TH:i:s\Z'),'code'=>'upgrade-step-failed'];
                UpgradeIO::record($job.'/state.json',$state);
                // The last persisted phase is authoritative; never convert failure into completion.
                throw new \RuntimeException('Upgrade stopped; inspect private state and run explicit recovery');
            }
        } finally { self::unlock($lock); }
    }
    private static function recover(string $job,array $p,array &$state): array
    {
        UpgradeMaintenance::gate(true); self::services(false);
        $mutated=in_array($state['phase'],['migrating','switching','starting','restoring','verifying'],true);
        if ($mutated) {
            if (!isset($state['database_receipt_sha256'],$state['storage_receipt_sha256'])) { throw new \RuntimeException('Verified recovery backup missing'); }
            self::phase($job,$state,'restoring');
            // Validate BOTH backups before the first destructive database operation.
            UpgradeStorage::verify($job,$state['storage_receipt_sha256']);
            DatabaseRestore::restore($p['administrator'],$job.'/database',$state['database_receipt_sha256'],$state['rehearsal']);
            UpgradeStorage::restore($job,$state['storage_receipt_sha256']);
            $state['invalidated_risk_keys']=UpgradeRedis::invalidate($p['old']['dependencies']['redis']);
        }
        if (is_link(self::POINTER)) {
            $current=readlink(self::POINTER);
            if ($current===$p['new']['snapshot']) { UpgradeSwitch::point(self::POINTER,$current,$p['old']['snapshot']); }
            elseif ($current!==$p['old']['snapshot']) { throw new \RuntimeException('Unknown recovery pointer'); }
        }
        UpgradeSwitch::adopt(self::POINTER,$p['old']['snapshot'],$p['old']['configuration_hashes'],$p['original_hashes']);
        UpgradeIO::command(['/usr/sbin/nginx','-t']);
        UpgradeIO::command(['/usr/bin/systemctl','reload','nginx.service']);
        self::services(true); self::waitHealthy($p['old']);
        UpgradeIO::record(self::AUTHORITY.'/release.json',['release'=>$p['old'],'recovered_job'=>$job]);
        self::phase($job,$state,'recovery-committed');
        UpgradeMaintenance::gate(false); UpgradeIO::command(['/usr/sbin/nginx','-t']);
        UpgradeIO::command(['/usr/bin/systemctl','reload','nginx.service']);
        self::phase($job,$state,'recovered');
        if (!unlink(self::AUTHORITY.'/pending.json')) { throw new \RuntimeException('Recovery barrier cleanup failed'); }
        UpgradeIO::sync(self::AUTHORITY);
        return ['recovered'=>true,'upgrade_complete'=>false];
    }
}
