# Spark 热点共享上网最小验证

用户要求：进入下一个连接场景，并保持笔记本互联网访问。继续采用小规模验证，不交付自动切换网络的生产功能。

## 本次选择

当前 Spark 通过 wlP9s9 连接 YYGU，上游地址 172.16.0.64、默认网关 172.16.0.1、5 GHz 信道 48。驱动声明允许同信道 managed + AP；信道 48 无 DFS / no-IR 限制。NetworkManager 1.46.0、dnsmasq、Avahi 均已存在。

采用同物理网卡新增临时 AP 虚拟接口 lw-ap0，保留原 STA 接口及连接配置。由 NetworkManager shared 模式分配热点地址、提供 DNS/DHCP 并 NAT 到默认上游，测试网关为 192.168.78.1/24；避开当前 K3s 的 10.42.0.0/24 以及双机互联、Docker 网段。新增 AP 同步发布现有部署 UUID，客户端沿用局域网发现功能。

替代路径：有线/USB 上游加无线热点可避免同网卡并发限制；笔记本直接连接同一外部 Wi-Fi 则复用第一场景。若同网卡实测不稳定，再评估前者。没有任何上游网络时，热点只能提供本地服务，无法提供互联网。

## 验证工具

`deployments/spark/hotspot-smoke.sh` 是临时验证脚本：

- `check` 仅只读预检，不要求 sudo；检查接口、当前信道、网段冲突、部署 ID 和依赖。
- `start` 需要 sudo，创建 root 私有的 /run 临时目录和 10 分钟 systemd transient unit；临时随机 WPA2 密码只输出给运行命令的终端。
- AP 连接使用 `save no` 且 `autoconnect no`；不启用开机热点。
- 运行单元与 SSH 独立。超时、启动失败、上游断开/信道变化或手动停止后，ExecStopPost 删除本次 profile/虚拟接口和临时文件。发现子进程随单元退出撤销广播。
- 不调用原 Wi-Fi 的 disconnect / down，不停止原发现服务，不重启主机。共享模式的 NAT/DHCP 由 NetworkManager 管理与回收。

无线驱动仍可能在创建第二接口时影响现有连接，不能把“配置不改动”当作“绝不掉线”的保证。执行前需确保可接触 Spark；测试失败后等待自动清理，再重连原 Wi-Fi。

## 验收

1. 启动日志出现热点已启动；上游 Wi-Fi 仍连接，Spark 互联网请求成功。
2. 笔记本连接测试 SSID，取得 192.168.78.x 地址，默认网关是 192.168.78.1。
3. 在笔记本能打开外部 HTTPS 网站；不能仅凭 Spark 自身能联网推断转发成功。
4. Lework 搜索发现同一部署，测试后端通过；无法解析 .local 时可手填 http://192.168.78.1:38081/v1 诊断。
5. 停止/10 分钟到期后，AP 和 profile 消失，原上游、双机网络及常驻发现服务正常。

未完成全部实测前，不声明热点共享上网可用。不会自动切换笔记本网络，不修改已有登录地址。

参考：[NetworkManager 1.46 shared 模式](https://www.networkmanager.dev/docs/api/1.46.2/nm-settings-nmcli.html)。

## 当前验证记录

2026-10-29：本地和远端 bash 语法检查通过，Spark `check wlP9s9` 通过。笔记本当前默认网关 172.16.0.1，笔记本与 Spark 分别通过现有网络访问外部 HTTPS 均返回 200。脚本已上传 `/home/lework/leros-discovery-20261029/hotspot-smoke.sh`。热点启动需用户输入 sudo 密码，尚未声明完成 AP 并发或客户端转发验收。

### 同网卡并发启动验证

用户执行启动命令后，2026-10-29 14:22:22 热点激活成功：lw-ap0 为 AP、192.168.78.1/24、信道 48 / 20 MHz；原 wlP9s9 仍为 managed，连接 YYGU，信道 48 / 160 MHz，默认路由仍经 172.16.0.1。Spark 外部 HTTPS 返回 200，测试服务 active/running，发现发布进程已在 lw-ap0 上发布。等待笔记本连接后验证转发、DHCP 与自动发现；仅凭此结果不认定客户端上网已经通过。

### 笔记本热点访问验证

用户切换热点后，笔记本 en0 地址为 192.168.78.40，默认网关 192.168.78.1。外部 HTTPS 返回 200；直接访问 192.168.78.1:38081/v1/GlobalConfig 返回 200；访问 spark.local 同一路径也返回 200，实际目标为 192.168.78.1。注意外部域名在本机解析为 198.18.x.x，可能存在本机代理/TUN，所以此项验证是当前笔记本环境的端到端访问成功，不是排除代理的纯路由基准测试。

从笔记本新建 Bonjour 浏览器扫描 8 秒，收到 `_lework._tcp`，host=spark.local，port=38081，地址只有 192.168.78.1，部署 ID=5093c886-d4f7-4a68-bc71-2450725c6e1c，与常驻服务相同。Spark 端 station dump 确认有热点客户端及收发流量，上游仍经 wlP9s9。桌面发现结果可见，未保存或变更登录。等待 10 分钟自动清理验收。

### 自动清理验收

14:32:17 systemd 到达 RuntimeMaxSec=600，触发 ExecStopPost，日志确认临时连接 UUID 已删除。检查确认 lw-ap0 已不存在，NetworkManager 不再列出 leros-hotspot-test，原 leros-discovery 服务 active，Spark 默认路由仍为 wlP9s9 → 172.16.0.1。笔记本随后恢复 172.16.0.162 / 默认网关 172.16.0.1。systemd 的 result=timeout 是预设测试时限触发，不是清理失败。

结论：本次同网卡同信道热点共享、当前笔记本环境的互联网访问、后端访问、DNS-SD 广播和到期清理均完成实机验证。后续生产化仍需长期运行、跨信道/换上游、用户入口与安全隔离设计；当前脚本不启用开机热点。截图：/tmp/lework-spark-hotspot.png。
