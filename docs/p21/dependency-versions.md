# 固定候选的真实依赖版本拒绝验证

本批只增加测试与证据，不修改安装器、服务或已固定的 330 成员候选包。
新工作流从 artifacts/v10/P21/candidate/validation.json 读取精确 run、artifact 和归档摘要，
下载原包后校验归档及 manifest，调用的是包内 installer/bin/preflight.php。
报告同时记录 candidate_source 与 test_source，不能将测试提交冒充候选源码。

## 实际支持边界

总计划 §3.2 指定 PHP 8.3 FPM 和 MySQL 8.x。Nginx、Redis、systemd 与 ClamAV 没有任意新增的
数字版本白名单；它们继续按现有配置、连接、读写、运行身份、扫描器状态和签名时效检查。

| 测试输入 | 拒绝及恢复证据 |
| --- | --- |
| 真实 PHP 8.1 CLI，且 posix/PDO MySQL/OpenSSL 均可用 | 仅 php 检查失败，其他七项保持健康；恢复原 CLI 后通过 |
| 真实 PHP 8.1 FPM 替换探测路径，原 8.3 FPM 进程仍在运行 | 仅 php 检查失败，不能由 active 状态掩盖版本错误；恢复原 inode、模式、字节后通过 |
| 真实 MariaDB 10.11 的独立数据库实例 | 同一 PDO 连接和非版本设置先通过，随后仅 mysql 检查失败；数据库保持空表，原 MySQL 8 环境仍通过 |

这是具体的真实不兼容输入矩阵，不声称穷举所有 PHP/MySQL 版本，也不把 MariaDB 称作 MySQL 9。
没有替换版本字符串、伪造 PDO 查询、增加生产测试钩子或删掉拒绝条件。
各场景均断言不产生安装完成锁、不改变安装权威记录。

## 夹具隔离与来源

PHP 8.1 来自 Ubuntu Jammy 官方归档，MariaDB 来自 Ubuntu Noble 官方归档。
下载使用独立 APT 索引和 Ubuntu 官方签名密钥，验证软件包后仅解包，不运行 Debian maintainer scripts，
不安装旧 PHP 或替换主机数据库软件包。报告保留包版本、架构与 SHA-256。
MariaDB 只监听随机 loopback 端口，使用私有临时数据目录，测试结束关闭并删除夹具。
FPM 探测文件在 finally 中恢复，核对原进程 PID 与文件身份。

参考：
- https://packages.ubuntu.com/jammy/php8.1-fpm
- https://packages.ubuntu.com/noble/mariadb-server-core
- https://mariadb.com/docs/server/clients-and-utilities/deployment-tools/mariadb-install-db

此测试不重建候选，也不重跑已通过的完整升级恢复链路。原依赖故障、文件和 Redis 权限、
八服务、恢复及浏览器证据继续绑定其原始报告。本批真实 CI 通过后再登记结果；G11 不由测试自动签署。
