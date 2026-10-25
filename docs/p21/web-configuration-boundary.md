# 网页配置与权限边界

当前实现和待办以 [STATUS.md](STATUS.md) 为准。本文件描述有效约束，不保存开发检查点。

## 输入归属

| 来源 | 内容 | 权限约束 |
| --- | --- | --- |
| root 预绑定 | 发行目录/摘要、计划目录、TLS 文件、存储目录、ClamAV socket | 不接受网页选择路径或替代值；实际内容、权限和来源在 root 侧重验 |
| 已认证网页 | 本机数据库/Redis、HTTPS origin、SMTP、风控/Turnstile、配额、文件类型和初始管理员 | 只接受明确字段；禁止角色、权限、散列、服务密钥及完成标记 |
| root 私有生成 | 六个独立服务密钥和 key ID | 只保存在密封计划；重试和恢复不重新生成已发布密钥，不回显 |
| Go 管理员权限目录 | 初始角色与权限 | 沿用原生权限、密码和审计规则，不由网页定义 |

## 提交与冻结

`POST /install/configuration` 只接受 `{csrf,draft}`。配置请求信封最多 32 KiB，
持久化草稿最多 24 KiB；登录、审批确认及执行请求最多 4 KiB。Nginx/PHP 总上限
32 KiB，入口按路由有界读取。浏览器检查用于减少输入错误，不能替代 root 校验。

InstallState 在会话、一次性 CSRF 和状态锁下验证字段。无效草稿消耗 CSRF，但不
保存配置。合法草稿先持久化 environment/checking，再独占写入并同步私有草稿。
重登、刷新及连接中断均不覆盖草稿、不解除冻结、不发出执行请求。
表单提交或离页清除秘密控件；不会从冻结草稿回填密码或写入浏览器存储。

## root 准备

`run-web-preparation.php BINDING STATE` 消费固定 root 绑定及冻结草稿。
绑定为 root0600 单链接文件，worker 读取上限 4 KiB；草稿属于 gojet-installer，
目录0700、文件0600、单链接。读取核对 inode、目录身份、长度和摘要，拒绝替换。

`prepare-web-configuration.php BINDING` 是独立 root stdin 入口，绑定上限 8 KiB；
`prepare-frozen-web-configuration.php BINDING STATE` 是冻结草稿准备入口。
网页不得指定这些参数。日常自动准备使用带持久尝试记录的 worker/timer。

NativeConfiguration 编译配置；已核验的原生 `platformapi --validate-bootstrap-admin`
在计划发布和 DDL 前验证管理员输入。root 从单一草稿快照生成密封计划和来源记录，
结束时再次核对草稿摘要。准备不创建账户、不执行迁移、不批准安装，也不写网页状态。

## 审批与执行

`approve-web-configuration.php PREPARED STATE` 重验计划、依赖及冻结草稿来源，
独占发布固定 root 执行绑定和 root:gojet-installer 0640 批准记录。批准记录不包含
配置秘密。重复或中断残留不被覆盖，root 不修改已有网页状态。

`POST /install/configuration/approve` 只接受 `{csrf}`；批准目录取服务端完成锁父目录。
已认证的网页账号核对批准身份、权限和草稿摘要，独占写入本地 approval，转换至
services/ready 并轮换 CSRF。随后仍需独立的安装提交动作。

`POST /install/services` 消耗新的 CSRF，发布受限 mailbox。root worker 在执行前
重新核验计划、依赖和请求，使用真实阶段记录展示进度。客户端成功字段或页面状态
不能授予执行、重试或完成权限。只有实际健康检查后的 root 完成锁能关闭安装流程。

## 重填与恢复

已发布计划的恢复保留原密钥；未发布计划的错误配置须由 root 停用旧尝试并建立
独立新尝试，旧会话、审批和失败记录不得沿用。新准备 timer 核对 root 重填来源，
使用独立 unit 和回执。具体命令见 [installation-preparation.md](installation-preparation.md)。

持久 FPM 配置自动切换及部分发布中断恢复尚未完成。现有回归已经覆盖真实网页
提交、root 准备/审批和执行；明确标注的状态夹具只证明对应页面状态，不能替代
完整原生链路、账户安全、升级/备份恢复或指定目标系统验收。
