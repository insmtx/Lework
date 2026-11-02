# Spark 本机模型持久启停流程

本功能只管理配置中唯一 Host 上已登记的本机模型，不关机、不强制停止、不替换默认模型。`spark.model_workflow: true` 是显式开关；同时必须启用 `model_control`、`maintenance_control`，并初始化共享持久数据库。初始化失败时服务器拒绝启动该配置，不降级到内存队列。未启用时保留旧的显式维护/模型接口。

## 操作与恢复语义

管理员确认一次后，POST `/v1/ops/spark/workflows` 返回 202。接受记录在 HTTP 返回前提交数据库；关闭浏览器不取消操作。`GET /workflows/current` 和 `GET /workflows/:operation_id` 读取数据库中的进度，agent 离线时仍可读取已有记录。相同 operation ID 和请求内容返回原记录；更换内容返回冲突。客户端不自动重试 POST。

流程先暂停新任务，等待所有配置的 Worker 对同一维护 revision/owner 达成一致，且十个工作计数均为零，再发送模型操作。等待空闲上限 10 分钟，整个操作上限 20 分钟。每次外部写入前先持久提交 intent，只有 CAS 成功者允许发一次请求；发送前再次读取完整状态。模型完成必须匹配 host/boot/model/action/operation/revision，不能凭物理 ready 判断成功。

Stop 完成后保持维护。Start 只有自己从接收任务切换到暂停时，才可在模型成功后恢复原入口；原本就在维护的 Start 保持暂停，管理员须明确恢复服务。恢复完成还需服务端与所有 Worker 确认相同的 inactive revision/owner。

手工 `/model` 和 `/maintenance` 使用同一个持久 Host reservation。它们也先写 intent，收到回执后保留占用，直到观察到精确的最终结果。手工暂停和恢复虽然共用维护 owner ID，数据库 reservation ID 每次独立生成。活动占用期间所有公开写能力为 false。内部协调读取原始状态，不会被自己的公开能力覆盖阻塞。

## 多副本、重启与超时

Host 表以 `host_id` 为主键，`org_id` 不允许通过更换配置重新绑定。所有副本必须共享同一数据库和安装绑定；上线前停用未接入 reservation 的旧副本。不得通过关闭 model_workflow 绕过未解决的占用；降级前必须按人工恢复流程完成隔离与核对。

只有接受记录的 runner 实例能继续新步骤。其他 runner 对记录只读，不抢占、不重放，也不写 interrupted 状态破坏仍健康的原实例。重启后没有自动接管；界面显示“等待核实/确认”，到 deadline 投影为需要管理员处理。期限到达不会清空 reservation。不能以数据库租约、原 revision 尚未变化或 agent 暂无响应证明请求没有在途。

任何 intent 后的 HTTP/数据库异常保留占用，结果为 unknown/attention。即使服务已经恢复，也不能把未确认的数据库记录当作安全重试许可。模型 busy 日志不会因为服务器或 agent 重启就自动重放。配置绑定、boot/model 或 root CLI 维护 owner/revision 改变都会阻止继续和自动恢复。

`maintenance_retained` 只在已确认 `completed_paused` 时为 true；其他阶段的 false 不表示任务入口已恢复。

## 人工恢复：先围堵旧执行者，再处理数据库占用

此版本没有网页“清除锁”按钮。下列流程是运维操作，不能由用户刷新页面或删除 host 日志代替：

1. 记录 operation ID、Host revision、operation version、boot/model/maintenance revision 和 owner；导出两张数据库记录供审计。不要复制 token、连接串或原始远端输出到用户诊断。
2. 停止所有会连接该 agent 的后端副本（包含旧版本和 CLI 代理）；确认 PID/容器退出，待请求已断开。仅关闭浏览器或一个副本不构成隔离。
3. 在目标机器核对 agent 的维护文件、模型 operation 日志以及 Docker 真实状态。若模型操作仍 busy、停止结果不明确或旧线程可能执行，继续保留数据库占用；先由负责该设备的运维人员完成模型控制组件的故障处理。**不得删除维护文件或模型 journal 来伪造完成**。当前协议不提供通用 busy reset，自助恢复此类故障仍是发布限制。
4. 需要保持维护时，通过现有 root 运维工具以准确 revision 执行 pause，建立新 owner/revision。明确模型与依赖可用且需要接收任务时，才执行明确的 resume，并核对全部消费者。CLI 命令参见 [维护运维文档](spark-maintenance.md)，不要手工把 JSON 改成 accepting。
5. 仅当旧进程已被隔离、设备最终状态可验证后，数据库管理员在单个事务中归档该 operation 为 `attention`（`error_code='interrupted'`），保留所有历史字段，并按以下 CAS 条件清空 Host 的占用。不要删除 Host 行或修改其 org。每条更新必须恰好一行；不满足就 ROLLBACK 并重新核查。

PostgreSQL 示例（占位值由步骤 1 的人工审查记录替换，执行前保留审计副本）：

```sql
BEGIN;
SELECT host_id, org_id, active_operation_id, revision
FROM leros_spark_workflow_host
WHERE host_id = 'REVIEWED_HOST' AND org_id = 7 FOR UPDATE;
SELECT operation_id, phase, version, runner_id, deadline
FROM leros_spark_workflow_operation
WHERE host_id = 'REVIEWED_HOST' AND org_id = 7
  AND operation_id = 'REVIEWED_OPERATION' FOR UPDATE;

UPDATE leros_spark_workflow_operation
SET phase = 'attention', error_code = 'interrupted', version = version + 1,
    updated_at = EXTRACT(EPOCH FROM NOW())::bigint
WHERE host_id = 'REVIEWED_HOST' AND org_id = 7
  AND operation_id = 'REVIEWED_OPERATION' AND version = 123;
-- 必须 UPDATE 1，否则 ROLLBACK。
UPDATE leros_spark_workflow_host
SET active_operation_id = '', revision = revision + 1
WHERE host_id = 'REVIEWED_HOST' AND org_id = 7
  AND active_operation_id = 'REVIEWED_OPERATION' AND revision = 456;
-- 必须 UPDATE 1，否则 ROLLBACK。
COMMIT;
```

6. 重启更新后的后端，先 GET current/status，核对没有占用、维护消费者一致、模型状态明确。旧 operation ID 保留原始历史，下一次操作使用新 ID。任何未知状态仍应阻止操作。

上述 SQL 不证明物理隔离；人工恢复不得跳过步骤 2–4。自动测试覆盖 reservation/CAS/历史保留和错误版本拒绝，真实硬件故障恢复尚未验收。

## 已验证与未验证矩阵

| 层 | 本次验证 | 限制 |
|---|---|---|
| Go/macOS 本地 | 临时 Unix socket 假 agent；正常/异常状态机、各 intent、不重放、十项计数、共享 reservation、race | 不代表 NVIDIA Spark 实机行为 |
| SQLite 本地文件 | 事务接受、同 ID 幂等、不同内容拒绝、四类写入竞争、CAS/释放、org 不可重绑 | 仅测试用，不作为生产数据库部署建议 |
| PostgreSQL 本地独立实例 | 20 并发 reservation 恰好一胜、同 ID、payload、org、CAS/释放 | 生产 DB 配置、网络故障、HA 切换未验收 |
| 桌面 React/store | 单次 Begin、读取进度、离线 agent 保留记录、严格协议、旧部署控制、身份隔离回归 | Windows/Linux/macOS 安装包实机重连未验收 |
| Python agent | 现有全部单测回归 | Linux ARM64 Docker/NVIDIA GPU 真实启动/正常停止和断电未验收 |
| MySQL 本地独立实例 | 20 并发 reservation、幂等、org、CAS/释放 | 生产配置及故障切换未验收 |
| 未测数据库 | MariaDB、其他方言 | 不声明支持，需按相同竞争/CAS测试验收 |

不能据此声明生产就绪。真实设备的 Worker 集合、所有模型调用入口、read-only mount、journal 持久化、DB 容灾、旧副本退出及人工故障恢复仍须单独验收。
