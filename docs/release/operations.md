# 日常运维与健康检查

返回[手册目录](README.md)。版本切换和故障恢复使用[升级与恢复](upgrade-recovery.md)。任何变更先核对当前发布、密封计划和维护窗口；不要用旧包的默认配置覆盖当前生成配置。

## 服务与入口

八个 unit 为 gojet-logreceiver、gojet-platformapi、gojet-redirectengine、gojet-analyticsworker、gojet-analyticsreconciler、gojet-mailworker、gojet-fileworker、gojet-operationsmonitor，后缀均为 .service。它们由 gojet 非 root 身份运行。日志接收器须先于依赖它的服务就绪；安装和升级程序负责启动顺序、完整健康验证及失败屏障。

下面是只读巡检，active/enabled 均不单独代表业务健康。journal 可能包含业务标识，分享前脱敏，不导出 EnvironmentFile 或数据库凭据。

```bash
for service in logreceiver platformapi redirectengine analyticsworker analyticsreconciler mailworker fileworker operationsmonitor; do
  systemctl is-active "gojet-$service.service"
  systemctl is-enabled "gojet-$service.service"
  systemctl show "gojet-$service.service" --property=User,MainPID,NRestarts,Result
  journalctl --unit "gojet-$service.service" --since '15 minutes ago' --no-pager --lines 40
done
nginx -t
```

检查规范域名 TLS 有效性、首页与 Docs、登录及授权后的 Workspace/Admin、真实 API 和短链响应；关注磁盘容量、错误率、队列积压、MySQL/Redis 可达性、ClamAV 签名时效、SMTP 与风险提供方错误。不得通过放开内部健康地址、伪造转发头或跳过 TLS 校验制造通过。维护期间公开流量应受到维护屏障保护，私有健康探针不能暴露给客户端。

首次安装的完整检查入口见[安装](install.md)。升级的 run/recover 只有完成私有与公开 TLS 健康检查才解除维护。目标主机重启后的八服务恢复、真实邮件/OAuth/Turnstile/支付及生产域名仍须 P22 验收记录；构建 CI 不代替这些记录。

## 配置与日志

保留当前生成的 systemd unit、受限 EnvironmentFile、Nginx 配置、FPM 入口记录和密封输入。不要手动修改已密封目录或包内文件；配置身份改变会导致后续验证拒绝。现有升级流程切换受验证的版本和配置；它不是允许任意在线修改域名、数据库、存储或密钥的通用配置编辑器。此类业务变更必须另行制定迁移和回退步骤。

Nginx 日志轮转模板为 deploy/logrotate/gojet-nginx。由操作员审阅路径及站点状态后纳入主机日志轮转配置；不要假设仅解包就已启用。日志应限制读取权限和保留周期，监测轮转后写入是否正常。不要为诊断打开 shell xtrace 或输出私有配置。

## 备份与恢复演练

升级 job 的数据库和文件快照服务于本次受控恢复，不是长期备份体系。运维需另行安排数据库、文件存储、当前配置和私有密封/收据的一致性备份；保留关联版本目录及校验摘要。离机备份应加密并限制访问，密钥应独立管理；定期在隔离主机恢复，记录时间点、完整性、恢复耗时及实际业务读写结果。

明确 RPO/RTO、保留周期和磁盘预算后安排维护窗口。备份与清理不得跨越进行中的升级/恢复任务。未验证新版本稳定、恢复窗口未结束之前，不能删除旧 release、原始 PREPARED、升级 job、备份或私有凭据。当前没有自动“安全清理所有旧版本”命令。

## 异常处理

先保存退出码、非敏感错误、unit 状态、当前阶段及对应摘要，确认没有仍在运行的变更任务。只处理当前故障。不要反复启动全部服务、重复初始化数据库或手工删除 started/lock/journal 文件；这些屏障防止重放和数据丢失。按照[恢复表](upgrade-recovery.md)选择对应入口，未知或外部资源冲突应保持现场并人工定位。
