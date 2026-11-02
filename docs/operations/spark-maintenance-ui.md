# Spark 维护模式部署

用于单台 Spark 的管理员维护操作。仅切换新任务准入，不停止正在运行的任务、不启动模型、不关机。模型和依赖是否可用仍需单独检查。

## 配置清单

| 位置 | 变更 |
| --- | --- |
| Lework 桌面客户端 | 升级到包含维护按钮的构建 |
| Leros 后端 | 升级维护 API 与调度器实现，启用 `spark.maintenance_control` |
| 本机 Worker | 保留已有维护状态读取能力，使用专用 ConfigMap |
| `leros-power-agent` | 升级脚本，显式增加 `--enable-maintenance`；默认仍只读 |
| 宿主机状态 | 沿用 `/var/lib/leros-spark/maintenance.json`，不得删除重建 |

当前现场代理由 Kubernetes Deployment `leros-spark` 运行，不能直接套用仓库 systemd 单元；需要运维同步其镜像、启动参数和权限。以下是部署要求，不是对现场已升级的声明。

## 操作步骤

1. 安排空闲窗口，暂停新任务提交及 Worker 创建/配置变更，备份现有镜像、启动参数、两个 ConfigMap 和 Pod 模板。含密钥的备份放在仓库外的私有目录。
2. 按 [宿主机维护配置](spark-maintenance.md) 保留已初始化的状态及只读消费者挂载。记录本机 `host_id`、节点标签、组织和 Worker ID。状态目录应为 `root:lework / 2750`，文件为 `root:lework / 0640`。
3. 将 `power_agent.py`、`maintenance_ctl.py`、`maintenance_state.py` 一起升级到 root 管理的安装目录。只有启用维护写入的代理以 root 运行，业务后端和 Worker 仍只读。代理只开放 Unix socket，不开放 TCP 管理端口；socket 目录和组权限只授予后端所需访问者。
4. 启用代理参数 `--enable-maintenance`，允许其写入持久状态目录；同时保留原有本机 `/etc/machine-id`、boot ID、Docker/Kubernetes 采集条件。代理不会自动初始化缺失的状态。Kubernetes 使用宿主机目录读写挂载给代理，不能使用 `emptyDir`；不要把写权限授予业务容器。
5. 在后端原有 `spark` 配置中增加开关，并将调度器绑定加入实际 `scheduler` 配置：

```yaml
spark:
  maintenance_control: true
  # 保留 socket_path、host_id、org_id、worker_ids、maintenance

scheduler:
  spark_maintenance:
    org_id: 1
    worker_ids: [1]
    node_name: master
    config_map: leros-spark-worker-config
    read_group: 1000
```

以上现场示例须与 `spark.org_id / worker_ids` 和节点实际标签一致。`scheduler` 的位置应沿用现有部署配置。`leros-spark-worker-config` 必须为这些 Worker 的专用 ConfigMap，复制其现有有效配置后保留顶层：

```yaml
spark_maintenance:
  state_path: /run/leros-spark-state/maintenance.json
  host_id: "<本机 host_id>"
```

全局 Worker ConfigMap 不应带本机 `spark_maintenance` 配置，避免其他 Worker 误用。调度器自动生成只读目录挂载、节点选择和读取组，重建时会保留；缺目录时启动失败，不会创建空目录后绕过维护。未绑定的 Worker 不注入这些配置。

6. 更新后端、代理及目标 Worker，等待就绪。若由 Helm 管理，同步到发布配置。用管理员进入“系统设置 → Spark 设备管理”，刷新后应显示“正常服务”，并可点击“进入维护模式”。缺配置、状态过期或消费者不同步时按钮保持不可用。
7. 确认进入维护，刷新直到服务端与 Worker 均为“维护中”；实际提交一条无副作用任务，确认被拒绝。检查模型与依赖就绪后点击“恢复服务”，刷新确认“正常服务”，再验证任务能完成。
8. 在空闲窗口重建目标 Worker，确认只读目录、专用 ConfigMap、节点和组权限保留，维护状态读取正常。未经此现场验证不能宣称部署验收完成。

若采用宿主机 systemd 部署，可在原 unit 的 drop-in 中设置 `User=root`、保留读取组，清空并替换 `ExecStart` 为原命令加 `--enable-maintenance`，增加 `ReadWritePaths=/var/lib/leros-spark`。先确保原 `KUBECONFIG` 在 root 服务环境可读，避免升级后失去采集能力。不要同时启动另一份代理争用同一 socket。

## 状态与回退

- 写入成功仅代表维护设置持久化；界面必须取得主机、后端和所有已绑定 Worker 同修订号的状态，才显示确认完成。
- 修订冲突、超时或连接断开：先刷新核对，不自动重试写入。后端拒绝旧开机标识和旧修订号。
- 恢复服务不保证模型已启动；模型不可用时，任务仍可能失败。
- 回退 UI 操作能力时关闭 `spark.maintenance_control` 并移除代理写入参数，但保留维护状态和消费者读取配置。若当前处于维护中，先通过已验证的 CLI 恢复并确认，不得删除状态解除限制。
- 模型启停和关机按钮继续禁用。维护中、零任务和设备断连均不表示可以拔电。

## 开发验证记录（2026-10-30）

本地 Python 36 项、桌面 57 项、状态层 87 项及桌面类型检查通过，Go 静态检查与构建通过；新增 Spark API、状态读取和调度器并发测试通过。Kubernetes 默认文件权限造成重复重建的比较问题已补充回归测试。以上不代替现场升级验收。

全量 Go 测试仍失败于既有 `TestComputeOccurrenceWindow_IntervalAnchorGrid`。扩大到整个 HTTP handler 包的并发检查，还发现既有 Gin 上下文与数据库访问竞争，涉及 `TestSendPhoneLoginCode_TooFrequent`、`TestSendPhoneLoginAndLoginByPhoneCode`、`TestCreateUser_DuplicatePhone`、`TestGetUser_ByPublicID`、`TestUpdateUser_Success`、`TestUpdateUser_MissingPublicID`、`TestListUser_All`、`TestListUser_ByKeyword`、`TestListUser_Pagination`、`TestUserCreateReadUpdateFlow`；本次未修改这些接口，不宣称全仓库测试全绿。

## 当前现场升级准备

已为 `master / 组织 1 / Worker 1` 生成私有准备包：`/home/lework/leros-maintenance-ui-20261030-Kvlopp`。两个镜像在 Spark 本机构建并保存离线包，未推送镜像仓库；现场应用结果见下文。准备包包含原配置备份、带 UID/旧值校验的双向补丁、校验清单及操作说明。

升级流程测试 9 项、镜像内测试 20 项、隔离容器权限测试及 6 项 Kubernetes 服务端预检通过。现场预检时维护为 `active=false, revision=5`，Worker 空闲；执行前将重新检查。

运维在空闲窗口执行准备包的 `upgrade.py --apply`，会暂停新任务、更新代理与后端、将 Worker 切到专用配置并重建，验证后恢复任务准入。变更阶段失败时保留维护；若输出 `resumed_confirmation_pending`，表示已恢复但确认未完成，应刷新核对，不重复执行。`--rollback` 只回退本包预期改动，发现漂移即停止。全程不停止模型、不重启整机。


### 现场升级结果（2026-10-30）

运维已执行升级命令，随后只读核对通过：后端与设备代理使用本地升级镜像，Worker 镜像保留原版本；三个 Deployment 均为 `1/1` 就绪，Pod 重启计数均为 `0`。后端维护控制已启用，调度器绑定到 `master / 组织 1 / Worker 1`，Worker 使用专用 `leros-spark-worker-config`，全局 Worker 配置已移除本机维护绑定。

升级结束状态为 `active=false, revision=7, operation_id=upgrade-20261030-Kvlopp`。设备代理与 Worker 均确认该状态，Worker 任务计数为零。模型与整机均未执行启停。

刷新验收时发现 Kubernetes 清单采集失败：现场镜像入口脚本生成 `/tmp/kubeconfig` 后通过旧的 `SPARK_KUBECTL_ARGS` 传递，新版采集程序不再读取该参数。已在代理 Deployment 补充标准环境变量 `KUBECONFIG=/tmp/kubeconfig`，保留原镜像入口与 ServiceAccount 权限。代理重建后页面显示“采集完成”、25 项工作负载，服务端和 Worker 均为“正常服务”；维护仍为 `active=false, revision=7`。

后续发布配置须保留这一环境变量及原入口脚本。该配置仅适用于当前镜像生成上述文件的部署；其他镜像应使用其实际只读 kubeconfig 路径。现场补丁前备份为准备包内 `agent-kubeconfig-before.json`，原升级包的严格回退会检测到此次代理配置漂移，不能直接重跑原包覆盖现场。

### 客户端维护闭环验证（2026-10-30）

经用户授权，通过桌面客户端实际点击并确认“进入维护模式”：17:30:03 页面显示服务端和 Worker 1 均为“维护中”，运行与排队任务均为 0。随即点击并确认“恢复服务”，17:30:12 页面显示双方均为“正常服务”。随后只读核对设备代理、持久状态及 Worker，确认 `active=false, revision=9`，Worker 空闲。

本轮验证了客户端确认、维护写入、双方状态同步及恢复流程；未启动或停止模型，未执行关机，也未重新提交推理任务。模型可用性、服务排空和安全关机不属于本轮验收结果。
