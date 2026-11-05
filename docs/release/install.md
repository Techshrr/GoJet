# 首次安装

返回[手册目录](README.md)。本流程在已批准的隔离测试主机执行；生产仍需独立授权。失败处理见[升级与恢复](upgrade-recovery.md)。首次安装与升级使用不同入口，不要将初始化命令用于已有业务数据库。

## 1. 主机与依赖准备

验证基线为 Ubuntu 24.04、Linux amd64、systemd、PHP 8.3 CLI/FPM（posix、PDO MySQL 等必需扩展）、MySQL 8、Redis、Nginx、ClamAV。生产主机无需 Node、pnpm 或容器。操作员负责依赖安装、数据库权限、DNS、证书和容量；安装器不会代办这些事项。

MySQL、Redis 使用配置指定的本机端口和账户。专用数据库必须适合首次初始化；数据库初始化账户需要该库的迁移权限。配置和预检会验证 MySQL 设置。不要复用 CI 的数据库、口令或关闭生产检查。确认 Nginx 标准 conf.d 包含关系、php8.3-fpm 标准池加载关系、80/443 或批准端口无冲突；现有服务、站点或未知 systemd 覆盖会触发拒绝，先解决冲突而不是覆盖。

TLS 证书、私钥必须匹配 HTTPS origin 和主机名；使用 root 所有的普通文件，证书 0644、私钥 0600，禁止符号链接。证书及私钥不得置于发布目录、上传存储或本次安装计划目录。

## 2. 验证并展开归档

下面命令由 root 执行。提前设置 ARCHIVE（已下载的绝对路径）、ARCHIVE_SHA、MANIFEST_SHA、SOURCE（40 位提交）、RELEASE（新建的 /opt/gojet/releases 下版本及提交目录）。后三个摘要来自独立批准记录。路径变量不是口令。RELEASE 必须尚不存在，父目录必须为 root 所有且不可被非 root 写入。

```bash
set -euo pipefail
: "${ARCHIVE:?}" "${ARCHIVE_SHA:?}" "${MANIFEST_SHA:?}" "${SOURCE:?}" "${RELEASE:?}"
printf '%s  %s\n' "$ARCHIVE_SHA" "$ARCHIVE" | sha256sum --check --strict
install -d -m 0755 /opt/gojet/releases
mkdir -m 0755 -- "$RELEASE"
(umask 022; tar --extract --gzip --file "$ARCHIVE" --directory "$RELEASE" --keep-old-files --same-permissions)
php8.3 "$RELEASE/installer/bin/verify-package.php" "$RELEASE" "$MANIFEST_SHA" "$SOURCE"
systemd-sysusers "$RELEASE/deploy/sysusers/gojet.conf"
systemd-tmpfiles --create "$RELEASE/deploy/tmpfiles/gojet.conf"
```

只展开已经独立核对摘要的可信候选归档；不可跳过摘要检查后执行包内 PHP。展开失败保留现场并诊断，不在部分展开的目录重试或改权限绕过检查。

配置文件存储根目录及其 quarantine、published 子目录，必须由 gojet 所有、0700，位于 webroot 之外。ClamAV Unix socket 必须允许 gojet 访问；按实际 socket 组配置补充组，不要放宽到所有用户。更新病毒库并确认实际引擎与签名可用；启用只读 VERSION 查询（EnableVersionCommand yes），无需开放管理命令。

## 3. Web 安装入口

提前创建 root 私有目录，准备 ENTRY_INPUT（0600 JSON），ENTRY（尚不存在的绝对入口目录；父路径 root 所有），保留原始输入用于中断恢复。保持路径短以满足 Unix socket 长度限制。输入严格包含下列字段，不接受额外字段：

| 字段 | 内容 |
| --- | --- |
| schema | gojet.web-entry.v1 |
| origin | 本站规范 HTTPS origin，无路径、查询或 fragment |
| release、manifest_sha256、source | 上一步已验证的 RELEASE、MANIFEST_SHA、SOURCE |
| bootstrap_sha256 | 独立生成的高熵 bootstrap token 的 SHA-256（token 私下交付操作员） |
| tls | certificate、private_key 绝对路径，以及 http_port 整数 |
| files | root 存储根目录、clamav_socket 绝对路径 |

生成 token 后只将摘要写入入口输入。不要把明文放入 URL、共享终端记录或工单。

```bash
: "${ENTRY:?}" "${ENTRY_INPUT:?}"
php8.3 "$RELEASE/installer/bin/provision-web-entry.php" "$ENTRY" < "$ENTRY_INPUT"
php8.3 "$RELEASE/installer/bin/install-web-preparation-schedule.php" "$ENTRY/binding.json" "$ENTRY/state"
```

打开同源 HTTPS 的 /install，以 bootstrap token 登录，填写配置及初始管理员，提交冻结草稿。root preparation 定时器将其转换为 ENTRY/prepared 中的密封计划；该步骤不会迁移或启动业务服务。先等待准备完成并由操作员审阅密封计划、域名、数据库、存储、权限和管理员，再执行批准：

```bash
PREPARED="$ENTRY/prepared"
php8.3 "$RELEASE/installer/bin/approve-web-configuration.php" "$PREPARED" "$ENTRY/state"
php8.3 "$RELEASE/installer/bin/authorize-entry-handover.php" "$PREPARED" "$ENTRY"
php8.3 "$RELEASE/installer/bin/install-web-preflight-schedule.php"
php8.3 "$RELEASE/installer/bin/install-web-apply-schedule.php"
```

等待浏览器显示全部八项当前预检通过；在浏览器确认已批准配置，再明确提交安装。保留浏览器进度和 root 执行记录。不要另开 CLI apply 与这个流程竞争。成功必须同时有匹配密封身份的 root 完成记录和完整健康验证，/install 返回 404 本身不是成功证据。

安装完成后，等待 preparation、preflight、apply 执行进程自然退出并确认无在途任务，再退役入口。CURRENT_WEB_JSON 为当前有效入口渲染输入；未发生配置替换时是 ENTRY/web.json，发生替换时必须从已记录的当前入口取得，不能沿用旧输入。

```bash
: "${CURRENT_WEB_JSON:?}"
php8.3 "$RELEASE/installer/bin/retire-installer.php" "$PREPARED" "$CURRENT_WEB_JSON"
```

退役会关闭安装入口相关池与定时器，保留私有审计记录，不删除业务配置和数据。

## 4. 无浏览器的替代入口

仅在尚未走上述 Web 流程时选用。准备 root 私有 PROFILE_JSON，包含 configuration、admin、tls 三个对象。configuration 使用 gojet.native-configuration.v1，字段如下；不要将这张表直接当作可运行配置。

| 字段 | 必需输入 |
| --- | --- |
| origin | 规范 HTTPS origin |
| database | port、name、user、password（本机数据库） |
| redis | port、db（0–15）、password |
| files | root、clamav_socket、max_upload_bytes、max_files、max_bytes、types（扩展名到 MIME 数组） |
| quotas | text、bio、qr |
| smtp | host、port、from、username、password；认证信息成对提供 |
| risk | name、endpoint（HTTPS，无内嵌凭据/查询/fragment）、policy_version |
| turnstile_secret | 该站点私有密钥 |
| auth_rate | limit（1–1000）、window_seconds（1–86400） |

省略 configuration.secrets，让 root 准备程序生成并固定服务密钥。admin 为 email、display_name、password；不接受任意角色。tls 为 certificate、private_key、http_port。严格校验以程序返回为准。PREPARED 必须为 root 私有父目录下尚不存在的目录。

```bash
: "${PREPARED:?}" "${PROFILE_JSON:?}"
php8.3 "$RELEASE/installer/bin/prepare-installation.php" "$PREPARED" "$RELEASE" "$MANIFEST_SHA" "$SOURCE" < "$PROFILE_JSON"
php8.3 "$RELEASE/installer/bin/apply-installation.php" "$PREPARED"
php8.3 "$RELEASE/installer/bin/check-installation.php" "$PREPARED"
```

apply 负责数据、服务、站点和完成检查，不能用手工复制默认 unit 或单独写 installed.lock 替代。check-installation 适用于这个首次安装身份；升级后使用升级自身的完整健康验证及当前版本记录。
