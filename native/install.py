#!/usr/bin/env python3
"""Install the self-contained systemd edition. Docker is not required on the host."""
import argparse, hashlib, json, os, platform, posixpath, secrets, shutil, socket, subprocess, tarfile
from pathlib import Path
from urllib.parse import urlsplit
ROOT=Path('/opt/linketic-native')
DATA=Path('/var/lib/linketic-native')
UNIT=Path('/etc/systemd/system/linketic.service')

def run(*args):return subprocess.run(args,check=True)
def sha(path):
    with path.open('rb') as f:return hashlib.file_digest(f,'sha256').hexdigest()
def main():
    parser=argparse.ArgumentParser(description='Linketic 原生安装（不需要 Docker）')
    parser.add_argument('--site-url',required=True,help='例如 https://www.linketic.com')
    args=parser.parse_args()
    import re
    if not re.fullmatch(r'https://[a-z0-9](?:[a-z0-9.-]*[a-z0-9])?(?::[1-9][0-9]{0,4})?',args.site_url) or not urlsplit(args.site_url).hostname:raise SystemExit('请输入完整 HTTPS 站点地址，不带末尾斜杠。')
    if os.geteuid()!=0:raise SystemExit('请在面板终端以 root 运行一次初始化。')
    if platform.machine()!='x86_64' or not Path('/run/systemd/system').is_dir():raise SystemExit('本包需要 Linux x86_64 和运行中的 systemd。')
    if not hasattr(tarfile,'data_filter'):raise SystemExit('请先更新系统 Python 3 安全补丁（需要 tarfile.data_filter）。')
    package=Path(__file__).resolve().parent
    manifest=json.loads((package/'native-release.json').read_text())
    archive=package/'linketic-runtime.tar.gz'
    if sha(archive)!=manifest['runtime_sha256']:raise SystemExit('安装包校验失败，未作修改。')
    if UNIT.exists() or ROOT.exists() or DATA.exists():raise SystemExit('发现已有原生部署。已停止，未覆盖程序或数据；请使用对应升级包。')
    if shutil.disk_usage('/var/lib').free<6*1024**3:raise SystemExit('至少需要 6 GiB 可用空间。')
    # These are loopback-only listeners. Fail before modifying the machine if occupied.
    for port in (18080,33306,36379,8080,8081,9089,9170,9443):
        with socket.socket() as check:
            try:check.bind(('127.0.0.1',port))
            except OSError:raise SystemExit(f'本机端口 {port} 已占用。未修改现有服务。')
    code=os.environ.get('INSTALL_ACCESS_CODE') or ''.join(secrets.choice('abcdefghijklmnopqrstuvwxyzABCDEFGHIJKLMNOPQRSTUVWXYZ0123456789') for _ in range(28))
    if not re.fullmatch('[A-Za-z0-9]{24,128}',code):raise SystemExit('INSTALL_ACCESS_CODE 必须为 24–128 位字母和数字。')
    os.umask(0o077)
    ROOT.mkdir(mode=0o755);DATA.mkdir(mode=0o700)
    rootfs=ROOT/'runtime';rootfs.mkdir(mode=0o755)
    def safe_member(member,destination):
        # Rootfs absolute links must point inside the application, including at extraction time.
        if member.issym() and member.linkname.startswith('/'):
            member.linkname=posixpath.relpath(member.linkname.lstrip('/'),posixpath.dirname(member.name) or '.')
        checked=tarfile.data_filter(member,destination)
        if checked is not None:
            checked.uid,checked.gid=member.uid,member.gid
        return checked
    with tarfile.open(archive,'r:gz') as tar:tar.extractall(rootfs,filter=safe_member,numeric_owner=True)
    (rootfs/'opt/linketic/native-mode').write_text('systemd-root-directory-v1\n')
    binds=[]
    for name,target in [('app','/var/lib/linketic'),('mysql','/var/lib/mysql'),('redis','/var/lib/redis'),('clamav','/var/lib/clamav')]:
        source=DATA/name;source.mkdir(mode=0o700);(rootfs/target.lstrip('/')).mkdir(parents=True,exist_ok=True)
        binds.append(f'BindPaths={source}:{target}')
    # Copy resolver contents rather than a host symlink into the isolated runtime.
    for name in ['hosts','resolv.conf']:
        dst=rootfs/'etc'/name
        if dst.is_symlink():dst.unlink()
        dst.write_bytes((Path('/etc')/name).read_bytes());dst.chmod(0o644)
    env=DATA/'service.env';env.write_text('SITE_URL='+args.site_url+'\nINSTALL_ACCESS_CODE='+code+'\n');env.chmod(0o600)
    content='''[Unit]
Description=Linketic website and services
After=network-online.target
Wants=network-online.target
[Service]
Type=simple
RootDirectory={root}
MountAPIVFS=yes
PrivateTmp=yes
PrivateDevices=yes
TemporaryFileSystem=/run:size=128M
EnvironmentFile={env}
{binds}
ExecStart=/usr/bin/python3 /opt/linketic/init.py
Restart=on-failure
RestartSec=5
TimeoutStopSec=90
KillMode=mixed
[Install]
WantedBy=multi-user.target
'''.format(root=rootfs,env=env,binds='\n'.join(binds))
    UNIT.write_text(content);UNIT.chmod(0o644)
    (DATA/'deployment.json').write_text(json.dumps({'version':manifest['version'],'source':manifest['source'],'site_url':args.site_url,'runtime_sha256':manifest['runtime_sha256']}))
    run('systemctl','daemon-reload');run('systemctl','enable','--now','linketic.service')
    print('\n原生服务已启动，无需 Docker。')
    print('面板反向代理目标：http://127.0.0.1:18080')
    print('网站安装地址：'+args.site_url)
    print('首次安装访问码：'+code)
    print('在网页中填写配置并完成安装。此刻尚未完成业务安装。')
if __name__=='__main__':main()
