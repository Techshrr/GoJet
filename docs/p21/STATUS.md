# P21 当前交付状态

更新：2026-10-26。目标环境：Ubuntu 24.04.4 LTS x86_64。
当前分支：`develop/p21-native-release-package`；开发 PR：[#207](https://github.com/Techshrr/GoJet/pull/207)。
P21/G11 尚未关闭，包保持 `installable=false`，尚未交付生产安装包。

本文件只保留当前状态。历史实现、失败与修复记录以 Git 提交及对应 CI 为准，
不在工作文档中继续追加已失效的“下一步”或“待 CI”检查点。

## 最新已核实的 CI

当前已核实源码：`66cf3e965532d9f2e05247d864eeb415b222b654`。

| 工作流 | Run | 结果 |
| --- | --- | --- |
| Native Payload Build | [40215936883](https://github.com/Techshrr/GoJet/actions/runs/40215936883) | 成功 |
| Native Release Package | [40215936834](https://github.com/Techshrr/GoJet/actions/runs/40215936834) | 成功 |
| Native Service Environment | [40215936830](https://github.com/Techshrr/GoJet/actions/runs/40215936830) | 成功 |
| Native Migration Execution | [40215936833](https://github.com/Techshrr/GoJet/actions/runs/40215936833) | 成功 |
| Native Data Bootstrap | [40215936811](https://github.com/Techshrr/GoJet/actions/runs/40215936811) | 成功 |
| Native Installer Web | [40215936903](https://github.com/Techshrr/GoJet/actions/runs/40215936903) | 成功 |
| P00 Bootstrap and G0 Traceability | [40215942511](https://github.com/Techshrr/GoJet/actions/runs/40215942511) | 成功 |

原生日志确认首次持久网页入口、服务重启、真实浏览器到 root 准备及缺失成功回执
恢复通过，也包含此前的重填 FPM 切换回归。同地址交接、重载失败及真实进程终止
后的恢复也已通过。本次完成回执、浏览器反馈和安装器收尾新增回归尚待对应 CI。
这些结果不是全部项目验收通过，也不替代指定 Ubuntu 24.04.4 目标机验证；
此前检查到的原生 runner 为 24.04.5。

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
已登记定时器和安装器 FPM 池；回执缺失及收尾中断可按相同身份显式续办，CI 待核实。

## 尚未完成的工作

| 范围 | 待完成事项 |
| --- | --- |
| 初装接续 | 完成回执/浏览器反馈/安装器收尾的原生 CI；统一从首次配置到完成反馈与收尾的连续验收 |
| 恢复 | 初次入口部分发布、重填发布和调度器发布自身的中断恢复等剩余边界 |
| 依赖与安全 | 完整缺失/错误版本矩阵，提权、篡改、用户及管理员账户安全回归；保留已发现缺陷 |
| 运维 | 原生升级、数据库备份恢复、兼容边界和完整恢复手册 |
| 最终验收 | 精确目标系统、统一候选源码/包摘要、全部受影响回归、G11 审查与签署 |
| 交付 | 仅运行文件的生产安装包，以及逐步中文安装、配置、命令和恢复说明 |

冻结条目 T001–T018 的验收要求保留于
[正式测试计划](../../artifacts/v10/P21/test-plan.json)。本文件不替代正式结果或改变标准。
P09 T024 等受保护路由验收及其他前置回归仍须满足；历史某一提交的失败记录不当作
当前失败，但也不凭六项 P21 工作流成功推断它们已经关闭。

## 文档入口

- [网页输入和权限边界](web-configuration-boundary.md)
- [准备、执行与恢复命令](installation-preparation.md)
- [原生网页配置](native-installer-web.md)
- [执行调度](execution-schedule.md)
- [依赖检查](dependency-preflight.md)
- [安全不变量](../security/SECURITY_INVARIANTS.md)

整体完成前不要求用户安装中间预览版本。不得降低安全门槛或用提交数量推算完成率。
生产部署需要单独授权。
