# G11 审查与 P22 环境交接

更新：2026-11-06。此文是已验证 P21 候选的审查及环境输入说明，不是 P22 已执行记录，也不冻结 P22 测试合同。
总计划 §11 允许在前置节点期间评审接口合同；P22 实施仍须满足 P21/G11 退出条件。

## 当前审查对象

| 项目 | 固定值或入口 |
| --- | --- |
| 候选源码 | 32ea29d766b0ac0790964517ce4e220a1208bf0d |
| 归档 SHA-256 | 7e7343c34571f69038e090f20775e1a941b5a33b80825e755813dd08500f1542 |
| manifest SHA-256 | fce4900d7f900de2ad7655d6f7411e0771f7a1d4417668aad7538974db5310ba |
| 文件名 | gojet-10.0.0-rc.1-linux-amd64-payload.tar.gz |
| 原生 CI | [42161906247](https://github.com/Techshrr/GoJet/actions/runs/42161906247) |
| 同包版本拒绝 CI | [42164219962](https://github.com/Techshrr/GoJet/actions/runs/42164219962) |
| 归档附件 | [13477846868](https://github.com/Techshrr/GoJet/actions/runs/42161906247/artifacts/13477846868)，保留至 2026-12-06，尚非永久正式 Release |
| 工程覆盖核对 | [18 项核对](contract-coverage.md) |
| G11 决定 | [decision.json](../../artifacts/v10/gates/G11/native-package/decision.json)，当前 PENDING |
| 完整证据入口 | [evidence-index.json](../../artifacts/v10/gates/G11/native-package/evidence-index.json) |

G11 的负责角色是 Release Engineer。正式决定需要可归属的审核人、批准时间、上述源码和归档摘要、结论及任何未解决问题。
准备好供审查不等于已批准；项目负责人可确认由谁承担该角色。当前 PR #207 尚无提交的 review，不能填入不存在的审核人或批准时间。
G1 由 Platform Lead 负责；G12 为 Platform Lead + QA；G13 为 Release Owner + Security Reviewer + Platform Lead。
不把 G13 的角色组合添加为 G11 的额外前置条件。

## 已核实的环境边界

仓库未提供 P22 主机清单、自托管 runner 配置或部署目标变量；当前工作区未发现已配置的 systemd PID 1 或本地虚拟机运行环境。
已有 Ubuntu 24.04.5 GitHub CI 的隔离安装记录不能作为外部干净目标机和整机重启证明。
仓库正式支持矩阵为 Ubuntu 24.04 LTS / Linux amd64；先前文档提及的特定 24.04.4 验收尚未绑定到实际主机。
接入时记录真实发行版本、镜像、架构和主机身份；不得将 24.04.5 结果写为 24.04.4。

## 开始 G12 所需的实际输入

| 输入 | 需要确认的内容 |
| --- | --- |
| 测试机接入 | 可用的 SSH 连接或自托管 runner；能在重启后重新接入 |
| 主机用途 | 专供本次验收、没有业务数据或旧 GoJet DB/config/state；允许安装依赖、启停服务和整机重启 |
| 主机身份 | Ubuntu 24.04 LTS、amd64、真实 systemd；记录初始镜像和存储布局 |
| HTTPS 入口 | 经确认的测试 origin、DNS/端口、匹配的证书；依照包内输入结构准备 |
| 私有配置 | 独立 MySQL/Redis、存储分区、初始管理员和配置所需外部服务输入，通过主机私有文件提供 |
| 责任记录 | 当前候选的 G11 决定及 G12 执行负责人；不沿用 CI 数据库、口令、配置或完成锁 |

可先提供测试机接入方式与 G11 审核人，其余主机配置可在只读接入检查后整理为具体执行记录。
生产接入、生产变更、外部真实支付及不可逆操作均不包含在本次交接授权内。

## G12 连续验收顺序与输出

| 阶段 | 执行范围 | 成功证据 |
| --- | --- | --- |
| 1. 干净主机与归档 | 保存主机身份及 DB/config/state 初始状态，下载上述原归档，验证 SHA-256/manifest | 原始主机清单、归档摘要、无复用状态记录 |
| 2. 依赖拒绝 | 按原合同逐一验证强制依赖缺失、错误、不健康以及权限/容量失败 | 失败结果、没有迁移或完成锁、恢复后的九项健康 |
| 3. 一次完整安装 | 执行随包手册，从 HTTPS 浏览器配置经 root 批准到八服务与完成锁 | 浏览器记录、root 阶段记录、迁移及服务状态、锁与入口退役 |
| 4. restart/reboot | 八服务 restart，再整机重启并重新连接同一主机 | 不同 boot ID、八 unit enabled/active、新 PID、全部健康和 P0 smoke |
| 5. 依赖与数据恢复 | Nginx test/reload/revert、MySQL 备份还原、Redis flush/reconnect/风险恢复、ClamAV clean/EICAR | 还原内容核对、恢复时间线、扫描结果、恢复后健康 |
| 6. 升级与回滚 | 使用经核验的兼容候选执行失败恢复及数据库必要回滚 | 候选身份、备份演练、切换/恢复时间线、回滚后八服务及业务健康 |
| 7. G12 复核 | 汇总同一主机和候选的原始证据，核对无手工修补 | Platform Lead + QA 的决定、审核人及批准时间 |

未来正式执行记录归入总计划指定的 artifacts/v10/P22 和 G12/G13 目录；本次不提前创建 PASS 记录。
G13 另需 owner-controlled 环境中的真实邮件、OAuth、Turnstile、支付、域名/SEO 和升级回滚验证，按实际启用能力制定并执行，不以测试替身冒充。

## 随包手册勘误

当前不可变包 runbooks/install.md 第 3 节仍写“等待浏览器显示全部八项当前预检通过”。本候选实际有九项：
package、host、systemd、nginx、php、mysql、redis、storage、clamav；必须全部通过。
源码的严格九字段校验、浏览器显示、六个主机负例和完整安装链路均已验证。
这里是数量文字勘误，不改变原归档、摘要、可执行行为或通过条件；操作时以全部九项通过为准。
下一次有必要重新构建发布包时同步修正包内数量文字，本次不为该文字重建已验证候选。
