# P21 当前交付状态

更新：2026-10-27。目标环境：Ubuntu 24.04.4 LTS x86_64。
当前分支：`develop/p21-native-release-package`；开发 PR：[#207](https://github.com/Techshrr/GoJet/pull/207)。
P21/G11 尚未关闭，包保持 `installable=false`，尚未交付生产安装包。

本文件只保留当前状态。历史实现、失败与修复记录以 Git 提交及对应 CI 为准，
不在工作文档中继续追加已失效的“下一步”或“待 CI”检查点。

## 最新已核实的 CI

原生安装链路已核实源码：`5b921a1509d5cf169bd6c66417e28f97eae527ca`。

| 工作流 | Run | 结果 |
| --- | --- | --- |
| Native Payload Build | [40325314726](https://github.com/Techshrr/GoJet/actions/runs/40325314726) | 成功 |
| Native Release Package | [40325314763](https://github.com/Techshrr/GoJet/actions/runs/40325314763) | 成功 |
| Native Installer Web | [40325314791](https://github.com/Techshrr/GoJet/actions/runs/40325314791) | 成功 |
| Native Service Environment | [40325314744](https://github.com/Techshrr/GoJet/actions/runs/40325314744) | 成功 |
| Native Migration Execution | [40325314725](https://github.com/Techshrr/GoJet/actions/runs/40325314725) | 成功 |
| Native Data Bootstrap | [40325314869](https://github.com/Techshrr/GoJet/actions/runs/40325314869) | 成功 |

完整首次安装连续链路、首次入口部分发布恢复、准备/预检/安装调度器发布恢复及
既有故障/安全边界回归均已通过原生 CI。连续链路从空数据库、真实网页配置，经
root 准备与独立审批、单次安装提交、真实定时器执行，到同地址桌面/手机完成反馈及
安装器收尾，不注入网页状态、批准或完成记录。
重填配置替换目录的发布恢复也已通过上述 Payload CI（job 121580387215）；
日志确认原有停用记录及执行屏障保留。新增 Redis 读写/删除验证已通过真实 ACL
逐项禁止 SET、GET、DEL，以及真实只读副本故障回归；已有键保留、成功清理、
删除受限后自动过期、恢复后重新通过均已核实。完整 T010 依赖矩阵仍未关闭。
原生日志绑定的包 SHA-256 为
`e67ae1944db771e3af90dfc6b22a30c44586acde2b4ad65703ad919214402dc2`，
manifest SHA-256 为 `4b7ec5b8e6254a53bb0c95ef66e50c702c6c0ceee0e822324958ebd708f77015`。
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
| 下一项：前置回归 | P09 T001–T025 在 485487a 上通过；T026/P20 证据格式衔接已修复并本地回放，当前提交 CI 待验证；前置关闭检查排队超时仍保留 |
| 初装接续 | 连续首次安装原生链路已通过；指定目标机与最终统一候选验收仍保留 |
| 恢复 | 发布恢复已通过；损坏记录/未知文件的人工处理与完整恢复手册 |
| 依赖与安全 | 完整缺失/错误版本矩阵，提权、篡改、用户及管理员账户安全回归；保留已发现缺陷 |
| 运维 | 原生升级、数据库备份恢复、兼容边界和完整恢复手册 |
| 最终验收 | 精确目标系统、统一候选源码/包摘要、全部受影响回归、G11 审查与签署 |
| 交付 | 仅运行文件的生产安装包，以及逐步中文安装、配置、命令和恢复说明 |

冻结条目 T001–T018 的验收要求保留于
[正式测试计划](../../artifacts/v10/P21/test-plan.json)。本文件不替代正式结果或改变标准。
P09 受保护浏览器回归已在 `485487a4657b83628ab61d6be49b7917ca9e07e9` 上通过：

| 工作流 | Run | 结果 |
| --- | --- | --- |
| P09 Real Files and ClamAV Integration（T001–T018） | [40373645411](https://github.com/Techshrr/GoJet/actions/runs/40373645411) | 成功 |
| P09 Files Health and Installer Preflight（T019–T020） | [40373650114](https://github.com/Techshrr/GoJet/actions/runs/40373650114) | 成功 |
| P09 Workspace Files Browser（T021–T025） | [40373645402](https://github.com/Techshrr/GoJet/actions/runs/40373645402) | 成功 |

原始浏览器附件 `12678043361` 已独立下载，ZIP 完整性与 SHA-256
`d783426ef9ab47e7c401509983e40a19cf40854f98a89ded5810a03f1a0be73b` 已复核。
五项结果均绑定上述源码；桌面正常和手机扫描器故障截图已查看。T024 确认实际 TLS
登录、认证/阶段拒绝、真实存储/ClamAV 结果、故障与恢复、过期/缺失状态清除、无安装
POST、无阶段推进和零意外浏览器错误。起始阶段及完成标记仍是明确夹具，不能作为
完整安装验收。接口映射和边界见 [P09 受保护浏览器回归](p09-protected-browser.md)。

同源码 T026 工作流 [40373644512](https://github.com/Techshrr/GoJet/actions/runs/40373644512)
在汇总附件时因旧的 9 张截图限制失败；旧校验器和 P20 文件安全消费者也仍要求公开
安装入口的旧响应结构。本轮改为明确的 12 张截图清单及共享的受保护证据校验器，
保留真实探测、认证、阶段、故障关闭、恢复及不越权声明。5 组负向测试通过。
原始汇总附件 `12678774357` 的 SHA-256
`3f68d50148469d78251591b6643cbd81fe58bfa91e2f5c674a35713178dde672` 与 ZIP 完整性已验证；
基于其中未改写的 25 项原始证据，本地回放 T026 和 P20 文件安全校验通过。
故意把扫描器故障改为健康、移除手机故障截图均被拒绝。此为新校验代码的本地验证，
不是新提交 CI 或整体关闭证明；新提交 CI 仍待验证。

同源码 P05 Closure 等前置关闭检查因 runner 排队超过其 15 分钟等待窗口而失败，
所等待的 P03/P05 基础工作流后来已成功；该问题与生产功能故障分别记录。
P09 Closure [40373645598](https://github.com/Techshrr/GoJet/actions/runs/40373645598) 仍失败，
后续必须解决前置 CI 编排及按同一候选重验，不能删减前置要求或凭局部成功宣称关闭。

## 文档入口

- [网页输入和权限边界](web-configuration-boundary.md)
- [准备、执行与恢复命令](installation-preparation.md)
- [原生网页配置](native-installer-web.md)
- [执行调度](execution-schedule.md)
- [依赖检查](dependency-preflight.md)
- [安全不变量](../security/SECURITY_INVARIANTS.md)

整体完成前不要求用户安装中间预览版本。不得降低安全门槛或用提交数量推算完成率。
生产部署需要单独授权。
