"""Assemble only after same-source native, browser and real upgrade gates pass.

Consumes public CI evidence, never a host's private plan or database backup.
This is a verified native candidate, not production or external-service approval.
"""
import json
import os
from pathlib import Path
import re
import shutil
import sys
import tempfile
import zipfile

from stage_release import inspect_archive, sha


def require(report, keys, label):
    if any(report.get(key) is not True for key in keys):
        raise ValueError('Missing or failed acceptance: '+label)


def validate(candidate, admin, transition, source):
    if not re.fullmatch('[a-f0-9]{40}', source):
        raise ValueError('Invalid source identity')
    identity = json.loads((candidate/'release.json').read_text())
    if identity['source'] != source:
        raise ValueError('Candidate source mismatch')
    inventory={}
    for line in (candidate/'SHA256SUMS').read_text().splitlines():
        digest, filename=line.split('  ',1)
        if not re.fullmatch('[a-f0-9]{64}',digest) or Path(filename).name != filename or filename in inventory:
            raise ValueError('Invalid candidate checksum inventory')
        path=candidate/filename
        if path.is_symlink() or not path.is_file() or sha(path) != digest:
            raise ValueError('Candidate checksum mismatch: '+filename)
        inventory[filename]=digest
    expected={p.name for p in candidate.iterdir() if p.name != 'SHA256SUMS'}
    if set(inventory) != expected:
        raise ValueError('Incomplete candidate checksum inventory')
    inspect_archive(candidate/f"gojet-{identity['version']}-linux-amd64-payload.tar.gz", identity)
    evidence = {}
    for directory, names in [(candidate, ['verification', 'target-integrity', 'installation-preparation',
                                         'dependency-preflight', 'static-routing', 'risk-projection-recovery', 'upgrade-boundaries']),
                             (admin, ['browser', 'settings-upgrade', 'tls-chain']),
                             (transition, ['cross-release'])]:
        for name in names:
            path = directory/(name+'.json')
            if path.is_symlink() or not path.is_file():
                raise ValueError('Missing evidence: '+name)
            report = json.loads(path.read_text())
            if 'source' in report and report['source'] != source:
                raise ValueError('Evidence source mismatch: '+name)
            evidence[name] = report
    for name in ['target-integrity', 'installation-preparation']:
        report=evidence[name]
        if any(report.get(key) != identity[key] for key in ['source', 'manifest_sha256', 'archive_sha256']):
            raise ValueError('Payload evidence mismatch: '+name)
    require(evidence['target-integrity'], ['actual_release_verified', 'strict_umask_staging_verified',
            'missing_tampered_extra_rejected', 'symlink_hardlink_rejected', 'owner_modes_rejected'], 'staging')
    require(evidence['verification'], ['reproducible_archive'], 'reproducibility')
    require(evidence['dependency-preflight'], ['real_native_dependencies', 'native_host_boundaries_verified',
            'dependencies_restored'], 'dependencies')
    require(evidence['static-routing'], ['four_static_apps', 'exact_asset_bytes', 'certificate_verified_product_tls'], 'static routes')
    require(evidence['installation-preparation'], ['continuous_first_install_browser_to_retirement',
            'eight_packaged_units_started', 'eight_daemons_nonroot', 'final_installation_lock_written',
            'installer_pool_and_timer_retirement', 'trusted_tls_to_actual_api'], 'first installation')
    require(evidence['installation-preparation']['upgrade'], ['native_upgrade', 'real_partial_ddl_recovered',
            'missing_schema_recovered', 'committed_data_preserved', 'first_install_authority_unchanged'], 'recovery')
    require(evidence['risk-projection-recovery'], ['complete_rebuild', 'background_pagination'], 'risk recovery')
    require(evidence['upgrade-boundaries'], ['native_nginx', 'native_redis', 'forged_loopback_headers_rejected',
            'stale_risk_keys_removed', 'unrelated_data_and_analytics_stream_preserved'], 'upgrade boundaries')
    require(evidence['settings-upgrade'], ['passed', 'existing_settings_preserved', 'actual_backup_restore'], 'settings')
    require(evidence['tls-chain'], ['passed', 'ca_issued_leaf_all_three_probes',
            'wrong_tls_name_refused', 'untrusted_chain_refused'], 'TLS')
    browser=evidence['browser']
    require(browser, ['passed', 'native_cookie_auth'], 'browser')
    checks=browser.get('checks', [])
    if len(checks) != 26 or any(row.get('passed') is not True for row in checks):
        raise ValueError('Incomplete browser acceptance')
    routes={row['path'] for row in checks}
    if len(routes) != 13 or {(row['viewport'],row['path']) for row in checks} != {(w,p) for w in [390,1440] for p in routes}:
        raise ValueError('Missing desktop/mobile coverage')
    require(evidence['cross-release'], ['passed', 'immutable_rc1_installed', 'actual_candidate_binaries_running',
            'post_migration_interruption_recovered', 'original_migration_checksums_preserved',
            'settings_and_administrator_preserved', 'file_backup_restore_verified',
            'original_installation_receipt_preserved'], 'immutable release transition')
    cross=evidence['cross-release']
    if cross.get('old_source') != '32ea29d766b0ac0790964517ce4e220a1208bf0d' or cross.get('migrations_before') != 31 or cross.get('migrations_after') != 32:
        raise ValueError('Wrong upgrade baseline')
    return identity, evidence


def main():
    candidate, admin, transition, output = map(Path, sys.argv[1:5])
    source=os.environ.get('GOJET_DELIVERY_SOURCE',os.environ['GITHUB_SHA'])
    identity, evidence=validate(candidate, admin, transition, source)
    repo=Path(__file__).resolve().parents[2]
    name=f"GoJet-Native-{identity['version']}-{source[:7]}"
    output.mkdir(parents=True,exist_ok=True)
    archive=output/(name+'.zip')
    if archive.exists():
        raise ValueError('Delivery archive already exists')
    with tempfile.TemporaryDirectory() as tmp:
        root=Path(tmp)/name
        root.mkdir()
        # Fixed allowlist: never copy a directory of unreviewed test artifacts.
        names=['release.json','stage-release.py','START-HERE.md',
               f"gojet-{identity['version']}-linux-amd64-payload.tar.gz",f'gojet-source-{source[:7]}.tar.gz']
        for filename in names:
            path=candidate/filename
            if path.is_symlink() or not path.is_file():
                raise ValueError('Missing delivery member: '+filename)
            shutil.copyfile(path,root/filename)
        (root/'evidence').mkdir()
        for label, report in evidence.items():
            (root/'evidence'/(label+'.json')).write_text(json.dumps(report,sort_keys=True,indent=2)+'\n')
        (root/'manuals').mkdir()
        for filename in ['README.md','install.md','operations.md','upgrade-recovery.md']:
            shutil.copyfile(repo/'docs/release'/filename,root/'manuals'/filename)
        shutil.copyfile(repo/'docs/p22/delivery-acceptance.md',root/'ACCEPTANCE-ZH.md')
        acceptance={'schema':'gojet.native-delivery.v1','source':source,'release':identity,
            'native_ci_passed':True,'workflow_run_id':os.environ['GITHUB_RUN_ID'],
            'verification_source':os.environ['GITHUB_SHA'],
            'build_workflow_run_id':os.environ.get('GOJET_BUILD_RUN_ID',os.environ['GITHUB_RUN_ID']),
            'production_deployed':False,'external_credentials_tested':False,'reboot_tested':False,
            'evidence_sha256':{p.name:sha(p) for p in sorted((root/'evidence').iterdir())}}
        (root/'acceptance.json').write_text(json.dumps(acceptance,sort_keys=True,indent=2)+'\n')
        members=sorted(p for p in root.rglob('*') if p.is_file())
        (root/'SHA256SUMS').write_text(''.join(sha(p)+'  '+p.relative_to(root).as_posix()+'\n' for p in members))
        with zipfile.ZipFile(archive,'x',compression=zipfile.ZIP_DEFLATED) as zipped:
            for path in sorted(p for p in root.rglob('*') if p.is_file()):
                info=zipfile.ZipInfo(name+'/'+path.relative_to(root).as_posix(),date_time=(2026,1,1,0,0,0))
                info.external_attr=0o100644 << 16
                info.compress_type=zipfile.ZIP_DEFLATED
                zipped.writestr(info,path.read_bytes())
        with zipfile.ZipFile(archive) as zipped:
            if zipped.testzip() is not None:
                raise ValueError('Delivery archive integrity failed')
    (output/(archive.name+'.sha256')).write_text(sha(archive)+'  '+archive.name+'\n')
    print(json.dumps({'package':archive.name,'sha256':sha(archive),'source':source,'production_deployed':False}))


if __name__=='__main__':
    main()
