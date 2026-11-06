# Native logging and rotation

All eight Go service units send stdout/stderr to journald. Use the host's journald
retention policy; do not apply logrotate to journal database files. The logreceiver
retains its inherited Unix socket and audit authority.

The package includes `deploy/logrotate/gojet-nginx` for the eventual root apply
step to install as `/etc/logrotate.d/gojet-nginx` (root-owned, mode0644). It covers
only `/var/log/gojet/nginx/*.log`. The shipped tmpfiles configuration creates the
parent directories root:www-data0750. Both master and workers reopen logs on
USR1, so workers need directory traversal. They cannot create or replace directory
entries because directory write permission remains root-only. New logs use www-data:adm0640.
The product gojet user cannot read these web logs.

Rotation is daily or above100MiB when the host's logrotate timer runs, with14
archives and delayed gzip compression. It renames and signals only the active
Nginx master with USR1, preserving open-request logging without copytruncate.
Missing/empty logs and stopped Nginx are allowed. No shell arguments or paths come
from Installer input. Do not log cookies, authorization headers, or request bodies;
final product routing must choose the appropriate access-log policy for sensitive
URLs before enabling logging.

The disposable native CI fixture starts real Nginx, sends HTTP requests across two
rotations, verifies the old descriptor stops receiving writes, checks exact gzip
bytes, permissions and denial to gojet, and exercises stopped/missing logs. It also reproduces the root:adm0750 traversal
failure with real workers and verifies recovery after restoring the shipped group.
This does not install a production virtual host or complete P21-T008 by itself;
full static/proxy routing and privileged apply remain separate work.
