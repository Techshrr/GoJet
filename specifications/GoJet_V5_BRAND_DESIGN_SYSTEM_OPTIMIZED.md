# GoJet V5 Brand & Design System — Greenfield Visual and Interaction Contract

**Document ID:** `GJ-V5-DS-GREENFIELD-2026-08-20`  
**Status:** APPROVED VISUAL, INTERACTION AND UX CONTRACT  
**Product contract:** `GoJet V5`  
**Implementation repository:** `Techshrr/GoJet`  
**Implementation remote:** `https://github.com/Techshrr/GoJet.git`  
**Implementation branch:** `main`  
**Development model:** `GREENFIELD / SPECIFICATION-FIRST`  
**Specification pack:** `specifications/`  
**Applies to:** Website, Auth, Docs, Workspace, Admin, Installer and public safety/resource surfaces  
**Master contract:** `GJ-V5-MP-GREENFIELD-2026-08-20`

> 本文是 GoJet V5 精确视觉值、交互状态、跨页面 UX pattern 和视觉验收值的唯一权威来源。Master Plan 与 Page-Level IA 只能引用本文 token/pattern，不得复制或重新定义颜色、尺寸、断点、密度、动效、组件状态表现或截图视口。
>
> 本文定义 Greenfield 目标合同，不引用旧 GoJet UI 作为完成证据。任何组件、token 或交互只有在当前仓库中实现并通过所属 Gate 后才可视为可用。

---

## 1. 权威边界、规范用语与机器映射

### 1.1 权威边界

- 本文唯一拥有 primitive/semantic/component token 的名称与精确值。
- Master Plan 拥有能力状态、安全不变量、性能发布预算和 Gate；本文只定义这些状态如何可见、可操作、可访问和如何收集视觉证据。
- Page-Level IA 拥有路由、页面任务和页面状态适用性；页面只能组合本文组件与 token。
- `MUST / 必须`、`MUST NOT / 禁止`、`SHOULD / 应当`、`MAY / 可以` 的含义继承 Master Plan §1.1。
- `REQUIRED` 表示 release target；UI 文案、截图和演示只有在当前仓库对应 Gate 通过后才能描述成已可用能力。

### 1.2 Token 命名和输出

- canonical 名称使用小写点分层：`{category}.{role}.{variant}`。为兼容既有验收词保留的 trust alias 使用本文列出的连字符名称。
- primitive token 只储存原始值；业务组件禁止直接引用 primitive。
- semantic token 表示跨组件角色；component token 只能引用 semantic、geometry、type 或 motion token。
- light/dark 使用相同 semantic 名称切换映射；页面禁止自建 dark override。
- 单一源文件目标为 `frontend/packages/tokens/src/tokens.json`，生成 CSS custom properties、TypeScript readonly map 和设计工具变量。生成产物不得手工编辑。
- JSON 中长度统一输出为 `px`，时间统一输出为 `ms`，颜色输出为大写六位十六进制或明确的 `rgba()`；仅 `color.transparent` 使用 `transparent`；无单位数值保持 number。
- token 删除或重命名属于破坏性变更；必须提供别名迁移、使用点清单、视觉差异和 G2 复核。相同名称不得在第二处重新赋值。
- `frontend/apps/**` 与 `frontend/packages/ui/**` 禁止出现本文未登记的十六进制颜色、`rgb/rgba`、间距、圆角、阴影、z-index、breakpoint 或 animation duration。

### 1.3 主题解析

`data-theme="light"` 与 `data-theme="dark"` 是显式值；`data-theme="system"` 在首个可执行脚本运行前由内联、无网络依赖的主题引导代码解析为 light/dark，避免主题闪烁。Website 默认 light；Workspace/Admin 默认 system；用户覆盖值持久化。SSR/静态 HTML 在脚本失败时必须仍可读。

---

## 2. 颜色 token

### 2.1 Primitive colors

Primitive 只允许在本表定义。

| Token | Value |
|---|---|
| `color.transparent` | `transparent` |
| `color.white` | `#FFFFFF` |
| `color.ink.950` | `#0B1220` |
| `color.slate.950` | `#0F172A` |
| `color.slate.900` | `#1E293B` |
| `color.slate.700` | `#334155` |
| `color.slate.600` | `#475569` |
| `color.slate.500` | `#64748B` |
| `color.slate.400` | `#94A3B8` |
| `color.slate.300` | `#CBD5E1` |
| `color.slate.200` | `#E2E8F0` |
| `color.slate.100` | `#F1F5F9` |
| `color.slate.50` | `#F8FAFC` |
| `color.blue.800` | `#1E40AF` |
| `color.blue.700` | `#1D4ED8` |
| `color.blue.600` | `#2563EB` |
| `color.blue.100` | `#DBEAFE` |
| `color.cyan.600` | `#0891B2` |
| `color.cyan.500` | `#06B6D4` |
| `color.sky.800` | `#075985` |
| `color.sky.700` | `#0369A1` |
| `color.sky.500` | `#0EA5E9` |
| `color.sky.400` | `#38BDF8` |
| `color.sky.300` | `#7DD3FC` |
| `color.sky.200` | `#BAE6FD` |
| `color.sky.50` | `#F0F9FF` |
| `color.green.800` | `#166534` |
| `color.green.700` | `#15803D` |
| `color.green.600` | `#16A34A` |
| `color.green.500` | `#22C55E` |
| `color.green.300` | `#86EFAC` |
| `color.green.50` | `#F0FDF4` |
| `color.amber.800` | `#92400E` |
| `color.amber.700` | `#B45309` |
| `color.amber.600` | `#D97706` |
| `color.amber.300` | `#FCD34D` |
| `color.amber.50` | `#FFFBEB` |
| `color.red.800` | `#991B1B` |
| `color.red.900` | `#7F1D1D` |
| `color.red.700` | `#B91C1C` |
| `color.red.600` | `#DC2626` |
| `color.red.500` | `#EF4444` |
| `color.red.300` | `#FCA5A5` |
| `color.red.50` | `#FEF2F2` |
| `color.violet.600` | `#7C3AED` |
| `color.violet.400` | `#A78BFA` |
| `color.canvas.light` | `#F7F9FC` |
| `color.canvas.dark` | `#070B14` |
| `color.surface.dark` | `#0D1422` |
| `color.surface.dark-muted` | `#121B2C` |
| `color.surface.dark-raised` | `#182235` |
| `color.surface.dark-selected` | `#172554` |
| `color.status.success-dark` | `#10271A` |
| `color.status.warning-dark` | `#2B200B` |
| `color.status.danger-dark` | `#301419` |
| `color.status.info-dark` | `#0C2535` |
| `color.overlay.light` | `rgba(15,23,42,.48)` |
| `color.overlay.dark` | `rgba(0,0,0,.64)` |

### 2.2 Semantic colors — theme mapping

“禁止”列是 lint 和评审条件，不是建议。对比值使用 WCAG 相对亮度公式，以该行规定的实际背景测量。

| Token | Light reference | Dark reference | 用途 | 禁止 | 对比要求 |
|---|---|---|---|---|---|
| `surface.canvas` | `color.canvas.light` | `color.canvas.dark` | 页面背景 | 表示状态 | 与 `text.primary` ≥4.5:1 |
| `surface.default` | `color.white` | `color.surface.dark` | card、form、table | overlay backdrop | 与 `text.primary` ≥4.5:1 |
| `surface.muted` | `color.slate.100` | `color.surface.dark-muted` | 分组区、read-only 区 | primary CTA | 与正文 token ≥4.5:1 |
| `surface.raised` | `color.white` | `color.surface.dark-raised` | menu、popover、dialog | 永久 canvas | 与正文 token ≥4.5:1 |
| `surface.inverse` | `color.ink.950` | `color.slate.50` | 反色小区域 | 页面主体 | 与 `text.inverse` ≥4.5:1 |
| `surface.overlay` | `color.overlay.light` | `color.overlay.dark` | dialog/drawer backdrop | 文本背景 | 不作为信息唯一载体 |
| `surface.transparent` | `color.transparent` | `color.transparent` | ghost/outline control 背景 | 承载正文 | 背后实际 surface 决定文字对比 |
| `text.primary` | `color.slate.950` | `color.slate.50` | 标题与正文 | disabled | 对所在 surface ≥4.5:1 |
| `text.secondary` | `color.slate.700` | `color.slate.300` | 次级正文 | placeholder-only label | 对所在 surface ≥4.5:1 |
| `text.muted` | `color.slate.600` | `color.slate.400` | metadata、辅助说明 | 唯一错误/安全说明 | 对所在 surface ≥4.5:1 |
| `text.placeholder` | `color.slate.600` | `color.slate.400` | placeholder 示例 | 替代 visible label | 对 input surface ≥4.5:1 |
| `text.disabled` | `color.slate.500` | `color.slate.500` | 原生 disabled 控件文字 | 必需信息、原因、帮助 | disabled 豁免仅适用于不可操作控件；相邻原因仍 ≥4.5:1 |
| `text.inverse` | `color.slate.50` | `color.slate.950` | inverse surface 文字 | 默认正文 | 对 `surface.inverse` ≥4.5:1 |
| `text.link` | `color.blue.700` | `color.sky.300` | inline link | 静态强调文字 | 对背景 ≥4.5:1，且有非颜色 affordance |
| `text.link-hover` | `color.blue.800` | `color.sky.200` | link hover/active | 默认 link | 对背景 ≥4.5:1 |
| `border.divider` | `color.slate.300` | `color.slate.900` | 装饰分隔 | 控件唯一边界 | 不承担信息对比 |
| `border.default` | `color.slate.500` | `color.slate.400` | 控件边界 | focus 或 invalid 唯一信号 | 对相邻 surface ≥3:1 |
| `border.strong` | `color.slate.700` | `color.slate.300` | hover、关键控件、拖拽边界 | danger 表示 | 对相邻 surface ≥3:1 |
| `action.primary` | `color.blue.600` | `color.blue.600` | 单一区域主要动作 | warning/danger/status | 与 `action.on-primary` ≥4.5:1 |
| `action.primary-hover` | `color.blue.700` | `color.blue.700` | primary hover | 静态状态 | 与 `action.on-primary` ≥4.5:1 |
| `action.primary-active` | `color.blue.800` | `color.blue.800` | primary press | 静态状态 | 与 `action.on-primary` ≥4.5:1 |
| `action.on-primary` | `color.white` | `color.white` | primary action label/icon | 普通正文 | 对三种 primary surface ≥4.5:1 |
| `action.destructive` | `color.red.700` | `color.red.700` | destructive button | status badge、普通错误文字 | 与 `action.on-destructive` ≥4.5:1 |
| `action.destructive-hover` | `color.red.800` | `color.red.800` | destructive hover | 静态 danger surface | 与 `action.on-destructive` ≥4.5:1 |
| `action.destructive-active` | `color.red.900` | `color.red.900` | destructive press | 静态 danger surface | 与 `action.on-destructive` ≥4.5:1 |
| `action.on-destructive` | `color.white` | `color.white` | destructive button label/icon | 普通 danger 文本 | 对三种 destructive surface ≥4.5:1 |
| `action.selected-bg` | `color.blue.100` | `color.surface.dark-selected` | selected/current 项背景 | success 表示 | 与 `action.selected-fg` ≥4.5:1 |
| `action.selected-fg` | `color.blue.800` | `color.sky.200` | selected/current 项文字 | 普通 link | 对 selected background ≥4.5:1 |
| `focus.ring` | `color.blue.600` | `color.sky.400` | `:focus-visible` 外环 | decoration | 对相邻两侧至少一侧 ≥3:1；双层 ring 不得被裁切 |
| `focus.backplate` | `color.white` | `color.surface.dark` | focus ring 与控件之间的隔离层 | 单独表示 focus | 与 `focus.ring` ≥3:1 |
| `status.success.fg` | `color.green.800` | `color.green.300` | 已安全、成功、完成 | primary action、计划权益 | 对对应 bg ≥4.5:1 |
| `status.success.bg` | `color.green.50` | `color.status.success-dark` | success 状态面 | 大面积品牌背景 | 与对应 fg ≥4.5:1 |
| `status.success.border` | `color.green.700` | `color.green.500` | success 边界/图标 | 唯一状态信号 | 对对应 bg ≥3:1 |
| `status.warning.fg` | `color.amber.800` | `color.amber.300` | review、grace、quarantine | destructive action | 对对应 bg ≥4.5:1 |
| `status.warning.bg` | `color.amber.50` | `color.status.warning-dark` | warning 状态面 | ambient brand | 与对应 fg ≥4.5:1 |
| `status.warning.border` | `color.amber.700` | `color.amber.600` | warning 边界/图标 | 唯一状态信号 | 对对应 bg ≥3:1 |
| `status.danger.fg` | `color.red.800` | `color.red.300` | block、撤销、破坏动作 | primary action | 对对应 bg ≥4.5:1 |
| `status.danger.bg` | `color.red.50` | `color.status.danger-dark` | danger 状态面 | decoration | 与对应 fg ≥4.5:1 |
| `status.danger.border` | `color.red.700` | `color.red.500` | danger 边界/图标 | 唯一状态信号 | 对对应 bg ≥3:1 |
| `status.info.fg` | `color.sky.800` | `color.sky.300` | pending、verifying、说明 | unconditional success | 对对应 bg ≥4.5:1 |
| `status.info.bg` | `color.sky.50` | `color.status.info-dark` | info 状态面 | hero fill | 与对应 fg ≥4.5:1 |
| `status.info.border` | `color.sky.700` | `color.sky.500` | info 边界/图标 | 唯一状态信号 | 对对应 bg ≥3:1 |

### 2.3 Trust、risk、quarantine 与 unavailable aliases

这些 alias 是稳定公开 token；实现不得把它们改写为页面私有色。

| Alias token | Semantic set | 固定图标 | 可见标签 | 用途 | 禁止 | 对比 |
|---|---|---|---|---|---|---|
| `risk-pending` | `status.info.*` | `Clock` | Pending review | 未产生可用目标裁决 | 表示 allow | fg 4.5:1；icon/border 3:1 |
| `risk-allow` | `status.success.*` | `CircleCheck` | Allowed | 当前指纹获 allow | 表示域名整体可信 | fg 4.5:1；icon/border 3:1 |
| `risk-review` | `status.warning.*` | `TriangleAlert` | Manual review required | 人工复核、禁止跳转 | 轻微提示 | fg 4.5:1；icon/border 3:1 |
| `risk-block` | `status.danger.*` | `ShieldX` | Blocked | 明确阻断 | 普通 invalid field | fg 4.5:1；icon/border 3:1 |
| `file-quarantined` | `status.warning.*` | `PackageLock` | Quarantined | 文件隔离且不可公开 | 等同扫描成功 | fg 4.5:1；icon/border 3:1 |
| `file-scanning` | `status.info.*` | `LoaderCircle` | Scanning | 扫描处理中 | 暗示即将通过 | fg 4.5:1；icon/border 3:1 |
| `file-safe` | `status.success.*` | `ShieldCheck` | Safe | 服务端已判定可发布 | 代替发布权限 | fg 4.5:1；icon/border 3:1 |
| `file-blocked` | `status.danger.*` | `ShieldX` | Blocked | 恶意或策略阻断 | 可重试提示 | fg 4.5:1；icon/border 3:1 |
| `file-scan-error` | `status.danger.*` | `TriangleAlert` | Scan unavailable | 不确定且保持 private | 表示 clean | fg 4.5:1；icon/border 3:1 |
| `surface-unavailable` | `status.danger.*` | `CircleOff` | Unavailable | 安全或运营原因不可用 | pending/loading | fg 4.5:1；icon/border 3:1 |

### 2.4 Data visualization aliases

| Token | Light reference | Dark reference | Use |
|---|---|---|---|
| `data.series.1` | `color.blue.600` | `color.blue.600` | 第一序列 |
| `data.series.2` | `color.cyan.600` | `color.cyan.500` | 第二序列 |
| `data.series.3` | `color.sky.700` | `color.sky.400` | 第三序列 |
| `data.series.4` | `color.violet.600` | `color.violet.400` | 第四序列 |
| `data.series.5` | `color.slate.600` | `color.slate.300` | 第五序列 |
| `data.positive` | `status.success.fg` | `status.success.fg` | 正向值，必须伴随符号/标签 |
| `data.negative` | `status.danger.fg` | `status.danger.fg` | 负向值，必须伴随符号/标签 |
| `data.grid` | `border.divider` | `border.divider` | chart grid；不承担数据语义 |

超过五个类别时使用纹理、marker shape 或分面，不新增临时 rainbow 色。

### 2.5 Code semantic colors

| Token | Light reference | Dark reference | Use | Contrast |
|---|---|---|---|---|
| `code.bg` | `color.slate.100` | `color.surface.dark-muted` | code block background | 与全部 code foreground ≥4.5:1 |
| `code.text` | `color.slate.950` | `color.slate.50` | plain code | 对 `code.bg` ≥4.5:1 |
| `code.comment` | `color.slate.600` | `color.slate.400` | comment | 对 `code.bg` ≥4.5:1 |
| `code.keyword` | `color.blue.700` | `color.sky.300` | keyword | 对 `code.bg` ≥4.5:1 |
| `code.string` | `color.green.800` | `color.green.300` | string | 对 `code.bg` ≥4.5:1 |
| `code.number` | `color.violet.600` | `color.violet.400` | numeric/literal | 对 `code.bg` ≥4.5:1 |

Syntax color 只辅助 token type；复制、screen-reader 与 plain-text fallback 必须保持完整代码。

---

## 3. Brand、gradient、logo 与 icon

### 3.1 Brand usage

品牌视觉以清晰路径、节点、分流和数据反馈为主。禁止游戏化、Web3 霓虹、大面积玻璃拟态、大面积紫色渐变、纯黑极客站、无层级极简和无法由当前仓库 release candidate 或明确 deterministic demo data 证明的产品截图。

状态色与品牌色必须分离。任何安全、付费、域名、文件和 destructive 状态必须同时呈现文字、图标及结构位置。

### 3.2 Gradient tokens

| Token | Exact value | Use | Prohibited |
|---|---|---|---|
| `gradient.hero-ambient` | `linear-gradient(135deg, {color.blue.600} 0%, {color.cyan.500} 55%, {color.sky.400} 100%)` | 低不透明度 hero ambient | H1 文字、整页背景、状态 |
| `gradient.brand-border` | `linear-gradient(90deg, color-mix(in srgb, {color.blue.600} 45%, transparent), color-mix(in srgb, {color.cyan.500} 35%, transparent))` | Website 限量边界 | product control |
| `gradient.data-highlight` | `linear-gradient(90deg, {color.blue.600} 0%, {color.cyan.500} 100%)` | route path、active progress | 风险级别 |

### 3.3 Jet Path

Jet Path 由 path、node、split 和 pulse 四种元素组成：空心 node 表示未激活，实心 node 表示 active/hit，split 表示 routing/A-B，pulse 表示事件。只用于 Hero、product transition、Routing、A/B、Analytics、empty illustration 和 loading illustration；禁止用于 input border、button、table cell 或每张 card。动效只引用 §7 token。

### 3.4 Logo 与 icon token

交付资产固定为 `logo-full-light.svg`、`logo-full-dark.svg`、`logo-mark.svg`、`favicon.svg`、`favicon.ico`、`apple-touch-icon.png`、`og-brand.png`。

| Token | Value | Use |
|---|---:|---|
| `asset.logo.website.height` | 32px | Website header |
| `asset.logo.product.height` | 28px | Workspace/Admin/Docs |
| `asset.logo.safe-area` | 0.5 | mark 高度倍率 |
| `icon.size.inline` | 16px | button/inline |
| `icon.size.navigation` | 18px | sidebar/page action |
| `icon.size.marketing` | 24px | Website feature |
| `icon.size.empty` | 32px | EmptyState |
| `icon.stroke.default` | 1.75 | inline 及更大 Lucide |
| `icon.stroke.small` | 2 | 小于 inline size 的 Lucide |

功能图标只使用 Lucide。品牌图标依次使用官方 Brand Kit、官方 SVG、Simple Icons，并登记到 `BRAND-ASSET-LICENSES.md`。禁止拉伸、任意改色、发光、附加描边或放在不满足对比的复杂图像上。

---

## 4. Typography token

### 4.1 Font family

| Token | Exact value |
|---|---|
| `font.family.latin` | `InterVariable, Inter, ui-sans-serif, system-ui` |
| `font.family.cjk` | `"PingFang SC", "Microsoft YaHei", "Noto Sans SC", system-ui` |
| `font.family.mono` | `ui-monospace, "SFMono-Regular", Consolas, "Liberation Mono", monospace` |

Inter 使用 self-hosted WOFF2 Latin subset；中文使用系统字体栈，不加载全量 CJK webfont。实现必须按页面 `lang` 选择 Latin/CJK fallback 顺序。

### 4.2 Marketing type scale

| Token | Desktop size | Mobile size | Weight | Line-height | Letter-spacing |
|---|---:|---:|---:|---:|---:|
| `type.display-xl` | 64px | 44px | 650 | 1.05 | -0.03em |
| `type.display-lg` | 52px | 38px | 650 | 1.08 | -0.025em |
| `type.h1` | 44px | 34px | 650 | 1.12 | -0.02em |
| `type.h2` | 34px | 28px | 620 | 1.18 | -0.015em |
| `type.h3` | 24px | 22px | 600 | 1.25 | -0.01em |
| `type.body-lg` | 18px | 17px | 400 | 1.65 | 0 |
| `type.body` | 16px | 16px | 400 | 1.65 | 0 |
| `type.meta` | 14px | 14px | 450 | 1.5 | 0 |

### 4.3 Product type scale

| Token | Size | Weight | Line-height | Letter-spacing |
|---|---:|---:|---:|---:|
| `type.product-page-title` | 24px | 650 | 1.25 | -0.015em |
| `type.product-section-title` | 18px | 600 | 1.35 | -0.01em |
| `type.product-body` | 14px | 400 | 1.5 | 0 |
| `type.product-compact` | 13px | 450 | 1.45 | 0 |
| `type.product-meta` | 12px | 450 | 1.4 | 0.01em |
| `type.product-table` | 14px | 400 | 1.4 | 0 |
| `type.label` | 14px | 600 | 1.4 | 0 |
| `type.code` | 13px | 400 | 1.55 | 0 |

`type.product-meta` 只用于 metadata、time stamp 和紧邻主标签的补充信息；禁止作为正文、错误、帮助、安全原因或 destructive 后果。Admin page title 使用 `type.product-page-title`，不得使用 marketing H1。

---

## 5. Spacing、radius、border、elevation 与 z-index

### 5.1 Spacing

基础网格为 `space.1`；只有本表可定义间距。

| Token | Value |
|---|---:|
| `space.0` | 0 |
| `space.1` | 4px |
| `space.2` | 8px |
| `space.3` | 12px |
| `space.4` | 16px |
| `space.5` | 20px |
| `space.6` | 24px |
| `space.8` | 32px |
| `space.10` | 40px |
| `space.12` | 48px |
| `space.16` | 64px |
| `space.20` | 80px |
| `space.24` | 96px |
| `space.32` | 128px |

组件内部只引用 `space.1`–`space.4`；product page section 引用 `space.6`/`space.8`；Website section desktop 引用 `space.20`/`space.24`/`space.32`，mobile 引用 `space.16`/`space.20`。

### 5.2 Radius

| Token | Value | Use |
|---|---:|---|
| `radius.none` | 0 | edge-attached side |
| `radius.xs` | 4px | compact tag |
| `radius.sm` | 6px | small control |
| `radius.md` | 8px | button/input |
| `radius.lg` | 12px | table/card |
| `radius.xl` | 16px | dialog/marketing visual |
| `radius.2xl` | 20px | hero container |
| `radius.full` | 999px | badge/avatar only |

### 5.3 Border 与 focus geometry

| Token | Value | Use |
|---|---:|---|
| `border.width.default` | 1px | component boundary |
| `border.width.emphasis` | 2px | selected indicator |
| `focus.ring.width` | 2px | focus outer ring |
| `focus.ring.offset` | 2px | ring offset |
| `link.underline.thickness` | 1px | inline link |
| `link.underline.offset` | 3px | inline link |

`:focus-visible` 组合固定为 `focus.ring.offset` 的 `focus.backplate` 内层和追加 `focus.ring.width` 的 `focus.ring` 外层；不能只改 border color，不能被 `overflow:hidden`、sticky header 或 overlay 裁切。

### 5.4 Elevation

同名 elevation 在主题内映射如下，禁止组件另写 shadow。

| Token | Light | Dark | Use |
|---|---|---|---|
| `elevation.0` | `none` | `none` | bordered region |
| `elevation.1` | `0 1px 2px rgba(15,23,42,.05)` | `0 1px 2px rgba(0,0,0,.24)` | product card |
| `elevation.2` | `0 8px 24px rgba(15,23,42,.08)` | `0 8px 24px rgba(0,0,0,.32)` | popover/dropdown |
| `elevation.3` | `0 20px 60px rgba(15,23,42,.12)` | `0 20px 60px rgba(0,0,0,.40)` | dialog/drawer |

Dark theme 的 raised component 同时使用 `border.default`；sidebar 不使用 elevation。

### 5.5 Z-index

| Token | Value | Layer |
|---|---:|---|
| `z.base` | 0 | normal content |
| `z.sticky` | 100 | sticky table/header |
| `z.dropdown` | 300 | menu/popover |
| `z.overlay` | 500 | modal backdrop |
| `z.dialog` | 600 | dialog/drawer |
| `z.toast` | 700 | toast |
| `z.command` | 800 | global command |
| `z.critical` | 900 | critical blocking notice |

Portal 只能挂入该层级表；禁止任意 `9999`。

---

## 6. Density、breakpoint、content width 与 layout grid

### 6.1 Breakpoint token

| Token | Min width | Range contract |
|---|---:|---|
| `breakpoint.base` | 0 | 0–639px |
| `breakpoint.sm` | 640px | 640–767px |
| `breakpoint.md` | 768px | 768–1023px |
| `breakpoint.lg` | 1024px | 1024–1279px |
| `breakpoint.xl` | 1280px | 1280–1535px |
| `breakpoint.2xl` | 1536px | ≥1536px |

Viewport breakpoint 用于 shell；组件首先使用 container query。禁止页面私有 breakpoint。

### 6.2 Content width token

| Token | Value | Use |
|---|---:|---|
| `content.website.container` | 1280px | Website outer content |
| `content.website.section` | 1200px | Website section |
| `content.prose.max` | 760px | Website/Docs prose |
| `content.prose.min-readable` | 620px | desktop long-form target floor |
| `content.workspace.max` | 1480px | Workspace |
| `content.admin.max` | 1600px | Admin |
| `content.installer.form` | 720px | Installer form |
| `content.installer.page` | 960px | Installer page |
| `content.dialog.sm` | 480px | confirm/simple dialog |
| `content.dialog.md` | 640px | form dialog |
| `content.dialog.lg` | 800px | detail dialog |
| `content.drawer.desktop` | 420px | right/left drawer |
| `content.tooltip.max` | 280px | tooltip |
| `content.toast.max` | 360px | toast |
| `content.combobox.max-height` | 320px | option list |

### 6.3 Layout grid token

| Token | Columns | Gutter | Outer margin | Applies |
|---|---:|---:|---:|---|
| `grid.desktop` | 12 | 24px | 32px | ≥`breakpoint.lg` |
| `grid.tablet` | 8 | 24px | 24px | `breakpoint.md` |
| `grid.mobile` | 4 | 16px | 16px | <`breakpoint.md` |

### 6.4 Shell geometry

| Token | Value |
|---|---:|
| `shell.website.header` | 64px |
| `shell.docs.header` | 56px |
| `shell.docs.sidebar-left` | 260px |
| `shell.docs.toc-right` | 220px |
| `shell.docs.column-gap` | 32px |
| `shell.workspace.header` | 58px |
| `shell.workspace.sidebar` | 248px |
| `shell.admin.header` | 56px |
| `shell.admin.sidebar` | 256px |
| `shell.product.sidebar-collapsed` | 68px |
| `shell.installer.header` | 56px |
| `shell.workspace.pad.desktop` | 32px |
| `shell.admin.pad.desktop` | 28px |
| `shell.product.pad.tablet` | 24px |
| `shell.product.pad.mobile` | 16px |

Desktop sidebar persistent；tablet sidebar collapsed 或 drawer；mobile 使用 drawer 与单列。Header、sidebar、dialog/drawer 打开不得改变主内容宽度。Mobile primary action 采用页面所属 Page-Level IA 明确的 sticky action；filter 使用 drawer，不得把整页横向滚动作为表格默认方案。

### 6.5 Density

| Token | Row height | Header height | Default surface |
|---|---:|---:|---|
| `density.compact` | 40px | 36px | Admin |
| `density.default` | 44px | 40px | Workspace/Admin |
| `density.relaxed` | 48px | 40px | Workspace onboarding/list |

Mobile 保留核心字段和 overflow actions；资源表在不足以显示核心列时转换为 list-row。密度不得降低 §13 的 touch target。

---

## 7. Canonical motion

所有 motion 参数只在本节定义；其他章节只能引用 token。

### 7.1 Motion token

| Token | Value | Use |
|---|---:|---|
| `motion.duration.fast` | 120ms | press、checkbox |
| `motion.duration.feedback` | 160ms | hover、active、menu、tooltip fade |
| `motion.duration.transition` | 240ms | dialog、drawer、dropdown、tabs |
| `motion.duration.product` | 480ms | reveal、chart、QR、success |
| `motion.duration.path` | 6000ms | Jet Path 单程 |
| `motion.duration.ambient-a` | 8000ms | blue halo |
| `motion.duration.ambient-b` | 10000ms | cyan halo |
| `motion.duration.reduced` | 120ms | reduced-motion opacity |
| `motion.delay.tooltip-open` | 400ms | pointer/focus tooltip open |
| `motion.delay.ambient-phase` | 2500ms | halo phase offset |
| `motion.ease.standard` | `cubic-bezier(.2,0,0,1)` | enter/move |
| `motion.ease.exit` | `cubic-bezier(.4,0,1,1)` | exit |
| `motion.ease.ambient` | `ease-in-out` | alternate ambient |
| `motion.distance.press` | 1px | pressed button translation |
| `motion.distance.reveal` | 16px | section reveal |
| `motion.distance.float` | 8px | product frame |
| `motion.distance.ambient` | 6px | halo translate max |
| `motion.distance.pointer` | 5px | desktop pointer parallax max |
| `motion.hero.scale-min` | 0.98 | halo scale minimum |
| `motion.hero.scale-max` | 1.035 | halo scale maximum |
| `motion.hero.opacity-min` | 0.48 | halo opacity minimum |
| `motion.hero.opacity-max` | 0.68 | halo opacity maximum |
| `motion.concurrent.ambient-max` | 3 | 同时活动 ambient node |
| `motion.concurrent.product-layer-max` | 3 | product floating layer |
| `motion.product.stage-width` | 720px | desktop stage |
| `motion.product.stage-height` | 440px | desktop stage |

### 7.2 Motion levels

| Level | Contract |
|---|---|
| 0 static | Long-form Docs、table row、settings form；无装饰 motion |
| 1 feedback | 只用 `motion.duration.fast` 或 `motion.duration.feedback` |
| 2 transition | 只用 `motion.duration.transition` |
| 3 product | 只用 `motion.duration.product`，同时 layer 不超过 `motion.concurrent.product-layer-max` |
| 4 ambient | 只用于 Website hero 与明确的 onboarding/empty illustration；使用 ambient/path token |

只动画 transform 与 opacity。禁止连续大面积 blur、box-shadow、backdrop-filter、width、height 或 layout-position 动画。Mobile product stage 宽度为容器宽度，只保留主 frame 与一个浮层，关闭 pointer parallax，无横向溢出。

### 7.3 Reduced motion

`prefers-reduced-motion: reduce` 时：

- 关闭 breathing、parallax、path travel、float、count-up、auto-scroll 和非必要 skeleton shimmer；
- enter/exit 统一使用 `motion.duration.reduced` opacity，不使用位移或缩放；
- progress、success、error、risk 和 file 状态在首帧即有文字与图标；
- CSS 与 JavaScript motion 都必须读取同一 media-query store；只暂停 CSS 为失败；
- focus、loading 和状态变更不得因减弱 motion 而消失。

### 7.4 Website ambient/product motion orchestration

- 每个 viewport 同时只允许一个主要 product demonstration loop；ambient halo/Jet Path 可并行，但总活动 node 受 `motion.concurrent.*` 限制。
- Product Stage 进入 viewport 后才开始非必要循环；离开 viewport 或 `document.hidden=true` 必须暂停。用户 hover/focus/interact 后自动演示暂停至少一个完整 cycle。
- 自动演示不得移动正文、H1、价格、legal/security 声明或 primary CTA；不使用 scroll-jacking、横向 pinned-scroll 或依赖路径动画才能理解的叙事。
- Hero/Product Stage 允许真实 UI screenshot、SVG diagram 与精制 illustration 混合；禁止用纯抽象线框代替应展示的真实产品界面，也禁止堆叠无意义漂浮图标。
---

## 8. 全局交互状态与可访问关联

### 8.1 State contract

| State | Visual/interaction contract | Accessibility contract |
|---|---|---|
| `default` | 使用组件默认 semantic token | 正确 role、name、value |
| `hover` | 只在 hover-capable pointer 启用；不得改变布局 | hover 不是唯一入口 |
| `focus-visible` | 使用 §5.3 ring；层级高于相邻内容 | 键盘 focus 可见且不被遮挡 |
| `active` | 使用 active semantic 与 `motion.distance.press` | activation 仍由原生 click/keyboard 触发 |
| `selected` | `action.selected-*` + indicator/check + `aria-selected`、`aria-current` 或 `aria-pressed` | 选择不只靠颜色 |
| `disabled` | 明确 disabled surface/text；无 opacity 继承；不响应事件 | 原生 `disabled` 优先；原因显示在控件外 |
| `loading` | 保持原宽高与原标签；spinner 不替换上下文 | `aria-busy="true"`；重复提交被阻止；动态结果进入 live region |
| `read-only` | `surface.muted` + 正常可读文字；允许选择和复制 | 使用原生 `readonly` 或静态 description，不伪装 disabled |
| `invalid` | danger border、icon、字段下错误文本 | `aria-invalid="true"`，错误 ID 进入 `aria-describedby` |
| `success` | success icon 与确认文本；不把整个 field 默认染绿 | 只在服务器确认或完整校验后宣布；消息进入 polite live region |

### 8.2 Name、description 与 error ID

- 每个 control ID 使用 `{form-id}--{field-name}`；帮助为 `{control-id}--help`，错误为 `{control-id}--error`，状态为 `{control-id}--status`。
- `aria-describedby` 按 help、error、status 顺序包含当前存在的 ID；错误出现后不得移除原帮助关联。
- Input、Select、Textarea、Combobox 必须有可见 `<label for>`。Placeholder 只提供示例。
- Checkbox/Radio group 使用 `fieldset` + `legend`；Switch 的 accessible name 描述设置对象，当前 on/off 由 checked state 表达。
- IconButton 的 accessible name 是动作与对象，例如 “Delete domain example.com”；图标自身 `aria-hidden="true"`。
- loading button 保留动作名并增加状态文本，例如 “Save changes, in progress”；spinner 本身不命名。
- 页面级异步错误保留在触发区域，Toast 不能成为唯一记录；字段错误紧邻字段。

### 8.3 Live region

| Token | Value | Use |
|---|---:|---|
| `feedback.timeout.success` | 4000ms | 非关键成功 toast |
| `feedback.timeout.info` | 5000ms | 非关键 info toast |
| `feedback.timeout.warning` | 8000ms | 可恢复 warning toast |
| `feedback.timeout.critical` | 0 | 不自动消失 |

Success/info 使用 `aria-live="polite"`；阻断错误使用可聚焦的 `role="alert"` 上下文块。相同消息不得被 timer 重复播报。

---

## 9. Component contract

### 9.1 State applicability matrix

`required` 表示组件故事和测试必须覆盖；`conditional` 表示只有该变体存在时覆盖；`prohibited` 表示不得伪造该状态。

| Component | default | hover | focus-visible | active | selected | disabled | loading | read-only | invalid | success |
|---|---|---|---|---|---|---|---|---|---|---|
| Button | required | required | required | required | conditional toggle | required | required | prohibited | prohibited | conditional completion |
| Link | required | required | required | required | conditional current | prohibited | prohibited | prohibited | prohibited | prohibited |
| Input/Textarea | required | required | required | required | prohibited | required | conditional async | required | required | required |
| Select | required | required | required | required | required value | required | conditional async | conditional static value | required | required |
| Checkbox/Radio/Switch | required | required | required | required | required checked | required | conditional async | conditional static value | required group | required group |
| Combobox | required | required | required | required | required option | required | required | conditional static value | required | required |
| Table | required | conditional row | required controls | conditional row action | conditional row | required control | required | conditional cells | conditional edit | conditional saved |
| Card | required | conditional interactive | conditional interactive | conditional interactive | conditional selectable | conditional action | conditional content | conditional content | conditional form | conditional result |
| Dialog/Drawer | required | required controls | required | required controls | prohibited container | required controls | required submit | conditional form | conditional form | conditional result |
| Tooltip | required | required trigger | required trigger | prohibited | prohibited | prohibited | conditional async | prohibited | prohibited | prohibited |
| Toast | required | conditional action | required action | required action | prohibited | prohibited | conditional progress | prohibited | prohibited | required variant |
| Tabs | required | required | required | required | required | required | conditional panel | prohibited | prohibited | prohibited |
| Pagination | required | required | required | required | required current | required unavailable | conditional fetch | prohibited | prohibited | prohibited |
| Breadcrumb | required | required | required | required | required current | prohibited | prohibited | prohibited | prohibited | prohibited |
| Chart | required | required datum | required controls | conditional datum | conditional series | required control | required | prohibited | conditional filter | required data |
| Navigation | required | required | required | required | required current | required unavailable item | conditional destination | prohibited | prohibited | prohibited |
| EmptyState | required | required CTA | required CTA | required CTA | prohibited | conditional CTA | prohibited | prohibited | prohibited | prohibited |
| DestructiveConfirmation | required | required controls | required | required controls | prohibited | required submit | required | prohibited | required reason/name | required persisted result |
| CommandPalette | required | required result | required result | required result | required current | conditional unavailable | conditional remote search | prohibited | prohibited | prohibited |
| ProgressRegion | required | conditional cancel/retry | required action | conditional action | prohibited | conditional action | required | prohibited | prohibited | required result |
| SelectionBar | required | required actions | required action | required action | required count | conditional action | prohibited | prohibited | prohibited | required bulk result |

### 9.2 Control geometry

| Token | Value |
|---|---:|
| `control.height.sm` | 32px |
| `control.height.md` | 36px |
| `control.height.lg` | 40px |
| `control.height.hero` | 44px |
| `control.height.auth` | 40px |
| `control.height.marketing` | 48px |
| `control.hit.minimum` | 40px |
| `control.hit.absolute-minimum` | 24px |
| `control.check.size` | 16px |
| `control.switch.width` | 36px |
| `control.switch.height` | 20px |
| `control.switch.thumb` | 16px |
| `control.textarea.min-height` | 96px |
| `control.otp.cell` | 40px |
| `control.tab.height` | 40px |
| `control.icon-button` | 36px |

#### Button 与 Link

- Button variants 固定为 primary、secondary、outline、ghost、destructive、link。每个操作区只允许一个 primary。
- Primary 使用 `action.primary`；hover/active 使用对应 semantic；destructive 使用 `action.destructive*` 并保留明确动词与对象。
- loading 保持宽度，阻止重复提交；若请求允许取消，提供独立可命名 Cancel，不把 loading button 再次变成取消。
- Toggle button 使用 `aria-pressed` 与 selected indicator。普通 submit 不产生 selected 外观。
- Inline Link 默认有 underline，引用 §5.3 link token；hover 加深且不得移除 underline。重要导航使用真实 `<a href>`。
- “Disabled link” 禁止使用 `<a aria-disabled>` 保留可导航 href；不可用导航显示静态文本和原因，命令型动作用 Button。

| Variant | Default | Hover | Active | Label | Border |
|---|---|---|---|---|---|
| primary | `action.primary` | `action.primary-hover` | `action.primary-active` | `action.on-primary` | 与 background 同值 |
| secondary | `surface.default` | `surface.muted` | `action.selected-bg` | `text.primary` | `border.strong` |
| outline | `surface.transparent` | `surface.muted` | `action.selected-bg` | `text.primary` | `border.strong` |
| ghost | `surface.transparent` | `surface.muted` | `action.selected-bg` | `text.primary` | none |
| destructive | `action.destructive` | `action.destructive-hover` | `action.destructive-active` | `action.on-destructive` | 与 background 同值 |
| link | `surface.transparent` | `surface.transparent` | `surface.transparent` | `text.link` / `text.link-hover` | none；使用 §5.3 underline |

所有 Button 的 focus-visible 使用 §5.3；disabled 使用 `surface.muted` + `text.disabled` + `border.divider`，不得对父组件设置 opacity；loading 保留该 variant 的 default 外观并增加 `aria-busy` 与 spinner。

#### Input、Textarea 与 Select

- Product、Auth、Marketing 高度分别引用 `control.height.md`、`control.height.auth`、`control.height.marketing`。
- Invalid 同时显示 danger border、`CircleAlert`、错误文本；Success 使用 `CircleCheck` 和状态文本，不给整个输入面持续铺绿色。
- Read-only 保持可复制、可 focus 的文本语义；Disabled 不进入 tab order。
- Textarea 默认 vertical resize，最小高度引用 `control.textarea.min-height`。
- 原生 Select 保留平台键盘行为；需要搜索、多选或异步选项时必须使用 Combobox，禁止用无语义 div 仿 Select。
- Select/Combobox 的 read-only 值显示为带 label 的静态 StatusValue，不把 disabled control 当作 read-only。
- OTP 支持完整 code paste、逐格 label、错误关联和一次性密码 autocomplete；不得自动提交未经用户确认的破坏动作。

| Control state | Surface | Text | Border | Companion |
|---|---|---|---|---|
| default | `surface.default` | `text.primary` | `border.default` | visible label |
| hover | `surface.default` | `text.primary` | `border.strong` | unchanged help |
| focus-visible | `surface.default` | `text.primary` | `border.strong` | §5.3 focus ring |
| disabled | `surface.muted` | `text.disabled` | `border.divider` | external reason |
| read-only | `surface.muted` | `text.primary` | `border.divider` | Read only text |
| invalid | `surface.default` | `text.primary` | `status.danger.border` | `status.danger.fg` icon/error |
| success | `surface.default` | `text.primary` | `status.success.border` | `status.success.fg` icon/message |

#### Checkbox、Radio、Switch 与 Combobox

- Checkbox/Radio control 引用 `control.check.size`，label hit area ≥`control.hit.minimum`；Space 切换，Radio 同组使用 Arrow keys 移动。
- Indeterminate checkbox 使用原生 `indeterminate`、可见减号和 screen-reader description；不得读作 checked。
- Switch 使用 `role="switch"` 或原生 checkbox switch abstraction；Space 切换，on/off 由 checked state 与可见结果文本表达。
- Checkbox/Radio/Switch 的 read-only 值显示为带组名的静态 checked/on/off 文本与 icon，不保留可操作 role。
- Combobox 输入使用 `role="combobox"`、`aria-expanded`、`aria-controls`、`aria-activedescendant`。ArrowDown/ArrowUp 移动、Enter 选择、Escape 关闭并保留输入、Home/End 到首尾；异步结果宣布数量。
- Combobox 无结果显示 EmptyState；错误显示持久 InlineMessage；loading 不清空已输入 query。

### 9.3 Table、Card、Chart 与 EmptyState

#### Table

- 语义数据使用 `<table>`、`<th scope>`、caption 或可访问名称；排序头使用 Button 与 `aria-sort`。
- 标准 table 只让内部 control 进入 Tab；只有实现完整 ARIA grid 时才使用 Arrow key cell navigation，二者不得混用。
- selection 必须有 checkbox、selected row indicator 和已选数量；bulk bar 不遮挡 mobile primary content。
- loading skeleton 与最终 column/row geometry 一致；empty、partial、error、stale 分开呈现。Error 保留 retry 与 correlation ID 显示位。
- Mobile 按 §6 转 list-row；若数据本身必须横向比较，只允许 DataRegion 内滚动，并为键盘提供可见 focus 与滚动说明。

#### Card

- 静态 Card 不响应 hover。Interactive Card 的整个命中区必须由单个 link/button 提供，不得嵌套交互元素。
- Selectable Card 使用 indicator、label 和 `aria-selected`/`aria-pressed`；不能只用 border color。
- Card 使用 `radius.lg`、`border.divider`、`elevation.0` 或 `elevation.1`；禁止默认 card wall。

#### Chart

- Chart 只通过 `ChartFrame`，序列按 `data.series.1`–`data.series.5` 固定分配；同一指标跨页面不得换色。
- 每个 Chart 有可见 title、单位、time range、legend 和数据表/下载等价路径。Tooltip 不能是唯一数据访问方式。
- Keyboard focus 可到 datum 或 series toggle；selected series 同时改变 line width/marker，不只改变颜色。
- Empty、loading、partial、error 和 stale 必须占用稳定 chart frame；禁止用零值图伪装无数据。

| Token | Value | Use |
|---|---:|---|
| `chart.height.sm` | 240px | compact card |
| `chart.height.md` | 320px | default Workspace/Admin |
| `chart.height.lg` | 400px | analytics detail |
| `chart.line.width` | 2px | default series |
| `chart.line.selected-width` | 3px | selected series |
| `chart.marker.size` | 6px | default datum |
| `chart.marker.selected-size` | 8px | focused/selected datum |

#### EmptyState

- 由 `icon.size.empty` 图标、具体标题、原因、一个 primary action 和一个文档/support link 组成；不存在可执行动作时不得显示虚假 CTA。
- EmptyState 不用于 permission denial、risk block、quarantine 或 service unavailable；这些使用 §10 的专用状态面。

### 9.4 Dialog、Drawer、Tooltip、Toast 与 destructive confirmation

#### Dialog 与 Drawer

- Dialog 使用 `radius.xl`、`elevation.3`、`z.dialog` 和 §6 dialog width；Drawer 使用 `content.drawer.desktop`，贴边侧 `radius.none`，内侧 `radius.xl`，mobile 占满 viewport inline size。
- 打开后 focus 进入标题后的第一个有效 control；实施 focus trap，背景 inert，Escape 关闭，关闭后返回触发点。提交中 Escape 不丢数据：先显示离开确认。
- Title 使用 `aria-labelledby`，必要说明/后果使用 `aria-describedby`。Close IconButton accessible name 固定为 “Close {dialog title}”。

#### Tooltip

- pointer 与 keyboard focus 都能触发；打开延迟引用 `motion.delay.tooltip-open`，关闭使用 `motion.duration.feedback`。
- 最大宽度引用 `content.tooltip.max`；不包含 link、button、form 或完成任务所需的唯一信息。
- Disabled control 的解释放在可 focus wrapper 或邻近文本，不让 disabled control 自身承担 tooltip trigger。

#### Toast

- 最大宽度引用 `content.toast.max`，层级 `z.toast`，timeout 引用 §8.3。
- Toast 包含短标题、结果、可命名 action/close；critical 不自动关闭。错误详情和修复步骤必须留在页面上下文。
- 屏幕阅读器 announcement 不包含隐藏的技术堆栈或 secret。

#### DestructiveConfirmation

- 使用 AlertDialog；标题必须是动词 + 对象，正文列出影响范围、可恢复性和现有公开资源影响。
- Delete 单一可恢复对象要求明确确认按钮；永久删除、bulk destructive、suspend、revoke 要求输入对象名或精确确认词。
- 审计动作要求 reason 字段；reason 为空或只有 whitespace 时为 invalid，提交不可用。
- destructive button 与 Cancel 不得同色同权重；初始 focus 放 Cancel 或 dialog title，不放 destructive action。
- 提交中保持 dialog 与后果可读；成功关闭后在原页面持久显示 actor、timestamp、result 与 audit event ID。

### 9.5 Tabs、Pagination、Breadcrumb 与 Navigation

- Tabs 使用 `tablist/tab/tabpanel`；ArrowLeft/Right（vertical 时 Up/Down）移动，Home/End 到首尾。自动激活只用于 panel 已本地可用；需要网络加载时 Enter/Space 激活。
- 当前 Tab 使用 selected indicator + `aria-selected="true"`；disabled Tab 不可 focus，并在邻近文本解释原因。
- Pagination 的 first/previous/next/last 都有 accessible name，当前页用 `aria-current="page"`；Website/Docs 可抓取分页使用真实 href。
- Breadcrumb 使用 `nav aria-label="Breadcrumb"` 与 ordered list；当前项用 `aria-current="page"` 且不重复链接自己。Mobile 在中间项折叠时仍保留 home、parent、current 的可访问名称。
- Sidebar、MobileDrawer、AppHeader、WorkspaceSwitcher、UserMenu 的 current/expanded/selected 必须映射原生或 ARIA state。Esc 关闭 menu/drawer 并返回 trigger；切换 workspace 后 announcement 包含新 workspace 名称。

### 9.6 Command palette、Global Create 与 Notifications

**Command palette**：Desktop 默认 `Ctrl/Cmd+K`；结果分 Navigation、Create、Recent/Accessible resources、Settings；type icon + label + disambiguation + shortcut；RBAC/tenant filtering 后才可显示。容器最大宽度引用 `content.dialog.md`，结果列表高度引用 `content.combobox.max-height`，层级使用 `z.command`；ArrowUp/Down、Enter、Esc 完整可用，关闭后 focus 返回 trigger。

**Global Create**：Workspace 与 mobile 使用同一入口，顺序 Link → QR → File → Text → Bio。Quota/permission/suspended 时显示原因和 remediation；选择后进入 route-backed Sheet/page，Back 返回来源上下文。

**Notifications**：badge 使用数字/点 + accessible label；desktop Popover 最大宽度引用 `content.drawer.desktop`，最多最近 6 条并提供 View all，mobile 使用 full-height Drawer。Security/Domain/Billing/Ticket/Resource 使用语义 icon + 文本，不新增随机颜色；事实状态重新从服务端获取。

### 9.7 Forms、editors、search/filter 与 long-running task UX

- Form 只有一个明确 primary submit；dirty 离开提示；server/client validation 使用同一错误模型，提交失败 focus 到 error summary/首个 invalid field。
- Text/Bio autosave 显示 `Saving… / Saved / Offline—changes not synced / Conflict`；password/payment/OAuth/API secret 禁止 browser autosave storage。OTP 支持整串粘贴，autocomplete/password-manager 语义正确。
- Filter bar 默认最高频 2–4 项，其余 More filters；active filters 可见，Clear all 永远可达；`filtered-empty` 与 true empty 区分。
- 长任务使用 `ProgressRegion`：task、state、progress/indeterminate、started time、safe cancel/retry、View details；超过一个 retry cycle 不得无限 loading。

### 9.8 Bulk selection、copy feedback、help 与 onboarding

- DataTable selection 使用 persistent `SelectionBar`，显示 selected count、跨页范围、Clear selection；筛选变化不得静默扩大选择集合。
- Bulk destructive action 显示影响与分项结果；partial failure 不得被单一 success Toast 掩盖。
- Copy 成功原位显示 `Copied`，不改变布局；secret copy 明确一次性显示。Contextual Help deep-link 到 Docs，不用不可关闭的新手气泡链。
- First-run onboarding 使用真实完成状态 checklist；完成/跳过后让出主内容空间，禁止虚构进度和 gamification。
---

## 10. File、destination risk、custom-domain 与 abuse 视觉语义

### 10.1 通用安全显示规则

- 本节只映射服务端状态，不在前端推导 entitlement、risk、ownership、DNS、HTTPS、file safety 或授权结果。
- 每个状态面必须同时包含固定 icon、固定状态名、具体原因/下一步和结构化状态区域；颜色单独变化视为失败。
- `allow`/`safe` 不等于用户拥有发布、下载、注册或管理权限；动作仍由服务端权限响应决定。
- reason、actor、timestamp 和 audit event ID 是审计展示；不得显示 secret、内部 rule detail、原始私有路径或可帮助绕过风控的证据。

### 10.2 File state

| Server state | Alias/icon | Visible headline | Structure and actions | Forbidden implication |
|---|---|---|---|---|
| `quarantined` | `file-quarantined` / PackageLock | File quarantined | 状态栏 + private 说明；Cancel、View policy | 即将自动公开 |
| `scanning` | `file-scanning` / LoaderCircle | Security scan in progress | live progress text；Cancel | 已安全 |
| `safe` | `file-safe` / ShieldCheck | File is safe to publish | safe 结果；Publish/Download 仍按权限 | 自动完成发布 |
| `blocked` | `file-blocked` / ShieldX | File blocked | 原因类别；Delete、Appeal/Support | Retry 可绕过裁决 |
| `scan_error` | `file-scan-error` / TriangleAlert | Scan unavailable; file remains private | fail-closed 说明；Retry when authorized、Delete | clean、public 或稍后必然发布 |

### 10.3 Destination risk

| Server state | Alias/icon | Visible headline | Redirect/UI behavior |
|---|---|---|---|
| `pending` | `risk-pending` / Clock | Destination review pending | 禁止目标访问；显示 safety surface |
| `allow` | `risk-allow` / CircleCheck | Destination allowed | 只有当前 fingerprint 精确 allow 才进入正常行为 |
| `review` | `risk-review` / TriangleAlert | Manual review required | 禁止目标访问；显示 review 原因类别与 support path |
| `block` | `risk-block` / ShieldX | Destination blocked | 禁止目标访问；不显示 unsafe destination link |
| `unavailable` | `surface-unavailable` / CircleOff | Destination unavailable | 禁止目标访问；显示运营原因与 correlation ID |

官方域名与客户自定义域名使用完全相同的视觉映射。UI 禁止出现“域名已验证，所以目标安全”的暗示；risk surface 不显示可点击目标，不执行 routing/A-B/UTM 的可见结果。

### 10.4 Custom-domain entitlement

`CAP-DOMAIN-ENTITLEMENT` 为 Master 标记的 `REQUIRED` 能力。以下只规定其展示；完成状态由当前仓库实现与 Gate 证据决定。

| Display state | Server authority represented | Token/icon | Required visible content | Actions |
|---|---|---|---|---|
| `locked` | 无 active entitlement | `status.info.*` / LockKeyhole | “Custom domains are not enabled”; 当前 plan | Primary `Upgrade to Business`; secondary `Request access` |
| `requested` | support ticket only；未授权 | `status.info.*` / Clock | “Request pending; access is not active”; ticket reference | View ticket；无 Add/Verify/Activate |
| `active` | `source=plan` 或 `source=manual_approval` | `status.success.*` / CircleCheck | source、`domain_limit`、used、remaining、start、expiry 或 “No scheduled expiry” | Add domain、Manage，仍受服务端检查 |
| `grace_period` | active entitlement 的降级截止派生状态 | `status.warning.*` / TriangleAlert | “Access ends on {absolute date}”; exact timezone；禁止新增 | Renew/Upgrade；Request manual review |
| `suspended` | 安全/滥用/所有权/admin 即时处置 | `status.danger.*` / ShieldX | “Custom-domain routing suspended”; reason category、effective time | View support path；无 activation |
| `expired` | entitlement deadline 已过且无另一有效来源 | `status.warning.*` / ClockAlert | expiry date、现有 domain 影响 | Upgrade；Request access |
| `revoked` | 已审计撤销 | `status.danger.*` / Ban | “Custom-domain access revoked”; reason category、effective time | Appeal/Support only when server permits |

`grace_period` 是派生显示状态，不是新 entitlement status。Business 升级 CTA 只启动计费/升级流程；`Request access` 只创建 support request；工单创建、回复、关闭均不授权。`manual_approval` 必须显示独立来源、批准管理员、决定时间、`domain_limit`、expiry 和 decision reason。

Capacity 固定显示为 `{used} of {domain_limit} domains used · {remaining} remaining`；remaining 为零时隐藏 Add domain 并显示 limit 原因与升级/申请路径，不以 disabled button 作为唯一解释。

### 10.5 Domain trust axes

不得用单一 “Domain Status” badge 合并原因。

| Axis | Server states | Presentation mapping | Required evidence text |
|---|---|---|---|
| Entitlement | locked/requested/active/grace_period/suspended/expired/revoked | §10.4 panel | source、limit、expiry/reason |
| Ownership verification | pending/verifying/verified/failed | `status.info.*` / `status.info.*` / `status.success.*` / `status.danger.*` + FileKey2/LoaderCircle/BadgeCheck/CircleX | TXT record result 与 checked time |
| DNS ingress | pending/valid/invalid | `status.info.*` / `status.success.*` / `status.danger.*` + Clock/Route/CircleX | expected vs observed target，敏感值脱敏 |
| HTTPS | pending/active/error/expired | `status.info.*` / `status.success.*` / `status.danger.*` / `status.danger.*` + Clock/LockKeyhole/TriangleAlert/CalendarX | certificate readiness、checked time |
| Domain risk | pending/allow/review/block | risk alias | decision、checked time、reason category |

只有服务端同时返回 entitlement active、ownership verified、DNS valid、HTTPS active、domain risk allow 时，UI 才显示整体 “Ready for links”。整体结果仍不得替代五条 axis。

### 10.6 Administrator decision 与 audit reason

- Approve dialog 显示 Workspace、plan、account standing、source、requested `domain_limit`、linked ticket、start、expiry、reason。
- Approve 必须输入正整数 `domain_limit` 与 reason；人工批准来源显示 `manual_approval`。Deny、suspend、revoke 必须输入 reason category 与 free-text reason。
- Suspend/revoke 使用 DestructiveConfirmation，明确对现有链接的即时影响；工单处理权限不得显示 entitlement decision control。
- 结果页固定显示 `Decision`、`Reason`、`Actor`、`Effective at`、`Audit event ID`；Toast 只确认提交，不承载审计详情。
- 安全 suspension 无 grace 表现；正常降级才使用 `grace_period` warning。

### 10.7 Abuse presentation

Abuse report 的 `received` 使用 `status.info.*` + Inbox，`under_review` 使用 `status.warning.*` + SearchCheck，`actioned` 使用 `status.danger.*` + Gavel，`closed_no_action` 使用 `surface.muted` + `text.primary` + CircleMinus。四种状态都显示状态名与 reason category；报告收到不等于对象有害，管理员裁决必须显示 reason 与 audit metadata。

---

## 11. Page composition、images 与 dark mode

### 11.1 Composition

Website 允许 Hero、Product Stage、Feature Rail、Split Feature、Workflow、Metric Band、Trust Band、Use-case Photo、可验证 Testimonial、CTA、Footer；单页使用五至七种 section pattern，避免每节 card grid。

Product 使用 Page、PageHeader、PageTitle、PageActions、PageSection、DataRegion、SettingsSection、FormSection、SplitPane。Workspace 默认 `density.default`，Admin 默认 `density.compact`；高风险操作不得与主要创建操作使用相同权重。

Homepage 与发布素材只能展示发布候选的真实产品 UI，移除账号隐私、secret、internal ID 和 debug UI；不得预画不存在的 dashboard 或 `REQUIRED` 完成状态。

### 11.2 Images and external assets

- Logo、icon、diagram 使用 SVG；摄影与产品 UI 主格式 AVIF，WebP fallback。
- 禁止 hotlink、低清 JPG、水印、来源不明资产、随机人脸和虚假产品界面。
- 外部资产在 `frontend/assets/ATTRIBUTION.md` 登记 source、author、license、downloaded_at、edited、usage location。
- Photography 只采用与可见场景相符的真实办公/创作画面；禁止握手商务照和指向空白屏幕。

### 11.3 Dark mode

Dark theme 使用 §2 同名 semantic mapping、§2.5 `code.*` 与 §5 dark elevation。禁止 CSS filter 反色。所有 safety/trust 状态必须分别测量 light/dark contrast；不得沿用未测的 light foreground。

---

## 12. SEO 与 Core Web Vitals 视觉保护

本节只定义设计实现对 Master G7/G9 的保护条件，不复制路由索引矩阵或业务状态。

### 12.1 Initial HTML 与 heading

- Indexable page 的 initial HTML（初始 HTTP HTML）必须直接包含可见 `<main>`、唯一可见 H1、首段正文和主要真实 `<a href>`；关闭 JavaScript 后仍可阅读和导航。
- CSS/animation 不得让 primary content 初始为 `display:none`、`visibility:hidden`、零 opacity 或 viewport 外等待 reveal。
- Heading DOM 层级不得因字号需要而跳级；视觉只能引用 §4 type token。每页 H1 数量的自动断言为 exactly 1。
- 重要 link 使用 §2 link token、underline 与可理解文字；禁止只有 icon、click handler 或 “Learn more” 无上下文重复列表。
- Safety、unavailable 与受保护的非 HTML resource 必须保留 Master 指定的 `X-Robots-Tag`；视觉 shell 不得用 client navigation 覆盖或移除该 response header。

### 12.2 Font

- Inter Latin WOFF2 使用 `font-display: swap`；首屏只 preload 实际使用的一个 Latin variable WOFF2，不 preload CJK font。
- Fallback 必须配置可测的 ascent/descent/line-gap/size-adjust 以匹配 Inter；font swap 产生的单页 lab CLS contribution 必须 ≤0.02。
- 字体失败、阻止和慢速网络三种测试下，正文在首次渲染仍可见；FOIT 持续时间必须为 0ms。

### 12.3 Image、embed、chart 与 layout shift

- 每个 `img` 必须有 width/height；responsive image 同时提供 `srcset` 与 `sizes`；video/embed/chart 使用明确 `aspect-ratio` 或固定 block size。
- LCP image 禁止 `loading="lazy"`，必须设置 `fetchpriority="high"`；同页不得有第二个 high-priority 大图。Below-fold image 使用 lazy。
- Skeleton 与最终内容使用同一 geometry；header/sidebar/banner/consent/Toast 不得在内容上方无预留插入。
- 由 Design System 控制的 image/font/skeleton/shell 在单一 lab run 中累计 CLS contribution 必须 ≤0.02；整页 CWV 预算仍由 Master §10.2/G9 裁决。

### 12.4 Mobile readability

- 在 `viewport.mobile` 下正文至少使用 `type.body`，line-height 不低于其定义；不得用 `type.product-meta` 承担正文。
- 页面根 `scrollWidth <= clientWidth`；code/table 的局部滚动容器必须可 focus、有可见说明且不扩大页面根宽度。
- prose 每行目标为 45–80 个 Latin 字符等价值；desktop 最大宽度引用 `content.prose.max`，mobile 使用 grid margin。
- Sticky action、cookie/banner 和 virtual keyboard 不得遮挡 focused control、H1 或 primary CTA。

### 12.5 可执行证据

每个 indexable template 至少保存：

1. 禁用 JavaScript 的 raw HTML 断言：`main=1`、visible H1=1、primary content 非空、关键 links 有 href；
2. font blocked 与 slow-font capture，记录 FOIT 和 font CLS contribution；
3. LCP element 记录、image intrinsic-size audit、root overflow assertion；
4. `viewport.mobile` 可读性与 heading outline；
5. Master G9 trace 中 image/font/shell CLS attribution。

---

## 13. Accessibility contract

- 目标为 WCAG 2.2 AA。正文与状态文字 contrast ≥4.5:1；大字、关键 icon、focus、选中 indicator 和必要控件边界 ≥3:1。
- 大字阈值按 WCAG：普通字 ≥24px，bold ≥18.66px；未达到阈值一律按正文 4.5:1。
- 所有功能完整键盘可达，focus 顺序与视觉/DOM 顺序一致；禁止正数 `tabindex`。
- Focused element 不被 sticky/overlay 完全遮挡，且至少有 `focus.ring.width` 可见边缘。
- Visible label、error association、group legend、accessible name 遵守 §8；安全状态遵守 §10 的非颜色语义。
- 200% zoom 不丢内容和操作；320 CSS px reflow 下除 §9 数据比较例外不得双向滚动。
- Hover 内容可由 focus 触发且可 dismiss、hover、persist；tooltip 不是唯一信息。
- target 至少 `control.hit.absolute-minimum`；主要 touch action、icon button wrapper、checkbox/radio label 至少 `control.hit.minimum`。
- `prefers-reduced-motion` 遵守 §7.3，状态含义不因 motion 消失。
- Locale 切换、错误、异步结果和 workspace 切换使用正确 lang 与 live-region 策略，不重复播报。

---

## 14. Design QA、viewport 与证据命名

### 14.1 Canonical evidence viewport

Evidence viewport 与 responsive breakpoint 是不同 token；截图只能使用下表。

| Token | Dimensions | Slug |
|---|---|---|
| `viewport.desktop` | 1440×900 | `desktop` |
| `viewport.tablet` | 1024×768 | `tablet` |
| `viewport.mobile` | 390×844 | `mobile` |
| `viewport.asset` | 1280×800 | `asset-1280x800` |

### 14.2 Naming

截图名称固定为：

```text
gjv5__{surface}__{case-id}__{state}__{theme}__{locale}__{viewport}.png
```

- `surface`：`website|auth|docs|workspace|admin|installer|public`
- `case-id`：测试清单中的小写 kebab-case 稳定 ID，不使用原始 URL、用户 ID 或 ticket ID
- `state`：服务端/UI state 的小写 kebab-case 值
- `theme`：`light|dark`
- `locale`：`en|zh-cn`
- `viewport`：§14.1 slug

Accessibility JSON 使用相同 stem 加 `__a11y.json`；contrast JSON 加 `__contrast.json`；keyboard trace 加 `__keyboard.json`；motion capture 加 `__motion.webm`。文件名禁止时间戳；时间、commit、browser 写入 manifest，保证视觉参考可稳定 diff。

### 14.3 Evidence manifest

每组 evidence 必须有 `manifest.json`，字段固定为：

```json
{
  "document_id": "GJ-V5-DS-GREENFIELD-2026-08-20",
  "branch": "main",
  "implementation_commit": "40-hex commit from the current repository",
  "build_id": "release-candidate identifier",
  "browser": "name and exact version",
  "os": "name and exact version",
  "viewport_token": "viewport.desktop",
  "theme": "light",
  "locale": "en",
  "case_id": "stable-case-id",
  "state": "default",
  "captured_at": "RFC3339 timestamp",
  "result": "pass"
}
```


### 14.4 Required evidence matrix

- 每个 §9 component 的全部 `required` 与实际存在的 `conditional` states；
- light/dark、en/zh-cn；全部 shell 与高风险 workflow 使用 desktop/tablet/mobile；
- keyboard path、focus-visible、accessible name、error association、live-region announcement；
- 每个 §2 semantic text/status pair 的实际 contrast ratio，light/dark 分开；
- 200% zoom、320 CSS px reflow、`prefers-reduced-motion`；
- entitlement locked/requested/active/grace_period/suspended/expired/revoked；
- ownership、DNS ingress、HTTPS、domain risk 分轴；
- file quarantined/scanning/safe/blocked/scan_error；
- destination pending/allow/review/block/unavailable；
- destructive approve/deny/suspend/revoke，包含 reason invalid 与 loading；
- long text、empty、partial、error、stale；无 root horizontal overflow、无 clipped text、无设计系统 CLS。
- command palette、Global Create、Notifications、dirty-form guard、autosave conflict、filtered-empty、bulk SelectionBar、long-running ProgressRegion；
- Website ambient/product motion 的 offscreen pause、hidden-tab pause、user-interaction pause 与 reduced-motion static equivalent。

### 14.5 Pass/fail thresholds

| Check | Pass threshold | Hard failure |
|---|---|---|
| Token lint | 未登记 raw visual value = 0；重复 token name = 0 | 任一 app 私有颜色/间距/motion |
| Contrast | 全部适用 pair 达到 §2/§13 比值 | 任一 safety、正文、focus 或必要边界不达标 |
| Automated accessibility | WCAG 2.2 A/AA violations = 0 | critical/serious 或未处理的 A/AA |
| Keyboard | 清单步骤完成率 = 100%；focus loss = 0 | trap、不可达、返回 trigger 失败 |
| Responsive | root overflow cases = 0；clipped required text = 0 | desktop/tablet/mobile 任一关键任务阻断 |
| Reduced motion | 无限/视差/路径/count-up 动画数 = 0 | CSS 或 JS 任一继续非必要 motion |
| Visual regression | 未批准 changed pixels = 0；动态数据 mask 只能覆盖时间/随机 ID | mask 覆盖 control、status、focus 或 primary content |
| State semantics | 颜色唯一表达 cases = 0；错误 association 缺失 = 0 | safety state 无文字/icon/reason |
| SEO/CWV visual protection | §12 五类证据全部存在；Design System CLS contribution ≤0.02 | primary content 依赖 JS、LCP lazy、intrinsic size 缺失 |

### 14.6 Completion rule

P03/G2 只有在 token source 与生成产物一致，Controls、Overlay、Navigation、Data、Feedback、Layout、light/dark、en/zh-cn、desktop/tablet/mobile、keyboard、contrast、zoom、reduced motion、SEO/CWV visual protection 和全部 trust states 均通过本节证据矩阵后完成。业务页面禁止先创建私有基础组件再反向补设计系统。
