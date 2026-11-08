"""Disposable systemd runner: immutable rc.1 -> candidate, interrupted recovery.

No test hooks ship in a release. A private copy of the candidate tools is killed
at the persisted switching boundary to test restoration after the real DDL.
"""
import hashlib
import json
import os
from pathlib import Path
import pwd
import secrets
import shutil
import signal
import subprocess
import sys
import tempfile

sys.path.insert(0, str(Path(__file__).resolve().parent))
from stage_release import stage

OLD_SOURCE = '32ea29d766b0ac0790964517ce4e220a1208bf0d'
OLD_ARCHIVE = '7e7343c34571f69038e090f20775e1a941b5a33b80825e755813dd08500f1542'
OLD_MANIFEST = 'fce4900d7f900de2ad7655d6f7411e0771f7a1d4417668aad7538974db5310ba'


def inject_checkpoint(code):
    start=code.index('    private static function phase(')
    end=code.index('    private static function waitHealthy(',start)
    block=code[start:end]
    hook="        UpgradeIO::record($job.'/state.json',$state);"
    if block.count(hook) != 1 or 'P22_STOP_PHASE' in code:
        raise ValueError('Ambiguous interruption checkpoint')
    injected=block.replace(hook,hook+"\n        if (getenv('P22_STOP_PHASE')===$phase) { posix_kill(getmypid(),9); }",1)
    return code[:start]+injected+code[end:]


def main():
    assert os.geteuid() == 0 and os.environ.get('P22_DISPOSABLE_NATIVE_CI') == '1'
    old_archive, candidate_dir = map(lambda s: Path(s).resolve(), sys.argv[1:3])
    candidate = json.loads((candidate_dir/'release.json').read_text())
    assert not Path('/var/lib/gojet-installer-authority/installed.lock').exists()
    assert not Path('/var/lib/gojet-upgrade-authority').exists()
    env = {'PATH': '/usr/sbin:/usr/bin:/sbin:/bin'}
    administrator = {'host': '127.0.0.1', 'port': 3306, 'name': 'gojet_p22_transition',
                     'user': 'root', 'password': os.environ['P21_MYSQL_ADMIN_PASSWORD']}
    def sql(query):
        return subprocess.check_output(['mysql', '-h127.0.0.1', '-uroot', '-NB',
            administrator['name'], '-e', query], env=env | {'MYSQL_PWD': administrator['password']},
            text=True, timeout=30).strip()
    with tempfile.TemporaryDirectory(prefix='gojet-transition-', dir='/var/lib') as tmp:
        root = Path(tmp); root.chmod(0o755)
        old, new = root/'old', root/'new'
        stage(old_archive, {'archive_sha256': OLD_ARCHIVE, 'manifest_sha256': OLD_MANIFEST,
              'source': OLD_SOURCE, 'version': '10.0.0-rc.1'}, old)
        stage(candidate_dir/f"gojet-{candidate['version']}-linux-amd64-payload.tar.gz", candidate, new)
        cert, key = root/'certificate.pem', root/'private.key'
        subprocess.run(['openssl', 'req', '-x509', '-newkey', 'rsa:2048', '-nodes', '-days', '1',
            '-subj', '/CN=localhost', '-addext', 'subjectAltName=DNS:localhost',
            '-keyout', str(key), '-out', str(cert)], check=True, capture_output=True)
        cert.chmod(0o644); key.chmod(0o600)
        storage = root/'files'
        user = pwd.getpwnam('gojet')
        for path in [storage, storage/'quarantine', storage/'published']:
            path.mkdir(mode=0o700); os.chown(path, user.pw_uid, user.pw_gid)
        config = {'schema': 'gojet.native-configuration.v1', 'origin': 'https://localhost:18443',
            'database': {'port':3306, 'name':administrator['name'], 'user':'gojet_transition', 'password':'p22-ci-only'},
            'redis': {'port':6379, 'db':6, 'password':''},
            'files': {'root':str(storage), 'clamav_socket':'/run/clamav/clamd.ctl', 'max_upload_bytes':1048576,
                      'max_files':100, 'max_bytes':10485760, 'types':{'pdf':['application/pdf']}},
            'quotas': {'text':100, 'bio':100, 'qr':100},
            'smtp': {'host':'localhost', 'port':2525, 'from':'native@example.test', 'username':'', 'password':''},
            'risk': {'name':'transition-ci', 'endpoint':'https://unused.invalid/check', 'policy_version':'native-v1'},
            'turnstile_secret':secrets.token_urlsafe(32), 'auth_rate':{'limit':10, 'window_seconds':60}}
        request = {'configuration':config,
            'admin':{'email':'transition@example.test','display_name':'Transition CI','password':secrets.token_urlsafe(32)},
            'tls':{'certificate':str(cert),'private_key':str(key),'http_port':18080}}
        private = root/'private'
        private.mkdir(mode=0o700)
        prepared = private/'prepared'
        def command(tool, name, *args, value=None, stop=None):
            result = subprocess.run(['php8.3', str(tool/'installer/bin'/name), *map(str,args)],
                input=None if value is None else json.dumps(value).encode(), capture_output=True,
                timeout=300, env=env | ({'P22_STOP_PHASE':stop} if stop else {}))
            if stop:
                assert result.returncode == -signal.SIGKILL, 'Interruption checkpoint not reached'
                return
            assert result.returncode == 0 and not result.stderr, 'Native transition command failed: '+name
            return json.loads(result.stdout)
        try:
            assert command(old,'prepare-installation.php',prepared,old,OLD_MANIFEST,OLD_SOURCE,value=request)['prepared']
            assert command(old,'apply-installation.php',prepared)['installation_complete']
            assert sql("SELECT COUNT(*) FROM gojet_schema_migrations WHERE status='applied'") == '31'
            sql("INSERT INTO admin_platform_settings (setting_key,version,value_json,updated_by) SELECT 'general',1,JSON_OBJECT('site_name','Preserve existing settings'),id FROM admin_administrators LIMIT 1")
            before_settings = sql("SELECT setting_key,version,CAST(value_json AS CHAR) FROM admin_platform_settings ORDER BY setting_key")
            before_admin = sql('SELECT COUNT(*) FROM admin_administrators')
            before_journal = sql('SELECT id,checksum FROM gojet_schema_migrations WHERE id<=31 ORDER BY id')
            marker = Path('/var/lib/gojet-installer-authority/installed.lock').read_bytes()
            evidence = storage/'published/transition-evidence'
            evidence.write_bytes(b'old file data\x00\xff'); evidence.chmod(0o640); os.chown(evidence,user.pw_uid,user.pw_gid)
            tools = root/'fault-tools'; (tools/'installer').mkdir(parents=True)
            shutil.copytree(new/'installer',tools/'installer',dirs_exist_ok=True)
            impl = tools/'installer/src/NativeUpgrade.php'
            impl.write_text(inject_checkpoint(impl.read_text()))
            def prepare(job):
                return command(new,'prepare-upgrade.php',prepared,new,candidate['manifest_sha256'],candidate['source'],job,value=administrator)['plan_sha256']
            failed_job=private/'interrupted-job'; digest=prepare(failed_job)
            command(tools,'run-upgrade.php',failed_job,digest,stop='switching')
            assert sql("SELECT COUNT(*) FROM gojet_schema_migrations WHERE status='applied'") == '32'
            assert command(new,'recover-upgrade.php',failed_job,digest)['recovered']
            assert sql("SELECT COUNT(*) FROM gojet_schema_migrations WHERE status='applied'") == '31'
            assert sql("SELECT setting_key,version,CAST(value_json AS CHAR) FROM admin_platform_settings ORDER BY setting_key") == before_settings
            assert evidence.read_bytes() == b'old file data\x00\xff'
            job=private/'successful-job'; digest=prepare(job)
            assert command(new,'run-upgrade.php',job,digest)['upgrade_complete']
            assert sql("SELECT COUNT(*) FROM gojet_schema_migrations WHERE status='applied'") == '32'
            assert sql('SELECT id,checksum FROM gojet_schema_migrations WHERE id<=31 ORDER BY id') == before_journal
            assert sql('SELECT COUNT(*) FROM admin_administrators') == before_admin
            assert sql("SELECT setting_key,version,CAST(value_json AS CHAR) FROM admin_platform_settings ORDER BY setting_key") == before_settings
            assert Path('/var/lib/gojet-installer-authority/installed.lock').read_bytes() == marker
            assert not Path('/var/lib/gojet-upgrade-authority/pending.json').exists()
            active=json.loads(Path('/var/lib/gojet-upgrade-authority/release.json').read_text())
            assert active['release']['dependencies']['source'] == candidate['source']
            names=['redirectengine','analyticsworker','analyticsreconciler','platformapi','mailworker','fileworker','operationsmonitor','logreceiver']
            for name in names:
                pid=subprocess.check_output(['systemctl','show',f'gojet-{name}.service','-p','MainPID','--value'],text=True).strip()
                assert Path('/proc/'+pid+'/exe').resolve() == new/'bin'/name
            print(json.dumps({'passed':True,'old_source':OLD_SOURCE,'source':candidate['source'],
                'immutable_rc1_installed':True,'actual_candidate_binaries_running':True,
                'migrations_before':31,'migrations_after':32,'post_migration_interruption_recovered':True,
                'original_migration_checksums_preserved':True,'settings_and_administrator_preserved':True,
                'file_backup_restore_verified':True,'original_installation_receipt_preserved':True,
                'production_acceptance':False,'reboot_tested':False}))
        finally:
            # This entire host is disposable CI. Stop only application processes
            # before temporary private keys/configuration are removed.
            subprocess.run(['systemctl','stop',*[f'gojet-{n}.service' for n in
                ['redirectengine','analyticsworker','analyticsreconciler','platformapi','mailworker','fileworker','operationsmonitor','logreceiver']]],capture_output=True,timeout=90)


if __name__ == '__main__':
    main()
