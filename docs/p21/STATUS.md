# P21 当前交付状态

更新：2026-10-29。目标环境：Ubuntu 24.04.4 LTS x86_64。
当前分支：`develop/p21-native-release-package`；开发 PR：[#207](https://github.com/Techshrr/GoJet/pull/207)。
P21/G11 尚未关闭，包保持 `installable=false`，尚未交付生产安装包。

本文件只保留当前状态。历史实现、失败与修复记录以 Git 提交及对应 CI 为准，
不把不同源码的成功结果合并成统一候选验收，也不沿用旧候选的正式签署。

## 最新已核实的原生安装链路

源码：`9009d0391239fa84b2a1219f4b883140d064017e`。

| 工作流 | Run | 结果 |
| --- | --- | --- |
| Native Payload Build | [40709927648](https://github.com/Techshrr/GoJet/actions/runs/40709927648) | 成功；payload job 122892102976、执行身份检查 job 122892103415 |
| Native Release Package | [40709927791](https://github.com/Techshrr/GoJet/actions/runs/40709927791) | 成功；job 122892102342 |

原生日志绑定的包 SHA-256 为
`bebc2b579942ce801a57e13fce3e5adcefcca2d87891a991501910061598eccc`，
manifest SHA-256 为 `13ef9d0c2cd94b3b8a2d80c23de391019dc14fea238fdcbadd7759e9642f7684`。
摘要来自 CI 的实际包验证输出，不表示本轮另外下载并复核了整个安装压缩包。
该 runner 为 Ubuntu 24.04.5，不能代替指定的 24.04.4 目标机验收。

已通过真实 CI 的范围：

- 从空数据库、真实网页配置，经 root 准备与独立审批、单次安装提交、真实定时器执行，
  到 31 项迁移、初始管理员、八个非 root 服务及同地址桌面/手机完成反馈和安装器收尾。
  连续首次安装不注入网页状态、批准或完成记录。
- 首次入口、准备/预检/安装调度器及重填配置的部分发布恢复，保留旧停用记录和执行屏障。
  原入口备份、站点交接、服务启动失败、缺失回执和收尾中断均有受验证的显式恢复路径。
- 实际服务停止、必要二进制缺失、数据库/Redis 凭据错误、存储权限错误、ClamAV 不可用
  及禁用版本命令均被拒绝，恢复后重新通过。
- Nginx/PHP-FPM 配置文件损坏而原守护进程仍活跃时，只有对应检查失败；移除故障配置后恢复。
- Redis 的 SET/GET/DEL ACL 拒绝、真实只读副本、成功清理、失败清理后的自动过期与已有键保留。
- MySQL 关闭 autocommit 的真实新连接被拒绝；本次新增实际 `log_bin=1` 且
  `log_bin_trust_function_creators=0` 时拒绝安装，恢复原值后新连接和完整预检重新通过。
  日志确认 `disabled_function_creator_trust_rejected=true`、
  `original_function_creator_trust_restored=true`、`dependencies_restored=true`。

`full_p21_t010_acceptance=false` 仍保留。错误版本矩阵、指定目标机、升级/备份恢复和
最终统一候选尚未全部验收。独立 Installer Web / Service Environment / Migration /
Data Bootstrap 工作流的较早成功记录见 Git 历史，不将其拼接成当前源码的独立 CI 成功。

## 当前回归

以下结果绑定 `adc19ceabff056bf050e15756190b1078209601f`，原生安装链路仍以本文件上方的 `9009d03` 为准。

| 范围 | Run | 结果 |
| --- | --- | --- |
| P10 浏览器 | [40735134311](https://github.com/Techshrr/GoJet/actions/runs/40735134311) | 成功，删除诊断修复已验证 |
| P20 T028 | [40735133756](https://github.com/Techshrr/GoJet/actions/runs/40735133756) | 重试成功 |
| P20 T029 | [40735133358](https://github.com/Techshrr/GoJet/actions/runs/40735133358) | 重试成功 |
| P20 T030 | [40735133538](https://github.com/Techshrr/GoJet/actions/runs/40735133538) | 重试成功 |
| P20 T031 | [40735134018](https://github.com/Techshrr/GoJet/actions/runs/40735134018) | 重试成功 |
| P20 T032 | [40735137521](https://github.com/Techshrr/GoJet/actions/runs/40735137521) | 重试成功 |
| P20 T033 | [40735133315](https://github.com/Techshrr/GoJet/actions/runs/40735133315) | 重试成功 |
| P20 T034 | [40735133496](https://github.com/Techshrr/GoJet/actions/runs/40735133496) | 重试成功 |
| P19 Evidence Coherence | [40735132815](https://github.com/Techshrr/GoJet/actions/runs/40735132815) | 成功 |
| P20 T036 | [40735133625](https://github.com/Techshrr/GoJet/actions/runs/40735133625) | 成功 |
| P20 T035 | [40735134094](https://github.com/Techshrr/GoJet/actions/runs/40735134094) | 重试后自动诊断通过，独立审查准入 FAIL |

以下原始性能失败与重试附件绑定较早的 `9009d03`，保留用于调查波动，不冒充 `adc19ce` 证据。

P19 汇总原始附件 `12836262674` 已下载并复核 ZIP、30 项精确源码 PASS 与全部输入摘要，
ZIP SHA-256 为 `f071122f39c85376496bcbae46ae824b17046e9957bdcef6690aea09a0b31a9a`。
本次生产者重试保留旧/新同名附件，汇总成功绑定当前成功 attempt，验证了附件选择修复。
P19/T036 单项成功不代表整个项目发布通过。

性能失败附件 `12826319298` 已下载并保留，SHA-256
`7a50901deb1074d33ef8b80ee5f4c868af730736f0ccba71c0870b2f85517904`。
原 INP 272ms，三个长任务分别为 52/122/92ms。一次同源码重试后，原始附件
`12837110796` 的 INP 为 72ms、longTasks 为空，SHA-256
`516c898ca04407be112fdf14244203e64089ca4f217f0df82df05da184bd6df7` 已复核。
没有修改阈值、采样、页面代码或性能断言；不能仅凭重试成功声称性能波动根因已修复。
CI 实验室数据不等于生产端性能遥测。

## 删除诊断修复验证与 T035 接续

job `122892173021` 的自动诊断拒绝 `unclassified deletion network failure`。
原始审查附件 `12831509890` 已下载并复核，SHA-256
`0e8f05502efe0ebe724e6e212e85fa9471453a1fe94500b50334d1c9baabb258`。
P10-T017 的实际删除已返回 204，公开资源为 410，数据库 tombstone、版本递增一次和
单条审计均确认；但报告在校验后继续引用可变数组。浏览器上下文关闭时收到的
`requestfailed` 事件被追加到原始数组，已计算的 `confirmed_no_content_completions`
仍为空，产生自相矛盾的 PASS 报告。T035 正确拒绝了这份不一致证据。

修复仅在 P10 浏览器证据生成端：先关闭上下文，再复制、校验并保存完整诊断快照。
保留原始网络事件，只按现有严格规则识别有完整持久化证明的那一笔 DELETE/204；
未知失败、其他请求或关闭失败仍拒绝。没有修改产品行为、T035 校验器或独立审查要求。

本地验证：3 项 P10 分类与异步关闭测试、9 项 T035 Python 测试、12 项 T035 Node
测试通过，P10 冻结契约与 Node 语法检查通过。原失败附件的延迟事件回放也确认新快照
保留事件且分类一致；回放没有改写原始附件，不充当新的真实浏览器 PASS。
修复已在 `adc19ceabff056bf050e15756190b1078209601f` 的真实 P10 浏览器
run [40735134311](https://github.com/Techshrr/GoJet/actions/runs/40735134311)、job `122981522084` 通过。
原始附件 `12837967081` 已下载并核对 ZIP 和精确源码，SHA-256
`83dbede652f242b1bc4bb159cb5028f664846000949871a487c32d7869b489a0`。
T017 的原始报告确实包含那一笔 DELETE/204 的 `net::ERR_ABORTED`，完整保留并正确分类；
`request_failures` 与 `confirmed_no_content_completions` 一致，持久化删除证明齐全，
console/page/HTTP 错误均为空。不是仅在没有延迟事件的运行中碰巧通过。

该源码 P09 汇总曾等待原始生产者超时，导致 T028–T035 连锁失败；同源码 P09 汇总
重试 job `123214748005` 与 T028 job `123215563144` 已通过。
P04 原 run `40735132971` 的 T007 有一次布局偏移超预算；同源码后续 run `40735476653`
成功，原 PR run 重试 job `123214944165` 也成功，失败记录保留。其底层波动原因未关闭。
下游 T029–T034 已按依赖重试通过。T035 job `123215886247` 的证据收集和自动诊断
通过，正式准入仍拒绝 `unsigned/incomplete/failed review`；不再出现删除诊断不一致。
当前审查包 [12866548808](https://github.com/Techshrr/GoJet/actions/runs/40735134094/artifacts/12866548808)
已独立下载并核对 ZIP，SHA-256 为
`b563907ac6f470ec408a9a0c8c4e62749c4ba5209426c8a8c6a6f37a08d3275b`。
72 项自动观察通过；331 份原始文件摘要全部匹配，当前审查截图为 113 张。
证据清单摘要为 `sha256:c93cfae857ab6206acf2c561a75d5275ed5eeee08a7296012dfebfc3cca4540f`。
`formal_p20_t035_claim=false` 与正式 FAIL 保留；上述核验不冒充独立 WCAG 审查。

自动诊断已通过，仍必须满足当前源码对应的独立 WCAG 审查。
Issue [#188](https://github.com/Techshrr/GoJet/issues/188) 的现有 PASS 绑定历史源码
`007b20244081a9d0336c19262e0f095c9d101976`，不能自动覆盖新候选。
较早 `5a6d456` 的 72 项自动观察通过及审查包只证明该源码，不替代当前源码验收。

## 性能归因接续

此前失败只记录了长任务持续时间与总体 INP，缺少脚本/布局归因。本轮在原浏览器测试
附加原生 Long Animation Frames 观察，记录脚本来源、调用位置、强制样式/布局耗时、
渲染阶段和阻塞时长；Event Timing 另外保留 `startTime`、`processingStart`、
`processingEnd`，并记录实际浏览器版本。使用
[Chrome 官方 Long Animation Frames API](https://developer.chrome.com/docs/web-platform/long-animation-frames)。

归因从导航前开始，包含缓冲的初始记录和最终读取时尚未派发的条目；不支持此 API 时
显式记录 `supported=false`，不伪造零耗时或成功结论。仅保留可序列化的指定字段，
不把 Window 对象写进证据。原 long-task、LCP、INP、CLS、网络条件、点击操作和
通过/失败规则保持不变，补充数据不替代正式准入。

2 项独立序列化环境中的观察器测试和 6 项既有 T036 性能准入测试通过；Node 语法、
工作流 YAML/Shell 和 P19 冻结契约检查通过。本地无可执行的 Chrome，新增诊断字段
仍待真实 CI 验证；这只是为定位性能波动补充证据，不宣称已修复性能问题。

## 签署与权限边界

候选执行的 `p21-regression-only` 模式验证固定祖先、P20 原签署父链和九项冻结文件，
保留 `merge_authoritative=false`、`formal_p20_t049_claim=false`、`new_signature_issued=false`。
原 `candidate_phase.verify` 与 T049 正式关闭校验不变。

发布矩阵将历史 P08/P09 closure 列为 HISTORICAL，当前 contract/integration/browser/
coherence 为必须通过的回归。旧签署 SHA 不匹配或历史附件过期的记录保留于 Git 历史，
不能直接替换 SHA/附件 ID 冒充新签署。已保留原成功日志的历史权限绑定也不表示
原 ZIP 仍可下载，更不颁发新签署。正式候选审查和 P21/G11 均未完成。

## 尚未完成的工作

| 范围 | 待完成事项 |
| --- | --- |
| 当前回归 | 删除诊断修复已通过真实 P10 浏览器和 T035 自动复核；验证性能归因并调查波动，完成统一候选回归 |
| 可访问性 | 当前精确源码、截图、源文件及证据包对应的独立审查与 T035 正式准入 |
| 依赖与安全 | 完整缺失/错误版本矩阵，提权、篡改、用户及管理员账户安全回归 |
| 恢复与运维 | 损坏记录/未知文件处理、原生升级、数据库备份恢复与完整恢复手册 |
| 最终验收 | 指定 Ubuntu 24.04.4 目标机、统一源码/包摘要、G11 审查与签署 |
| 交付 | 仅运行文件的生产安装包，以及逐步中文安装、配置、命令和恢复说明 |

冻结 T001–T018 的验收要求保留于 [正式测试计划](../../artifacts/v10/P21/test-plan.json)。
本文件不替代正式结果或改变标准。

## 文档入口

- [网页输入和权限边界](web-configuration-boundary.md)
- [准备、执行与恢复命令](installation-preparation.md)
- [原生网页配置](native-installer-web.md)
- [执行调度](execution-schedule.md)
- [依赖检查](dependency-preflight.md)
- [P09 受保护浏览器回归](p09-protected-browser.md)
- [安全不变量](../security/SECURITY_INVARIANTS.md)

整体完成前不要求用户安装中间预览版本。不得降低安全门槛或用提交数量推算完成率。
生产部署需要单独授权。
