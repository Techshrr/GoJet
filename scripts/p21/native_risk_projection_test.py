"""Native recovery command against an owned disposable MySQL schema and Redis keys."""
import json
import os
from pathlib import Path
import secrets
import shutil
import subprocess
import sys
import tempfile

from catalog import catalog

repo = Path(__file__).resolve().parents[2]
fixture, monitor = (str(Path(arg).resolve()) for arg in sys.argv[1:])
password = os.environ['P21_MYSQL_ADMIN_PASSWORD']
port = int(os.environ.get('P21_MYSQL_TEST_PORT', '3306'))
name = 'gojet_p21_risk_' + secrets.token_hex(8)
php = json.loads(os.environ.get('P21_PHP_COMMAND', '["php8.3"]'))
connection = dict(host='127.0.0.1', port=port, name=name, user='root', password=password)

def execute_php(code, *args):
    result = subprocess.run(php+['-r', code, str(repo), *args], input=json.dumps(connection).encode(),
                            capture_output=True, timeout=90)
    assert result.returncode == 0 and not result.stderr, 'Isolated projection schema setup/cleanup failed'

prefix = 'require $argv[1]."/installer/src/UpgradeIO.php";$c=json_decode(stream_get_contents(STDIN),true);'
with tempfile.TemporaryDirectory(prefix='gojet-risk-rebuild-') as temporary:
    root = Path(temporary)
    shutil.copytree(repo/'migrations', root/'migrations')
    (root/'migration-catalog.json').write_text(json.dumps(catalog(root/'migrations')))
    execute_php(prefix+'$db=GoJet\\Installer\\UpgradeIO::connection($c,false);'
                '$db->exec("CREATE DATABASE `".$c["name"]."` CHARACTER SET utf8mb4 COLLATE utf8mb4_0900_ai_ci");')
    try:
        code = prefix+'require $argv[1]."/installer/src/Migrations.php";'
        code += 'GoJet\\Installer\\Migrations::apply(GoJet\\Installer\\UpgradeIO::connection($c),$argv[2],hash_file("sha256",$argv[2]."/migration-catalog.json"),$argv[3],"10.0.0-rc.1");'
        execute_php(code, str(root), subprocess.check_output(['git','rev-parse','HEAD'],cwd=repo,text=True).strip())
        env = os.environ | dict(GOJET_MYSQL_DSN=f'root:{password}@tcp(127.0.0.1:{port})/{name}?parseTime=true&charset=utf8mb4',
            GOJET_REDIS_ADDR=os.environ.get('P21_REDIS_TEST_ADDR','127.0.0.1:6379'), GOJET_REDIS_DB='14',
            GOJET_RISK_POLICY_VERSION='p21-recovery-v1', P21_OPSMONITOR_BIN=monitor)
        result = subprocess.run([fixture], env=env, capture_output=True, text=True, timeout=120)
        assert result.returncode == 0 and not result.stderr, 'Projection recovery fixture failed: '+result.stderr
        report = json.loads(result.stdout)
        assert report['complete_rebuild'] and report['background_pagination']
        print(json.dumps(report))
    finally:
        execute_php(prefix+'$db=GoJet\\Installer\\UpgradeIO::connection($c,false);$db->exec("DROP DATABASE `".$c["name"]."`");')
