# P21 当前交付状态

更新：2026-11-06。分支：`develop/p21-native-release-package`；开发 PR：[#207](https://github.com/Techshrr/GoJet/pull/207)。
P21/G11 尚未关闭，包保持 `installable=false`，未部署生产。

最新已验证候选源码：`32ea29d766b0ac0790964517ce4e220a1208bf0d`。
[完整原生 CI 42161906247](https://github.com/Techshrr/GoJet/actions/runs/42161906247) 成功，实际新包已独立下载复核。
[新包证据](../../artifacts/v10/P21/host-preflight/validation.json)与[18 项合同覆盖核对](contract-coverage.md)记录本批结果。

## 当前增量：主机支持与容量检查

新增 Ubuntu 24.04 / Linux x86_64 / 64 位 PHP、发布和存储分区空间检查；六种原生负例通过。
九项预检贯穿 root 执行、完成锁与升级健康验证，并在桌面/移动浏览器显示。
打包新增 PHP 引用完整性验证。整批已通过八服务、MySQL 恢复、配置切换、15 个中断恢复阶段、
209 条风险投影和 10 类升级候选拒绝；新包包含 331 个成员，独立生成候选索引与 CI 完全一致。
[专用 CI 42164219962](https://github.com/Techshrr/GoJet/actions/runs/42164219962) 在这份新包上通过真实 PHP 8.1 CLI/FPM 和 MariaDB 10.11 拒绝验证；其他八项健康、原依赖恢复、安装权威不变。
18 项工程覆盖核对已完成；G11 正式责任审查与批准仍待完成。原版本报告保留原摘要。
详见[主机预检](host-preflight.md)。

以下为前序批次的独立记录，所列源码及摘要不冒充当前候选。

## 当前增量：真实依赖版本拒绝证据

测试源码 `a49023029aedcc2c915dbdf8d0f54d34d400c79b`，候选源码及归档保持上文身份。
[专用 CI 42075233647](https://github.com/Techshrr/GoJet/actions/runs/42075233647) 已通过：
真实 PHP 8.1 CLI、PHP 8.1 FPM、MariaDB 10.11 均被拒绝；对应场景的其他七项依赖保持健康，
恢复原环境后重新通过，不产生完成锁、迁移或安装权威变更。
报告及下载附件摘要已独立核验，详见[版本拒绝验证](dependency-versions.md)。
这项此前缺失的具体证据已补齐；不重建候选，也不重复运行完整升级恢复链路。

## 当前增量：发布手册与候选索引

四份独立操作手册位于 docs/release，构建时纳入包内 runbooks，进入 manifest、SBOM 和校验和。
构建检查相对链接、实际包内命令文件及 shell 语法；原生 PHP 完整性测试另验证篡改手册被拒绝。
最终 candidate.json 在同一原生 CI 的全部检查结束后生成，绑定精确归档、manifest、SBOM、
九个二进制、迁移、手册以及八份原始报告，不修改已构建归档。
缺失报告、异源提交、不同归档、失败断言或不完整恢复覆盖均拒绝生成成功索引。
[Native Payload 42001339781](https://github.com/Techshrr/GoJet/actions/runs/42001339781) 第一次执行成功：
两项 job、14 项 Python 检查、Go 回归、前端构建、连续安装、八服务、MySQL 恢复、209 链接风险重建、
10 类不兼容候选拒绝及 15 个中断恢复阶段通过。最终 candidate.json 在本次 CI 内生成。
实际下载 330 成员归档并独立重建索引成功；8 份原始报告、manifest、SBOM 与四份手册摘要均相符。
归档 SHA-256：`e8abdc99ea137f41ba0e5df8a3ac0bf7fe38f65acfc9de24b0d3fd51f013e8d9`。
G11 证据导航已建立于 artifacts/v10/gates/G11/native-package；索引保留待审查状态。
原始报告的范围限制未改写成全项验收通过，亦未借用上一轮包摘要。

## 本次交付：完整升级与恢复

整批实现提交：`35e0de359f496260444482b8d4e5cc49aee9838d`。
基础流程验证源码：`d8bc11a38a82de0ca4c75cdc80830aa4e27385b2`。此前测试脚本的导入遮蔽和登录关联 ID 已修正；
本次同时修正 Nginx 重载后普通访问就绪的确认。

已实现的连续流程：验证当前安装和候选包、保留原配置及密钥、停止八个服务、维护隔离、
数据库与文件备份、真实恢复演练、迁移、原子配置切换、重启八服务、完整健康验证和开放访问。
显式恢复涵盖部分 DDL、文件恢复、旧配置切回以及数据库丢失；提交后的重试保留新增业务数据。
命令、恢复阶段和支持边界见 [原生升级与恢复](native-upgrade.md)。

| 验证范围 | 当前结果与证据 |
| --- | --- |
| MySQL 恢复组件 | 已通过本地及 [Migration CI 41831361934](https://github.com/Techshrr/GoJet/actions/runs/41831361934)，与当前验证源码一致 |
| 完整数据库结构恢复 | 2026-11-04 本地真实 MySQL：31 条迁移、104 张表、4 个触发器；恢复演练、候选表清除、数据库丢失恢复和篡改拒绝通过 |
| 文件恢复及维护边界 | 文件内容/权限恢复、配置指针切换、Nginx 维护令牌、Redis 定向失效及无令牌 TLS 就绪探测均通过 |
| 首次安装→升级→恢复 | [Native Payload 41831361993](https://github.com/Techshrr/GoJet/actions/runs/41831361993) **成功**；实际八个 systemd 服务及 15 个中断/恢复阶段全部通过 |

`751a112` 在升级前登录检查失败，原因是测试遗漏接口必需的关联 ID，已修正。
`7708c4f` 已进入真实部分 DDL 失败后的恢复，数据库、文件和八服务私有健康检查通过；
随后普通访问断言失败，暴露了 Nginx 重载命令返回与工作进程就绪之间的空隙。
当前代码在升级、恢复及提交后重试三条路径中，均等待无维护令牌的完整健康检查稳定通过，
再记录完成并清理屏障。本地真实 Nginx/TLS 及上述完整原生 CI 均已通过。

本次整体验证还确认：部分 DDL 失败和候选包损坏后恢复、原库被删除后恢复、
提交后新增数据保留、原管理员会话和登录审计保留、原安装完成锁不变。
同一源码的 Migration、Service Environment、Data Bootstrap、Installer Web、Release Package
五项相关工作流均成功；具体 run/job 与原始安装报告见 [验证记录](../../artifacts/v10/P21/upgrade/validation.json)。

CI 实际验证的安装包 SHA-256：`677c91382130701a9247a7ce831ffb0d418b593d84182bceb19b1ea92e35ee22`。
manifest SHA-256：`50fe20e28adc771fbc7426ea094b901c1f68af50cb73e2f1cc3ef4ecda239fa4`。
这些摘要来自 CI 原始日志；本次未另外下载整个压缩包作独立复核。

## 剩余交付边界

1. P21：操作手册已随包交付，候选包及证据已固定；18 项工程覆盖核对及当前候选验证已完成，剩余 G11 责任审查与批准；T010 不再列为代码或证据缺口。
2. P22/G12：指定干净目标系统安装、八服务重启及整机重启恢复。
3. P22/G13：真实业务外部依赖、生产升级回滚和 Release Owner 验收；执行生产操作须另行授权。

升级测试的候选夹具沿用同批编译二进制，增加迁移和静态内容，不能证明任意历史版本均兼容。
总计划 P21/G11 没有“先完成两次正式发布”的要求；此前把这项写作必需门槛不准确。
干净主机、重启和真实业务生产验证按总计划属于 P22/G12/G13，不作为 P21 继续修代码的理由。
以上范围依据 [总计划 P21/P22 与 G11–G13](../../specifications/GoJet_V10_MASTER_PLAN_OPTIMIZED.md)，
不改变各 Gate 的证据及负责角色审查要求。
本次没有重新签署历史候选，没有以自动检查冒充独立审查。

## 后续增量：完整风险投影恢复

源码 `b0794aef620ad14889d29f0e748d2deb5e7324eb` 修复后台仅刷新前 100 条的缺陷，
并把显式分页重建接到升级和恢复中：八服务停止后从 MySQL 重建，完成后才切换/启动/验证。
恢复先停止可能遗留的重建临时服务，避免它与数据库还原并发；过期/未知/目标变更继续拒绝放行。

真实 MySQL、Redis 的 209 链接本地测试已通过，包含人工覆盖、绝对有效期、失败边界、
不消费任务和不改动无关数据；相关 Go 定向测试、PHP 语法与 Python 检查通过。
[Native Payload 41895162234](https://github.com/Techshrr/GoJet/actions/runs/41895162234)
已成功验证带此业务数据的完整八服务升级恢复、15 个中断/恢复阶段和残留重建服务停止。
同一源码的 Migration、Service Environment、Installer Web、Initial Administrator、Data Bootstrap、
Release Package 六项工作流均成功。完整原生任务包含 `go test ./...`、四个前端构建和实际浏览器安装链路。

新包 SHA-256：`4472fcc82a6829f98112aeeec0b99f1ae46d895ead15cf3c66026f39eff52921`；
manifest SHA-256：`7a62c6110b78bb96f9853ec45e247422e761610586b3acc957f8899637ac6741`。
[新增验证记录](../../artifacts/v10/P21/upgrade/risk-projection-validation.json) 保存原始报告、run/job、
包和 artifact 摘要；这些摘要来自 CI，未另外下载整个压缩包独立复核。此前 `d8bc11a` 的证据保持独立。

## 后续增量：升级前兼容性检查

后续源码 `683bb5f5354600f529f883aaaf342b0064ebacdf` 已实现准备阶段的版本顺序、
九个程序身份及双方恢复能力检查。本地矩阵以及 10 类不兼容候选的原生拒绝矩阵均通过；
[CI 41958283206](https://github.com/Techshrr/GoJet/actions/runs/41958283206) 完整成功，
同时通过正常升级、209 链接风险投影、八服务、MySQL 恢复和 15 个中断/恢复阶段。
同一源码的六项相关 P21 工作流全部成功。本批整体验证一次完成，没有失败后重跑。
支持范围见 [升级兼容性](upgrade-compatibility.md)，原始报告与运行号见
[兼容性验证记录](../../artifacts/v10/P21/upgrade/compatibility-validation.json)。

新包 SHA-256：`c65efea6f5042bfb0332c897e9bc25066c4477df612f17ff50c750ec6cb673a3`；
manifest SHA-256：`43acbd4bdc33c8c7da65930bdd9ddf02ecf7da39ef578773de5a5d2b2b6436b8`。
摘要由 CI 报告，未另行下载压缩包作独立复核。

## 历史记录与文档入口

[2026-10-29 状态快照](STATUS-2026-10-29.md) 原样保留既往提交、性能波动、T035 和历史签署记录。
这些事项不占用当前升级恢复主线；只有确实阻断当前功能的缺陷进入本次修复。

- [准备、执行与恢复命令](installation-preparation.md)
- [原生升级与恢复](native-upgrade.md)
- [网页输入和权限边界](web-configuration-boundary.md)
- [依赖检查](dependency-preflight.md)
- [正式测试计划](../../artifacts/v10/P21/test-plan.json)

生产部署需要单独授权。
