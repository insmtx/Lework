# Spark 局域网服务发现

前提：Avahi、python3-dbus、systemd 可用；后端端口对笔记本可达。安装工具不自动安装系统包或修改网卡配置。

将本目录复制到 Spark 后，运维执行（接口/端口按实际部署填写）：

```sh
sudo bash install-discovery.sh wlP9s9 38081
```

使用持久部署 UUID，仅在指定网卡发布 `_lework._tcp`，服务重启后 UUID 保持不变。
双机共用一个后端时，只在提供入口的主机安装。没有替换主机角色或自动故障转移功能。
重装会备份旧配置，不覆盖 UUID；更换网卡/端口时重新运行安装命令。

```sh
systemctl status leros-discovery
journalctl -u leros-discovery -n 30
sudo bash install-discovery.sh --uninstall
```

卸载只移除本服务单元并停止广播，保留 ID/配置，其他 Avahi 服务保持原样。
安装失败时查看 journal；恢复对应 .bak 文件并重启该服务可回退。UUID 不是身份凭据，不应据此自动迁移登录令牌。

私有化客户端首次配置时自动搜索，已有配置的用户从“系统设置 → 服务连接 → 搜索局域网设备”主动搜索。
选择结果只填入地址，必须测试、保存。手填不依赖广播，可继续使用。
已保存的 .local 名称由操作系统解析，IP 变化无需修改 URL；网络阻止 mDNS 时可手填 IP。
Windows/Linux 客户端和 macOS 打包权限需要各平台实测，不能仅凭开发态通过视为全部完成。

## 临时热点共享上网验证

`hotspot-smoke.sh` 为 10 分钟测试工具，尚非生产热点管理。详见 [验证范围](../../docs/design/2026-10-29-spark-hotspot-spike.md)。要求 Spark 已连接可上网的 2.4/5 GHz Wi-Fi，网卡允许同信道 STA/AP 并发，当前信道不受 DFS/no-IR 限制。

```sh
bash hotspot-smoke.sh check wlP9s9
sudo bash hotspot-smoke.sh start wlP9s9
sudo journalctl -u leros-hotspot-test -n 30 --no-pager
```

只有日志出现“热点已启动”后再用笔记本连接命令输出的 SSID/随机密码。无需将密码发给他人。确认笔记本取得 192.168.78.x 地址，再分别测试互联网和 Lework 自动发现。测试热点到期自动关闭；提前结束用 `sudo systemctl stop leros-hotspot-test`。

本脚本不主动断开原 Wi-Fi，但网卡驱动并发能力仍需实测；请在能接触 Spark 时运行。失败时检查日志，等待回收后连接原 Wi-Fi。热点仅共享已有上游，没有上游时无法提供互联网。

## 临时网线直连验证

`wired-smoke.sh` 仅针对已识别的 Spark 用户 RJ45 网口 enP7s7，采用独立链路本地地址、不设置默认网关；详见 [验证范围与原配置](../../docs/design/2026-10-29-spark-wired-spike.md)。

```sh
bash wired-smoke.sh check enP7s7
sudo bash wired-smoke.sh start enP7s7
sudo journalctl -u leros-wired-test -n 30 --no-pager
```

笔记本有线使用 DHCP/自动取址，Wi-Fi 保持连接。系统取得 169.254 地址后测试发现和后端；验证时需明确请求经过有线，不能用 Wi-Fi 访问成功代替直连验收。

10 分钟后恢复原活动连接，提前结束可执行 `sudo systemctl stop leros-wired-test`。本机原有线配置与 Wi-Fi 网段重叠，恢复后建议拔掉测试网线，避免再次引入路由冲突。已在当前 Spark 与 macOS 上完成自动取址、拔插后恢复、有线发现及 Wi-Fi 并行上网验证；仍是临时工具，不作为生产部署配置。

## 持久网线直连

`install_wired.py` 将已验证的 enP7s7 用户 RJ45 网口设为专用直连口。它保留旧连接，仅关闭其自动连接；新建持久 `leros-direct` 连接，IPv4/IPv6 都使用链路本地地址且不设默认路由。Wi-Fi 继续承担上网，有线独立发布同一设备的 Lework 服务。该模式不适用于用此网口接路由器获取互联网；其他型号或接口名需重新验证。

先完成上面的局域网发现安装，再确认用户网口和旧配置 UUID。当前测试设备命令如下（其他设备不可照搬 UUID）：

```sh
python3 install_wired.py check enP7s7 6b864655-27f4-3c72-b22e-8ce1d7dfd5e5
sudo python3 install_wired.py install enP7s7 6b864655-27f4-3c72-b22e-8ce1d7dfd5e5
```

可在网线未连接时安装，之后插线自动启用；首次取址可能需要几十秒。建议通过 Wi-Fi SSH 安装，避免有线改址导致会话中断。笔记本有线保持 DHCP/自动取址、Wi-Fi 正常联网。

恢复记录位于 `/var/lib/leros-wired/state.json`（仅 root 可读）；包含原连接属性快照和原自动连接开关，不含 Wi-Fi 密码。重复安装复用受管 UUID，不覆盖首次恢复依据；中断后先回退再重装。安装异常会尝试自动回退，若回退也失败，保留记录供重试。不要手工删除状态文件。

```sh
systemctl status leros-discovery-wired
journalctl -u leros-discovery-wired -n 30 --no-pager
nmcli -f NAME,UUID,TYPE,DEVICE connection show
ip -brief address show enP7s7
sudo python3 install_wired.py rollback
```

回退只删除本工具创建的有线连接和专属广播服务、恢复旧自动连接开关，不改原 IP/DNS/网关；无网线时不强行激活旧配置。用户已经切到其他连接时不强行切回。原配置仍有网段冲突，回退后应拔掉直连网线。旧配置被删除/改绑网口时工具会停止回退并保留记录，交由运维处理。

验收必须覆盖：网线拔插后自动重连、有线地址访问后端与发现、Wi-Fi 同时上网；重启后需再次检查。这里只管理当前入口 Spark；双机互联端口不在本工具操作范围内。客户端手填地址和原登录流程保持可用。

2026-10-29 已在当前 Spark 与 macOS 笔记本完成持久安装、首次接线和拔插恢复验收：自动取址、有线广播、有线后端访问与 Wi-Fi 同时上网均通过。服务已设为开机启动，但整机重启尚未实测。详见 [持久直连记录](../../docs/design/2026-10-29-spark-wired-persistent.md)。

## 单机设备管理：只读预检（2026-10-29）

`power_agent.py` 是独立的只读采集器，不包含启停、关机、维护、恢复命令。`--snapshot` 输出一次清单；Unix socket 模式只提供 `GET /status`，所有 POST 返回 405。没有 TCP 监听，也不使用 mDNS 记录作为授权依据。

无需安装即可核对本机：

```bash
python3 deployments/spark/power_agent.py --snapshot --node master
```

`--node` 必须由运维填写本机 Kubernetes 节点名。采集前验证该节点的 machineID 与本机一致，并固定使用本机 `unix:///var/run/docker.sock`，忽略 Docker 远端上下文。节点不存在或身份不符均标为采集失败。采集固定范围：指定节点的非 `kube-system` Pod，以及本机全部 Docker 容器（含已退出容器）。仅返回名称、标识、状态，不返回环境变量、参数、凭证或探测错误原文。采集失败是未知，不是无服务。当前全部工作负载标为未登记启停策略；模型状态始终为未配置可管理模型，不根据容器名称猜测推理服务。

### 可选部署准备

模板 `leros-power-agent.service` 尚未安装到现场，实际部署应与后端升级在同一维护窗口安排。安装前由运维：

1. 将脚本放到 root 管理的 `/usr/local/lib/leros-spark/power_agent.py`，将 unit 放到系统服务目录。
2. root 管理 `/etc/leros-power-agent.conf`，内容 `SPARK_NODE=master`（按实际节点替换）。unit 使用现场已有 `lework` 用户；先确认该用户已经具备只读 Kubernetes 查询和现有 Docker 访问权限。本功能不自动加入 docker 组、不发放 sudo 权限。
3. `/run/leros-power` 使用 0750，socket 使用 0660。后端容器只挂载该专用目录，并配置对应数值组 ID；不要挂载宿主机根目录、Docker socket 或扩大其他容器权限。注意已有 Docker 组本身具备较高权限，采集器必须由运维维护，不允许普通成员修改脚本。
4. 后端配置以下安装绑定，`host_id` 取采集器输出，`org_id` 取实际默认组织，`worker_ids` 只填写已确认位于此主机的 Worker。示例值不是现场组织配置：

```yaml
spark:
  socket_path: /run/leros-power/agent.sock
  host_id: <采集器返回的主机 ID>
  org_id: 123
  worker_ids: [1]
```

后端支持 `GET /v1/ops/spark/status` 和 `POST /v1/ops/spark/preflight`（空 JSON 对象）。两者均要求真实用户登录、安装绑定的组织、实时管理员校验；不接受请求自带主机地址或 Worker ID。预检不改变任何状态，`ready` 和三项变更能力始终为 false。

现有现场后端 v0.6.5 不包含这些 API。只更新桌面时会显示“当前后端版本尚不支持 Spark 设备管理”，不会绕过鉴权连接 SSH。当前功能不能承诺可拔电，也没有提供真实执行或恢复命令。后续必须完成可恢复维护协议、已登记服务的停止/启动策略、开机恢复和实机关机验收，才能开放执行能力。

### 验证与回退

- Python：`python3 -m unittest discover -s deployments/spark/tests`。
- Go：`go test ./backend/internal/spark`、`go test ./backend/internal/api/handler -run Spark`。
- 桌面：管理员状态/只读预检、普通成员不查询、组织切换丢弃旧响应、30 秒过期清除、旧后端不支持、所有变更按钮禁用。
- 现场仅通过 SSH stdin 执行了 `--snapshot --node master`，没有持久安装。读取到 8 个业务 Pod、16 个 Docker 容器，其中 10 个已退出；计数是本次观察，不是固定部署配置。
- 未来若仅安装此只读模块，回退可关闭对应 unit、移除后端 `spark` 配置和专用 socket 挂载；不涉及业务容器副本、模型或网络配置。旧业务镜像的升级/数据库回退不包含在这个模块的回退范围内。

## 单机任务维护基础（尚未部署）

本次增加真实 Worker 准入、Server 新任务入口和可靠任务投递的持久维护检查。未配置时保留原行为；一旦配置，缺失、损坏、错主机或不支持的状态文件都拒绝新接单。暂停前已经持久接收的任务继续执行；重启后遗留 inbox 任务等待显式解除维护。维护不会延长任务有效期，过期任务仍按已有规则处理。自动化等来源可能继续产生待执行记录，不能把维护解释为冻结所有业务写入。

`maintenance_state.py` 目前仅提供安装端写入原语 `StateStore.initialize/read/change`，没有 CLI、网络变更入口或 sudo 授权。`change` 校验修订号与操作 ID，通过 flock、临时文件 fsync、同目录 replace 和目录 fsync 持久化。不要由业务容器调用，也不要在恢复出错时删除文件重新初始化。fsync/replace 失败需重新读取确认实际状态，不能仅凭异常假定未写入。

后续安装控制器时，由运维创建持久目录（示例 `/var/lib/leros-spark`），root 拥有、共享只读数值组、目录权限 2750，写入文件 0640；setgid 保证原子替换后仍可由只读服务组读取。Server/Worker **只读挂载整个状态目录**到相同的容器路径（示例 `/run/leros-spark-state`），不能用单文件 `subPath` 挂载，否则无法看到原子替换。目录应位于本机持久磁盘，不使用临时目录或网络文件系统。普通成员不能写入目录、配置或脚本。

未来部署配置示例（不是当前线上启用指令）：

```yaml
# Server，添加到既有 spark 配置内
spark:
  maintenance:
    state_path: /run/leros-spark-state/maintenance.json
    host_id: <与 spark.host_id 相同的本机 ID>

# Worker 配置文件顶层
spark_maintenance:
  state_path: /run/leros-spark-state/maintenance.json
  host_id: <同一台本机 ID>
```

Server 拦截 AddMessage、CreateInitialMessage、CreateTask、CreateSession、RunAutomationNow，返回 HTTP 503；查询、取消、审批回复和结果上报不受该入口限制。后台投递在领取和发布前检查。已在途的请求/投递最终由 Worker 准入检查兜底；状态查询等待在途准入完成后再生成任务计数，不能仅凭 Server 接单状态宣布排空。

桌面只读显示 Server 和各 Worker 的“尚未接入 / 状态未知 / 已暂停 / 正常接单”。旧 Worker 缺少维护字段表示尚未接入，缺失字段、状态过期或主机不匹配不能显示已暂停。**这不是完整关机功能**：完整排空控制器、模型/服务停止与恢复、系统关机及实机验收仍未完成；全部启停与关机按钮继续禁用。本次没有升级远端镜像、安装维护文件、暂停任务或改变运行服务。
