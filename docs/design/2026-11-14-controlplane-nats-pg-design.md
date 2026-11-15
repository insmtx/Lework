# 控制面接入 NATS + PostgreSQL 设计

> 日期：2026-11-14 ｜ 状态：已确认，待实现 ｜ 范围：控制面（`lework serve` 与裸 `lework`）
>
> 相关文档：`docs/architecture/as-built.md`、`docs/operations/control-plane.md`

## 1. 目标

把控制面从「进程内 Hub + 本地 SQLite」迁移到「NATS JetStream + PostgreSQL」基座，
使同一套代码可以部署多个 `serve` 实例并由负载均衡随意分发连接，同时**不改变**
现有 agent 协议、浏览器/手机 API 和交互行为。

裸 `lework`（同进程启动控制面 + 节点 agent）与 `lework serve` 共用同一套装配，
因此两者都要求 NATS + PG。当前仍只部署一个实例；本次交付的是多实例能力本身。

## 2. 非目标 / 反目标

非目标：

- 不做组织、多租户、RBAC；
- 不把 Session / 消息 / diff / 权限复制进 PG 或 NATS 作为第二状态机；
- 不改 agent 的出站 WSS 协议与注册流程；
- 不做前端事件回放（`?since=`）；断线仍是「重连 + 重拉快照」；
- 不复活旧企业版的 worker、组织化 subject 和业务表。

反目标：

- 控制面直接连 OpenCode；
- 保留 SQLite / 进程内总线的生产运行模式（含显式开关）；只有单测允许内存 store + 本地总线；
- 把「多实例」实现成「依赖 sticky session」。

## 3. 复用原有企业版设计（硬性要求）

这次不新造消息层，直接沿用旧企业版已验证的结构：

| 能力 | 复用物 | 说明 |
|---|---|---|
| 状态事件总线 | `mq.EventBus`（`Publish` / `SubscribeBytes`） | 状态类事件走 JetStream，可续传、可多实例 fanout |
| 节点请求（发送方） | `mq.CoreRequester.RequestReply` | Core NATS request/reply，与旧版 `WorkerOpsStatusSubject` 同一套路，不进 JetStream |
| 节点请求（应答方） | `mq.CoreSubscriber`（新增，约 30 行） | owner 订阅 `cp.node.<id>.request` 并 `Respond`；queue group 用 node ID |
| 瞬时事件 | `mq.CorePublisher`（新增，约 15 行） | `pty.output` 这类高频、无需回放的数据走 Core NATS |
| 消费组 | `mq.PullSubscriber`（新增，约 50 行） | 同名 durable **pull** consumer，多实例平分消息；现有 `Subscribe` 是 push consumer，无法共享 |
| Stream 声明 | `messaging.StreamConfigs()` | 新增控制面 stream，由 `mq.NewNATS` 启动时确保存在 |
| Subject 命名 | `backend/pkg/messaging` | 新增 `cp.` 前缀，避免与旧 `org.*` 通配冲突 |
| 配置 | `config.NATSConfig` / `config.DatabaseConfig` | 复用 `backend/config` 现有类型，不新造配置结构 |
| PG 连接 | `gorm.io/driver/postgres` | 控制面只 AutoMigrate 自己的 5 张表，不跑旧版全量迁移 |

三个新增接口只加在 `mq` 包，不改现有实现语义，旧企业版调用方不受影响。

## 4. 拓扑

```text
agent ──WSS──▶ 任意 serve 实例（owner）
                 │
                 ├─ 状态事件: bus.Publish("cp.events.node.<id>", event)        [JetStream]
                 ├─ 终端输出: core.Publish("cp.pty.<id>.<pty>", pty.output)     [Core NATS]
                 ├─ 订阅 "cp.node.<id>.request"，转发给本地 conn 并 Respond
                 └─ 每 10s: bus.Publish("cp.events.node.<id>", node.snapshot)

所有 serve 实例 ──SubscribeFrom("cp.events.node.>", DeliverNew)──▶ 本地 event.Hub ──▶ SSE
notifier        ──PullSubscribe("controlplane-push-consumer")──▶ 推送（集群内只发一次）
有终端客户端的实例 ──Core Subscribe("cp.pty.<node>.<pty>")──▶ 浏览器 WS
对账 ticker（每实例）: PG last_seen 超时 → bus.Publish("node.offline")

浏览器/手机 ──HTTP──▶ 任意 serve 实例
                 ├─ 本地 Registry 有连接 → 直接 conn.Request
                 └─ 否则 Core NATS RequestReply("cp.node.<id>.request") → owner → conn.Request
```

关键点：

- **状态事件只有一条发布路径**：agentgw 发布到 NATS，所有实例（含自己）从 NATS 收回后进本地 Hub。
- **请求两种路径**：本地连接直接走，跨实例走 Core NATS request/reply。
- **终端输出不复用状态事件路径**：`pty.output` 高频且无需回放，走 Core NATS，只有挂了该 PTY WebSocket 的实例按需订阅。
- **在线状态以 PG 为准**：`nodes.last_seen` + `heartbeat_timeout` 判定 `connected`，不依赖某实例的内存注册表。
- **无需 sticky**：SSE、普通 HTTP、PTY WebSocket 都能落在任意实例。

## 5. Subject / Stream / Consumer

新增 `backend/pkg/messaging/controlplane.go`：

```go
// cp.events.node.<node_id>：agent 状态事件与节点状态快照（JetStream）
func ControlPlaneEventSubject(nodeID string) (string, error)
func ControlPlaneEventWildcard() string // cp.events.node.>

// cp.node.<node_id>.request：跨实例转发 agent 请求（Core NATS）
func ControlPlaneNodeRequestSubject(nodeID string) (string, error)

// cp.pty.<node_id>.<pty_id>：终端输出（Core NATS，不入 stream）
func ControlPlanePTYSubject(nodeID, ptyID string) (string, error)

func StreamNameControlPlane() string        // CONTROLPLANE_EVENT_STREAM
func ControlPlanePushConsumer() string      // controlplane-push-consumer
```

Stream 配置沿用旧版 `SESSION_RUN_STREAM` 的参数思路：

```go
StreamNameControlPlane: {
    Subjects:          []string{ControlPlaneEventWildcard()},
    Storage:           nats.FileStorage,
    Retention:         nats.LimitsPolicy,
    Discard:           nats.DiscardOld,
    MaxAge:            24 * time.Hour,
    MaxMsgsPerSubject: 10000,
}
```

节点请求 subject 与 `cp.pty.>` **不注册到任何 stream**，与旧版 `ops.status` 一致。
`pty.output` 单条可能很大且频繁，写进 JetStream 会撞 `MaxMsgsPerSubject` 被 `DiscardOld`
直接丢帧，还多一次同步 PubAck；终端断线本就该丢数据，不需要回放。

## 6. 代码改动（按包）

### 6.1 `backend/pkg/messaging/controlplane.go`（新增）

subject、stream、consumer 常量与构造函数（含 `cp.pty.*`）；补单元测试（参考 `subject_test.go`）。

### 6.2 `backend/internal/controlplane/config`

- 新增 `NATS *config.NATSConfig`、`Database *config.DatabaseConfig`。
- 新增 `ValidateCluster()` 要求两者存在且 URL 合法，由进程入口在打开资源前调用；
  `Validate()` 保持原职责，避免单测被迫构造无关依赖。错误信息不回显凭据。
- 统一配置文件加载（`backend/cmd/lework/fileconfig.go`）新增顶层 `nats:`、`database:` 段，
  因此 `deployments/devcj/server.config.yaml` 可以被直接复用；`lework.yaml` 也补同样两段。
- 新增 `--nats-url`、`--database-url` flag，优先级仍是 flag > 文件。

### 6.3 `backend/internal/controlplane/store`

- 新增 `OpenPostgres(cfg config.DatabaseConfig) (*Store, error)`：GORM postgres + AutoMigrate。
- 保留 `Open(path)`（SQLite）仅供单测使用，不再由 `serve` 调用。
- 记录「运行时唯一真相在 OpenCode」的约束不变。

### 6.4 `backend/internal/controlplane/agentgw`

- 构造参数从 `hub` 改为三条依赖：`bus mq.EventBus`、`core mq.CorePublisher`、`coreSub mq.CoreSubscriber`。
- 节点上线：
  - 用**每连接一个可取消 ctx** 订阅 `cp.node.<id>.request`（Core NATS，queue group = node ID）；
    处理函数把请求转给 `conn.Request`，成功回 `Result`、失败回 `Error`，最后 `msg.Respond`。
    queue group 保证重连瞬间出现双 owner 时也只有一个实例应答。
  - 发布 `node.connected` 状态事件。
- 状态事件：`bus.Publish(ControlPlaneEventSubject(id), event)`。
- 瞬时事件（`pty.output` / `pty.exit`）：从 `Data` 解析最小字段 `pty_id`，
  `core.Publish(ControlPlanePTYSubject(id, ptyID), event)`；`pty_id` 缺失或解析失败时丢弃并记日志
  （不打印负载，终端内容可能含机密）。
- 状态事件发布失败即关闭该连接，让 agent 重连并触发 resync；PTY 事件发布失败只丢该帧。
- 节点下线：取消该连接的 request 订阅 ctx，发布 `node.offline`。
- 断开顺序保持现有「先 Unregister 再关连接」的约束。

### 6.5 `backend/internal/controlplane/event`

- `event.Hub` 保持不变（本地扇出、慢消费者淘汰策略不变）。
- 新增桥接器：`SubscribeBytes("cp.events.node.>", 0, handler)`（DeliverNew）把 NATS 事件写入本地 Hub。
- 仅生产装配使用 NATS 桥；单测可注入直接连 Hub 的本地实现。

### 6.6 `backend/internal/controlplane/node`

- 新增 `Router` 接口：`Request(ctx, nodeID, method, params)`。
- 实现：本地 `Registry` 有连接直接用；否则走 `mq.CoreRequesterBytes.RequestBytes`；
  只有 `nats.ErrNoResponders` 映射为 `ErrNodeOffline`，超时仍按 504 上报，避免把「慢节点」说成离线。
- `api.callAgent`、PTY 的 attach/write/resize/close 全部改走 `Router`。
- 在线判定改为 PG `nodes.last_seen`（新增 `store.IsOnline(nodeID, now, timeout)` 或在列表查询里算），
  不再用本地 `registry.Get` 判断节点是否存在。

### 6.7 `backend/internal/controlplane/api`（PTY 跨实例）

- `handlePTYWebSocket` 不再只订阅本地 Hub，而是 attach 成功后按需
  `core.Subscribe` 订阅 `cp.pty.<node>.<pty>`，只转发匹配该 PTY 的 output；
  `pty.exit` 与现有一致用于收尾。
- 订阅先于 attach 建立（保持现有「先订阅再 attach」的顺序，避免握手期丢输出）。
- 浏览器落在哪个实例与 PTY 在哪个实例无关；同一实例上多个 PTY 各自一条订阅。

### 6.8 `backend/internal/controlplane/overview`

- 新增事件 `node.snapshot`：payload 是该节点的 `[]overview.Session`，语义为**整节点替换**。
- owner 每 10s 为本地每个节点发布一次快照；新实例启动后 10s 内收敛到一致视图。
- `api/overview` 的节点集合来自 PG `nodes` 表，`connected` 由 `last_seen` 判定；
  `node.connected` / `node.offline` 事件只用于触发客户端刷新。
- 新增对账 ticker（每个实例都跑，10s）：查询 `last_seen < now - heartbeat_timeout` 的节点，
  若本地投影仍认为它在线则发布一次 `node.offline`。本地 map 记录已发布状态避免刷屏；
  owner 正常断开时自己也发，两个来源幂等。**不做选主**：owner 异常死亡后最多 10s + timeout 收敛。

### 6.9 `backend/internal/controlplane/push`

- `Notifier` 改用 `PullSubscriber` 订阅 `cp.events.node.>`，durable consumer
  `controlplane-push-consumer`；每个实例都跑，消息被集群内一个实例取走，同一条审批只推一次。
- handler 完成解码并交给本地推送队列后即 Ack（推送本身异步）；实例崩溃未 Ack 时由 JetStream 重投，
  可能重复推送一次，可接受（本地 `seen` 去重会压掉短时间内的重复）。
- 推送失败仍旁路降级，不影响事件转发。

### 6.10 `backend/internal/infra/mq`（新增接口，不改现有语义）

```go
// CorePublisher 发送不落盘的实时数据（如 pty.output）。
type CorePublisher interface {
    PublishCore(ctx context.Context, subject string, data []byte) error
}

// CoreSubscriber 订阅 Core NATS，把请求体交给 handler，并把返回值作为回复。
type CoreSubscriber interface {
    SubscribeCore(ctx context.Context, subject, queue string, handler func(context.Context, []byte) []byte) error
}

// CoreRequesterBytes 是 RequestReply 的字节版，避免控制面依赖 nats.Msg。
type CoreRequesterBytes interface {
    RequestBytes(ctx context.Context, subject string, request []byte) ([]byte, error)
}

// PullSubscriber 用同名 durable pull consumer 做集群内消费组。
type PullSubscriber interface {
    SubscribePull(ctx context.Context, stream, subject, durable string, handler func(context.Context, []byte) error) error
}

// EventSubscriber 以原始负载订阅 stream 事件，供不关心 nats.Msg 的调用方使用。
type EventSubscriber interface {
    SubscribeBytes(ctx context.Context, subject string, startSeq int64, handler func(context.Context, []byte) error) error
}

// ErrNoResponders 让控制面无需直接 import nats 就能识别「没有 owner」。
var ErrNoResponders = nats.ErrNoResponders
```

- `SubscribePull` 内部 `js.PullSubscribe` + `Fetch` 循环；handler 正常返回后 `Ack`，
  返回错误时 `Nak`。多个实例同时 Fetch 同一条 durable 时消息被平分。
- 现有 `Subscribe` / `SubscribeFrom` / `SubscribeManualDurable` 行为不变。

### 6.11 `backend/internal/controlplane`（装配）

- `New` 接收已打开的 `mq` 组合依赖（`EventBus` + `CorePublisher` + `CoreSubscriber` +
  `CoreRequester` + `PullSubscriber`），装配 bridge、Router、快照发布器、对账 ticker。
- `Close` 依次停止 ticker、桥接、pull 订阅，再关闭 bus 与 store。
- `Run` 的 SSE / HTTP / 生命周期逻辑不变。
- 单测用 fake bus + 内存 store，不依赖真实 NATS/PG。

### 6.12 `backend/cmd/lework`

- 裸 `lework` 与 `serve` 都必须读到 NATS 与 PG 配置，缺失时启动失败并给出明确错误；
  `openControlPlane` 只保留一条装配路径。
- 新增 `--nats-url`、`--database-url` flag，统一配置加载新增顶层 `nats:`、`database:` 段。
- `lework -h`、示例配置、`docs/operations/control-plane.md` 同步更新。

## 7. 兼容与迁移

- 现有 SQLite 数据不自动迁移：节点会重新注册；`preferences`（我的目录、上次目标）需要重选。
  如需要，可另加一次性导出脚本（本次不做）。
- 裸 `lework` 与 `serve` 统一要求 NATS + PG，本地零依赖模式删除（单测除外）；
  `deployments/*/lework.yaml` 需要补 `nats:` / `database:` 段。
- 单实例部署的行为与现在一致；多实例只要求「同 NATS、同 PG、普通轮询负载均衡」。
- 事件 subject 使用 `cp.` 前缀，与旧版 `org.*` stream 不重叠。

## 8. 失败与边界

- 无 owner 应答：Core NATS 返回 `nats.ErrNoResponders` → API 返回 409 `node_offline`。
- owner 异常死亡：请求立即 409；对账 ticker 在 10s 内按 PG `last_seen` 发布 `node.offline`，
  overview 的 `connected` 本来就以 PG 为准，不依赖事件。
- 节点重连竞态：旧连接关闭、新订阅建立期间可能短暂双 owner；request subject 的 queue group
  保证只有一个实例应答，最终由 PG `last_seen` 收敛。
- NATS 不可用：控制面启动失败；运行期状态事件发布失败关闭 agent 连接促使其重连（触发 resync），
  PTY 帧直接丢弃。
- PG 不可用：启动失败；运行期请求返回 5xx。
- 推送重复/丢失：pull consumer 保证集群内只有一个实例取走；实例崩溃未 Ack 会重投，可能重复推送一次。
- 终端跨实例：`pty.output` 无回放，实例或网络断开期间的输出不补偿；浏览器重连后重新 attach。
- Hub 慢消费者：保持现有淘汰 + SSE `resync` 行为。

## 9. 测试

- 单测：
  - subject / stream / consumer 构建；
  - `Router` 本地命中、远程回退、`ErrNoResponders` → `ErrNodeOffline`；
  - `node.snapshot` 整节点替换投影；
  - config 校验（缺 NATS/PG 必须失败）与 `fileconfig` 两条路径；
  - `mq` 新增三接口（Core publish/subscribe 应答、pull 只投一次）；
  - SQLite 存储回归（仅测试用）。
- 集成（`-tags integration`，NATS + PG）：
  - 实例 A 的 agent 状态事件能被实例 B 的 SSE 收到；
  - 请求打到 B、节点连在 A，仍能成功；
  - PTY：浏览器在 B 上 attach A 的终端，输入、输出、resize、exit 全通；
  - 两个实例同时跑 notifier 时推送只发一次；
  - 杀掉 owner 后 10s 内收到 `node.offline`，且 overview 显示离线；
  - 实例 B 启动后 10s 内 overview 与 A 一致。

## 10. 工作拆分

1. `mq` 新增 `CorePublisher` / `CoreSubscriber` / `PullSubscriber` + 单测。
2. `messaging` 的 `cp.*` 常量与 stream 配置 + `controlplane/config` 的 NATS/PG 校验与 flag。
3. `store.OpenPostgres` + 装配路径统一（裸 `lework` 与 `serve` 同一入口）。
4. NATS → 本地 Hub 桥接 + agentgw 发布改造（状态事件 + PTY） + `node.snapshot`。
5. `Router` 请求转发 + API/PTY 在线判定与跨实例终端订阅。
6. push pull consumer + 节点对账 ticker。
7. 集成测试、示例配置与运维文档。

## 11. 留给 builder 的决定

- durable consumer 的 `MaxAckPending`、重连退避等参数按现有 `mq` 默认值，不额外调优。
- 对账 ticker 周期 10s，与 `heartbeat_timeout` 同源配置。
- PTY 转发沿用现有 `ptyBridgeBuffer` 与丢弃策略，不新增背压机制。
- 事件回放（`?since=`）本次不做，但 `Seq` 字段保持原样，后续可接。
- 旧 SQLite 数据的一次性迁移脚本可选，不阻塞本次交付。
