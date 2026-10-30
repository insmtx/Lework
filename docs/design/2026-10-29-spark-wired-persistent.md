# Spark 用户 RJ45 网口持久直连

## 已确认的范围

用户确认采用临时测试通过的方案：enP7s7 专用于笔记本直连，Wi-Fi 承担互联网。保留旧连接供回退，关闭旧连接自动连接。仅配置提供 Lework 后端入口的 Spark，不修改双机互联口、后端认证或另一台设备。

旧连接 UUID `6b864655-27f4-3c72-b22e-8ce1d7dfd5e5`，名称“有线连接 3”，地址 `172.16.0.64/16`，网关 `172.16.0.1`，自动连接 yes、优先级 -999。该地址与 Wi-Fi 重复，但有线直连与 Wi-Fi 并非同一二层网络，是本次需避免的冲突。原配置不删除。

## 实现

- `deployments/spark/install_wired.py`：check/install/rollback，显式指定接口和原 UUID；目前只允许已核对的 enP7s7。
- NetworkManager 新建持久 `leros-direct`，随机 UUID、自动连接 yes、优先级 999；IPv4/IPv6 都为 link-local、never-default，不设置 DNS、网关、DHCP 服务或 NAT。
- 有线单独的 `leros-discovery-wired.service` 与 `/etc/leros-discovery-wired.conf`；复用现有发现脚本、后端端口/协议及设备 ID。现有 Wi-Fi 广播单元不变。
- `/var/lib/leros-wired/state.json` 是仅 root 可读的恢复记录；网络变更前持久化原自动连接开关、原配置属性与新 UUID。只恢复被工具修改的开关，不用快照覆盖其他运维修改。
- CLI 互斥锁避免并发安装/回退；失败自动尝试回退，恢复失败/进程中断保留记录。重复安装不覆盖基线，不复制连接。
- 无载波也可安装；接线后 NM 自动启用，IPv4 自分配可能需要几十秒。回退不会强制抢占用户自行切换的其他活动连接。

## 验证记录

2026-10-29：

- 本地先运行 11 个安装/回退行为测试，因安装器尚未实现而失败；实现后 11/11 通过。
- Spark 实际 Python 环境运行同一测试集 11/11 通过；NetworkManager 属性读取和安装器只读预检通过；systemd-analyze verify 通过。
- 此时网线未连接，持久安装尚未执行。临时配置的实机结果见 [网线验证](2026-10-29-spark-wired-spike.md)；不能将其当作持久配置的重启验收。
- 独立审查发现两项回退边界（清理期间用户切换连接、受管连接改绑其他网口），均补测试复现失败并修复；本地与 Spark 最终均 13/13 通过，最终版本预检和 systemd 校验再次通过。
- 下一步：由用户在终端执行 sudo 安装；核对网络与服务，再接线/拔插验证。整机重启需要单独安排，不能影响正在运行的模型任务。

## 边界

此模式将用户口固定为直连用途，不实现“接路由器走 DHCP、直连时自动回退”的双模式，也不负责通过有线给笔记本分享互联网。Windows/Linux 客户端及其他 USB 网卡未验证。手填后端地址始终保留，发现 UUID 不是身份凭证。端口变更后需回退并重新安装有线配置。

最终上传安装器 SHA-256：`628771e42f7ed18da68130293c8fbf2295c3784e194024e9adbd2edbb00f4261`。用户已执行 sudo 安装；2026-10-29 15:57 通过 SSH 核对安装结果。

## 持久安装实机核对（2026-10-29 15:57）

- 有线广播于 15:56:11 启动，systemd enabled/active；日志确认在 enP7s7 发布 spark.local:38081，Wi-Fi 广播仍 active。
- 持久连接 UUID `98d343c5-c628-495e-b89a-7f6cacc41f2e`，自动连接 yes、优先级 999；IPv4/IPv6 link-local、never-default=yes，无 IPv4 网关。
- 原配置保留，autoconnect=no。
- 用户网口 enP7s7 此时 DOWN，笔记本 en5 尚不存在（未接适配器/网线）；接线实测待执行。
- Wi-Fi 仍为 172.16.0.64/16，返回笔记本 172.16.0.162 的路由走 wlP9s9；双机端口仍为 10.100.216.1/24 与 10.100.217.1/24。
- 开机启用状态已确认，未实际重启整机，不能宣称已通过重启验收。

## 首次接线验证（2026-10-29 16:08–16:09）

- Spark NetworkManager 16:07:54 在检测到载波后自动启用 `leros-direct`（UUID 与安装记录一致）；取得 IPv4 169.254.50.134/16。
- macOS en5 保持 DHCP，自动取得 169.254.174.195/16，无路由器；默认路由仍走 en0、172.16.0.1。
- `curl --interface en5 --noproxy '*' http://169.254.50.134:38081/v1/GlobalConfig` 返回 HTTP 200；使用 spark.local 的同样请求也返回 200，remote_ip 为 169.254.50.134。
- 8 秒 Bonjour 扫描仅加入 en5 的 IPv4 组播；收到 169.254.50.134 的 `_lework._tcp` 广播，解析得到 spark.local:38081、path=/v1、部署 ID 5093c886-d4f7-4a68-bc71-2450725c6e1c。
- 强制 `--interface en0 --resolve www.baidu.com:443:183.2.172.177 --noproxy '*'` 的 HTTPS 请求返回 200；地址来自 Spark 当前 DNS 查询，避开笔记本 VPN fake-IP 影响。
- Spark 返回笔记本 Wi-Fi 地址 172.16.0.162 的路由仍走 wlP9s9；Wi-Fi 和有线发现服务均 active。
- 已请求用户拔插后再次验收。未重启整机。

## 持久配置拔插验收（2026-10-29 16:10）

- NetworkManager 日志：16:09:42 载波断开，16:09:53 恢复；自动激活同一个 `leros-direct` UUID `98d343c5-c628-495e-b89a-7f6cacc41f2e`，没有恢复旧静态连接。
- Spark enP7s7 恢复 169.254.50.134/16；macOS en5 自动取得新的 169.254.37.19/16，未手动设置地址或网关。
- 原连接 autoconnect 仍为 no；有线和 Wi-Fi 发现服务均 active。
- 有线专用 8 秒扫描再次取得 Spark 的 `_lework._tcp` 广播及相同部署 UUID。
- 强制有线访问 IPv4 后端与 spark.local 均返回 HTTP 200，远端地址均为 169.254.50.134。
- 强制 Wi-Fi 访问公网 HTTPS 返回 HTTP 200；笔记本默认路由仍走 en0/172.16.0.1，Spark 返回笔记本 Wi-Fi 地址的路由仍走 wlP9s9。
- 本机组合的持久直连安装、首次连接、拔插恢复验收通过。配置保持启用，无需像临时测试一样清理。整机重启、其他客户端平台仍未实测。

## 整机重启验收准备（2026-10-29 16:13）

- 用户要求继续，开始准备尚未完成的重启恢复验收。
- 重启前 boot_id：`0ba2e985-64de-4abf-bd35-1419ef1768e8`；uptime 5:24。
- NetworkManager、Avahi、有线/Wi-Fi 发现服务均 enabled/active；k3s、Docker 均 enabled；本机后端 GlobalConfig HTTP 200。
- 持久直连 UUID 不变，原连接 autoconnect=no；回退脚本存在。
- GPU 瞬时利用率 0% 不能证明没有任务。已请用户确认可中断后在终端执行 sudo systemctl reboot，并保持两条笔记本网络连接。尚未收到重启结果，不能视作重启验收通过。

用户明确暂不重启，重启验收延期；当前回到同一局域网纯 Wi-Fi 的客户端发现→选择→测试→保存连接流程验证。
