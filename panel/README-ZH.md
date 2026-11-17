# Linketic 面板网页安装版

这是网页安装方式的源代码目录。可交付 ZIP 由完整安装验收流程构建，包含已编译的应用镜像；用户无需在服务器上编译程序。发布包内的 INSTALL-ZH.md 和 UPGRADE-ZH.md 是安装及升级说明。

## 安装入口

1. 将发布包解压到网站目录以外，在宝塔 Docker 镜像管理中导入 linketic-image.tar，再使用包内 docker-compose.yml 创建编排。设置 SITE_URL 和独立安装访问码。
2. 在宿主机 Nginx 面板设置域名和 HTTPS，反向代理至 http://127.0.0.1:18080。保留 Host、Cookie 和 Set-Cookie，设置真实来访地址，不缓存安装页、API 或后台。
3. 打开域名，输入访问码，填写管理员、SMTP、Turnstile 和 Google Web Risk 配置，点击“检查并安装”。

应用自带独立数据库、Redis、文件扫描及后台服务。数据库凭据由程序生成，业务初始化在网页完成。首次使用需联网下载病毒库，页面显示进度。SMTP 安装检查验证加密连接和凭据，不发送邮件。

## 环境与保留数据

- Linux x86_64、Docker Engine 和 Compose；建议至少 4 GB 可用内存、10 GB 可用磁盘。
- 发布包无需访问构建仓库，但运行时需能连接病毒库下载服务、所配置 SMTP、Google 和 Cloudflare。
- 四个数据卷保存应用配置及文件、数据库、Redis 和病毒库。重新创建容器保留数据；删除数据卷会删除站点数据。
- panel.1 / panel.2 / panel.3 更新时使用原编排和原四个数据卷，仅更新镜像。完整旧账户升级测试以 panel.2 为起点，其他起点不应表述为已逐版本实测。
- 安装成功后安装写接口返回 410，不能重新覆盖现有站点。失败时保留恢复状态，网页可以继续未完成步骤。
- 不依赖 Docker 的全新部署使用独立 Native ZIP，参见 native/INSTALL-ZH.md。不支持直接将旧 systemd 数据覆盖进容器或将容器数据复制到原生目录。

## 验证边界

自动验收使用真实 MySQL、Redis、ClamAV、Go 服务、Nginx 和浏览器。SMTP、Google Web Risk、Turnstile 使用隔离协议测试服务，不能将该结果表述为真实收件箱或真实第三方登录已验证。

发布包的面板路径面向宿主机 Nginx（例如宝塔）。面板镜像导入界面和 1Panel 容器化 OpenResty 网络尚未实测；容器内 127.0.0.1 不能直接当作宿主机地址。以发布包内来源、校验和和验收记录为准。
