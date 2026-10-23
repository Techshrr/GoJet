# 已批准计划的 root 执行调度

这是开发和验收说明，尚不是完整生产安装手册。完整网页配置向导与 P21/G11 未关闭。

现有 `install-web-preflight-schedule.php` 仍然只启用依赖观察。它不会自动执行安装。
管理员在完成 `provision-web-apply.php` 并核对绑定的密封计划后，可以显式运行
`php8.3 /固定发行目录/installer/bin/install-web-apply-schedule.php`，授权系统接手
该计划的网页请求。命令不接受参数；固定发行目录必须是已经核验的发布目录。
`render-web-apply-schedule.php` 只输出同一组单元，便于管理员预先审阅。

执行调度使用 `gojet-installer-apply.service` 和 `gojet-installer-apply.timer`。
启动后首次检查在约 1 秒后，随后每 10 秒按日历时刻检查。日历触发在空请求被跳过后
仍可接收后续提交。没有请求时不启动 worker；同一个 oneshot 单元不会并发启动。
网页进程不调用 systemctl、sudo 或任意命令，不能指定执行路径。

调度安装器复用已经验证的单元发布边界：只允许 root，拒绝既有配置和 drop-in，
在写单元前持久化 started 记录，验证单元语法及实际加载来源，再启用 timer 并记录
ready。执行调度与预检调度使用各自的记录；预检授权不会隐含执行授权。
部分安装或中断不会被新命令覆盖，必须检查 `/var/lib/gojet-installer-authority`
下对应的记录和 `/etc/systemd/system` 单元。不得用删除记录作为通用重试办法。

worker 仍重新验证 root 绑定、专用账户所有权、私有 mailbox、已批准摘要和密封发行包，
在数据/服务修改前写入一次性 `web-apply-started.json`。该记录一旦存在，后续 timer
触发直接跳过；执行失败、超时、断电或中断均不会自动重放。900 秒执行超时会终止
整个 worker 进程组，随后须根据私有阶段日志执行既有恢复流程。即使预检显示通过，
执行前仍重新检查真实依赖；阶段观察文件不能授予完成或重试权限。

最终完成仍须通过实际健康检查并原子写入 root 完成锁。完成锁关闭网页入口，后续
执行触发跳过。网页 404 不能单独证明安装成功，应以 root 完成记录和健康检查为准。
管理员需要停止尚未开始的自动接收时，可停止 `gojet-installer-apply.timer`；若服务
已经运行，停止 timer 不会中止当前服务。不要把停止或重新启用 timer 当作安装恢复。

原生测试在隔离 runner 上运行真实 PID1、PHP、数据库和安装服务。浏览器观察夹具
不能替代该执行证据。正式生产包将只包含运行必需文件；工程测试不随包交付。

验证结果（2026-10-23）：bf245095573914a42ab7c1dc1bc36a9097574be0 的
Payload 39929702376/job 120336635749 已通过真实 timer 安装、重放拒绝和最终完成锁。
executor-context job 120336635479 另行确认实际非 root 身份、NNP=1 和空 capabilities。
此前 storage/clamav 的共同阻塞来自保护环境中的用户切换失败，现已关闭。

健康探针由 systemd 临时服务直接以 gojet 身份启动，随后保持 NoNewPrivileges、
只读系统目录、内核保护及空 capability 集；不在 root worker 内部通过 runuser/setpriv
切换身份。探针仅限已验证发行包的 filepreflight 或 logreceiver --check，固定环境字段，
有独立执行超时和进程组清理。父 worker 的保护不变，预检 timer 必须八项检查全部通过。

原始 artifact 12493925215 的 ZIP 摘要/完整性、内层归档与对应源码证据已独立核验，
详见 STATUS.md。CI runner 为 Ubuntu 24.04.5，不替代指定 24.04.4 目标机验收。
HTTP mailbox 由非特权适配器提交；完整网页配置与浏览器到实际执行链仍待完成。
