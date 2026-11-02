# Spark 停机计划校验

用于运维整理**单台主机的候选停止与恢复顺序**。工具只读取 JSON 文件，不调用 Docker、Kubernetes 或系统关机命令，不修改维护状态。

当前未接入客户端，不能据此开放“安全关机”。`policy_valid=true` 仅表示登记与本次快照匹配、依赖图有效；输出始终为 `executable=false`。

## 1. 准备清单

使用同一主机上 `power_agent.py --snapshot --node master` 的新鲜输出，保存为 `snapshot.json`。依赖现场 Kubernetes/Docker 查询权限；容器化代理沿用其实际配置。快照有效期 30 秒，未来时间容差 5 秒；文件不应包含环境变量、密钥或启动参数。

根据采集结果创建 `registry.json`。以下仅演示格式，必须替换主机标识和实际组件 ID；不要照抄为现场配置：

```json
{
  "version": 1,
  "host_id": "本机采集的 host_id",
  "targets": [
    {
      "id": "database",
      "kind": "docker",
      "component_id": "采集中的容器 ID",
      "scope": "local",
      "depends_on": [],
      "stop_strategy": "docker_graceful",
      "restore_strategy": "previous_state"
    },
    {
      "id": "worker",
      "kind": "kubernetes",
      "component_id": "lework-prod/实际 Pod 名称",
      "scope": "local",
      "depends_on": ["database"],
      "stop_strategy": "kubernetes_scaledown",
      "restore_strategy": "previous_state"
    }
  ]
}
```

- `depends_on` 表示依赖：示例中先停止 Worker，再停止数据库；恢复顺序相反。
- 必须覆盖快照内全部组件，包括已停止的容器；原本已停止的目标进入 `keep_stopped`，不会列入恢复顺序。
- `scope` 仅接受 `local`。跨机模型不能登记为本机可控模型；当前双机 DeepSeek 不属于单机启停范围。
- 策略名称只声明候选方式，不表示已实现或已验证。工具不接受 shell 命令、任意执行路径或未知配置字段。
- 当前精确匹配快照中的组件 ID；Pod 重建或容器变化会使登记失配。未来执行器必须另外绑定控制器 UID、记录副本数及自动重启策略，不能直接用本工具的 Pod 名称执行停机。

## 2. 执行只读校验

在仓库根目录运行：

```bash
python3 -I deployments/spark/shutdown_plan.py \
  --registry /实际路径/registry.json \
  --snapshot /实际路径/snapshot.json
```

每个输入文件最多 1 MiB，最多 256 个目标。工具不会保存或修改输入。退出码 `1` 表示仍被阻塞（当前所有有效输入均为此结果）；`2` 表示输入格式、读取或登记内容错误。

重点查看：

| 字段 | 含义 |
| --- | --- |
| `policy_valid` | 清单覆盖、状态与依赖关系是否通过本轮校验 |
| `unregistered` / `missing_targets` | 尚未登记的实际组件 / 清单中未找到的目标 |
| `stop_order` / `restore_order` | 候选停止 / 恢复顺序；校验失败时为空 |
| `keep_stopped` | 保持原停止状态的目标 |
| `blockers` | 漏登记、状态未知、循环依赖及尚未实现的执行条件 |
| `plan_digest` | 比较本次候选内容的摘要，不是执行授权或可重复使用的预检票据 |

容器运行、Pod 为 `Running` 仅用于候选排序，**不代表模型已就绪或服务健康**。任务与模型请求排空、控制器停止、原有副本/重启策略恢复、systemd 服务覆盖及系统关机仍需后续实现和独立验证。没有独立硬件证据时，客户端不能显示“可以拔电”。

## 3. 本轮现场核对（2026-10-30）

从当前代理只读采集到 25 项工作负载，清单采集完整。使用空登记文件校验，25 项均被正确识别为未登记，未生成停止顺序，`executable=false`。没有自动填入或猜测现场依赖，没有修改部署、维护状态或模型。

维护模式已完成的验收见 [Spark 维护模式部署](spark-maintenance-ui.md)。本工具属于后续停机计划基础，不是完整安全关机交付。
