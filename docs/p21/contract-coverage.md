# P21 合同覆盖核对

日期：2026-11-06。依据：总计划 §4.1、§4.2、P21/P22、G11–G13，以及冻结的 P21-T001–T018 测试计划。
本文件区分工程覆盖核对与 Release Engineer 的 G11 决定；不代签、不部署生产。

## 本次发现和处理

对照总计划 §4.1，原候选缺少明确的操作系统/CPU 支持边界和磁盘容量检查。
现已新增固定的 Ubuntu 24.04 / Linux x86_64 / 64 位 PHP 检查，发布目录须保留另一份完整解包内容的空间，
存储根目录、quarantine、published 各自文件系统须有可用空间。检查结果进入第九项 host，
贯穿准备、实际数据执行前复查、升级健康检查、安装完成健康检查和浏览器快照。
不新增 CPU 核数、内存或任意 GB 门槛；业务与数据库备份容量仍须按实际数据规划。

首次原生包验证发现 HostPreflight.php 未进入组装清单。修正同时加入固定 PHP 引用的包内闭合检查，
在生成归档及独立读回时拒绝缺少直接/间接引用文件，避免源码存在而发布包缺文件。
失败 run 42150614477 保留为失败，不代表新候选通过。

## 逐项核对

下表的“覆盖”表示对应实现与验证路径已找到，最终结果须以本次原生结果记录为准；不表示十八项已由负责人批准。

| 项目 | 实现与验证依据 | 边界及处理 |
| --- | --- | --- |
| T001 冻结入口 | validate_contract.py 检查祖先、不可变规范和工具链；本批 Native Package 已通过 | 不重签历史候选，不把历史签署争议重新列为代码阻塞 |
| T002 八服务二进制 | verify_build.py：八服务加 filepreflight；版本/source、ELF amd64、静态链接、八个独立 unit | 每次包重建使用新的 source；历史二进制不能冒充当前包 |
| T003 四前端 | 冻结依赖构建，真实 Nginx 下核对四应用路由与静态字节；PHP/token 资产纳入包 | 不把源码或开发服务器当交付物 |
| T004 迁移目录 | catalog.py：31 项原始字节、编号/摘要；归档 manifest 与编译报告一致 | 没有伪造 down migration |
| T005 迁移执行 | migrations/native.json：空库 31 项、锁争用、摘要漂移、部分 DDL、重复运行；备份还原另有报告 | 已下载当前实现的原始组件报告 |
| T006 允许清单 | archive.py 与真实 PHP 验包：路径/链接/模式/额外文件/篡改拒绝；新增 PHP 引用完整性检查 | 本轮组装遗漏已修正；新包需重新完整验证 |
| T007 摘要与 SBOM | 实际归档成员、SHA256SUMS、manifest、SBOM 文件摘要及独立下载核对 | 新旧候选分开存放，原始报告不反写 |
| T008 原生配置 | 八非 root unit 与 EnvironmentFile；environment/result.json 和 logrotate.json；Nginx 语法/原子发布 | 组件源码与当前 installer/deploy Git 对象相同，记录绑定 |
| T009 安装访问与状态 | installer-http.json、installer-state.json；真实 TLS/FPM；八路由顺序、会话、CSRF、限流、脱敏、锁后 404 | 组件 false 范围保留；实际 apply 由完整安装链路证明 |
| T010 强制预检 | 既有服务/权限/协议故障；真实 PHP 8.1 CLI/FPM 与 MariaDB 10.11 拒绝；新增六种主机/空间负例 | 既有版本报告仍绑定原候选；新包的六种主机负例已通过，随后在同一新归档重验实际依赖版本拒绝，不复用旧包摘要 |
| T011 站点与管理员 | typed 配置校验、TLS/路径冲突；data-bootstrap/result.json：继承管理员权限、生成密钥、真实平台会话 | 无公共提权路径；原生完整安装另验证浏览器到 root 的连续流程 |
| T012 特权 apply | sealed request、root worker、全局锁、重放拒绝；独立 executor-context 检查降权和约束 | 浏览器成功不等于 root apply 成功 |
| T013 服务与完成锁 | 八 unit 启动、非 root PID、DB/cache/storage/ClamAV/Nginx/HTTP；失败不写完成锁 | 整机重启属于 P22/G12，不混入此处宣称通过 |
| T014 升级恢复 | 真 MySQL 部分 DDL/缺库恢复、配置切换、209 条风险投影、15 个中断点、10 类不兼容包拒绝 | 测试升级沿用同批二进制；不承诺任意历史版本兼容 |
| T015 操作手册 | 四份随包中文手册；包内路径、相对链接、shell 语法和篡改拒绝 | 总计划 P21 要求静态验证；逐命令干净主机演练属于 P22，不能人为新增前置门槛 |
| T016 浏览器 | native-web/browser.json：桌面/移动、键盘、错误/过期/执行/关闭状态、无横向溢出、意外控制台错误 0；新增 host 失败截图 | 已实际查看桌面及移动截图；快照夹具仅证明 UI，主机拒绝由真实原生测试证明 |
| T017 不可变候选 | finalize_candidate.py 绑定精确包及八报告，拒绝缺报告、异源、失败、错误摘要；增加 host 基线要求 | 新包全链路及独立下载复核已通过；待当前包版本补证和 G11 批准 |
| T018 责任验收 | G11 决定文件与 Release Engineer 责任保持 | 尚未批准；不伪造签署，不将自动测试等同于批准 |

组件原始报告和每份下载 ZIP 的摘要见
[组件证据索引](../../artifacts/v10/P21/host-preflight/components/index.json)。
这些报告来自源码 9e8aa7617c69ea0932ef4e1b9338006203ee8ca7；后续组装修正仅改变构建校验，
索引保存 installer/deploy/业务代码及相关组件测试与新包源码相同的 Git 对象。

## 剩余边界

本次主机检查与打包修正的新候选全链路已成功，归档已独立下载并逐报告核验，candidate.json 独立重建完全一致。
新包版本拒绝补证完成后，P21 工程证据交给 G11 责任审查；
P22 保留指定干净目标机安装、八服务 restart/reboot、真实 P0 外部服务、生产升级/回滚和 Release Owner 验收。
P22 生产操作没有授权，不在本次执行。

当前包及 CI 结果见[新候选验证记录](../../artifacts/v10/P21/host-preflight/validation.json)。
