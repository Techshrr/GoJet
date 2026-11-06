<?php
declare(strict_types=1);
require dirname(__DIR__,3).'/installer/src/UpgradeCompatibility.php';
use GoJet\Installer\UpgradeCompatibility as C;
function rejected(callable $fn): void { try { $fn(); } catch (Throwable) { return; } throw new RuntimeException('Expected compatibility rejection'); }
foreach ([['10.0.0-rc.0','10.0.0-rc.1'],['10.0.0-rc.1','10.0.0-rc.1'],['10.0.0-rc.9','10.0.0-rc.10'],
    ['10.0.0-rc.10','10.0.0'],['10.0.0','10.0.0']] as [$old,$new]) { C::versions($old,$new); }
foreach ([['10.0.0-rc.10','10.0.0-rc.9'],['10.0.0','10.0.0-rc.10'],['10.0.0-rc.1','10.0.0-rc.0'],
    ['9.0.0','10.0.0'],['10.0.0','10.1.0'],['10.0.0','11.0.0'],['10.0.0-rc.01','10.0.0'],
    ['10.0.0','10.0.0+build'],['10.0.0','10.0.0-beta.1'],['10.0.0','10.0.0-rc.-1'],
    ['10.0.0','10.0.0-rc.1000000000'],['10.0.0',"10.0.0\n"]] as [$old,$new]) { rejected(fn()=>C::versions($old,$new)); }
$source=str_repeat('a',40); $version='10.0.0-rc.1';
$identity=['service'=>'operationsmonitor','version'=>$version,'commit'=>$source];
C::identity(json_encode($identity),'operationsmonitor',$version,$source);
$cap=$identity+['risk_projection_restore'=>'gojet.risk-projection-restore.v1'];
C::identity(json_encode($cap),'operationsmonitor',$version,$source,true);
foreach (['service'=>'platformapi','version'=>'10.0.0-rc.2','commit'=>str_repeat('b',40)] as $key=>$bad) {
    $row=$identity; $row[$key]=$bad; rejected(fn()=>C::identity(json_encode($row),'operationsmonitor',$version,$source));
}
foreach ([$identity,$cap+['unexpected'=>true],$identity+['risk_projection_restore'=>true],
    $identity+['risk_projection_restore'=>'gojet.risk-projection-restore.v2'],[],null] as $row) {
    rejected(fn()=>C::identity(json_encode($row),'operationsmonitor',$version,$source,true));
}
rejected(fn()=>C::identity('{broken','operationsmonitor',$version,$source,true));
echo json_encode(['version_matrix'=>true,'numeric_rc_order'=>true,'downgrade_rejected'=>true,
    'noncanonical_and_unsupported_versions_rejected'=>true,'binary_identity_binding'=>true,
    'missing_wrong_and_malformed_capability_rejected'=>true,'native_systemd_tested'=>false])."\n";
