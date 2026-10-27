# P21 当前交付状态

更新：2026-10-28。目标环境：Ubuntu 24.04.4 LTS x86_64。
当前分支：`develop/p21-native-release-package`；开发 PR：[#207](https://github.com/Techshrr/GoJet/pull/207)。
P21/G11 尚未关闭，包保持 `installable=false`，尚未交付生产安装包。

本文件只保留当前状态。历史实现、失败与修复记录以 Git 提交及对应 CI 为准，
不在工作文档中继续追加已失效的“下一步”或“待 CI”检查点。

## 最新已核实的 CI

原生安装链路最新已核实源码：`407d8d163645b8ee33fbfe09d61d8ac4d9da3bc4`。
下表 Payload 与 Release Package 为该源码；其余独立工作流保留此前 `5b921a1` 的验证记录，
不把不同源码的结果合并成统一候选验收。

| 工作流 | Run | 结果 |
| --- | --- | --- |
| Native Payload Build | [40476527019](https://github.com/Techshrr/GoJet/actions/runs/40476527019) | 成功 |
| Native Release Package | [40476526982](https://github.com/Techshrr/GoJet/actions/runs/40476526982) | 成功 |
| Native Installer Web | [40325314791](https://github.com/Techshrr/GoJet/actions/runs/40325314791) | 成功 |
| Native Service Environment | [40325314744](https://github.com/Techshrr/GoJet/actions/runs/40325314744) | 成功 |
| Native Migration Execution | [40325314725](https://github.com/Techshrr/GoJet/actions/runs/40325314725) | 成功 |
| Native Data Bootstrap | [40325314869](https://github.com/Techshrr/GoJet/actions/runs/40325314869) | 成功 |

完整首次安装连续链路、首次入口部分发布恢复、准备/预检/安装调度器发布恢复及
既有故障/安全边界回归均已通过原生 CI。连续链路从空数据库、真实网页配置，经
root 准备与独立审批、单次安装提交、真实定时器执行，到同地址桌面/手机完成反馈及
安装器收尾，不注入网页状态、批准或完成记录。
重填配置替换目录的发布恢复也已通过上述 Payload CI（job 122096012130）；
日志确认原有停用记录及执行屏障保留。新增 Redis 读写/删除验证已通过真实 ACL
逐项禁止 SET、GET、DEL，以及真实只读副本故障回归；已有键保留、成功清理、
删除受限后自动过期、恢复后重新通过均已核实。完整 T010 依赖矩阵仍未关闭。
原生日志绑定的包 SHA-256 为
`ff118b9b016eecf5d6ea6115777a951ea29f0e44c7e6b24c6786c11cc2a89f83`，
manifest SHA-256 为 `6a069babacad39f6fc045e39277f7e99a7ed96b582c3206b343ba8bceae3e34d`。
此处摘要来自该 CI 的原生验证输出，不表示本轮另外下载并复核了整个压缩包。
这些结果不是全部项目验收通过，也不替代指定 Ubuntu 24.04.4 目标机验证；
本轮原生 runner 为 24.04.5。

## 当前实现

- 网页配置表单、认证/CSRF、字段和体积限制、私有草稿冻结及 root 来源校验。
- root 准备密封计划、生成独立服务密钥、准备定时器及禁止中断后自动重做。
- 独立 root 审批、手机浏览器确认、独立安装提交；审批不自动执行安装。
- 真实浏览器请求接入 root timer；31 项迁移、初始管理员、八个非 root 服务、
  站点/TLS、实际健康检查和 root 完成锁。桌面/手机可观察真实执行阶段。
- 服务和站点失败/中断恢复；完整计划已发布而成功回执缺失时的显式恢复。
- 无计划的错误配置可由 root 停用旧尝试并创建独立新尝试。旧记录保留、旧会话
  不继承；新尝试经真实网页提交，由独立准备 timer 处理，不覆盖旧 unit/回执。

原生回归包含真实服务故障及恢复，不等同于所有部署场景都能无干预安装。
重填入口的持久 FPM 切换、同地址登录与提交、服务重启后的持久性及缺失回执恢复
已经通过原生回归，首次入口部署也已通过。新增 root 授权的同地址站点交接，保留
原入口配置供失败/中断恢复，交接后正式站点拒绝 `/install`；对应原生 CI 已通过。
新增与批准计划绑定的只读完成回执、桌面/手机完成反馈，以及完成后显式 root 停用
已登记定时器和安装器 FPM 池；回执缺失及收尾中断可按相同身份显式续办，原生 CI 已通过。

## 尚未完成的工作

| 范围 | 待完成事项 |
| --- | --- |
| 当前回归与签署 | 419ca40 的 P20 T001–T008 和 T036 已通过；补齐 T028 所需 P15 触发链，修复 P18 连续浏览器会话；正式签署关闭仍单独保留 |
| 初装接续 | 连续首次安装原生链路已通过；指定目标机与最终统一候选验收仍保留 |
| 恢复 | 发布恢复已通过；损坏记录/未知文件的人工处理与完整恢复手册 |
| 依赖与安全 | 完整缺失/错误版本矩阵，提权、篡改、用户及管理员账户安全回归；保留已发现缺陷 |
| 运维 | 原生升级、数据库备份恢复、兼容边界和完整恢复手册 |
| 最终验收 | 精确目标系统、统一候选源码/包摘要、全部受影响回归、G11 审查与签署 |
| 交付 | 仅运行文件的生产安装包，以及逐步中文安装、配置、命令和恢复说明 |

冻结条目 T001–T018 的验收要求保留于
[正式测试计划](../../artifacts/v10/P21/test-plan.json)。本文件不替代正式结果或改变标准。
P09 受保护浏览器及证据衔接已在源码
`025160f88b9421e250be546e25a8d795d18b3c30` 上验证。本次状态更新仅记录该源码的
结果，不把文档提交或之后的候选自动视为已验收。

| 工作流 | Run | 结果 |
| --- | --- | --- |
| P09 Files Contract | [40436097392](https://github.com/Techshrr/GoJet/actions/runs/40436097392) | 成功 |
| P09 Real Files and ClamAV Integration（T001–T018） | [40436095497](https://github.com/Techshrr/GoJet/actions/runs/40436095497) | 成功 |
| P09 Files Health and Installer Preflight（T019–T020） | [40436097544](https://github.com/Techshrr/GoJet/actions/runs/40436097544) | 成功 |
| P09 Workspace Files Browser（T021–T025） | [40436097055](https://github.com/Techshrr/GoJet/actions/runs/40436097055) | 成功 |
| P09 Evidence Coherence（T026 及 P20 文件安全部分） | [40438004082](https://github.com/Techshrr/GoJet/actions/runs/40438004082) | 成功 |
| P09 Evidence Coherence（原 PR run 重试） | [40436095637](https://github.com/Techshrr/GoJet/actions/runs/40436095637) | 成功 |
| P05 Closure（重试） | [40436095514](https://github.com/Techshrr/GoJet/actions/runs/40436095514) | 成功 |
| P06 Closure（重试） | [40436095600](https://github.com/Techshrr/GoJet/actions/runs/40436095600) | 成功 |
| P07 Closure（重试） | [40436095610](https://github.com/Techshrr/GoJet/actions/runs/40436095610) | 成功 |

汇总附件 `12708563113` 已独立下载，ZIP 完整性及 SHA-256
`2bca2223631daaa534d69a7e06988ba2f6a507a312f7e12dc6119df21e0f35cd` 已复核。
26 项结果均为 PASS 且绑定上述源码；索引中的 25 项输入及 12 张截图摘要均与原文件
匹配。该汇总采用同源码后续 Integration run `40441546345`；其余生产者见附件清单。
P20 文件安全附件 `12708752801` 的 ZIP 与 SHA-256
`36af10a381356fb0d5ec772acbc7c58c5ad76aacb511ef3c453b4202c43867a9` 也已验证。
其 `formal_p20_t028_claim=false`、`admin_governance_bound=false` 和
`next_case_unlocked=false` 均保留，不能作为整个 P20 T028 通过证明。

T024 覆盖实际 TLS 登录、认证/阶段拒绝、真实存储/ClamAV 结果、故障与恢复、过期/
缺失状态清除、无安装 POST、无阶段推进和零意外浏览器错误。起始阶段及完成标记
仍是明确夹具，不能作为完整安装验收。接口映射和边界见
[P09 受保护浏览器回归](p09-protected-browser.md)。共享校验器的 5 组负向测试也已在
上述一致性 CI 中通过，旧公开入口结构不能替代当前受保护证据。

本次按依赖顺序重试，消除了该源码的 P05–P07 及 P09 一致性检查的排队超时失败；
没有修改等待窗口、删减前置要求或声称已解决所有未来候选的 CI 排队问题。
重试进一步定位到两项独立阻塞：

- P08 Closure [40436095696](https://github.com/Techshrr/GoJet/actions/runs/40436095696)，
  job `122064703375`，受影响工作流及证据下载通过；T016 拒绝历史签署的 pre-sign SHA
  `7b5c0f095c2a5852cd55697cbb08bdad1dc65263` 与当前父提交 `485487a` 不一致。
- P09 Closure [40436097385](https://github.com/Techshrr/GoJet/actions/runs/40436097385)，
  job `122064710826`，26 项前置工作流及当前证据下载通过；绑定历史 P08 附件
  `9477523498` 时 HTTP 404，T027 尚未执行。原历史 run `32579404081` 仍为成功，
  但附件列表为空。原 job `97047721945` 日志保留 signed PASS 和上传摘要
  `aede44ebad79a78adc872f33a874f8aea1ae24bb430b0e06eaa2da32739c0a2d`，没有完整
  closure/T016 JSON，不能还原原 ZIP 或假称附件仍可下载。

下一步需要恢复可独立验证的历史原始证据，或建立有明确审查的新候选签署/证据链。
P09 自身的历史签署也绑定旧 pre-sign SHA，恢复附件后仍需处理该校验；不能仅换一个
附件 ID 或改写签署 SHA。冻结计划、原历史签署和严格关闭校验保持不变，P08/P09
当前候选的签署关闭以及 P21/G11 均未完成。

## 当前开发接续

发布矩阵 `scripts/p20/release_matrix.py` 已把 P08/P09 closure 列为 HISTORICAL，
把当前 P08/P09 contract/integration/browser/coherence 列为必须通过的回归。
上述历史关闭失败仍需留存，但不能误称它们属于当前发布矩阵的必需生产者。
本轮没有改变此分类或放宽任何签署校验。

此前 P20 候选入口把后续开发源码当成原签署的直接子提交，导致候选检查停止。
该入口阻塞已在 `419ca40970cbe8ff4122de6fdd57304c54af0208` 的回归执行模式下解除；
原历史 P20 签署仍不能自动作为 P21 新提交的签署，最终候选审查与冻结文件分别保留。

本轮继续 T010 的独立原生故障矩阵：新增 Nginx/PHP-FPM 配置损坏但守护进程仍活跃、
MySQL 新连接继承关闭 autocommit 的场景，要求相关单项失败、整体不 ready、其余依赖
正常；恢复现场后重新检查通过。上述故障场景已在 `407d8d1` 的 Payload run `40476527019`、job `122096012130`
通过，后续连续首次安装、31 项迁移、八服务启动、故障恢复及安装器收尾也通过。
没有改动生产预检逻辑，完整错误版本矩阵和正式 T010 验收仍开放。

P20 候选执行入口现增加明确的 `p21-regression-only` 模式，接入 Candidate Freeze、
T036 和 T037 的候选检查。该模式仅适用于固定 P21 契约祖先的后续源码，验证 P20
原签署的直接父子关系、集成提交及 P21 契约父链，逐字节绑定九项冻结文件，拒绝
工作区修改及符号链接。原 `candidate_phase.verify` 和 T049 关闭校验保持不变。
回归附件额外保存 `candidate-authority/execution-scope.json`，明确
`merge_authoritative=false`、`formal_p20_t049_claim=false`、`new_signature_issued=false`，
不宣称 P21/P22 完成。候选回归成功不替代新的正式签署或最终发布验收。

24 项发布校验测试通过，包括真实临时 Git 历史中的冻结文件篡改、无关祖先、错误
checkout、权限声明提升、签署入口误用等负向测试；本地实际源码的回归入口也已验证。
候选与性能 CI 已验证，详见下表；可观测性及完整下游验收未在此宣称通过。

| 419ca40 工作流 | Run | 已核实结果 |
| --- | --- | --- |
| P20 Candidate and Traceability Freeze | [40503849056](https://github.com/Techshrr/GoJet/actions/runs/40503849056) | T001–T008、全仓 Go 测试、前端类型检查与双次构建通过 |
| P20-T036 Performance and Bundle Isolation | [40503849528](https://github.com/Techshrr/GoJet/actions/runs/40503849528) | 候选/契约前置及正式 T036 通过 |

原始候选附件 `12740452712` 已下载并核对 ZIP 完整性、八项 PASS 及精确源码，SHA-256
`f6fc98bb21e3958b956f82c513a1429d0250e3a6795004cbda35c07c1c037b86`。
`execution-scope.json` 保留 `p21-regression-only` 和非签署/非合并权限声明。
T036 原始附件 `12741196886` 的 ZIP 完整性和 SHA-256
`783324912eb66b7d8b65dae283eff7fdf159411597fd3c38e4c14fd31efe3253` 已复核。
正式 T036 为 PASS，范围仅限同源码 P04/P07/P19 的性能与 bundle 隔离，
`next_case_unlocked=false`；CI 实验室结果不等同于生产端性能遥测。

当前下游阻塞与修复：

- T028 run `40503846835`、job `122188017331` 等待
  `p15-authentication-oauth-account.yml` 超时。该源码的七项 P15 原始生产者均成功，
  但契约/汇总工作流的路径过滤未触发。现使整条八工作流 P15 证据链覆盖 T028 的
  PR 路径集合，保留原有触发路径、只读权限及全部证据校验。
- P18 Docs Quality run `40503873670`、job `122188116135` 在 T019 通过后第二次
  启动 Orca 时进程退出，T020–T024 未完成。现让 T019–T021 在同一个真实桌面/Orca
  会话内顺序运行，每项仍独立打开浏览器；失败必须停止后续项。T022–T024 原样保留，
  不跳过真实辅助技术验证或改写浏览器断言。此处没有断言 Orca 退出的更底层原因。

路径覆盖、YAML/Shell 语法以及中间项失败时停止后续项的本地检查已通过。
新增工作流配置需由新提交 CI 验证；T028–T035 和 P21/G11 整体验收仍未关闭。

## 文档入口

- [网页输入和权限边界](web-configuration-boundary.md)
- [准备、执行与恢复命令](installation-preparation.md)
- [原生网页配置](native-installer-web.md)
- [执行调度](execution-schedule.md)
- [依赖检查](dependency-preflight.md)
- [安全不变量](../security/SECURITY_INVARIANTS.md)

整体完成前不要求用户安装中间预览版本。不得降低安全门槛或用提交数量推算完成率。
生产部署需要单独授权。
