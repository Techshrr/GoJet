# 隔离测试机试装入口（开发预览）

## 固定试装版本与下载

本次固定使用已通过原生安装 CI 的源码版本
`9ea4135d17cca3e60f5a19b10e3f4eeb3397497e`，不是当前开发分支的最新提交。

- 下载页：https://github.com/Techshrr/GoJet/actions/runs/39616094148
- 下载项：`gojet-v10-p21-payload-9ea4135d17cca3e60f5a19b10e3f4eeb3397497e`
- Artifact ID：`12352609963`。下载 GitHub Actions artifact 可能需要登录。
- 包内程序：`payload/gojet-10.0.0-rc.1-linux-amd64-payload.tar.gz`
- CI artifact 有保留期限；失效后应重新提供经验证的同版本包，不能随意换用 latest。

2026-10-22 已独立下载原始 ZIP，验证 ZIP 完整性、程序归档、成员清单和 manifest。

| 对象 | SHA256 |
| --- | --- |
| Actions ZIP | `c300fc279fe40d184ad7084d8245c06b6b7942f6a1f4e373601d04acbc066d12` |
| 程序 tar.gz | `835e15fbe55b2a8cf1196218495dfc1f698f8e8e9968e0670255c29e0639212e` |
| manifest.json | `a88eb128b74911479c2befca095faaf3581fc064e9183c7778c9b004865b2e83` |

这是开发预览包，不是正式发行版。manifest 的 `installable=false` 保持原值，表示
完整 P21 验收尚未关闭。原生安装链路已在一次性 CI 主机验证，但尚未在用户测试机验证。
不把这个标记改成 true，也不以预览试装代替完整验收。

## 本轮试装范围

先走现有 root CLI：准备密封计划 → 初始化数据 → 启动服务 → 发布 HTTPS 站点 →
最终健康检查与完成锁。无需先部署网页安装向导、预检 timer 或 web apply mailbox。
这些可选安装器组件不应继续阻挡首次产品试装。

已验证的原生链路包括 31 项迁移、初始管理员、八个非 root 服务、静态站点/API、
HTTPS 和完成锁。邮件投递、外部风控、Turnstile、支付及全业务用户流程不因此视为通过。
第一次验收目标是安装成功、能打开产品并逐项记录实际业务问题。

## 开始前必须确定的测试环境

使用全新、可丢弃的 Ubuntu 24.04 x86_64 虚拟机，并保留安装前整机快照。不要在现有
业务服务器、宝塔站点或已有 GoJet 数据的机器上试跑。当前安装会写入 systemd、Nginx
及数据库，尚无通用的一键卸载/升级保证。

需提供：系统版本/架构、测试域名、拟使用的 HTTPS/HTTP 端口。不要发送服务器密码、
私钥、数据库密码或管理员密码到聊天；这些只写在目标机 root 私有配置文件中。

目标机前提（需结合实际机器准备，不能只复制 CI 测试脚本）：

- systemd；Nginx；PHP 8.3 CLI/FPM、PDO MySQL、OpenSSL。
- 原生 MySQL 8，专用空数据库及账号；迁移权限和触发器相关设置符合预检要求。
- 原生 Redis、独立逻辑库和对应凭证；MySQL/Redis 均通过 loopback 访问。
- ClamAV/clamd、已更新签名、可访问的 Unix socket，启用只读 VERSION 查询。
- 按包内 sysusers/tmpfiles 配置准备 gojet 账号、运行/日志目录；gojet 可以访问 clamd socket。
- 独立文件存储目录及 quarantine/published 子目录，归 gojet 所有、权限 0700。
- root 管理的发布目录、私有计划父目录和 `/var/lib/gojet-installer-authority`。
- 与测试域名匹配的证书和私钥，私钥仅 root 可读；没有冲突的 gojet units、Nginx
  gojet.conf 或计划监听端口。产品 API/重定向服务固定使用 loopback 8081/8080。

不要运行 `scripts/p21/native_*_test.py` 来安装用户环境：它们会制造服务故障、删除
自己的测试记录并恢复夹具，是一次性 CI 测试，不是用户安装脚本。

## 唯一安装入口

先按上面的独立摘要验证归档，解压到新的 root 管理发布目录，保留归档成员权限。
使用固定版本源码中的受信任校验器检查该目录，不能先执行未经校验的归档程序。

以下变量表示已准备好的绝对路径，不是可直接执行的默认配置：

- `TRIAL_RELEASE`：已经解压并核验的发布目录。
- `TRIAL_TOOLS`：可信固定版本源码中的 installer 目录。
- `TRIAL_PLAN`：尚不存在的计划目录，其父目录由 root 管理。
- `TRIAL_INPUT`：root 0600 的完整 JSON 配置文件。

```sh
php8.3 "$TRIAL_TOOLS/bin/verify-package.php" "$TRIAL_RELEASE" \
  a88eb128b74911479c2befca095faaf3581fc064e9183c7778c9b004865b2e83 \
  9ea4135d17cca3e60f5a19b10e3f4eeb3397497e

php8.3 "$TRIAL_RELEASE/installer/bin/prepare-installation.php" \
  "$TRIAL_PLAN" "$TRIAL_RELEASE" \
  a88eb128b74911479c2befca095faaf3581fc064e9183c7778c9b004865b2e83 \
  9ea4135d17cca3e60f5a19b10e3f4eeb3397497e < "$TRIAL_INPUT"

php8.3 "$TRIAL_RELEASE/installer/bin/apply-installation.php" "$TRIAL_PLAN"
php8.3 "$TRIAL_RELEASE/installer/bin/check-installation.php" "$TRIAL_PLAN"
```

每条命令成功后才执行下一条。JSON 顶层必须有 configuration、admin、tls。
configuration 完整字段见 [native-configuration.md](native-configuration.md)，
admin 为 email/display_name/password，tls 为 certificate/private_key/http_port。
六个加密密钥必须独立随机生成并保留；不得使用 CI 密码、占位符或临时测试服务地址
冒充可用外部配置。实际输入应在测试域名、端口、服务凭证确定后生成。

## 成功判据与失败处理

仅当 apply 返回 installation_complete=true、独立健康检查成功，且 root 完成锁存在，
才记录“原生安装完成”。之后浏览器检查首页、文档、管理员登录和工作台，再逐项测试
链接创建/跳转、文本、文件上传、二维码及分析。结果要区分通过、失败、尚未测试。
邮件/验证码/支付等需真实外部配置，不能凭页面可打开就标记通过。

发生失败先保留返回信息、相关服务状态和私有 phase journal；对外反馈前遮蔽秘密。
不要删除 installed.lock、篡改密封计划、清空数据库或删除失败 journal 后盲目重试。
服务/站点阶段有专门恢复命令，需针对失败阶段检查后使用；整机重试优先还原试装前
快照，避免把多轮残留当成新的空机安装。

## 本轮明确不作为试装前置条件

完整网页配置向导、进度展示、自动切换安装 vhost、升级/卸载、全部外部集成，以及
可选的网页预检调度安装器，都不纳入这份固定版本 CLI 试装入口。它们仍是后续
工作；正式上线和完整验收仍未完成。
