#!/usr/bin/env python3
"""Read-only, credential-free inventory before any P22 host mutation.

Run as root with Python 3. Output is evidence for review, never G12 acceptance.
No service, package, database, configuration or filesystem changes are made.
"""

import datetime
import hashlib
import json
import os
from pathlib import Path
import platform
import shutil
import subprocess
import sys


def command(argv):
    try:
        result = subprocess.run(argv, text=True, capture_output=True, timeout=15,
                                env={"PATH": "/usr/sbin:/usr/bin:/sbin:/bin", "LC_ALL": "C"})
        # Fixed commands below expose neither environments nor configuration contents.
        return {"exit_code": result.returncode, "stdout": result.stdout.strip()}
    except (OSError, subprocess.TimeoutExpired) as error:
        return {"exit_code": None, "error": type(error).__name__}


def file_text(path):
    try:
        return Path(path).read_text().strip()
    except OSError:
        return None


def path_state(path):
    p = Path(path)
    try:
        info = p.lstat()
        state = {"exists": True, "uid": info.st_uid, "gid": info.st_gid,
                 "mode": oct(info.st_mode & 0o7777), "symlink": p.is_symlink()}
        if p.is_dir() and not p.is_symlink():
            state["nonempty"] = next(p.iterdir(), None) is not None
        return state
    except FileNotFoundError:
        return {"exists": False}
    except OSError as error:
        return {"exists": None, "error": type(error).__name__}


def main():
    identity = file_text("/etc/machine-id")
    pid1 = file_text("/proc/1/comm")
    os_release = file_text("/etc/os-release")
    report = {
        "schema": "gojet.p22-host-inventory.v1",
        "observed_at": datetime.datetime.now(datetime.timezone.utc).isoformat(),
        "read_only": True,
        "effective_uid": os.geteuid(),
        "os_release": os_release,
        "architecture": platform.machine(),
        "kernel": platform.release(),
        "pid1": pid1,
        "boot_id": file_text("/proc/sys/kernel/random/boot_id"),
        "machine_id_sha256": hashlib.sha256(identity.encode()).hexdigest() if identity else None,
        "virtualization": command(["systemd-detect-virt"]),
        "container": command(["systemd-detect-virt", "--container"]),
        "service_units": command(["systemctl", "list-unit-files", "--no-pager", "--no-legend",
                                  "gojet*", "nginx*", "php*fpm*", "mysql*", "mariadb*",
                                  "redis*", "clamav*"]),
        "active_services": command(["systemctl", "list-units", "--type=service", "--state=running",
                                    "--no-pager", "--no-legend"]),
        "listeners": command(["ss", "-H", "-lnt"]),
        "native_packages": command(["dpkg-query", "-W", "-f=${binary:Package}\t${Version}\t${db:Status-Status}\n",
                                    "nginx", "php8.3-cli", "php8.3-fpm", "mysql-server",
                                    "mariadb-server", "redis-server", "clamav-daemon"]),
        "state_paths": {p: path_state(p) for p in [
            "/opt/gojet", "/etc/gojet", "/var/lib/gojet", "/var/lib/gojet-installer",
            "/var/lib/mysql", "/var/lib/redis", "/var/www", "/etc/nginx/conf.d",
            "/etc/nginx/sites-enabled"]},
        "filesystems": {},
        "clean_host_accepted": False,
        "g12_pass": False,
        "review_required": "Review owner-declared clean host, all existing services/state and real OS before installing dependencies. Existing/unknown data must never be erased to force acceptance.",
    }
    for path in ["/", "/opt", "/var/lib", "/tmp"]:
        try:
            usage = shutil.disk_usage(path)
            report["filesystems"][path] = {"device": os.stat(path).st_dev,
                                          "total_bytes": usage.total, "free_bytes": usage.free}
        except OSError as error:
            report["filesystems"][path] = {"error": type(error).__name__}
    report["inventory_complete"] = all([
        os.geteuid() == 0, os_release, identity, report["boot_id"], pid1 == "systemd",
        report["service_units"]["exit_code"] == 0,
        report["active_services"]["exit_code"] == 0,
        report["listeners"]["exit_code"] == 0,
        all(state["exists"] is not None for state in report["state_paths"].values()),
        all("error" not in fs for fs in report["filesystems"].values()),
    ])
    print(json.dumps(report, ensure_ascii=False, indent=2, sort_keys=True))
    return 0 if report["inventory_complete"] else 2


if __name__ == "__main__":
    sys.exit(main())
