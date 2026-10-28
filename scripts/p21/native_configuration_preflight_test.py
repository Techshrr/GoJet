"""Invalid native configuration on a disposable CI host; no simulated replies."""
import os
from pathlib import Path
import subprocess
import tempfile


def exercise_service_configuration(call):
    assert os.geteuid() == 0
    rejected = []
    for check, unit, directory, command, content in [
        ('nginx', 'nginx.service', '/etc/nginx/conf.d',
         ['/usr/sbin/nginx', '-t'], b'gojet_preflight_invalid_directive on;\n'),
        ('php', 'php8.3-fpm.service', '/etc/php/8.3/fpm/pool.d',
         ['/usr/sbin/php-fpm8.3', '-t'], b'[global]\ngojet_preflight_invalid_directive = yes\n'),
    ]:
        def active_pid():
            subprocess.run(['systemctl', 'is-active', '--quiet', unit], check=True,
                           capture_output=True, timeout=10)
            result = subprocess.run(['systemctl', 'show', '--property=MainPID', '--value', unit],
                                    check=True, capture_output=True, text=True, timeout=10)
            pid = int(result.stdout.strip())
            assert pid > 0
            return pid

        assert subprocess.run(command, capture_output=True, timeout=15).returncode == 0
        pid = active_pid()
        fd, name = tempfile.mkstemp(prefix='gojet-preflight-invalid-', suffix='.conf', dir=directory)
        fault = Path(name)
        try:
            with os.fdopen(fd, 'wb') as stream:
                stream.write(content)
                stream.flush()
                os.fsync(stream.fileno())
            # The real parser must reject the included file while the existing
            # daemon stays active: is-active alone cannot prove readiness.
            assert subprocess.run(command, capture_output=True, timeout=15).returncode != 0
            assert active_pid() == pid
            report = call()
            assert report['ready'] is False and report['checks'][check] is False
            assert all(value is True for key, value in report['checks'].items() if key != check)
            assert active_pid() == pid
        finally:
            fault.unlink()
        assert subprocess.run(command, capture_output=True, timeout=15).returncode == 0
        assert active_pid() == pid
        assert call()['ready'] is True, 'Native configuration did not recover: ' + check
        rejected.append(check)
    return dict(invalid_configuration_rejected=rejected, live_daemon_pids_preserved=True,
                configuration_files_removed=True, recovered_readiness_verified=True)


def exercise_mysql_settings(call):
    assert os.geteuid() == 0
    # Supplied only by the disposable native payload workflow; never copied to
    # the release, command arguments, preflight request or evidence output.
    password = os.environ['P21_MYSQL_ADMIN_PASSWORD']

    def sql(statement):
        result = subprocess.run(['mysql', '--batch', '--skip-column-names',
                                 '-h127.0.0.1', '-uroot'], input=statement,
                                env={'PATH': '/usr/sbin:/usr/bin:/sbin:/bin', 'MYSQL_PWD': password},
                                capture_output=True, text=True, timeout=10)
        if result.returncode:
            raise RuntimeError('Native MySQL fixture command failed')
        return result.stdout.strip()

    original = sql('SELECT @@GLOBAL.autocommit;')
    assert original == '1', 'Native MySQL fixture requires healthy initial autocommit'
    assert call()['ready'] is True
    try:
        sql('SET GLOBAL autocommit=0;')
        # A separate real connection inherits the incompatible setting, as the
        # production PDO probe does. No response/version override is used.
        assert sql('SELECT @@SESSION.autocommit;') == '0'
        report = call()
        assert report['ready'] is False and report['checks']['mysql'] is False
        assert all(value is True for key, value in report['checks'].items() if key != 'mysql')
    finally:
        sql('SET GLOBAL autocommit=1;')
    assert sql('SELECT @@GLOBAL.autocommit, @@SESSION.autocommit;') == '1\t1'
    assert call()['ready'] is True, 'Native MySQL settings did not recover'

    # Exercise the other production PDO admission condition against the real
    # server. A host without binary logging cannot prove this rejection branch.
    original_trust = sql('SELECT @@GLOBAL.log_bin_trust_function_creators;')
    assert original_trust == '1', 'Native MySQL fixture requires initial function-creator trust'
    assert sql('SELECT @@GLOBAL.log_bin;') == '1', 'Native MySQL fixture requires real binary logging'
    try:
        sql('SET GLOBAL log_bin_trust_function_creators=0;')
        assert sql('SELECT @@log_bin, @@log_bin_trust_function_creators;') == '1\t0'
        report = call()
        assert report['ready'] is False and report['checks']['mysql'] is False
        assert all(value is True for key, value in report['checks'].items() if key != 'mysql')
    finally:
        sql('SET GLOBAL log_bin_trust_function_creators=1;')
    assert sql('SELECT @@log_bin, @@log_bin_trust_function_creators;') == '1\t1'
    assert call()['ready'] is True, 'Native MySQL function-creator trust did not recover'
    return dict(disabled_autocommit_rejected=True, real_session_default_verified=True,
                binary_logging_enabled=True, disabled_function_creator_trust_rejected=True,
                original_function_creator_trust_restored=True,
                original_setting_restored=True, recovered_readiness_verified=True)
