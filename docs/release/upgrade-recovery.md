# 升级、配置切换与恢复

返回[手册目录](README.md)。需要保留[安装](install.md)的原始密封目录、当前发布、备份及 root 私有输入。

## 升级前

在隔离环境演练本次候选，并安排目标数据库的维护窗口。按安装章节独立校验候选归档并解包到新目录，不能覆盖当前 release。当前兼容范围为 10.0.0 及规范的 10.0.0-rc.N（N 为 0–999999999、无多余前导零）；拒绝降级、正式版回 RC、跨 minor/major、其他 prerelease 和 build metadata。版本规则通过仍须两端九个可执行文件身份以及恢复能力匹配，失败发生在停止服务和创建升级 job 之前。

设置 TOOL_RELEASE 为用于本次任务、已经独立验证的工具版本目录；CURRENT_PREPARED 为当前安装密封计划；CANDIDATE、MANIFEST_SHA、SOURCE 为新候选身份；JOB 为 root 私有父目录下尚不存在的目录。ADMIN_CONNECTION_JSON 是 0600 私有文件，严格包含 host（127.0.0.1）、port、name、user、password；使用具备备份、恢复及必要数据库权限的专用管理连接，不能假设应用账户具备这些权限。

```bash
set -euo pipefail
: "${TOOL_RELEASE:?}" "${CURRENT_PREPARED:?}" "${CANDIDATE:?}" "${MANIFEST_SHA:?}" "${SOURCE:?}" "${JOB:?}" "${ADMIN_CONNECTION_JSON:?}"
php8.3 "$TOOL_RELEASE/installer/bin/prepare-upgrade.php" "$CURRENT_PREPARED" "$CANDIDATE" "$MANIFEST_SHA" "$SOURCE" "$JOB" < "$ADMIN_CONNECTION_JSON"
```

仅在命令成功且返回 prepared 为 true 后记录返回的 plan_sha256 为 PLAN_SHA；保留输出与 job 身份，不能从失败输出猜测摘要。prepare 不执行迁移或版本切换。审批本次具体计划后才运行：

```bash
: "${PLAN_SHA:?}"
php8.3 "$TOOL_RELEASE/installer/bin/run-upgrade.php" "$JOB" "$PLAN_SHA"
```

## 执行及完成条件

执行器开启维护保护并停止八个写入服务，备份数据库和文件，实际演练数据库恢复，检查迁移前缀并迁移，清理失效 Redis 风险投影、从 MySQL 权威数据完整重建，切换受验证的配置及二进制，启动八服务，再完成私有与公开 TLS 健康检查。只有成功提交后才结束维护。未知、过期、目标已变更的风险状态不得被恢复成旧的 allow。

备份采用全实例 MySQL 写锁，最长 300 秒，可能影响同实例其他数据库，必须纳入窗口评估。文件快照上限为 1 GiB/100000 文件；风险清理和重建各有 100000 条及 60 秒边界，重建 systemd worker 另有 75 秒上限。超过容量边界或超时即拒绝完成，应在维护前核对规模，不能删除数据来勉强通过。

成功记录必须绑定本次计划和候选身份，包含配置切换、数据库、存储、风险重建、八服务和 TLS 检查。首次安装 installed.lock 保留其原始权威身份，不应伪装成新版本；升级后不要用旧首次安装检查推断当前健康。保留升级 journal、恢复快照和当前部署记录。

## 升级中断后的恢复

确认主执行进程已经退出；不要与仍在运行的 run 并发恢复。保留原 JOB、PLAN_SHA 和 TOOL_RELEASE，用同一已验证工具执行：

```bash
php8.3 "$TOOL_RELEASE/installer/bin/recover-upgrade.php" "$JOB" "$PLAN_SHA"
```

恢复根据原始阶段决定动作：数据库变更前恢复旧部署；需要数据库恢复时先停止遗留风险重建 worker，再恢复已验证备份和文件、重建旧版本所需风险投影、恢复配置并验证八服务和 TLS。它覆盖部分 DDL、数据库缺失和恢复自身中断。已 committed/complete 的任务不得回写旧快照覆盖提交后的新业务数据；终态重试保持新数据。保持维护直到命令完成全部检查。失败则保留现场、检查具体依赖或权限后恢复同一任务，不能改 plan_sha 或重建假 job。

## 首次安装故障恢复

以下均由 root 使用原始已验证 RELEASE 执行。只在匹配的中断阶段使用，且先停止相关定时器并等待/确认对应 worker 已退出；不要用 SIGKILL 将活跃业务变成可重试状态。命令拒绝说明状态不匹配，应先核对，不可删屏障。

| 场景 | 入口及限制 |
| --- | --- |
| 入口仅发布了部分文件 | installer/bin/recover-web-entry.php，参数原 ENTRY，从 stdin 提供原 ENTRY_INPUT；验证原始身份后修复已拥有资源 |
| preparation 定时器安装中断 | installer/bin/recover-web-schedule.php，参数 preparation、原 binding.json、原 state；配置替换时还需原有绑定身份 |
| preflight/apply 定时器安装中断 | installer/bin/recover-web-schedule.php，参数 preflight 或 apply；不清除执行屏障 |
| 完整密封计划已发布但 ready 收据缺失 | installer/bin/recover-web-preparation.php，参数原 binding.json、state、经核对的 seal SHA-256；不用于已批准或开始 apply 的计划 |
| 首次服务激活失败/中断 | installer/bin/recover-services.php，参数原 PREPARED；只清理本次拥有的激活资源并保留数据，之后明确恢复原 apply |
| 首次站点激活失败/中断 | installer/bin/recover-site.php，参数原 PREPARED；只恢复匹配的站点资源，成功完成的站点不适用 |
| 入口退役中断 | installer/bin/retire-installer.php，参数原 PREPARED、当前有效 web.json；验证完成身份后重复调用 |
| 迁移/初始管理员写入失败 | 保留数据库、迁移日志和密封计划；诊断实际错误并按经审核的数据库备份恢复方案处理，不重新初始化或删 journal |

在首次数据执行和批准之前，需要更换已冻结配置时，使用 installer/bin/restart-web-configuration.php 和 installer/bin/switch-web-configuration.php 的既有受验证流程；它们需要新的隔离目录、原草稿摘要、新 bootstrap 摘要和当前 FPM 渲染身份。该操作不是覆盖原草稿。不要在部分替换后继续批准旧计划；保留新旧绑定并先恢复匹配的替换阶段。已完成安装的配置变更不能套用此入口。

这些恢复入口不接管未知配置、外部数据库或任意 systemd unit。所有恢复完成后重新检查对应完整健康结果；保留原始错误和恢复记录，不能仅以退出码之外的单个 HTTP 状态宣布成功。
