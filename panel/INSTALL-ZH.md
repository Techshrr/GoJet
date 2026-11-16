# Linketic 网页安装包

适用于 Linux x86_64、宿主机 Nginx 的宝塔面板。建议至少 4 GB 可用内存、10 GB 可用磁盘；需要已安装 Docker / Compose。包内已经包含编译完成的应用和运行依赖，不需要安装宿主机 PHP、MySQL 或 Redis，也不需要运行安装命令。

已安装 panel.1 / panel.2 / panel.3 的站点请阅读 **UPGRADE-ZH.md**，保留原项目及数据卷更新镜像，无需再次安装。

## 1. 上传并导入应用

在宝塔文件管理中上传本 ZIP，解压到网站目录以外的独立目录。打开 **Docker → 本地镜像 → 导入镜像**，选择解压出的 `linketic-image.tar`。导入后镜像名应为 `linketic-panel:10.0.0-rc.2-panel.4`。

## 2. 部署

在 Docker 编排中选择包内 `docker-compose.yml`（或粘贴其内容），修改两项后创建项目：

- `SITE_URL`：`https://www.linketic.com`，不带末尾斜杠。
- `INSTALL_ACCESS_CODE`：自行设置至少 24 位、不可猜测的独立访问码，用于保护首次安装入口。不要使用示例值。

保持项目名称 `linketic` 和四个数据卷不变。应用只监听宿主机 `127.0.0.1:18080`，无需放行此端口。

## 3. 绑定域名和 HTTPS

在宝塔创建反向代理网站，绑定 `www.linketic.com`，目标填 `http://127.0.0.1:18080`，发送域名保留 `www.linketic.com`。申请有效 HTTPS 证书，开启 HTTP 跳转 HTTPS。

反代需要保留 Host、Cookie 和响应 Set-Cookie，将 X-Real-IP 设置为实际来访地址；不要缓存安装页面、API 或管理后台。`linketic.com` 在面板另设 HTTPS 301 跳转至 `https://www.linketic.com`，证书需覆盖对应域名。

## 4. 打开网站安装

访问 **https://www.linketic.com**，会自动进入安装页。输入访问码，再填写：

- 管理员邮箱、名称、密码；
- SMTP 主机、STARTTLS 端口（通常 587）、发件邮箱、用户名与密码；
- Google Web Risk API 密钥；
- Turnstile Site key、Secret key，并完成人机验证。

点击“检查并安装”。数据库账户、内部密钥、32 项数据库迁移、管理员、后台服务和风控接入由程序自动准备。无需填写风控接口地址或策略版本。

首次启动需要联网下载 ClamAV 病毒库；外部服务会使用你填写的凭据实际验证。SMTP 检查仅建立加密连接并验证账户，不发送邮件。错误配置可以在页面修改；已进入初始化的失败可用“重试未完成步骤”恢复。安装完成后所有安装写接口关闭，使用管理员账户进入后台。

## 数据与范围

- 四个数据卷保存应用配置及文件、数据库、Redis、病毒库。停止或重新创建容器会保留这些数据；**不要删除数据卷**。本 ZIP 不包含任何预设管理员、API 密钥或用户数据。
- 测试覆盖真实 MySQL、Redis、ClamAV、Nginx、Go 服务、浏览器安装、初始化失败恢复、后台页面与重启。外部 SMTP、Google、Cloudflare 在 CI 中使用协议测试服务；用户的真实账户将在安装页面中在线验证。
- 本包支持新安装及 panel.1 / panel.2 / panel.3 保留原数据卷更新；不用于覆盖旧 systemd 原生安装，不包含跨部署迁移流程。
- 宝塔面板内点击导入操作没有在你的面板上实测；应用镜像、配置和 HTTPS 反代后的网页流程已由自动测试验证。1Panel 容器化 OpenResty 网络接入尚未验收，不能直接使用宿主机回环地址。
- 主机仍需能够连接 ClamAV 病毒库、SMTP、Google 和 Cloudflare。配置网站时请使用自己的域名及有效证书。

包内 `release.json` 记录源码、镜像标识和测试链接，`SHA256SUMS` 可用于核对文件完整性。
