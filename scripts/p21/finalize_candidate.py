"""Bind one immutable candidate to reports from this CI job; never approve a gate."""
import json
import os
from pathlib import Path
import re
import subprocess
import sys
import tarfile
from archive import BINS, digest, encode, verify_archive
from runbooks import PATHS, validate as validate_runbooks

# These checks are required evidence, not a replacement for the producing tests.
REQUIRED = {
    'target-integrity.json': ('actual_release_verified', 'wrong_authority_rejected',
        'missing_tampered_extra_rejected', 'runbook_tampering_rejected', 'symlink_hardlink_rejected', 'owner_modes_rejected',
        'checksum_inventory_rejected', 'restored_payload_verified'),
    'risk-projection-recovery.json': ('native_mysql', 'native_redis', 'background_pagination',
        'complete_rebuild', 'native_recovery_command', 'unknown_expired_changed_targets_withheld',
        'manual_override_preserved', 'absolute_allow_deadline_preserved', 'bounded_and_canceled_failure',
        'redis_failure_blocks_completion', 'jobs_authority_and_unrelated_redis_preserved'),
    'upgrade-boundaries.json': ('native_nginx', 'native_redis', 'forged_loopback_headers_rejected',
        'private_probe_accepted', 'stale_risk_keys_removed', 'repeated_invalidation_safe',
        'reload_acknowledgement_not_readiness', 'public_tls_probe_without_capability',
        'unrelated_data_and_analytics_stream_preserved'),
    'static-routing.json': ('actual_compiled_archive', 'four_static_apps', 'exact_asset_bytes',
        'private_paths_not_served', 'certificate_verified_product_tls', 'wrong_host_sni_rejected'),
    'dependency-preflight.json': ('real_native_dependencies', 'stopped_nginx_fpm_rejected',
        'unreachable_mysql_redis_rejected', 'bad_database_redis_credentials_rejected',
        'missing_clamav_rejected', 'unsafe_storage_rejected', 'dependencies_restored'),
    'installation-preparation.json': ('single_command_native_apply',
        'continuous_first_install_browser_to_retirement', 'continuous_installation_without_state_fixtures',
        'eight_packaged_units_started', 'eight_daemons_nonroot', 'eight_daemons_no_restart',
        'trusted_tls_to_actual_api', 'installer_pool_and_timer_retirement',
        'isolated_installation_completion_verified', 'final_installation_lock_written',
        'live_tls_mismatch_blocks_completion', 'stopped_services_block_completion'),
}
UPGRADE = ('native_upgrade', 'native_database_required_recovery', 'durable_risk_projection_restored',
    'orphaned_projection_worker_stopped', 'old_and_new_executable_identity_and_recovery_contract_verified',
    'real_partial_ddl_recovered', 'missing_schema_recovered', 'committed_data_preserved',
    'first_install_authority_unchanged', 'existing_administrator_session_preserved',
    'stale_risk_projections_removed', 'unrelated_redis_data_preserved')
PHASES = {'prepared', 'stopping', 'adopting', 'backing-up', 'backed-up', 'rehearsing', 'migrating',
    'switching', 'starting', 'verifying', 'restoring', 'recovery-committed', 'recovered', 'committed', 'complete'}
REJECTIONS = {'downgrade', 'new-minor', 'new-major', 'noncanonical-rc', 'unsupported-prerelease',
    'build-metadata', 'binary-version', 'binary-source', 'binary-service', 'missing-recovery-capability'}
SOURCE_REPORTS = {'verification.json', 'native-build.json', 'target-integrity.json',
                  'static-routing.json', 'dependency-preflight.json', 'installation-preparation.json'}


def require(condition, reason):
    if not condition:
        raise ValueError(reason)


def finalize(archive, build_report, source, context):
    require(re.fullmatch(r'[0-9a-f]{40}', source) is not None, 'invalid source')
    require(context.get('repository') == 'Techshrr/GoJet' and context.get('job') == 'payload', 'foreign producer')
    require(all(re.fullmatch(r'[1-9][0-9]*', str(context.get(key, ''))) for key in ('run_id', 'run_attempt')),
            'missing CI run identity')
    archive = Path(archive)
    root = archive.parent
    archive_sha = digest(archive.read_bytes())
    verification = verify_archive(archive, archive_sha, source)
    require((root/(archive.name+'.sha256')).read_text() == archive_sha+'  '+archive.name+'\n', 'checksum companion mismatch')
    with tarfile.open(archive, 'r:gz') as stream:
        files = {row.name: stream.extractfile(row).read() for row in stream}
    manifest = json.loads(files['manifest.json'])
    manifest_sha = digest(files['manifest.json'])
    runbooks = validate_runbooks(files)
    require({name for name in files if name.startswith('bin/')} == {'bin/'+name for name in BINS}, 'incomplete executable inventory')
    require({name.split('/')[1] for name in files if name.startswith('static/')} == {'site', 'docs', 'workspace', 'admin'}, 'incomplete static apps')
    require({name for name in files if name.startswith('deploy/systemd/')} ==
            {'deploy/systemd/gojet-'+name+'.service' for name in BINS-{'filepreflight'}}, 'incomplete service units')
    catalog = json.loads(files['migration-catalog.json'])
    migrations = catalog['migrations']
    require(len(migrations) > 0 and len({row['path'] for row in migrations}) == len(migrations), 'invalid migration inventory')
    require({row['path'] for row in migrations} == {name for name in files if name.startswith('migrations/')}, 'migration catalog mismatch')
    for row in migrations:
        require(digest(files[row['path']]) == row['sha256'], 'migration bytes mismatch')
    sbom = json.loads(files['sbom.cdx.json'])
    components = [row for row in sbom['components'] if row['type'] == 'file']
    require(len(components) == len(manifest['files']) and {row['name'] for row in components} == set(manifest['files']), 'SBOM file inventory mismatch')
    for row in components:
        require(row['hashes'] == [{'alg': 'SHA-256', 'content': manifest['files'][row['name']]['sha256']}], 'SBOM file hash mismatch')
    reports, evidence = {}, {}
    for name in ('verification.json', 'native-build.json', *REQUIRED):
        path = Path(build_report) if name == 'native-build.json' else root/name
        require(path.is_file() and not path.is_symlink(), 'missing or linked evidence: '+name)
        raw = path.read_bytes()
        value = json.loads(raw)
        require(isinstance(value, dict), 'invalid evidence: '+name)
        if name in SOURCE_REPORTS:
            require(value.get('source') == source, 'foreign evidence source: '+name)
        reports[name] = value
        evidence[name] = {'sha256': digest(raw), 'bytes': len(raw),
            'binding': 'report-source-and-ci-job' if name in SOURCE_REPORTS else 'ci-job-only'}
        for check in REQUIRED.get(name, ()):
            require(value.get(check) is True, 'missing successful check: '+name+':'+check)
    built = reports['native-build.json']
    require(built['version'] == manifest['version'], 'build version mismatch')
    rows = built['native_version_checks']
    require(len(rows) == len(BINS) and {row['service'] for row in rows} == BINS, 'build executable inventory mismatch')
    for row in rows:
        require(row['commit'] == source and row['version'] == manifest['version'] and
                row['sha256'] == digest(files['bin/'+row['service']]), 'build executable identity mismatch')
    require(built['migration_catalog'] == catalog, 'build migration catalog mismatch')
    package = reports['verification.json']
    require(all(package.get(key) == value for key, value in verification.items()), 'assembly evidence mismatch')
    require(package.get('reproducible_archive') is True and package.get('runbooks') == runbooks, 'missing reproducibility/runbooks evidence')
    for name in ('target-integrity.json', 'installation-preparation.json'):
        row = reports[name]
        require(row.get('archive_sha256') == archive_sha and row.get('manifest_sha256') == manifest_sha,
                'foreign archive evidence: '+name)
    installation = reports['installation-preparation.json']
    require(installation.get('native_migrations_applied') == len(migrations), 'migration execution count mismatch')
    upgrade = installation['upgrade']
    for check in UPGRADE:
        require(upgrade.get(check) is True, 'missing successful upgrade check: '+check)
    for key, expected in (('interrupted_phases', PHASES), ('incompatible_candidates_rejected_before_mutation', REJECTIONS)):
        values = upgrade.get(key, [])
        require(len(values) == len(expected) and set(values) == expected, 'incomplete upgrade coverage: '+key)
    risk = reports['risk-projection-recovery.json']
    require(risk.get('links') == 209 and upgrade.get('risk_projection_links_checked') == 209, 'incomplete risk fixture')
    require(upgrade.get('production_acceptance') is False and risk.get('production_acceptance') is False,
            'unexpected production claim')
    baseline = reports['dependency-preflight.json']['baseline']
    require(isinstance(baseline, dict) and len(baseline) == 8 and all(v is True for v in baseline.values()), 'incomplete dependency baseline')
    return {'schema': 'gojet.release-candidate-evidence.v1', 'source': source, 'version': manifest['version'],
        'producer': context, 'archive': {'name': archive.name, 'sha256': archive_sha, 'bytes': archive.stat().st_size,
            'members': len(files), 'manifest_sha256': manifest_sha, 'sbom_sha256': digest(files['sbom.cdx.json']),
            'checksum_inventory_sha256': digest(files['SHA256SUMS'])},
        'runbooks': {name: manifest['files'][name] for name in sorted(PATHS)},
        'evidence': evidence, 'native_test_evidence_complete': True,
        'installable': False, 'g11_approval': 'pending-technical-review', 'production_acceptance': False,
        'limitations': ['Runbook commands checked statically; operator instructions not executed as a standalone procedure.',
            'Native tests ran in isolated CI; target-host reboot and real production providers remain P22 work.',
            'Upgrade fixture uses the same compiled binaries; arbitrary historical releases are not certified.',
            'Reports without a source field are bound to the producing CI job, not independently source-attested.',
            'This index is neither a release signature nor deployment authorization.']}


def main():
    require(len(sys.argv) == 4, 'usage: finalize_candidate.py ARCHIVE NATIVE_BUILD SOURCE')
    output = Path(sys.argv[1]).parent/'candidate.json'
    # Remove stale success before checking any producer or report input.
    output.unlink(missing_ok=True)
    source = sys.argv[3]
    require(os.environ.get('GITHUB_SHA') == source and subprocess.check_output(
        ['git', 'rev-parse', 'HEAD'], text=True).strip() == source, 'CI checkout source mismatch')
    context = {key: os.environ.get(env, '') for key, env in (
        ('repository', 'GITHUB_REPOSITORY'), ('run_id', 'GITHUB_RUN_ID'),
        ('run_attempt', 'GITHUB_RUN_ATTEMPT'), ('job', 'GITHUB_JOB'))}
    result = finalize(sys.argv[1], sys.argv[2], source, context)
    output.write_bytes(encode(result))
    print(json.dumps(result, sort_keys=True))


if __name__ == '__main__':
    main()
