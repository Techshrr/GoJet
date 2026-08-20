# GoJet V5 Master Plan — Greenfield Implementation Contract

**Document ID:** `GJ-V5-MP-GREENFIELD-2026-08-20`  
**Status:** APPROVED GREENFIELD IMPLEMENTATION CONTRACT  
**Product contract:** `GoJet V5`  
**Implementation repository:** `Techshrr/GoJet`  
**Implementation remote:** `https://github.com/Techshrr/GoJet.git`  
**Implementation branch:** `main`  
**Implementation commit:** dynamically resolved 40-hex commit from the repository under test  
**Development model:** `GREENFIELD / NO LEGACY CODE DEPENDENCY`  
**Specification pack:** `specifications/`  
**History rule:** this repository owns its own history; prior GoJet repositories, branches, commits, Git objects, migrations, generated artifacts and runtime configuration MUST NOT be imported as implementation dependencies or normative evidence  
**Architecture:** Nginx + eight Go services + PHP 8.3 FPM installer + MySQL 8.x + Redis + ClamAV + local filesystem  
**Production Docker/Compose/Node runtime:** PROHIBITED

> 本文是 GoJet V5 的范围、架构、安全、实施节点和发布验收最高级契约。GoJet V5 采用完全 Greenfield 实现：产品行为只由本 Specification Pack 定义，不以任何旧 GoJet 仓库、版本、分支、提交或数据库历史作为正确性来源。`APPROVED` 只表示本文可用于实施，不表示软件已经完成；所有 `REQUIRED` 能力只有在当前仓库中完成实现并取得所属 Gate 证据后才能宣称可用。

---

## 1. 文档治理

### 1.1 规范性用语

- **MUST / 必须**：发布前不可豁免；不满足即 Gate 失败。
- **MUST NOT / 禁止**：任何实现、配置和发布包均不得出现。
- **SHOULD / 应当**：除非存在记录在案且经责任角色批准的工程原因，否则必须执行。
- **MAY / 可以**：在不削弱 MUST 约束时允许采用。

### 1.2 能力状态

| 状态 | 含义 | 发布处理 |
|---|---|---|
| `REQUIRED` | GoJet V5 正式发布必须在当前仓库中完整实现并验证 | 所属 Node 与 Gate 全通过后才能宣称可用 |
| `REMOVED` | 正式生产链明确禁止或删除 | 代码、发布包和生产文档不得提供 |
| `DEFERRED` | 明确不在 GoJet V5 发布范围 | 不得出现在正式功能承诺或可达导航中 |
| `DECISION REQUIRED` | 仅允许在依赖节点开始前暂存的未决设计项 | 首个依赖节点进入前必须关闭；release candidate 数量必须为零 |

Greenfield 项目不使用任何“继承既有实现即可视为完成”的 legacy-derived 状态，也不使用 `MUST PRESERVE`。能力是否完成只能由当前仓库的实现和证据证明。

### 1.3 文档优先级

1. 本 Master Plan：范围、架构、安全、节点、Gate、发布策略；
2. `GoJet_V5_BRAND_DESIGN_SYSTEM_OPTIMIZED.md`：唯一精确视觉和交互值；
3. `GoJet_V5_PAGE_LEVEL_IA_OPTIMIZED.md`：路由、页面组成、任务流和页面 SEO；
4. Capability Matrix 与 Route Registry：实现追踪；
5. 实现代码：必须同时满足全部适用契约。

稳定交叉引用如下：Design System 为 `GJ-V5-DS-GREENFIELD-2026-08-20`，Page-Level IA 为 `GJ-V5-IA-GREENFIELD-2026-08-20`。本文件只规定能力、策略和验收边界；所有精确视觉值必须引用 Design System，所有 path、Route ID、页面组成和页面级任务流必须引用 Page-Level IA。本文中的 Surface 名称只是追踪标签，不创建或修改路由。

下级文档不得重新定义上级文档的权威值。发生冲突时必须修正下级文档或实现，并记录变更原因、影响、批准人和验证证据。

### 1.4 变更控制

对本文中架构、P0 能力、安全 fail closed 行为、SEO 索引策略或发布 Gate 的修改，必须提交变更记录，至少包含：需求来源、旧规则、新规则、风险、迁移方案、回滚方案、受影响节点、受影响 Gate 和批准角色。为了通过测试而弱化安全或删除功能的变更不得批准。任何 `DECISION REQUIRED` 项必须在依赖它的节点 Entry Conditions 评审前关闭，不得带入节点 Exit Conditions 或 release candidate。

---

## 2. Greenfield 目标能力与实现追踪

### 2.1 八个 Go 运行程序

| 服务 ID | 可执行程序 | 目标源码路径 | 状态 | GoJet V5 不变量 |
|---|---|---|---|---|
| `SVC-REDIRECT` | `redirectengine` | `services/redirectengine/cmd/server` | `REQUIRED` | 低延迟解析、风险优先、点击记录和重定向语义必须完整 |
| `SVC-ANALYTICS-WORKER` | `analyticsworker` | `services/analyticsworker/cmd/worker` | `REQUIRED` | Redis 事件消费和分析写入不得丢失 |
| `SVC-ANALYTICS-RECONCILER` | `analyticsreconciler` | `services/analyticsreconciler/cmd/reconciler` | `REQUIRED` | 对账和恢复流程必须幂等、可重复执行 |
| `SVC-PLATFORM-API` | `platformapi` | `services/platformapi/cmd/server` | `REQUIRED` | 认证、授权、租户与业务写入由服务端裁决 |
| `SVC-MAIL-WORKER` | `mailworker` | `services/platformapi/cmd/mailworker` | `REQUIRED` | 邮件任务、模板、退避与重试链路完整 |
| `SVC-FILE-WORKER` | `fileworker` | `services/platformapi/cmd/fileworker` | `REQUIRED` | 文件队列、隔离、扫描与发布状态机完整 |
| `SVC-OPS-MONITOR` | `operationsmonitor` | `services/platformapi/cmd/operationsmonitor` | `REQUIRED` | 运行状态、风险任务和恢复监控可观察 |
| `SVC-LOG-RECEIVER` | `logreceiver` | `services/logreceiver/cmd/server` | `REQUIRED` | 结构化日志接收及敏感字段治理完整 |

所有八个程序必须由独立 systemd unit 管理，使用非 root 服务账户，并具备启动、停止、重启、自动恢复、日志查询和健康检查证据。不得把多个程序隐藏在单一长驻脚本中。

### 2.2 Capability implementation matrix

下表是当前仓库的目标能力清单，不引用旧实现。`IA reference` 只指向 Page-Level IA 的 Surface/章节，不定义 path 或 Route ID；Page-Level IA Route Registry 是路由唯一权威。

| Capability ID | 能力 | 状态 | Required implementation contract | Owner | Gate | IA reference |
|---|---|---|---|---|---|---|
| `CAP-LINKS` | 短链 CRUD、自定义码、状态码、密码、到期、点击上限、一次性、批量与导出 | `REQUIRED` | 真实 API/Redis/redirectengine 全链路实现与验证 | P05 | G3/G6/G10 | Workspace Links，IA §§7-8 |
| `CAP-LINK-ROUTING` | Geo/Device/Language/Source routing | `REQUIRED` | 所有可达目标进入同一 risk fingerprint | P05/P16 | G3/G6 | Link Detail，IA §8.3 |
| `CAP-LINK-AB` | A/B destinations | `REQUIRED` | 权重校验、目标变更失效和审计 | P05/P16 | G3/G6 | Link Detail，IA §8.3 |
| `CAP-LINK-UTM` | UTM mutation | `REQUIRED` | safety surface 前不得执行 UTM | P05 | G3/G6 | Link Detail，IA §8.3 |
| `CAP-LINK-HISTORY` | 版本历史、恢复与变更原因 | `REQUIRED` | 变更可归因，恢复后重新执行 risk | P05 | G3/G6 | Link History，IA §8.3 |
| `CAP-OFFICIAL-DOMAINS` | 官方短域名管理与选用 | `REQUIRED` | 官方域名与自定义域名执行同一 destination-risk | P05/P17 | G3/G6 | Workspace/Admin domains，IA §§7,10 |
| `CAP-QR` | QR 创建、列表、删除、分享 QR 与下载 | `REQUIRED` | 真渲染、真下载、目标权限一致 | P08 | G3/G10 | Workspace QR，IA §§7-8 |
| `CAP-TEXT` | Text Sharing | `REQUIRED` | 访问控制、生命周期、正确状态码和 noindex | P10 | G3/G7 | Workspace/Public Text，IA §§7-8,13 |
| `CAP-BIO` | Link in Bio | `REQUIRED` | 默认 noindex，公开链接标记 UGC | P11 | G3/G7 | Workspace/Public Bio，IA §§7-8,13 |
| `CAP-FILES` | File Sharing、密码、到期、下载限额、retention 与管理员资源处置 | `REQUIRED` | 隔离、扫描、发布、下载授权和保留期必须保持完整 | P09/P17 | G3/G6/G10 | Workspace/Admin/Public Files，IA §§7-8,10,13 |
| `CAP-CLAMAV-REQUIRED` | ClamAV 正式环境强制依赖 | `REQUIRED` | installer 必须硬失败；运行时缺失、超时、过期或不确定均 fail closed | P09/P22 | G6/G12/G13 | Installer/File states，IA §§8.5,13 |
| `CAP-ANALYTICS` | 点击事件、聚合、维度、恢复和 reconciliation | `REQUIRED` | Redis 事件、MySQL 聚合和幂等恢复均验证 | P07 | G3/G9/G10 | Workspace/Admin Analytics，IA §§7,10 |
| `CAP-CAMPAIGNS` | Campaign 与 conversion tracking | `REQUIRED` | Workspace 隔离并与 Links/Analytics 关联 | P07/P12 | G3 | Workspace Campaigns，IA §7 |
| `CAP-FOLDERS-TAGS` | Folders、Tags 与组织过滤 | `REQUIRED` | Workspace 隔离、CRUD、过滤和批量关联 | P12 | G3/G6 | Workspace organization，IA §§7-8 |
| `CAP-WORKSPACE` | Workspace、成员、邀请、角色和 RBAC | `REQUIRED` | 服务端 tenant/RBAC 权威，保护最后一个 owner | P12 | G3/G6/G10 | Workspace shell/members，IA §§4.4,7 |
| `CAP-NOTIFICATIONS` | 站内通知中心：安全、域名、计费、工单与资源任务通知 | `REQUIRED` | 服务端事件驱动；read/unread、mark-all-read、深链、去重、敏感信息脱敏；通知不替代邮件或审计 | P12/P13-P17 | G3/G5/G6/G10 | Workspace Notifications，IA §§4.4,7-8 |
| `CAP-BILLING` | Plans、quota、subscription、invoice 与 billing cycle | `REQUIRED` | 权益与配额使用结构化服务端数据 | P13 | G3/G6/G10 | Workspace/Admin Billing，IA §§7,10 |
| `CAP-PAYMENTS` | Orders、payments、FX、invoice PDF；Alipay/WeChat/Epay/PayPal/Stripe/Crypto | `REQUIRED` | 幂等结算，金额、币种、签名和状态由服务端验证 | P13 | G3/G6/G10 | Workspace/Admin Payments，IA §§7,10 |
| `CAP-PAYMENT-CALLBACKS` | 支付回调与回调事件 | `REQUIRED` | 隔离为支付入口，不得误归类为用户通用 webhook | P13 | G6/G10/G13 | Admin payment evidence，IA §10 |
| `CAP-TICKETS` | 客户/管理员 Support Tickets 与 replies | `REQUIRED` | 域名申请只产生请求，不直接授权 | P14 | G3/G6/G10 | Workspace/Admin Support，IA §§7,10 |
| `CAP-MAIL` | Mail、templates、queue、retry 与 branding | `REQUIRED` | 队列、重试、模板变量和秘密脱敏 | P14 | G3/G6/G10 | Admin Mail，IA §10 |
| `CAP-AUTH` | 注册、邮箱验证码、验证、登录、重置、Session 与账户 | `REQUIRED` | Session、CSRF、Origin、限流和 token lifecycle | P15 | G3/G5/G6/G10 | Auth/Account，IA §§6-7 |
| `CAP-OAUTH` | Google/Facebook/GitHub/QQ/WeChat/Rainbow 登录、注册与绑定 | `REQUIRED` | state、PKCE where applicable、redirect URI、绑定与解绑 | P15 | G3/G6/G10/G13 | Auth/Admin OAuth，IA §§6,10 |
| `CAP-TURNSTILE` | 场景化 bot protection | `REQUIRED` | token 服务端验证，配置与失败模式验证 | P14/P15/P17 | G6/G10/G13 | Auth/Ticket/Admin，IA §§6-7,10 |
| `CAP-DOMAIN-OWNERSHIP` | 自定义域名 Workspace 绑定与 TXT 所有权 | `REQUIRED` | TXT secret hash、rotation 与跨 Workspace 所有权必须保持完整 | P06 | G3/G6 | Domains，IA §§9,11 |
| `CAP-DOMAIN-HTTPS` | 自定义域名 TLS readiness 与 Link assignment guard | `REQUIRED` | HTTPS 未 active 不得绑定短链 | P06 | G3/G6 | Domains，IA §§9,11 |
| `CAP-DOMAIN-ENTITLEMENT` | 自定义域名资格与限额 | `REQUIRED` | active `business` 自动或独立 `manual_approval`；工单本身不授权；服务端全检查点强制 | P06/P13/P14/P17 | G6/G10 | Domain entitlement flows，IA §§9,11 |
| `CAP-DOMAIN-RISK` | ingress DNS、域名信誉、分轴状态与周期复验 | `REQUIRED` | Ownership/DNS/HTTPS/Risk 独立；周期复验；安全失效立即停用 | P06/P16/P17 | G6/G13 | Domain risk flows，IA §§9,11 |
| `CAP-DESTINATION-RISK` | 目标扫描、复扫、fingerprint 和人工审核 | `REQUIRED` | 每条 `short_links` 的 primary/routing/A-B；官方/自定义 hostname 完全同策略 | P05/P16 | G6/G10/G13 | Link/Admin risk，IA §§8.3,10-13 |
| `CAP-ABUSE` | Public abuse reports 与管理员处置 | `REQUIRED` | 报告、证据、裁决、恢复与关联资源均审计 | P16/P17 | G6/G10 | Public/Admin Abuse，IA §§10,13 |
| `CAP-ADMIN-ACCESS` | 管理员、权限、TOTP、Session 与高风险操作 | `REQUIRED` | 专用权限、MFA/Session 与 reason requirement | P17 | G3/G6/G10 | Admin Access，IA §10 |
| `CAP-OPS-AUDIT` | Operations、alerts、diagnostics、correlation logs 与 audit | `REQUIRED` | 八服务可观察、敏感字段脱敏、动作可归因 | P17 | G3/G6/G13 | Admin Operations/Audit，IA §§10,12 |
| `CAP-ANNOUNCEMENTS-SETTINGS` | Announcements、public/runtime settings、brand assets | `REQUIRED` | 权限、发布状态、缓存和秘密遮蔽 | P17/P19 | G3/G6/G7 | Website/Admin Platform，IA §§5,10 |
| `CAP-API-KEYS` | 用户 API keys | `REQUIRED` | scope、secret-once、rotation、revocation、expiry、rate limit 和 audit | P17 | G3/G6 | Developer Workspace，IA §7 |
| `CAP-USER-WEBHOOKS` | 通用用户 outbound webhooks | `REQUIRED` | endpoint ownership、签名、secret rotation、retry/backoff、SSRF、disable 和 audit | P17 | G3/G6 | Developer Workspace，IA §7 |
| `CAP-NATIVE-INSTALL` | Native Installer、systemd apply、upgrade 与 rollback | `REQUIRED` | 实现 native 入口、PHP Installer、八服务 systemd apply、native upgrade 与失败恢复；生产链不得存在 Docker 路径，ClamAV 为强制依赖 | P21/P22 | G1/G11/G12/G13 | Installer，IA §13.1 |
| `CAP-NATIVE-ONLY-RELEASE` | Native-only 生产发布、打包、fresh install 与 rollback | `REQUIRED` | 生产仓库与发布包禁止 production Docker 路径；ClamAV 安装检查 hard fail；以 native-only archive 完成 clean-host install、upgrade、backup/restore、rollback 和 production validation | P21/P22 | G1/G11/G12/G13 | Installer/release，IA §13.1 |
| `CAP-TECHNICAL-SEO` | 预渲染、index policy、canonical、sitemap、hreflang 与状态码 | `REQUIRED` | build、Nginx、content registry 和 release Gate 共同执行 | P18/P19 | G7/G9/G13 | Website/Docs/Public，IA §§3,13,16 |

每个 `REQUIRED` capability 的执行矩阵必须补齐 Backend、DB/Migration、API、UI、RBAC、States、Browser、Security、Observability、Release 十列；不适用列必须记录 `N/A` 和理由，不能留空。任何“旧版本做过”“外部仓库有代码”或截图都不能替代当前仓库的实现证据。

### 2.3 Removed/Deferred disposition ledger

| ID | 状态 | 处置 | Release consequence |
|---|---|---|---|
| `DEP-DOCKER-PRODUCTION` | `REMOVED` | Dockerfile、Compose、Docker installer/upgrade/rollback/package contract 不进入生产链 | release archive、installer、runbook 或 production validation 出现 production Docker 路径即 G1/G11 硬失败 |
| `DEP-NODE-PRODUCTION` | `REMOVED` | Node HTTP/SSR/PM2/dev server 不进入正式主机 | Node 仅可在开发、构建、测试和打包环境存在 |
| `CAP-S3-STORAGE` | `DEFERRED` | S3-compatible 可作为后续扩展，但 V5 fresh install 与 release 不依赖也不宣称验收 | local filesystem 是唯一强制存储路径；未来扩展不得削弱 quarantine、scan、authorization 或 audit |
| `CAP-BIO-OPT-IN-INDEX` | `DEFERRED` | Bio owner opt-in indexing 不属于 V5 | Bio 保持 noindex 且不进入 sitemap |
| `DEP-LEGACY-GOJET-CODE` | `REMOVED` | prior GoJet repository code、migration、Git history、generated artifact 或 runtime config 不得导入 | 任一实现依赖旧仓库才能 build/test/run 即 G0/G1 硬失败 |

本合同初始 `DECISION REQUIRED` 数量为零。新增未决项只能进入 change-control ledger，并必须在首个依赖节点开始前关闭。

## 3. 正式生产架构

```text
                                  Nginx
                 ┌──────────────────┼──────────────────┐
                 │                  │                  │
        Static Website/Docs   Workspace/Admin       /install/
                 │                  │              PHP 8.3 FPM
                 └──────────────────┼──────────────────┘
                                    │
                             Go Platform API
                                    │
          ┌────────────┬────────────┼────────────┬────────────┐
          │            │            │            │            │
       MySQL 8.x     Redis     Go workers      ClamAV    Local storage

Eight Go executables: independent systemd units
Only public ingress: Nginx
Production Docker, Docker Compose, PM2, Node SSR and Node runtime: prohibited
```

### 3.1 依赖责任

| 组件 | 生产责任 | 硬失败条件 |
|---|---|---|
| Nginx | TLS、静态文件、反向代理、安全头、索引头、规范化跳转 | 配置测试失败、证书不可用、错误 upstream |
| PHP 8.3 FPM | 只运行 Web Installer | 非 `/install/` 业务依赖 PHP、版本不符 |
| MySQL 8.x | 持久业务事实、迁移、审计 | 版本不符、迁移失败、备份不可恢复 |
| Redis | 重定向实时态、分析事件、限流、短期状态 | 认证/连通/写读检查失败 |
| systemd | 八个 Go 进程生命周期 | unit 缺失、权限错误、重启不恢复 |
| ClamAV | 文件恶意内容检测 | 缺失、不健康、超时、签名超期、结果不确定 |
| Local filesystem | V5 required default object storage | 位于 Web Root、权限过宽、隔离区可公开读取 |

Nginx 是唯一公网监听者；Go service、PHP-FPM、MySQL、Redis 和 ClamAV 只能通过 loopback、Unix socket 或受控私网端点连接，不能各自暴露公网端口。PHP 8.3 FPM 的正式请求范围仅限安装期的 `/install/`；安装锁定后 Installer 必须不可执行并返回 Page-Level IA 规定的关闭状态。ClamAV 同时是安装前置条件和持续运行依赖：安装、升级或健康检查不得把 unavailable 降级成 warning。

S3-compatible 支持可以保留为扩展，但不得成为 fresh install、运行或发布 Gate 的强制依赖，也不得绕过 local filesystem 的 quarantine、scan、authorization 和 audit 语义。

### 3.2 Node.js 边界

Node.js 只允许用于开发、构建、测试、静态预渲染和发布打包。正式主机不能运行 Node HTTP 服务、Node SSR、PM2 或前端开发服务器。构建产物必须能由 Nginx 静态提供。

---

## 4. Docker 删除与原生替代合同

| 删除对象 | 状态 | 必须提供的 native replacement | 验收证据路径 |
|---|---|---|---|
| 根 `Dockerfile`, `compose.yaml` 和服务级 Dockerfiles | `REMOVED` | 可重复 cross/build-host Go release build、八个 versioned binaries、独立 systemd units | `artifacts/v5/P21/package-manifest.json`; `artifacts/v5/gates/G11/denylist.txt` |
| `deploy/docker/` 与两个 `deploy/compose.*.yaml` | `REMOVED` | 原生 Nginx vhost、systemd、environment-file 和 tmpfiles/logrotate ownership contract | `artifacts/v5/P21/native-config-inventory.txt`; `artifacts/v5/P22/systemd/` |
| `scripts/installdocker.sh` 与 `install.sh --docker` | `REMOVED` | 单一 native preflight/apply/verify workflow；不识别或明确拒绝 `--docker` | `artifacts/v5/P22/installer/`; `artifacts/v5/gates/G12/dependency-failures/` |
| `installhostnginx.sh` 中 Compose 行为 | `REMOVED` | Nginx template render、`nginx -t`、原子替换和 reload/revert | `artifacts/v5/P22/nginx/` |
| `upgrade.sh`, `rollback.sh`, `scripts/lib.sh` 的 Docker 依赖 | `REMOVED` | native backup、migration lock、versioned release switch、八服务 lifecycle、restore | `artifacts/v5/P22/upgrade/`; `artifacts/v5/P22/rollback/` |
| `scripts/packagerelease.sh` 复制的 Docker assets | `REMOVED` | allowlist-driven native archive、manifest、SHA-256、SBOM 与签名材料 | `artifacts/v5/P21/` |
| Docker-only CI/validation | `REMOVED` | Go binary、static output、native config、package denylist、clean-host install 与 rollback tests | `artifacts/v5/gates/G1/`; `artifacts/v5/gates/G11/`; `artifacts/v5/gates/G12/` |
| Docker 文档和运行命令 | `REMOVED` | native install/operations/upgrade/rollback/troubleshooting runbooks | `artifacts/v5/P21/docs-scan.txt` |

### 4.1 Native replacement minimum

删除 Docker 只有在以下替代物全部存在并通过 Gate 时才算完成：

- 八个 Go binary 必须可重复构建并暴露可记录的版本/commit；每个 binary 使用独立 systemd unit、非 root 账户、明确的 `After`/`Requires`、EnvironmentFile、工作目录、restart policy、stop timeout 和最小文件权限。不得用一个 supervisor script 冒充八个 unit。
- Nginx template 必须覆盖 TLS、static Website/Docs/Workspace/Admin、Go upstream、仅安装期的 PHP-FPM、真实状态码、安全头、`X-Robots-Tag` 和 canonical normalization；render 后先 `nginx -t`，成功后才 atomic reload，失败必须保持旧配置。
- Native installer 必须验证操作系统支持矩阵、CPU/磁盘/权限、Nginx、PHP 8.3 FPM、MySQL 8.x、Redis、systemd、ClamAV daemon/signature freshness、local-storage ownership 和 package checksum。任一强制项失败都不得写安装完成锁。
- MySQL replacement 必须提供一致性备份、checksum、restore drill、migration catalog、全局 migration lock 和失败边界；Redis replacement 必须提供 auth/read-write/latency health、重连与由 MySQL 权威数据恢复 destination-risk cache 的证据。
- Local filesystem 必须提供 Web Root 外的 upload/quarantine/published 分区、最小权限、容量检查、backup/restore 与清理/retention job；public download 只能经 Go authorization path。
- Release archive 必须至少包含八个 binaries、完整 Greenfield migration catalog、static frontend、PHP Installer、Nginx/systemd/native helper、config examples、runbooks、manifest、SHA-256 checksums 和 SBOM；不得包含 Docker/Compose、`node_modules`、开发服务器、构建缓存或生产 secret。

### 4.2 Upgrade and rollback invariants

- Upgrade 必须先核验 archive/checksum/version compatibility，再保存当前 config、release manifest 和 MySQL 一致性备份；不可恢复的备份禁止继续。
- Migration 必须串行持锁、逐项记录 applied state；不能声称所有 schema 变更都可 down-migrate。若新代码无法与旧 schema 兼容，rollback runbook 必须恢复升级前数据库备份，而不是只切回 binary。
- Code/static/config 切换必须使用 versioned immutable release 与 atomic active-pointer switch；健康检查通过前不得清理前一版本。
- Rollback 必须恢复匹配的 binaries、static assets、Nginx/systemd config 和必要数据库状态，随后验证八服务、MySQL、Redis、ClamAV、Nginx、关键用户流程与 audit correlation。
- 失败的 upgrade/rollback 必须保留完整脱敏日志和当前可恢复状态；不得通过手工编辑生产文件形成不可复现的“成功”。

生产文档不得同时宣传 Docker 为受支持方案。历史删除说明可以出现 `Docker`，但安装命令、架构图、包清单和生产 runbook 不得提供 Docker 路径。

---

## 5. 服务端权威与数据不变量

### 5.1 服务端唯一裁决

Authentication、Authorization、Tenant Isolation、RBAC、Quota、Payment、Destination Risk、File Security、Domain ownership、Custom-domain entitlement、Rate Limit 和 Audit 必须由 Go API/worker/redirectengine 执行。前端隐藏按钮只是体验，不是授权。

### 5.2 租户与 RBAC

- 每个 Workspace 资源查询必须带服务端 workspace 边界；
- 资源 ID 不得替代所有权检查；
- 高风险管理员动作使用独立权限，不以“已登录管理员”代替；
- 域名人工授权权限与工单处理权限分离；
- 跨 Workspace 资源访问返回一致的拒绝语义，不泄露资源存在性。

### 5.3 写入与审计

敏感写操作必须记录 actor、workspace、action、resource、reason、request correlation ID、结果和时间。日志禁止写 password、session/access token、OAuth/payment/webhook secrets、数据库密码、ClamAV 原始敏感路径或用户文件内容。

---

## 6. 自定义域名资格与安全合同

### 6.1 三层安全模型

1. **Capability entitlement**：Workspace 是否有资格注册和使用自定义域名；
2. **Domain trust**：所有权、DNS 入口、HTTPS、信誉与持续合规；
3. **Destination risk**：该域名下每条短链的所有可达目标安全性。

三层必须全部通过。官方域名与自定义域名在第三层没有任何差异。

### 6.2 权益模型

```text
capability = custom_domains
source = plan | manual_approval
status = requested | active | suspended | expired | revoked
domain_limit = positive integer
starts_at
expires_at = nullable
granted_by = nullable for plan, administrator ID for manual approval
support_ticket_id = required for manual approval
decision_reason = required for manual approval/suspension/revocation
```

- 当前最高套餐代码 `business` 在有效订阅和正常账户状态下自动获得 `source=plan`；
- 其他套餐可以提交工单，但工单只产生 `requested` 申请，不产生权限；
- `manual_approval` 必须源自已存在的 support request，并由不等同于工单处理权限的专用 entitlement 权限独立批准；工单的创建、回复、分类、关闭或管理员参与均不产生权限；
- plan feature 展示 JSON 不得成为授权边界；
- `domain_limit`、有效期和状态必须由结构化服务端数据执行；
- 任何来源均可因滥用、安全或所有权问题进入 `suspended` 或 `revoked`。

授权解析器是服务端唯一权威：它必须读取有效 plan source 与 manual source，计算当前允许的最高有效 `domain_limit`、起止时间和安全覆盖状态，并返回可审计的决策原因。UI cache、pricing card、support-ticket state、DNS/TLS 验证结果均不得替代该解析器。管理员 approval 写入、plan event 和每个业务检查点必须使用同一解析语义。

### 6.3 降级和撤销

普通套餐降级时，解析器必须先检查是否仍存在有效 `manual_approval`。若存在，按该来源及其 `domain_limit` 继续服务；若不存在，立即禁止新增、恢复、轮换或新绑定域名，现有有效域名进入七个自然日 `grace_period`。`grace_period` 是由降级时间和截止时间计算的展示/策略状态，权威 plan entitlement 在截止前仍为 `active`；到期且不存在另一有效来源时转为 `expired`，现有自定义域名 routing 返回品牌不可用页，不得回落或改写为官方域名。

滥用、欺诈、所有权丢失、DNS 指向失效、HTTPS 不安全、domain-risk 非 allow 或安全处置立即暂停相关能力或域名，不适用 billing grace。安全暂停优先于 plan/manual entitlement，并在 redirect request path 生效；后台异步 job 不能成为唯一防线。

### 6.4 域名生命周期

```text
entitlement check
→ hostname normalization / IDN punycode / public suffix validation
→ cross-workspace uniqueness / platform hostname exclusion
→ denylist and platform abuse history
→ DNS TXT ownership verification
→ DNS ingress target verification
→ HTTPS readiness
→ domain risk decision
→ active
→ periodic ownership, DNS, HTTPS and risk revalidation
```

拒绝 IP literal、wildcard、不可注册后缀、平台自有主机名和已绑定其他 Workspace 的主机名。Internationalized domain 必须保存/比较 normalized ASCII，同时保留经过安全处理的 display form。Entitlement、Ownership、DNS ingress、HTTPS 和 Domain Risk 必须是独立状态，不得以单个 `active` 模糊原因。每次周期复验必须记录 policy version、last checked、next due、result 和 evidence reference；任一状态缺失、过期、格式错误或非通过状态时不得继续解析到客户目标。

### 6.5 强制检查点

服务端在注册、开始验证、完成验证、激活、恢复、证书轮换、短链创建/编辑/绑定、域名限额、套餐续费/降级/到期、Workspace 暂停/恢复、管理员批准/暂停/撤销和 redirectengine 域名解析时检查权威状态。并发注册和绑定必须以数据库约束/事务保证 hostname 跨 Workspace 唯一与 `domain_limit` 不超发。任一直接 API、crafted/unsupported client、并发请求或缓存失效绕过均为 G6 硬失败。

Custom-domain redirect、safety interstitial、unavailable page 以及相关非 HTML 资源不得进入主站 sitemap，并必须按 Page-Level IA Route Registry 返回适用的 `X-Robots-Tag: noindex, nofollow` 或 HTML robots policy。此要求不把具体 path 的定义权从 Page-Level IA 移到本文。

---

## 7. 文件与目标地址安全

### 7.1 文件发布

```text
upload
→ extension allowlist
→ MIME and magic-byte verification
→ size and quota checks
→ randomized private storage name
→ quarantine
→ ClamAV scan
→ policy decision
→ publish only when safe
```

上传区、隔离区和已发布区必须分离并位于 Web Root 外。ClamAV 缺失、不健康、超时、病毒库超过策略允许时间或返回不确定结果时必须 fail closed；文件保持隔离，不得获得公共下载响应。

验收至少覆盖 EICAR、干净文件、超时、daemon 不可用、签名过期、重新扫描、隔离区直访、重启恢复、重复任务幂等和日志脱敏。

### 7.2 Destination risk

Destination-risk 扫描查询覆盖所有 `short_links`，不按官方/自定义域名过滤。目标集合包括主 destination、routing rules 和 A/B destinations。目标集合生成 fingerprint；任一可达目标变化必须使旧决策失效。

redirectengine 只接受绑定当前 fingerprint 的精确 `allow`。`review`、`block`、missing、malformed、unknown 或 stale 均不得访问用户目标，而是进入 GoJet safety surface。风险处理优先于智能路由、A/B、UTM、密码、到期、点击上限和一次性访问逻辑。

人工 override 必须包含权限、原因、操作者、时间和审计事件；目标或所有权变化使不再适用的 override 失效。外部信誉服务只能增强内置 provider，不能替代内置语义与平台策略。

### 7.3 SSRF

任何抓取目标、预览、风险或 webhook 发送器必须在 DNS 解析前后及每次重定向后阻止 loopback、private、link-local、metadata、保留 IPv4/IPv6、非常规端口策略违规和 DNS rebinding；响应体、跳转次数、下载大小和总时长必须受限。

---

## 8. 前端和六个 Surface

产品包含 Website、Auth、Docs、Workspace、Admin、Installer 六个 Surface，共享同一 Design System；Installer 是受限运维入口，在视觉上保持品牌一致，但不复用 Marketing Hero 或营销动效。

开发/构建目标栈：pnpm workspace、React 19、TypeScript strict、Vite、Tailwind CSS v4、TanStack Router/Query/Table、React Hook Form、Zod、Recharts、Motion for React、Sonner、cmdk、Lucide；Docs 使用 Astro Starlight 静态构建和 Pagefind。本节是当前 Greenfield 仓库的目标栈合同。

目录职责：

```text
frontend/apps/site       Website + Auth
frontend/apps/docs       static Docs
frontend/apps/workspace  customer workspace
frontend/apps/admin      governance/admin
frontend/packages/ui     GoJet components
frontend/packages/tokens authoritative generated tokens
frontend/packages/api-client/auth/charts/icons/domain/motion/utils
```

禁止业务页面直接复制上游组件示例、创造私有颜色/间距/动效或绕开统一 API client。

---

## 9. SEO 与 Indexation 发布合同

### 9.1 可抓取输出

所有需要索引的 Website、产品、解决方案、指南、法律和 Docs 页面必须生成 build-time static HTML 或等价预渲染 HTML。首次 HTTP 响应必须包含最终 title、description、canonical、robots、lang、H1、主要正文、真实 `<a href>` 内链和适用 JSON-LD，不得依赖客户端 JavaScript 补齐。

禁止 crawler-only 动态渲染、空 SPA shell、loading-only HTML 和客户端 200 错误页。理解页面所需 CSS、JS、图片和字体不得被 robots 阻止。

### 9.2 Index Matrix policy

具体 path、Route ID、locale variant 和 page purpose 由 Page-Level IA 的 route tree 与 registries（§§2-3、6-7、10、13）唯一管理；本文只规定下列不可被页面实现改变的策略类别。

| Route category | Policy | Sitemap contract |
|---|---|---|
| 经 IA 批准的 Website、产品、解决方案、定价、安全、指南、About 与 legal canonical content | `index,follow` | 只允许 canonical、indexable、successful URL |
| 经 IA 批准且 published 的英文/简中 Docs content | `index,follow` | 进入对应 locale child sitemap |
| Auth、Workspace、Admin、Installer、API、站内搜索、preview、private、operations | `noindex,nofollow`；非 HTML 使用 header | 禁止 |
| 官方/自定义 short redirect、File/Text share、Bio、safety/unavailable 和其他 UGC | `noindex`；按资源类型使用 HTML meta 或 `X-Robots-Tag` | 禁止 |

Bio 维持 noindex，除非后续批准的独立 capability 同时定义 owner opt-in、ownership、risk、quality 和 canonical-domain 规则。UGC 渲染链接使用 `rel="ugc nofollow"`。

### 9.3 URL、语言和 canonical

- 默认英文，简体中文 `/zh-CN/`；Docs 为 `/docs/en/` 与 `/docs/zh-CN/`；
- 每个本地化页面 self-canonical；hreflang 双向完整并包含适用的 `x-default`；
- 语言切换使用显式链接，不按 IP 强制跳转；
- HTML、sitemap、内链、Open Graph 和 hreflang 使用同一 canonical；
- URL 大小写、尾斜杠、查询参数和经 IA 批准的兼容 alias 只允许一条 Nginx/build 规范化规则；
- 永久迁移 301/308，临时迁移 302/307，缺失 404，永久移除 410；
- 禁止 redirect chain/loop、canonical chain、hreflang 指向 redirect。

### 9.4 Sitemap 与结构化数据

一个 sitemap index 可拆 Website、Docs、Guides 子 sitemap。只含 absolute HTTPS、UTF-8、canonical、indexable、200 URL；`lastmod` 来自真实内容变化。

Required JSON-LD：域名首页 `WebSite`，首页或 About `Organization`，层级页 `BreadcrumbList`。Article、SoftwareApplication、Product 必须逐页满足可见内容和当前搜索引擎资格；禁止虚假评分、评价、合规声明和隐藏内容。结构化数据只提供资格，不承诺展示或排名。

### 9.5 SEO 证据

G7 必须保存 raw HTML、响应头/状态、canonical/robots/lang/hreflang、唯一 title/description/H1、内链图、孤儿页报告、sitemap 校验、structured data 结果、重定向图、soft-404 报告、移动渲染、CWV 和 Nginx `X-Robots-Tag` 证据。生产环境补充 Search Console URL Inspection 和 sitemap report；发现或提交不等于已收录或有排名。

---

## 10. Accessibility、性能与缓存

### 10.1 Accessibility

目标为 WCAG 2.2 AA：全键盘、清晰 focus-visible、可见 label、正确 name/role/value、错误关联、状态不只依赖颜色、zoom/reflow 无信息损失、完整 reduced-motion 路径。对比度与颜色映射只引用 Design System `GJ-V5-DS-GREENFIELD-2026-08-20` §§2、13；geometry、focus、layout、motion 与 component measurements 只引用 §§5-9；viewport 与 evidence requirements 只引用 §14。本文不得复制第二套精确视觉值。

### 10.2 性能预算

| 范围 | 预算 |
|---|---|
| Website initial JS | ≤150 KB gzip |
| LCP | ≤2.5 s（p75 目标与 lab Gate） |
| INP | ≤200 ms |
| CLS | ≤0.1 |
| 图片 | AVIF + WebP fallback，explicit dimensions，responsive srcset |
| 字体 | self-host subset，避免不可见文本阻塞 |
| Workspace/Admin | route split；chart lazy；surface bundle 隔离 |

### 10.3 Cache

- hashed static assets：`public, max-age=31536000, immutable`；
- Marketing/Docs HTML：短缓存并可 revalidate；
- Auth、Workspace/Admin HTML、敏感 API、Payment、Security：`no-store` 或明确的 private no-store；
- 风险、不可用和 UGC 响应不得被共享缓存错误复用。

---

## 11. P00-P22 开发节点合同

P00-P22 必须按下表依赖有向无环图执行；表中列出的全部前置节点均达到 Exit Conditions 后，后续节点才能开始实现。可以在前置节点期间评审 interface contract，但不得把后续节点的实现结果作为前置节点的通过条件。

| Node | Required predecessors | Node | Required predecessors |
|---|---|---|---|
| P00 | none | P12 | P03, P04 |
| P01 | P00 | P13 | P06, P12 |
| P02 | P01 | P14 | P06, P12, P13 |
| P03 | P02 | P15 | P04, P14 |
| P04 | P03 | P16 | P05, P06, P09, P15 |
| P05 | P04 | P17 | P06, P12, P13, P14, P16 |
| P06 | P01, P05 | P18 | P17 |
| P07 | P05 | P19 | P18 and all public capability owners P05-P17 |
| P08 | P05 | P20 | P00-P19 |
| P09 | P01, P04 | P21 | P20 |
| P10 | P04 | P22 | P21 |
| P11 | P05 |  |  |

每个节点都必须满足以下统一执行规则：

- **Required Tests** 列出不可删减的最小场景。节点实现开始前，节点负责人必须在该节点证据目录提交 `test-plan.json`，每个 case 含 stable ID、precondition、exact command/driver、oracle、expected exit/status、evidence file 和 owner；禁止 placeholder、只写“manual check”或用 mock 代替合同要求的真实依赖。
- **Evidence** 统一存入 `artifacts/v5/Pxx/`，至少包含 `environment.json`、`source.json`、`commands.log`、`test-plan.json`、机器可读 results、`evidence-index.json` 和 reviewer-signed `review.md`。`source.json` 必须记录 `repository=Techshrr/GoJet`、`remote=https://github.com/Techshrr/GoJet.git`、`branch`、被测 `implementation_commit`、三个 specification document ID、build/toolchain versions；被测 commit 必须是当前仓库可解析的 40-hex SHA。
- **Exit Conditions** 只有在 Deliverables 存在、Required Tests 全通过、Evidence 可复核、所属 Gate reviewer 已签核、P0/P1 defect 为零且 `DECISION REQUIRED` 为零时才成立。截图不能替代服务端/数据证据。
- 任何节点不得以 mock 代替要求的真实 MySQL、Redis、worker、Nginx、ClamAV、local filesystem 或 browser evidence；production-only external channel 可以在 G13 owner-controlled environment 完成，但此前必须有非生产 contract test。
- 每个 UI 节点必须同时验证 default/loading/empty/error/read-only/permission/conflict/long-content/mobile/reduced-motion 中实际适用的状态；不得只验收 happy path。

### P00 — Specification Freeze & Repository Bootstrap

- **Goal:** 建立独立 Greenfield 仓库、冻结规格身份、能力矩阵、路由矩阵、架构决策和证据结构。
- **Entry Conditions:** `Techshrr/GoJet` 可写；`main` 为唯一初始集成分支；三个 specification 文件可读取且 document ID 一致。
- **In Scope:** repository bootstrap、目录骨架、Go module/workspace strategy、frontend workspace、migration numbering、API/error conventions、RBAC vocabulary、security invariants、Capability Matrix、Route Registry、ADR、evidence schema、CI skeleton。
- **Excluded Work:** 导入任何旧 GoJet 代码、Git objects/history、migration、build artifact、generated frontend、runtime config；根据旧实现反推产品行为。
- **Deliverables:** repo skeleton、`README.md`、Capability Matrix、Route Registry snapshot、ADR index、security invariants、toolchain lock、evidence schema。
- **Required Tests:** clean clone 可初始化；无 external GoJet code path；spec ID/cross-reference lint；目录与 package boundary lint；secret scan；license inventory。
- **Evidence:** `artifacts/v5/P00/`；除统一证据文件外包含 repository tree、spec checksum、dependency provenance 和 bootstrap review。
- **Exit Conditions:** 所有 REQUIRED/REMOVED/DEFERRED 能力有 owner 与 Gate；未决状态为零；当前仓库不依赖任何 prior GoJet repository 即可进入 P01。

### P01 — Engineering Foundation

- **Goal:** 建立独立构建、类型检查、API client、测试和证据框架。
- **Entry Conditions:** P00 通过，前端目标栈冻结。
- **In Scope:** workspace、apps/packages、strict TS、Router/Query、CI、code splitting。
- **Excluded Work:** 批量页面视觉迁移。
- **Deliverables:** 可重复 build/test/typecheck 命令和 artifact 目录规范。
- **Required Tests:** 各 app 独立构建；包边界；无循环依赖；无 production Node runtime。
- **Evidence:** `artifacts/v5/P01/`；除统一证据文件外必须包含：build logs、bundle graph、dependency report。
- **Exit Conditions:** G1 工程子项通过，后续节点可独立验证。

### P02 — Brand Foundation

- **Goal:** 冻结品牌、Logo、图标、影像和动效方向。
- **Entry Conditions:** P01 token/package 边界可用。
- **In Scope:** 原语、品牌语义、资产许可、light/dark、Jet Path。
- **Excluded Work:** 页面特有组件和未经实现的产品截图。
- **Deliverables:** Design System 品牌章节和 attribution contract。
- **Required Tests:** Logo safe area、颜色角色、资产来源和 reduced motion review。
- **Evidence:** `artifacts/v5/P02/`；除统一证据文件外必须包含：brand boards、license records、reference captures。
- **Exit Conditions:** 品牌值只在 Design System 一处权威定义。

### P03 — Design System

- **Goal:** 完成 token、组件、状态、密度、响应式和可访问性基础。
- **Entry Conditions:** P02 批准。
- **In Scope:** primitives、semantic tokens、controls、overlay、navigation、data、feedback、layout。
- **Excluded Work:** 业务 API 和页面级私有样式。
- **Deliverables:** tokens、组件合同、Story/evidence pages。
- **Required Tests:** focus、keyboard、contrast、dark、responsive、motion/reduced motion。
- **Evidence:** `artifacts/v5/P03/`；除统一证据文件外必须包含：`artifacts/v5/P03/components/`。
- **Exit Conditions:** G2 通过；业务页面不得补造基础组件。

### P04 — Product Shells

- **Goal:** 建立 Website、Auth、Docs、Workspace、Admin 和 Installer 外壳。
- **Entry Conditions:** P03 通过。
- **In Scope:** header/sidebar/nav/breadcrumb/content/error/notification boundaries。
- **Excluded Work:** 模块深层 CRUD。
- **Deliverables:** 六类 shell，以及按 Design System §§6、14 和 Page-Level IA §16 生成的 responsive screenshot/workflow evidence。
- **Required Tests:** route transition、focus、overflow、navigation、no reload、no layout jump，并覆盖 Page-Level IA §15 page-state applicability matrix。
- **Evidence:** `artifacts/v5/P04/`；除统一证据文件外必须包含：按 Design System §14 与 Page-Level IA §16 生成的 capture matrix；本文不复制 viewport 数值。
- **Exit Conditions:** Shell 路由和状态边界可供垂直切片使用。

### P05 — Links Vertical Slice

- **Goal:** 以 Links 完成第一套正式端到端产品模式。
- **Entry Conditions:** P04 通过；CAP-LINKS contract 已冻结。
- **In Scope:** list/search/filter/create/detail/edit/analytics/routing/A-B/UTM/access/QR/history/delete/bulk/RBAC/mobile。
- **Excluded Work:** 其他模块批量复制未验收模式。
- **Deliverables:** 完整 Links UI 与真实 API 集成。
- **Required Tests:** 主目标/路由/A-B risk fingerprint、allow/review/block/missing、权限、并发写、redirect status。
- **Evidence:** `artifacts/v5/P05/`；除统一证据文件外必须包含：API、Redis、browser、audit、safety surface captures。
- **Exit Conditions:** G2/G3/G4/G5/G6 首次完整通过。

### P06 — Custom Domains

- **Goal:** 实现 `CAP-DOMAIN-ENTITLEMENT`、验证、绑定、revalidation 和安全暂停。
- **Entry Conditions:** P05 destination-risk parity 已证明；P00 的 entitlement schema/permission contract 已批准；P01 migration、API 与并发测试 harness 可用。不得依赖尚未执行的 P13/P14/P17 交付。
- **In Scope:** entitlement store/resolver；active `business` 自动来源；带既有 support ticket reference 的 `manual_approval` 核心 API；`domain_limit` 原子执行；七日正常降级 grace；TXT、ingress DNS、HTTPS、domain risk、周期复验与 redirect-time enforcement。P14 后续补齐申请 UX/mail，P17 后续补齐管理员 queue/UI，但不得改变本节点的权威模型。
- **Excluded Work:** 以客服回复、前端按钮或 feature JSON 直接授权。
- **Deliverables:** entitlement store/API、domain state machine、redirect enforcement、Page-Level IA state/API contracts 和用户 domain flow；管理员 entitlement queue/UI 由 P17 交付。
- **Required Tests:** 无资格 direct API/crafted client 拒绝；并发 limit 不超发；跨 tenant hostname 冲突；工单存在但无独立 approval 仍拒绝；business/manual source coexistence；降级 grace/到期；abuse/ownership loss immediate suspension；DNS/TLS/risk revalidation；官方/自定义 primary/routing/A-B destination-risk parity。
- **Evidence:** `artifacts/v5/P06/`；除统一证据文件外必须包含：`artifacts/v5/P06/` request/response、DB、audit、DNS/TLS 和 browser records。
- **Exit Conditions:** 所有检查点服务端强制，G6 domain suite 通过。

### P07 — Analytics

- **Goal:** 实现并呈现点击事件、聚合、留存、恢复和转换数据。
- **Entry Conditions:** P05 redirect/click 链路稳定。
- **In Scope:** worker、reconciler、overview/detail filters、campaign conversion、time zone。
- **Excluded Work:** 虚构预测指标。
- **Deliverables:** analytics UI/API 与恢复运行手册。
- **Required Tests:** Redis event、MySQL aggregate、reconcile idempotency、empty/partial/stale states。
- **Evidence:** `artifacts/v5/P07/`；除统一证据文件外必须包含：worker logs、known event totals、UI captures。
- **Exit Conditions:** 数据对账一致且 G3/G9 通过。

### P08 — QR

- **Goal:** 完成 QR 创建、预览、下载和资源治理。
- **Entry Conditions:** Links vertical slice 组件可复用。
- **In Scope:** QR CRUD、目标关联、格式、真实渲染、权限和配额。
- **Excluded Work:** 未验证的视觉特性宣传。
- **Deliverables:** QR pages、API integration、download evidence。
- **Required Tests:** 扫码可达、不同 viewport、权限、删除、错误输出。
- **Evidence:** `artifacts/v5/P08/`；除统一证据文件外必须包含：generated assets、scanner result、browser/API logs。
- **Exit Conditions:** CAP-QR 完整通过并进入 G10。

### P09 — Files and Mandatory ClamAV

- **Goal:** 建立不可绕过的隔离、扫描和发布链路。
- **Entry Conditions:** P01 test harness 与 P04 file UI state shell 可用；native private/quarantine/published storage 和 ClamAV test endpoint 可在隔离环境验证。
- **In Scope:** allowlist、MIME/magic、quota、random name、quarantine、scan、publish、retention、download auth。
- **Excluded Work:** 以客户端扫描或扩展名替代 ClamAV。
- **Deliverables:** file state machine、health checks、installer preflight、admin status。
- **Required Tests:** EICAR、clean、timeout、daemon down、signature stale、indeterminate response、rescan、duplicate claim、service restart、direct quarantine/public path access、Installer/upgrade hard fail。
- **Evidence:** `artifacts/v5/P09/`；除统一证据文件外必须包含：`artifacts/v5/P09/clamav/` 和 HTTP/file permission records。
- **Exit Conditions:** 所有不确定状态 fail closed，G6 file suite 通过。

### P10 — Text Sharing

- **Goal:** 实现 Text 资源创建、访问、管理和过期控制。
- **Entry Conditions:** Resource pattern 和 public noindex 策略可用。
- **In Scope:** CRUD、权限、公开响应、状态、copy/download、abuse entry。
- **Excluded Work:** 公开内容进入主站 sitemap。
- **Deliverables:** Workspace 和 public text surfaces。
- **Required Tests:** auth/private/public/expired/not-found/noindex/status codes。
- **Evidence:** `artifacts/v5/P10/`；除统一证据文件外必须包含：API/browser/header records。
- **Exit Conditions:** CAP-TEXT 和 G7 UGC policy 通过。

### P11 — Bio

- **Goal:** 实现 Bio 页面编辑、发布和链接管理，维持默认 noindex。
- **Entry Conditions:** Public resource and Design System patterns 可用。
- **In Scope:** editor、preview、publish、link safety、UGC rel、responsive。
- **Excluded Work:** 未批准的 owner opt-in index capability。
- **Deliverables:** Bio Workspace/public surfaces。
- **Required Tests:** ownership、risk-blocked link、mobile、noindex、sitemap exclusion。
- **Evidence:** `artifacts/v5/P11/`；除统一证据文件外必须包含：browser/headers/sitemap diff。
- **Exit Conditions:** CAP-BIO 完整且不扩大索引面。

### P12 — Workspace, Members and Organization

- **Goal:** 完成 Workspace、成员、角色、邀请、Campaign 和 Tag 治理。
- **Entry Conditions:** P04 shell、P03 data components 通过。
- **In Scope:** membership lifecycle、RBAC、invite、switcher、settings、campaign/tag、`CAP-NOTIFICATIONS` core store/read-state/API/UI，以及后续模块可复用的 notification producer contract。
- **Excluded Work:** 前端权限替代服务端检查。
- **Deliverables:** pages、permission matrix、tenant regression suite、notification center API/UI、event category schema 和 deep-link contract。
- **Required Tests:** owner/admin/member/viewer、cross-workspace、expired invitation、last-owner protection、notification read/unread/mark-all-read、dedupe、deep-link authorization、secret redaction、API partial/offline。
- **Evidence:** `artifacts/v5/P12/`；除统一证据文件外必须包含：API/DB/audit/browser results。
- **Exit Conditions:** G3/G5/G6 tenant、RBAC 与 notification core 通过。

### P13 — Billing, Payments and Entitlements

- **Goal:** 实现计费/支付/FX，并为能力提供权威 entitlement。
- **Entry Conditions:** P06 entitlement resolver/domain core 与 P12 Workspace lifecycle 已通过；Billing/Payment contract inventory 已锁定。
- **In Scope:** plans、quota、orders、transactions、callbacks、FX、business domain entitlement、downgrade grace。
- **Excluded Work:** 前端 price card 或 features JSON 直接授权。
- **Deliverables:** billing lifecycle、entitlement resolver、idempotent callback、invoice/payment UI。
- **Required Tests:** paid/failed/refund、duplicate callback、currency、upgrade/downgrade/expiry、manual source coexistence。
- **Evidence:** `artifacts/v5/P13/`；除统一证据文件外必须包含：redacted payment events、DB state、audit、browser。
- **Exit Conditions:** G3/G6/G10 billing 场景通过。

### P14 — Support Tickets and Mail

- **Goal:** 实现工单/邮件并支持域名资格申请入口。
- **Entry Conditions:** P06 entitlement/request core、P12 Workspace ownership 和 P13 plan lifecycle 已通过；不得依赖尚未执行的 P17 管理员 UI。
- **In Scope:** ticket/thread/attachment policy、mail templates/queue/retry、custom-domain request topic/linkage，以及 request 与 entitlement decision 的不可混淆状态。P17 只增加独立审批 UI/permission，不得让 ticket mutation 获得授权副作用。
- **Excluded Work:** 创建或回复工单即授权。
- **Deliverables:** support UI、admin ticket UI、mail events、entitlement request linkage。
- **Required Tests:** requester ownership、attachment safety/ClamAV、Turnstile、mail retry/idempotency、ticket create/reply/close 后仍无 entitlement、request-to-ticket linkage integrity。
- **Evidence:** `artifacts/v5/P14/`；除统一证据文件外必须包含：ticket/API/mailworker/audit records。
- **Exit Conditions:** CAP-TICKETS/CAP-MAIL 完整通过且 request 与 approval 分离。

### P15 — Authentication, OAuth and Account

- **Goal:** 完成注册、验证、登录、重置、社交登录、Session 和账户安全。
- **Entry Conditions:** P04 Auth shell 与 P14 mail/Turnstile request paths 已通过；security-header policy 可测试。
- **In Scope:** email codes、verification、forgot/reset、OAuth callback、session list/revoke、MFA UI contracts。
- **Excluded Work:** 正式 token 存 localStorage。
- **Deliverables:** auth pages/API、session policies、account security UI。
- **Required Tests:** CSRF、Origin、rate limit、token expiry/reuse、OAuth state、session revoke。
- **Evidence:** `artifacts/v5/P15/`；除统一证据文件外必须包含：HTTP headers、API/browser/security logs。
- **Exit Conditions:** G3/G5/G6 auth suite 通过。

### P16 — Trust, Destination Risk and Abuse

- **Goal:** 实现目标风险、域名风险与滥用治理。
- **Entry Conditions:** P05 fingerprint、P06 domain state、P09 file state 与 P15 auth/session controls 已通过。
- **In Scope:** scans/rescans、semantic provider、review queue、overrides、reports、domain reputation/revalidation。
- **Excluded Work:** ClamAV 充当 URL classifier；外部 provider 成为唯一控制。
- **Deliverables:** worker/queue/admin review/public safety surfaces。
- **Required Tests:** official/custom parity、all target variants、SSRF、provider failure、manual override invalidation、abuse suspension。
- **Evidence:** `artifacts/v5/P16/`；除统一证据文件外必须包含：risk records、Redis keys、audit、browser/interstitial captures。
- **Exit Conditions:** missing/unknown/review/block 均 fail closed，G6 通过。

### P17 — Admin, Permissions and Audit

- **Goal:** 完成管理员运营、安全、审批、服务和审计能力。
- **Entry Conditions:** P06 domain core、P12 RBAC、P13 entitlement/billing、P14 request/ticket 和 P16 risk actions 已通过。
- **In Scope:** administrators/roles、users/workspaces/resources/files/risk/abuse/tickets/plans/payments/mail/services/settings/audit；专用 domain-entitlement approve/deny/suspend/revoke permission；`CAP-API-KEYS` lifecycle；`CAP-USER-WEBHOOKS` signing/retry/disable/SSRF controls。
- **Excluded Work:** 所有管理员共享无限权限。
- **Deliverables:** admin route set、permission catalog、domain-entitlement queue/decision API and UI、API-key service/UI、outbound-webhook service/worker/UI、high-risk confirmations、immutable audit views。所有新增 route/path 以 Page-Level IA 为准。
- **Required Tests:** permission denial；ticket-manager cannot approve；reason required；approve/deny/suspend/revoke/restore audit；API-key secret-once/rotation/revocation/scope/expiry；webhook signature/rotation/retry/idempotency/disable/SSRF/DNS-rebinding；session/MFA；secret redaction。
- **Evidence:** `artifacts/v5/P17/`；除统一证据文件外必须包含：role matrix、API/browser/audit records。
- **Exit Conditions:** 高风险动作可归因、可审查、不可越权；`CAP-DOMAIN-ENTITLEMENT` 管理链与 `CAP-API-KEYS`、`CAP-USER-WEBHOOKS` 分别通过 G3/G6，且均以当前仓库证据证明完成。

### P18 — Docs and Multilingual Discovery

- **Goal:** 构建英文/简中静态 Docs、搜索和 API 参考。
- **Entry Conditions:** P17 已完成全部 V5 API 状态与管理员能力；P04 Docs shell 和已验证 capability/route implementation matrix 可用。
- **In Scope:** Page-Level IA 定义的英文/简中 Docs route families、Pagefind、breadcrumbs、TOC、next/previous、native self-hosting guide。
- **Excluded Work:** Docker production guide；把 REQUIRED 写成现有能力。
- **Deliverables:** static docs build、sitemap child、hreflang sets、content owners。
- **Required Tests:** raw HTML、search、broken links、code blocks、canonical/hreflang、mobile。
- **Evidence:** `artifacts/v5/P18/`；除统一证据文件外必须包含：build output、crawl report、screenshots。
- **Exit Conditions:** Docs 无孤儿页、无错误部署指令、G7 通过。

### P19 — Website and Technical SEO

- **Goal:** 完成真实产品内容、品牌表达、转换路径和索引能力。
- **Entry Conditions:** P18 Docs/locale discovery 已通过，P05-P17 的全部 public capability owner 已冻结可陈述状态；禁止假 Dashboard。
- **In Scope:** Page-Level IA §3 中批准为 indexable 的 Website Route Registry、metadata、JSON-LD、sitemaps 和 internal links。本文不新增 path。
- **Excluded Work:** thin keyword pages、crawler-only rendering、虚假评分/合规承诺。
- **Deliverables:** static Website、SEO matrix、asset attribution、social cards。
- **Required Tests:** raw HTML、status codes、canonical、hreflang、structured data、orphan links、CWV。
- **Evidence:** `artifacts/v5/P19/`；除统一证据文件外必须包含：crawl/validator/lighthouse-like lab/browser captures。
- **Exit Conditions:** G4/G5/G7/G8/G9 通过。

### P20 — Whole Product Verification

- **Goal:** 对全部 Surface 和能力执行统一回归。
- **Entry Conditions:** P00-P19 全部达到 Exit Conditions；candidate implementation commit、schema catalog、frontend build 和 evidence index 已冻结。
- **In Scope:** capability matrix closure、P0 real workflows、cross-surface consistency、failure recovery。
- **Excluded Work:** 新增未排期功能。
- **Deliverables:** release candidate evidence index、defect closure ledger。
- **Required Tests:** register→verify→login→link→redirect→analytics→QR→file→text→bio→domain→ticket→billing→notification→admin。
- **Evidence:** `artifacts/v5/P20/`；除统一证据文件外必须包含：`artifacts/v5/P20/` 全链路时间线和关联 IDs。
- **Exit Conditions:** G0-G10 全部通过，无 P0/P1 未关闭缺陷。

### P21 — Native Release Package

- **Goal:** 实现 `CAP-NATIVE-INSTALL` 与 `CAP-NATIVE-ONLY-RELEASE` 的无 Docker/Node runtime 原生发布包。
- **Entry Conditions:** P20 通过，版本冻结。
- **In Scope:** `CAP-NATIVE-INSTALL` 原生入口、PHP Installer、systemd apply 与 upgrade 验证；`CAP-NATIVE-ONLY-RELEASE` 的 eight binaries、migrations、static frontend、Nginx/systemd、checksums、SBOM、manifest、upgrade/rollback 和生产 Docker 路径移除。
- **Excluded Work:** Docker/Compose、node_modules、开发服务器、生产凭据。
- **Deliverables:** versioned archive、checksum、SBOM、package manifest、runbooks。
- **Required Tests:** clean extraction；八 binary version/commit；Greenfield migration catalog consistency、编号唯一性、空库全量迁移与回滚边界；manifest allowlist/denylist；no Docker/Compose/Node runtime/secret；Nginx/systemd render；checksum/SBOM；upgrade/rollback runbook static validation。
- **Evidence:** `artifacts/v5/P21/`；除统一证据文件外必须包含：`artifacts/v5/P21/` package inventory。
- **Exit Conditions:** G11 证明 `CAP-NATIVE-INSTALL` 完整且 `CAP-NATIVE-ONLY-RELEASE` 完整，安装候选不可变。

### P22 — Fresh Install and Production Candidate

- **Goal:** 用 `CAP-NATIVE-ONLY-RELEASE` 候选在干净主机完成安装、恢复、升级、回滚和生产验证，同时验证 `CAP-NATIVE-INSTALL`。
- **Entry Conditions:** P21 archive checksum 固定。
- **In Scope:** `CAP-NATIVE-INSTALL` Installer/apply/upgrade 行为；`CAP-NATIVE-ONLY-RELEASE` 的 Nginx、PHP 8.3 FPM、MySQL 8.x、Redis、ClamAV、local storage、systemd、TLS、backup/restore 和 rollback。
- **Excluded Work:** 已配置开发机、Docker、手工补丁绕过 installer。
- **Deliverables:** immutable fresh-install record、dependency matrix、eight-service health、production smoke、backup/restore proof、upgrade record、rollback record、installer lock proof 和 Release Owner decision。
- **Required Tests:** 每个 mandatory dependency 的单独 hard-fail case；empty database migrations；八 unit enable/start/restart/reboot recovery；Nginx test/reload/revert；DB backup/restore；Redis flush/reconnect/risk-cache rebuild；EICAR/clean file；upgrade failure recovery；database-required rollback；真实 P0 user/admin/payment/mail/OAuth/Turnstile/domain/SEO flows。
- **Evidence:** `artifacts/v5/P22/`；除统一证据文件外必须包含：`artifacts/v5/P22/` terminal logs、HTTP records、service status、screenshots。
- **Exit Conditions:** G12/G13 证明 `CAP-NATIVE-INSTALL` 完整且 `CAP-NATIVE-ONLY-RELEASE` 的 fresh-install/production/rollback 合同完整，并由 Release Owner 批准。

---

## 12. G0-G13 验收 Gate

统一证据根目录：`artifacts/v5/gates/Gxx/`。每个 Gate 必须包含 `environment.json`、`source.json`、`commands.log`、机器可读 test/crawl/scan results、`evidence-index.json` 和由下列 Accountable Roles 签名的 `decision.json`。只有 Pass Criteria 全满足且 Hard Failures 为零才能记为 PASS；conditional pass 不允许发布。证据必须可复核、带版本/时间/环境标识并脱敏。

### G0 — Scope and Traceability

- **Execution Stage:** P00，P20 复核。
- **Pass Criteria:** Capability Matrix、Route Registry、源证据、状态和 owner 完整。
- **Hard Failures:** P0 丢失；REQUIRED 无当前仓库实现证据却宣称完成；节点退出仍有未决项。
- **Evidence Path:** `artifacts/v5/gates/G0/traceability/`；必须包含 capability/route/specification diff、status/owner coverage。
- **Accountable Roles:** Product Owner + Backend Lead。

### G1 — Native Architecture

- **Execution Stage:** P01、P21、P22。
- **Pass Criteria:** `CAP-NATIVE-INSTALL` 的 native 入口、PHP Installer 与八服务 systemd apply 完整通过；`CAP-NATIVE-ONLY-RELEASE` 独立构建；Nginx 唯一入口；依赖版本正确。
- **Hard Failures:** `CAP-NATIVE-INSTALL` native 流程缺失或失败；`CAP-NATIVE-ONLY-RELEASE` 仍允许 production Docker/Compose/Node/PM2；循环依赖；PHP 承担业务 API。
- **Evidence Path:** `artifacts/v5/gates/G1/native-architecture/`；必须包含 dependency graph、八 unit、Nginx config test、runtime port/process inventory。
- **Accountable Roles:** Platform Lead。

### G2 — Design System

- **Execution Stage:** P03，所有 UI 节点复核。
- **Pass Criteria:** 只使用权威 token；light/dark、focus、density、responsive、motion 完整。
- **Hard Failures:** 页面私有颜色/间距；安全状态只靠颜色；冲突 motion 值。
- **Evidence Path:** `artifacts/v5/gates/G2/design-system/`；必须包含 token lint、component captures、contrast report。
- **Accountable Roles:** Design System Owner + Accessibility Reviewer。

### G3 — Functional and API Conformance

- **Execution Stage:** 每个业务节点，P20 汇总。
- **Pass Criteria:** 真实 API/MySQL/Redis/worker/mail/storage 流程满足 capability contract。
- **Hard Failures:** mock 冒充；跨租户；缺失 REQUIRED 能力；错误状态码。
- **Evidence Path:** `artifacts/v5/gates/G3/functional-api/`；必须包含 test logs、request/response、DB/Redis/worker/storage proof。
- **Accountable Roles:** Backend Lead + QA Lead。

### G4 — Browser and Responsive

- **Execution Stage:** P04-P20。
- **Pass Criteria:** Design System §§6、14 的 breakpoint/viewport/evidence contract 完成 Page-Level IA §16 screenshot/workflow matrix，且真实 browser navigation/state contract 通过。
- **Hard Failures:** Page-Level IA §15 page-state matrix 中存在未覆盖的适用状态；horizontal overflow、pageerror、console error、nav reload、clipped text、focus broken、layout jump。
- **Evidence Path:** `artifacts/v5/gates/G4/browser-responsive/`；必须包含 automated browser logs、Design System §14 与 Page-Level IA §16 定义的 screenshot matrix、必要视频。
- **Accountable Roles:** Frontend Lead + QA。

### G5 — Accessibility

- **Execution Stage:** P03-P20。
- **Pass Criteria:** WCAG 2.2 AA、axe、keyboard、labels、focus、contrast、zoom、reduced motion。
- **Hard Failures:** 键盘阻塞；不可见焦点；无 label；颜色唯一语义；严重对比度失败。
- **Evidence Path:** `artifacts/v5/gates/G5/accessibility/`；必须包含 axe results、keyboard script、screen-reader/contrast/zoom/reduced-motion evidence。
- **Accountable Roles:** Accessibility Reviewer。

### G6 — Security

- **Execution Stage:** 所有安全敏感节点，P20/P22 复核。
- **Pass Criteria:** Auth/RBAC/tenant/CSRF/session/CSP/Turnstile/rate/SSRF/secrets/audit 通过；ClamAV 全部不确定态 fail closed；domain entitlement/ownership/DNS/HTTPS/risk 分层强制；官方和自定义 hostname 的 primary/routing/A-B destination-risk 完全一致。
- **Hard Failures:** ClamAV 缺失/不健康/超时/过期/indeterminate 仍安装或发布；risk missing/stale/malformed/review/block 仍达目标；无 entitlement direct API/并发请求可注册或绑定；工单自动授权；跨 Workspace；downgrade/abuse policy 错误；敏感日志。
- **Evidence Path:** `artifacts/v5/gates/G6/security/`；必须包含 EICAR/ClamAV failure matrix、direct-API denial、manual approval audit、domain limit/grace/revalidation、risk parity、fingerprint invalidation。
- **Accountable Roles:** Security Reviewer + Backend Lead。

### G7 — SEO and Indexation

- **Execution Stage:** P18/P19/P20/P22。
- **Pass Criteria:** raw HTML、metadata、canonical、hreflang、status、internal links、sitemap、structured data、noindex matrix 正确。
- **Hard Failures:** empty shell；200 soft 404；UGC/private URL 入 sitemap；redirect/canonical chain；孤儿 index 页。
- **Evidence Path:** `artifacts/v5/gates/G7/seo/`；必须包含 crawler output、raw HTTP、headers/status、sitemap/JSON-LD validators、Search Console production evidence。
- **Accountable Roles:** SEO Owner + Frontend Lead。

### G8 — Visual Quality

- **Execution Stage:** P03-P20。
- **Pass Criteria:** spacing、alignment、responsive、dark、image、icon、motion 符合 Design System。
- **Hard Failures:** placeholder icon、虚构产品 UI、随机插画、破损图片、失控 motion。
- **Evidence Path:** `artifacts/v5/gates/G8/visual/`；必须包含 Design System §§1-14 conformance 与 Page-Level IA §16 screenshot matrix/diffs。
- **Accountable Roles:** Design Lead。

### G9 — Performance

- **Execution Stage:** P04、P07、P19、P20。
- **Pass Criteria:** bundle/CWV/image/font/cache 预算达标。
- **Hard Failures:** Website 加载 Workspace bundle；LCP/INP/CLS 超预算；未定尺寸主要图片；长主线程任务。
- **Evidence Path:** `artifacts/v5/gates/G9/performance/`；必须包含 bundle report、lab trace、field data when available、cache/image/font headers。
- **Accountable Roles:** Performance Owner。

### G10 — Full-stack P0

- **Execution Stage:** P20。
- **Pass Criteria:** 完整真实用户与管理员时间线成功，关联 ID 可追踪。
- **Hard Failures:** 任一 P0 环节依赖 mock、跳过 worker/redirect/payment/risk、证据不可关联。
- **Evidence Path:** `artifacts/v5/gates/G10/full-stack-p0/`；必须包含 correlated end-to-end timeline、API/DB/Redis/worker/browser records。
- **Accountable Roles:** QA Lead + Product Owner。

### G11 — Native Package

- **Execution Stage:** P21。
- **Pass Criteria:** `CAP-NATIVE-INSTALL` 的 Installer/systemd apply/native upgrade 资产与行为完整；`CAP-NATIVE-ONLY-RELEASE` 的八 binaries、完整 migration catalog、static frontend、PHP Installer、Nginx/systemd/native helpers、runbooks、checksums、SBOM 和 manifest 完整且相互匹配。
- **Hard Failures:** `CAP-NATIVE-INSTALL` 所需 native 资产缺失或不可执行；`CAP-NATIVE-ONLY-RELEASE` archive 含 Docker/Compose/Node runtime/`node_modules`/生产 secret；缺任一服务或 migration；checksum/SBOM/manifest 不一致；package 依赖构建主机残留。
- **Evidence Path:** `artifacts/v5/gates/G11/native-package/`；必须包含 archive inventory、allowlist/denylist、eight binary versions、checksum/SBOM verification。
- **Accountable Roles:** Release Engineer。

### G12 — Fresh Install

- **Execution Stage:** P22 clean host。
- **Pass Criteria:** exact P21 archive 验证 `CAP-NATIVE-INSTALL` Installer/apply 行为，并在声明支持的 clean host 从零满足 `CAP-NATIVE-ONLY-RELEASE` 的 preflight、migration、local-storage permissions、Nginx、Installer lock 和八 unit enable/start/reboot recovery，无手工补丁。
- **Hard Failures:** `CAP-NATIVE-INSTALL` native 流程失败；`CAP-NATIVE-ONLY-RELEASE` 的任一 mandatory dependency 缺失/错误仍继续；ClamAV 不健康仍完成；复用旧 DB/config/state；手工修补；服务未 enable；Installer 未锁定。
- **Evidence Path:** `artifacts/v5/gates/G12/fresh-install/`；必须包含 clean-host identity、preflight failure cases、installer/migration/unit/Nginx/HTTP/EICAR logs。
- **Accountable Roles:** Platform Lead + QA。

### G13 — Production Validation and Rollback

- **Execution Stage:** P22 owner-controlled production。
- **Pass Criteria:** owner-controlled production 验证 `CAP-NATIVE-INSTALL` native upgrade/失败恢复，并完成 `CAP-NATIVE-ONLY-RELEASE` 的八服务 restart、Nginx atomic reload/revert、DB backup/restore、Redis reconnect/risk-cache rebuild、ClamAV、Mail、OAuth、Turnstile、真实支付渠道、SEO production checks，以及包含必要数据库恢复的 upgrade/rollback。
- **Hard Failures:** `CAP-NATIVE-INSTALL` native upgrade/失败恢复失败；`CAP-NATIVE-ONLY-RELEASE` 无可执行 rollback；只切 binary 而 schema 不兼容；backup 未 restore-test；数据不可恢复；安全依赖降级；生产验证使用测试替身；rollback 后任一 P0 或八服务不健康。
- **Evidence Path:** `artifacts/v5/gates/G13/production-rollback/`；必须包含 redacted production runbook output、backup restore proof、upgrade/rollback timeline、post-rollback P0、Search Console diagnostics。
- **Accountable Roles:** Release Owner + Security Reviewer + Platform Lead。

---

## 13. 证据与缺陷关闭规则

证据目录必须保存：环境清单、版本、执行命令、开始/结束时间、结果、失败详情、关联 issue、reviewer 和批准时间。截图不能替代服务端授权证据；数据库截图不能替代用户可见流程；单元测试不能替代 G10/G12 的真实链路。

P0/P1 缺陷必须关闭并重新运行受影响 Gate。P2 缺陷可延期的前提是不会违反本合同的 MUST、安全、可访问性或索引策略，并有 owner、日期和非发布阻塞理由。

---

## 14. Release Definition of Done

只有以下全部成立才允许发布：

- P00-P22 全部达到 Exit Conditions；
- G0-G13 全部由指定角色通过；
- 八个 Go 程序及所有 REQUIRED 业务能力完整通过；
- 正式包不含 Docker/Compose/Node runtime；
- Web Installer 在依赖缺失、尤其 ClamAV 不健康时拒绝安装；
- Custom domain entitlement、所有权、DNS、HTTPS、域名风险和目标风险不可绕过；
- Website/Docs raw HTML、canonical、hreflang、sitemap、状态码和 noindex 矩阵正确；
- Desktop/Tablet/Mobile、WCAG 2.2 AA 和性能预算通过；
- fresh install、upgrade、backup/restore、rollback 和 production validation 有证据；
- 三份优化规范、Capability Matrix、Route Registry 和实现一致。

---

## 15. 本文档任务边界

本文定义当前 Greenfield 仓库的实现合同。Specification 文档本身不等于功能完成；所有源代码、数据库、前端、部署与发布变更都必须在 `Techshrr/GoJet` 中按 P00-P22 与 G0-G13 执行并留下当前仓库证据。
