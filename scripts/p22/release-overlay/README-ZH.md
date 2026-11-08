# GoJet / Linketic 原生完整交付包

版本：10.0.0-rc.2；运行程序提交：ce305a9207fd2fe46396e7cf433137c48ab5aec1。
本包整合预编译程序、四个前端、源码、安装器、迁移与恢复工具、中文手册及 Google Web Risk 1.0.0 接入包。
现阶段无需重新开通计费测试机。原测试机已销毁，本次验收在隔离 CI 环境完成。

## 已验证范围

- 实际 Ubuntu 24.04 原生依赖、首次安装、网页到 root 安装任务、八项业务服务及安装器退役。
- 26 项桌面/移动端后台页面检查，真实管理员 Cookie、权限、CSRF/Origin、TOTP 与设置保存。
- 原始 rc.1 包升级至本候选；迁移 31→32；迁移后强制中断，再恢复旧数据库/设置/文件，最后成功升级实际二进制。
- 多阶段恢复、部分 DDL 和数据库缺失恢复、提交后数据保护、风险数据重建。
- 严格 umask 077 下解包及真实 PHP 完整性检查；证书链、错误域名与不受信任证书检查。
- Google Web Risk 接入包本地 24 项测试通过，测试源码与随包归档一致。

原生构建和首次安装证据：
https://github.com/Techshrr/GoJet/actions/runs/42460255512
该次运行的 native-admin、upgrade-candidate 通过；当时跨版本测试脚本失败，因此整条运行不是成功。
跨版本测试修正后，在下列运行复用完全相同的候选归档，并核对构建来源及运行程序源码未变化，最终通过：
https://github.com/Techshrr/GoJet/actions/runs/42481392530
测试工具提交为 9913180dfed3acd017f401fc4caffa010bfae133。
源码归档保持运行程序的原始 ce305a9 身份；verification-fixes-9913180.patch 只更新验收工具，可在源码根目录用 git apply 应用以复现最终测试。

## 包内入口

| 文件 | 用途 |
|---|---|
| release.json | 运行程序、源码提交、归档与 manifest 摘要 |
| stage-release.py | 校验归档并展开到新的不可变发布目录 |
| manuals/install.md | 全新安装，包含依赖、TLS、网页安装与 CLI 替代流程 |
| manuals/upgrade-recovery.md | 已有实例的升级、备份和失败恢复 |
| manuals/operations.md | 运维检查和记录保留 |
| integrations/GoJet-WebRisk-1.0.0.zip | 独立 Google Web Risk 接入程序、原生探针、测试及说明 |
| acceptance.json / evidence/ | 真实执行的原生验收记录及各项摘要 |

## 以后部署时

1. 对本 ZIP 核对随附 sha256，再解压；在包目录执行 sha256sum --check SHA256SUMS。
2. 按 manuals/install.md 准备 Ubuntu 24.04 amd64、systemd、PHP 8.3、Nginx、MySQL 8、Redis、ClamAV，以及域名和证书。系统依赖需要通过受信任的系统软件源安装。
3. root 执行 python3 stage-release.py stage。它会打印发布目录；后续以 release.json 里的身份和手册为准。已有安装使用升级流程，不能重新初始化。
4. Google Web Risk 组件在 Nginx、gojet 用户和站点证书就绪后，按其 README 安装并输入私有 API key；自动运行现场接入验证。新主机使用新的实际出口 IP 时，需同步 Google 密钥的 IP 限制。
5. 网页安装配置中填写自己的数据库、SMTP、Turnstile 和初始管理员凭据。Web Risk 验证通过后，提供方 google-web-risk，端点 https://www.linketic.com:9443/v1/check，策略版本 linketic-risk-v1。Google key 不填入 GoJet 网页表单。

## 验收边界

这是有完整原生 CI 证据的 RC 交付包，尚未部署生产环境。整机重启、目标域名和证书续期、当前外部服务凭据、真实邮件及支付未在本轮验收。
Google 真实接口的成功记录来自你在 2026-11-07 提供的旧测试机输出；本轮只重跑接入包本地测试，未将其冒充新的 Google 现场验证。
部署凭据、数据库备份、管理员密码、Cookie 和证书私钥均不包含在包内。保存实际部署产生的私有升级目录和回滚数据；它们不是本交付包预先附带的文件。
