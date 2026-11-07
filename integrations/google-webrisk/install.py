#!/usr/bin/env python3
"""Add a restricted external provider without modifying the approved GoJet package.

Ubuntu 24.04 only. No DB, 80/443 site, package, firewall or installer-state edits.
Every owned file is recorded; rollback refuses to remove altered files.
"""
import argparse
import fcntl
import getpass
import hashlib
import ipaddress
import json
import os
import re
import shutil
import socket
import stat
import subprocess
import sys
import time
from datetime import datetime, timezone
from pathlib import Path

from adapter import VERSION, GoogleLookup
from verify import verify

HERE = Path(__file__).resolve().parent
ROOT = Path("/opt/gojet-integrations/google-webrisk") / VERSION
PRIVATE = Path("/etc/gojet-webrisk")
KEY = PRIVATE / "api-key"
RECEIPT = PRIVATE / "installation.json"
UNIT = Path("/etc/systemd/system/gojet-webrisk.service")
NGINX = Path("/etc/nginx/conf.d/gojet-webrisk.conf")
HOOK = Path("/etc/letsencrypt/renewal-hooks/deploy/90-gojet-webrisk-nginx")
SOURCE_FILES = ("adapter.py", "verify.py", "install.py", "test_adapter.py", "gojet-webrisk.service", "README.md", "test_install.py",
                "gojet-webrisk-probe", "probe-main.go", "BUILD.json", "fixture_server.py")


def run(*argv):
    result = subprocess.run(argv, capture_output=True, text=True, timeout=30)
    if result.returncode:
        # Commands never contain secrets. No command stderr is echoed because
        # unrelated Nginx config paths may include private deployment data.
        raise RuntimeError("command failed: " + " ".join(argv[:3]))
    return result.stdout.strip()


def sha(raw):
    return hashlib.sha256(raw).hexdigest()


def write_new(path, raw, mode=0o644):
    fd = os.open(path, os.O_WRONLY | os.O_CREAT | os.O_EXCL | os.O_NOFOLLOW, mode)
    try:
        with os.fdopen(fd, "wb") as file:
            os.fchmod(file.fileno(), mode)
            file.write(raw)
            file.flush()
            os.fsync(file.fileno())
    except Exception:
        path.unlink()
        raise


def public_directory(path):
    if not path.exists():
        path.mkdir(mode=0o755)
        path.chmod(0o755)
    st = path.lstat()
    if not stat.S_ISDIR(st.st_mode) or st.st_uid != 0 or stat.S_IMODE(st.st_mode) != 0o755:
        raise RuntimeError("code directory must be root-owned mode 0755: " + str(path))


def checked_private_dir():
    if not PRIVATE.exists():
        PRIVATE.mkdir(mode=0o700)
    st = PRIVATE.lstat()
    if not stat.S_ISDIR(st.st_mode) or st.st_uid != 0 or stat.S_IMODE(st.st_mode) != 0o700:
        raise RuntimeError("private configuration directory must be root-owned mode 0700")


def manifest():
    lines = (HERE / "SHA256SUMS").read_text().splitlines()
    entries = {}
    for line in lines:
        digest, name = line.split("  ", 1)
        if name not in SOURCE_FILES or name in entries or not re.fullmatch(r"[a-f0-9]{64}", digest):
            raise RuntimeError("invalid package manifest")
        file = HERE / name
        if file.is_symlink() or not file.is_file() or sha(file.read_bytes()) != digest:
            raise RuntimeError("package checksum mismatch: " + name)
        entries[name] = digest
    if set(entries) != set(SOURCE_FILES):
        raise RuntimeError("incomplete package manifest")
    return entries


def nginx_config(host, ip):
    return f'''# Managed by GoJet Google Web Risk integration {VERSION}.
# Source allowlist uses the actual TCP peer, never X-Forwarded-For.
limit_req_zone $binary_remote_addr zone=gojet_webrisk_rate:1m rate=5r/s;
limit_conn_zone $binary_remote_addr zone=gojet_webrisk_connections:1m;
geo $realip_remote_addr $gojet_webrisk_local_peer {{
    default 0;
    {ip} 1;
    127.0.0.1 1;
}}
server {{
    listen {ip}:9443 ssl;
    server_name {host};
    ssl_certificate /etc/letsencrypt/live/{host}/fullchain.pem;
    ssl_certificate_key /etc/letsencrypt/live/{host}/privkey.pem;
    ssl_protocols TLSv1.2 TLSv1.3;
    access_log off;
    error_log /var/log/nginx/gojet-webrisk-error.log warn;
    allow {ip};
    allow 127.0.0.1;
    deny all;
    client_max_body_size 8k;
    client_body_timeout 10s;
    client_header_timeout 10s;
    keepalive_timeout 5s;
    limit_conn gojet_webrisk_connections 8;
    limit_req zone=gojet_webrisk_rate burst=10 nodelay;
    limit_req_status 429;
    if ($host != {host}) {{ return 421; }}
    if ($gojet_webrisk_local_peer = 0) {{ return 403; }}
    location = /v1/check {{
        limit_except POST {{ deny all; }}
        proxy_pass http://127.0.0.1:9089;
        proxy_set_header Host localhost;
        proxy_set_header Connection close;
        proxy_set_header Authorization "";
        proxy_set_header Cookie "";
        proxy_set_header X-Forwarded-For "";
        proxy_connect_timeout 2s;
        proxy_read_timeout 12s;
        proxy_send_timeout 5s;
        proxy_next_upstream off;
    }}
    location / {{ return 404; }}
}}
'''.encode()


def validate_host(host, ip):
    if not re.fullmatch(r"[a-z0-9](?:[a-z0-9.-]*[a-z0-9])?", host) or "." not in host:
        raise RuntimeError("invalid public hostname")
    address = ipaddress.ip_address(ip)
    if address.version != 4 or not address.is_global:
        raise RuntimeError("server IP must be a public IPv4 address")
    answers = {item[4][0] for item in socket.getaddrinfo(host, 9443, type=socket.SOCK_STREAM)}
    if answers != {ip}:
        raise RuntimeError("hostname DNS must point directly and exclusively to this server IPv4; proxy/other addresses found")
    local = json.loads(run("ip", "-j", "address", "show"))
    if ip not in {a["local"] for interface in local for a in interface.get("addr_info", [])}:
        raise RuntimeError("supplied IP does not belong to this host")
    route = json.loads(run("ip", "-j", "route", "get", ip))[0]
    if route.get("dev") != "lo":
        raise RuntimeError("self-connection must use the local loopback route")
    certificate = f"/etc/letsencrypt/live/{host}/fullchain.pem"
    if not Path(f"/etc/letsencrypt/live/{host}/privkey.pem").is_file():
        raise RuntimeError("existing site private key missing")
    run("openssl", "x509", "-in", certificate, "-noout", "-checkhost", host)
    run("openssl", "x509", "-in", certificate, "-noout", "-checkend", "604800")


def discover_server_ip(host):
    if not re.fullmatch(r"[a-z0-9](?:[a-z0-9.-]*[a-z0-9])?", host) or "." not in host:
        raise RuntimeError("invalid public hostname")
    answers = {entry[4][0] for entry in socket.getaddrinfo(host, 9443, type=socket.SOCK_STREAM)}
    if len(answers) != 1:
        raise RuntimeError("site DNS must contain one direct server address")
    return answers.pop()  # validate_host verifies public IPv4, local ownership and route before changes.


def check_owned(receipt):
    for name, digest in receipt["files"].items():
        path = Path(name)
        if path.is_symlink() or not path.is_file() or sha(path.read_bytes()) != digest:
            raise RuntimeError("owned file missing or changed; preserve for review: " + name)


def save_report(host, ip):
    report = verify("https://" + host + ":9443/v1/check", ip)
    report["installed_files_sha256"] = json.loads(RECEIPT.read_text())["files"]
    stamp = datetime.now(timezone.utc).strftime("%Y%m%dT%H%M%S%fZ")
    output = PRIVATE / ("acceptance-" + stamp + ".json")
    write_new(output, (json.dumps(report, ensure_ascii=False, indent=2) + "\n").encode(), 0o600)
    print(json.dumps(report, ensure_ascii=False, indent=2))
    print("验收记录：" + str(output))
    return report["passed"]


def install(host, ip):
    hashes = manifest()
    validate_host(host, ip)
    run("nginx", "-t")
    run("systemctl", "is-active", "nginx")
    checked_private_dir()
    if RECEIPT.exists():
        if RECEIPT.is_symlink():
            raise RuntimeError("installation receipt must be an ordinary file")
        receipt = json.loads(RECEIPT.read_text())
        if (receipt.get("version"), receipt.get("host"), receipt.get("ip"), receipt.get("source_hashes")) != (VERSION, host, ip, hashes):
            raise RuntimeError("existing installation differs; preserve it")
        check_owned(receipt)
        print("已有同版本安装，保留密钥与配置，继续真实验收。")
        return save_report(host, ip)
    for path in (ROOT, UNIT, NGINX, HOOK):
        if os.path.lexists(path):
            raise RuntimeError("existing unowned path: " + str(path))
    # Reject an existing unit under /usr/lib as well, rather than overriding it.
    result = subprocess.run(["systemctl", "show", "gojet-webrisk.service", "-p", "LoadState", "--value"],
                            capture_output=True, text=True)
    if result.stdout.strip() != "not-found":
        raise RuntimeError("existing Web Risk unit")
    for address, port in (("127.0.0.1", 9089), (ip, 9443)):
        with socket.socket() as sock:
            sock.bind((address, port))
    if KEY.exists() or KEY.is_symlink():
        st = KEY.lstat()
        if not stat.S_ISREG(st.st_mode) or st.st_uid != 0 or stat.S_IMODE(st.st_mode) != 0o600:
            raise RuntimeError("API key must be a root-owned regular mode-0600 file")
        GoogleLookup(KEY.read_text().strip())
    else:
        if not sys.stdin.isatty():
            raise RuntimeError("run from an interactive terminal to enter the API key privately")
        key = getpass.getpass("Google Web Risk API key（输入不显示）：")
        GoogleLookup(key)  # Validate locally, no network yet.
        write_new(KEY, (key + "\n").encode(), 0o600)
        del key
    changed = []
    root_created = False
    try:
        public_directory(ROOT.parent.parent)
        public_directory(ROOT.parent)
        public_directory(ROOT)
        root_created = True
        for name in ("adapter.py", "verify.py", "gojet-webrisk-probe"):
            path = ROOT / name
            write_new(path, (HERE / name).read_bytes(), 0o755 if name == "gojet-webrisk-probe" else 0o644)
            changed.append(path)
        write_new(UNIT, (HERE / "gojet-webrisk.service").read_bytes())
        changed.append(UNIT)
        write_new(NGINX, nginx_config(host, ip))
        changed.append(NGINX)
        HOOK.parent.mkdir(parents=True, exist_ok=True)
        write_new(HOOK, b"#!/bin/sh\nset -eu\n/usr/sbin/nginx -t\n/usr/bin/systemctl reload nginx\n", 0o755)
        changed.append(HOOK)
        run("systemd-analyze", "verify", str(UNIT))
        run("nginx", "-t")
        run("systemctl", "daemon-reload")
        run("systemctl", "enable", "--now", "gojet-webrisk.service")
        run("systemctl", "reload", "nginx")
        for _ in range(20):
            try:
                with socket.create_connection(("127.0.0.1", 9089), timeout=1):
                    break
            except OSError:
                time.sleep(0.25)
        else:
            raise RuntimeError("adapter did not start")
        receipt = {"version": VERSION, "host": host, "ip": ip, "source_hashes": hashes,
                   "files": {str(path): sha(path.read_bytes()) for path in changed},
                   "installed_at": datetime.now(timezone.utc).isoformat()}
        write_new(RECEIPT, (json.dumps(receipt, indent=2) + "\n").encode(), 0o600)
    except Exception:
        subprocess.run(["systemctl", "disable", "--now", "gojet-webrisk.service"], capture_output=True)
        for path in reversed(changed):
            path.unlink()
        if root_created:
            ROOT.rmdir()
        subprocess.run(["systemctl", "daemon-reload"], capture_output=True)
        if subprocess.run(["nginx", "-t"], capture_output=True).returncode == 0:
            subprocess.run(["systemctl", "reload", "nginx"], capture_output=True)
        print("适配器安装失败，已撤回本次新增配置；私有 API 密钥保留，可安全重试。")
        raise
    # Acceptance failure intentionally retains the service + receipt for diagnosis.
    return save_report(host, ip)


def remove():
    checked_private_dir()
    receipt = json.loads(RECEIPT.read_text())
    check_owned(receipt)
    # Only this release's fixed paths may be removed, even if receipt is edited.
    expected = {str(ROOT / "adapter.py"), str(ROOT / "verify.py"), str(ROOT / "gojet-webrisk-probe"), str(UNIT), str(NGINX), str(HOOK)}
    if set(receipt["files"]) != expected:
        raise RuntimeError("unexpected owned paths")
    run("systemctl", "disable", "--now", "gojet-webrisk.service")
    for name in receipt["files"]:
        Path(name).unlink()
    ROOT.rmdir()
    run("nginx", "-t")
    run("systemctl", "reload", "nginx")
    run("systemctl", "daemon-reload")
    RECEIPT.rename(PRIVATE / ("removed-" + str(time.time_ns()) + ".json"))
    print("适配器已撤回，密钥与验收记录保留；使用该端点的风险检查将不可用。")


def main():
    parser = argparse.ArgumentParser()
    parser.add_argument("--host")
    parser.add_argument("--server-ip")
    parser.add_argument("--remove", action="store_true")
    args = parser.parse_args()
    if os.geteuid() != 0:
        raise RuntimeError("root required")
    os.umask(0o077)
    release = dict(line.split("=", 1) for line in Path("/etc/os-release").read_text().splitlines() if "=" in line)
    if release.get("ID", "").strip('"') != "ubuntu" or release.get("VERSION_ID", "").strip('"') != "24.04":
        raise RuntimeError("Ubuntu 24.04 required")
    if Path("/proc/1/comm").read_text().strip() != "systemd":
        raise RuntimeError("native systemd host required")
    if not args.remove and not args.host:
        raise RuntimeError("--host required")
    checked_private_dir()
    fd = os.open(PRIVATE / "install.lock", os.O_RDWR | os.O_CREAT | os.O_NOFOLLOW, 0o600)
    with os.fdopen(fd, "w"):
        fcntl.flock(fd, fcntl.LOCK_EX | fcntl.LOCK_NB)
        if args.remove:
            remove()
            return 0
        ip = args.server_ip or discover_server_ip(args.host)
        return 0 if install(args.host, ip) else 1


if __name__ == "__main__":
    try:
        sys.exit(main())
    except Exception as error:
        print("停止：" + str(error), file=sys.stderr)
        sys.exit(1)
