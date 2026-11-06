# P21 候选交付与证据复核

## 当前候选（2026-11-06）

源码 `32ea29d766b0ac0790964517ce4e220a1208bf0d`；归档包含 331 个成员，新增原生主机支持/容量检查与包内 PHP 引用完整性校验。
归档 SHA-256：`7e7343c34571f69038e090f20775e1a941b5a33b80825e755813dd08500f1542`。

[完整原生验证](../../artifacts/v10/P21/host-preflight/validation.json)和[真实版本拒绝验证](../../artifacts/v10/P21/host-preflight/dependency-versions/validation.json)均通过，实际附件已下载核验，candidate.json 独立重建一致。
[18 项工程覆盖核对](contract-coverage.md)已完成；G11 正式批准仍待完成，未部署生产。当前 CI 归档保留至 2026-12-06，未发布永久正式 Release。

当前候选的 G11 审查、目标机输入及随包手册“八项/九项”文字勘误见[交接说明](p22-handoff.md)。

## 前序候选记录：270e845

本批实现源码为 `270e8459f2862525fd8e75db7a375b4346da4dcf`。
发布手册、归档及证据索引作为一个批次验证，不改变 Go/PHP 业务逻辑、数据库迁移或前端交互。

## 操作手册随包交付

[手册源目录](../release/README.md)的四份文档由显式白名单复制到包内 runbooks，
纳入 manifest、SBOM 和 SHA256SUMS。包构建检查文档间链接、实际交付文件引用和 shell 语法；
独立的目标机 PHP 校验器拒绝手册字节篡改。静态校验不会执行运维命令，真实安装、升级和
恢复由原生测试分别验证，二者不能相互替代。

手册说明了配置、凭据、授权、备份、健康验证、阶段恢复和支持范围。容量边界以代码为准：
文件快照的 100000 条上限包含目录和根条目；不能把它当成可容纳 100000 个文件再外加任意目录。
业务域名、数据库位置、存储路径及密钥的任意在线更换不属于版本升级器的通用能力。

## 不可变候选索引

CI 在全部原生测试成功后运行 scripts/p21/finalize_candidate.py，将 candidate.json
写在归档旁，不改动归档字节。索引包含：

- 源码、版本、GitHub run/attempt/job 身份和归档摘要；
- manifest、SBOM、完整 SHA256SUMS 清单的摘要；完整成员列表仍在 manifest 中；
- 四份手册的摘要、大小与模式；
- 八份原始报告的摘要及各自绑定范围。

生成器核对八服务及助手、四个静态应用、八个 unit、迁移与 SBOM 文件摘要，
以及安装、风险重建、兼容性拒绝和十五阶段中断恢复证据。
缺少报告、不同源码或归档、失败断言、不完整恢复覆盖均拒绝生成成功索引。
若报告本身没有 source 字段，索引明确标记只绑定同一个 CI job，不伪称报告独立证明源码身份。

索引既不是签名也不是 G11 批准。归档、报告和 candidate.json 必须一起保存；
从独立批准记录取得归档及 manifest 摘要后复核，不把同一未验证下载内部的摘要当作信任根。
构建器的 `not_evaluated` 表示该组件不负责判断的范围，不表示整个产品尚未实现那些功能。

## 本次检查与审查边界

本地运行 Python 测试集合：13 项通过、1 项因缺少 PATH 中的 php8.3 跳过；
发布契约、Python 编译与 diff 检查通过。PHP/Go 生产代码未修改。
[CI 42001339781](https://github.com/Techshrr/GoJet/actions/runs/42001339781) 第一次执行成功，
包含 14 项 Python 检查（CI 无跳过）、Go 回归、前端构建、浏览器、依赖、安装和升级恢复。
原生构建工作流 42001339303 也成功。

下载附件后独立验证 ZIP 和内层归档摘要，检查 330 个成员、833 个 SBOM 组件、四份手册
和八份原始报告，重新生成 candidate.json 与 CI 原件完全一致。
[原始候选索引](../../artifacts/v10/P21/candidate/candidate.json)、
[下载复核](../../artifacts/v10/P21/candidate/download-verification.json)和
[本批验证记录](../../artifacts/v10/P21/candidate/validation.json)已保留。
归档 SHA-256 为 `e8abdc99ea137f41ba0e5df8a3ac0bf7fe38f65acfc9de24b0d3fd51f013e8d9`。
附件 ID 13404611783，GitHub 当前保留至 2026-12-05；尚未作为永久正式 Release 发布。

CI runner 实际为 Ubuntu 24.04.5，不能冒充指定 Ubuntu 24.04.4 目标机验收。
原始依赖报告保留其组件范围标记 full_p21_t010_acceptance=false；后续[专用版本拒绝验证](dependency-versions.md)
已在同一不可变包上补齐真实 PHP CLI/FPM 与数据库不兼容输入证据，原报告不反写；
[十八项审查导航](../../artifacts/v10/gates/G11/native-package/review-checklist.json)逐项说明证据范围，
没有把自动化检查成功改写成十八项全部验收通过。

G11 按总计划由 Release Engineer 负责；G1 原生架构复核由 Platform Lead 负责。
QA 属于 P22/G12 的共同负责角色，Security Reviewer 属于 G13；不将 G13 的角色签署
额外添加为 G11 的四方前置审批。技术证据可以先备齐，不能由自动化结果冒充负责人签署。
本次不重新签署历史候选，不修改冻结总计划或测试合同。

P22 继续承担目标洁净主机、整机重启、真实外部依赖和生产恢复验收。
原生 CI 的同编译二进制升级夹具不证明任意历史版本兼容；当前包维持 installable=false。
