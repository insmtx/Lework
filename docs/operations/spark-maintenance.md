# Spark 暂停与恢复接单联调

## 范围

适用于已能显示 Spark 设备状态的单机部署。本阶段通过宿主机运维命令验证暂停与恢复接单，不停止模型、业务服务或设备；本页记录 CLI 联调，现场仍运行该版本。新增桌面维护操作的升级方法见 [维护模式部署](spark-maintenance-ui.md)。

`maintenance_ctl.py` 固定操作 `/var/lib/leros-spark/maintenance.json`，仅允许 root 执行 `init / status / pause / resume`。主机标识来自本机 `/etc/machine-id`，不接受客户端指定目标。`pause / resume` 必须带预期修订号及操作 ID；执行成功仅说明文件已写入并读回，仍需核对实际消费者状态。

## 安装与配置

1. 确认后端、Worker 镜像都包含维护准入实现；只读管理采集器可保持原部署方式。
2. 确认没有运行中任务；整个联调窗口暂停新任务提交，以及 Worker 创建、扩缩容和配置修改，再安排后端和 Worker 滚动重启。不要把当前截图的零任务作为持续空闲的保证。
3. 备份两个 ConfigMap 与两个 Deployment；备份含敏感配置，应置于仓库外的私有目录。
4. 将仓库 `deployments/spark/maintenance_ctl.py` 和 `maintenance_state.py` 安装到 `/usr/local/lib/leros-spark/`，root 拥有、权限 `0644`；安装目录应为 root 拥有、权限 `0755`。
5. 创建 root 拥有、组为 `lework`、权限 `2750` 的持久目录 `/var/lib/leros-spark`，执行一次初始化：

```bash
sudo python3 -I /usr/local/lib/leros-spark/maintenance_ctl.py init
sudo python3 -I /usr/local/lib/leros-spark/maintenance_ctl.py status
```

新安装的 `active` 应为 `false`。重复 `init` 保留已有状态；检测到初始化标记但状态文件丢失时拒绝重建，需运维核对并从可信备份恢复。不要删除状态文件或 `.maintenance-initialized` 来解除维护。

6. 后端配置正文的 `spark` 节点追加以下内容，保留原有 socket、主机、组织及 Worker 绑定：

```yaml
spark:
  maintenance:
    state_path: /run/leros-spark-state/maintenance.json
    host_id: "<与 spark.host_id 一致的本机 ID>"
```

Worker 配置正文顶层追加：

```yaml
spark_maintenance:
  state_path: /run/leros-spark-state/maintenance.json
  host_id: "<同一台本机 ID>"
```

7. 为后端和目标 Worker 追加宿主机目录 `/var/lib/leros-spark` → 容器 `/run/leros-spark-state` 的只读 `hostPath` 挂载（类型 `Directory`），并配置 `lework` 数值组权限及本机节点选择。挂载整个目录，不使用单文件 `subPath`。不要修改其他 Worker 共用的配置；先核对 ConfigMap 使用者。
8. 应用配置并等待两个 Deployment 滚动更新完成。在客户端刷新，确认服务端与 Worker 接单状态均为“正常接单”。若由 Helm 或其他控制器管理，需同步更新发布配置，防止后续升级覆盖。

当前现场 Worker 由后端调度器管理。现场旧版本的手工挂载尚未纳入调度器模板；新版可配置专用 Worker 绑定（见上述升级文档）。未升级前，重新创建 Worker、修改其配置或升级镜像后，必须复核挂载与接单状态。挂载丢失时 Worker 会拒绝接单，不能把本轮联调配置视为已完成长期部署集成。

## 暂停与恢复验证

先执行 `status` 获取当前修订号。下面的 `1 / 2` 仅展示首次操作的顺序，实际必须使用最新返回值；为每轮测试使用不同操作 ID。

```bash
sudo python3 -I /usr/local/lib/leros-spark/maintenance_ctl.py pause \
  --expected-revision 1 --operation-id smoke-20261030-01
```

刷新 Lework，确认**服务端和 Worker 接单状态都为“已暂停”**。新任务应收到维护提示；已接收任务允许完成。旧 Worker、状态未知或仅一方暂停均不能认定维护已生效。

本轮未停止服务。恢复前由运维确认后端、Worker 及其依赖正常，使用暂停返回的修订号和同一操作 ID：

```bash
sudo python3 -I /usr/local/lib/leros-spark/maintenance_ctl.py resume \
  --expected-revision 2 --operation-id smoke-20261030-01
```

再次刷新，确认双方恢复“正常接单”，再用无副作用的测试任务验证可执行。该命令不启动已停止服务、不自动检查依赖健康；如果曾停止服务，应先按对应恢复流程完成检查，不能直接用它代替完整恢复。

注意：维护不会延长任务有效期；超时任务仍按原有规则处理。暂停与任务数为零都不代表设备可以断电。

## 异常与回退

- 修订号或操作 ID 不匹配：保持现状，重新读取，确认是否有其他运维操作；不要盲目换修订号重试。
- 写入或刷盘报错：可能已替换文件，结果视为未知。先 `status`，再核对双方接单状态。
- 配置或挂载异常：已启用维护的消费者会拒绝接单；修复读取条件，不删除状态文件绕过检查。
- 若要撤销本轮部署，先显式恢复接单并确认空闲，再按备份还原本轮配置与挂载；保留持久状态与初始化标记。部分应用失败时按每个对象的实际状态回退，不覆盖其他人后续变更。自动回退持有与暂停写入相同的排他锁直到滚动更新完成；回退后 CLI 文件仍保留，但消费者已撤销接入，写入状态不代表可以暂停接单。

## 当前现场联调记录（2026-10-30）

### 部署与验证

- 目标：`lework-prod` 中的 `leros`、`leros-worker-o1-w1`，节点 `master`，组织 / Worker ID 均为 `1`。
- 后端、Worker 镜像均标记 `v0.7.1_57c8f57f`；采集器仍由独立 Deployment `leros-spark` 运行。
- 准备包：`/home/lework/leros-maintenance-20261030-k12tnoif`，含私有备份、带 UID/旧值校验的补丁及 SHA256 清单。应用前重新核对共享 ConfigMap 使用者，四个补丁通过服务端预检。运维已完成安装，两个 Deployment 均为 `1/1` 就绪。
- 持久目录为 `root:1000 / 2750`，状态文件为 `root:1000 / 0640`。本地 Python 测试 34 项、Go Worker 与状态读取并发回归通过；回退锁竞争及共享配置使用者检查已补充回归测试。
- 完整应用后的回退入口为准备包内 `bash rollback.sh --apply`，需先解除维护；部分应用失败或对象漂移须逐项核对备份。

| 验证项 | 现场结果 |
| --- | --- |
| 暂停状态传播 | 第 2 版状态为暂停；14:50:37 客户端显示服务端、Worker 均“已暂停” |
| 恢复状态传播 | 第 3 版状态解除暂停；14:51:41 双方均“正常接单” |
| 暂停时真实提交 | 第 4 版暂停期间，新建任务被拒绝，提示 `spark_maintenance_active` |
| 恢复任务调度 | 第 5 版解除暂停后，任务成功创建并由 Worker 执行 |
| 单机模型完整回复 | 临时 Qwen 下，任务 `task_ZZPdP8sgLw` 返回“接单恢复验证通过”；Worker 运行 `req_203` 于 15:58:24 发布 `run.completed` |
| 收尾 | 对话默认恢复 DeepSeek，翻译默认未改；临时 Qwen 于 16:01:53 正常退出，退出码 0；维护保持 `active=false, revision=5` |

以上时间均为北京时间。此记录确认暂停拒绝、恢复接单及恢复后的纯文字任务执行；不代表任务排空、模型自动启停、整机关机或开机恢复已完成验收。

### 模型验证边界

原默认模型 `deepseek-flash` 在 2026-10-28 18:51 已停止，早于本次维护联调；其端口 `30000` 拒绝连接。该容器配置 `--nnodes 2 --tp 2 --node-rank 0`，依赖双机运行，不能以恢复单机接单代替模型恢复。第一次测试运行 `req_199` 因此未得到回复，已在 15:17:18 取消，避免持续重试。

按运维选择，本轮仅临时启动本机既有容器 `spark-qwen3.6-35b-a3b`，约 3 分钟完成加载。Worker Pod 直接访问 `172.16.0.64:10026`，实测 1.12 秒返回纯文字结果。随后复用客户端已有的 `Qwen3.6-35B-A3B` 内置对话配置（代理地址 `http://10.42.0.1:58089`），临时设为对话默认，完成 Lework 任务验证。

测试结束后已恢复原 DeepSeek 对话默认；翻译默认、模型地址及密钥均未修改，另一台 Spark 未操作。停止临时 Qwen 前，其运行和等待请求数均为 0。原 DeepSeek 容器仍保持停止，恢复配置不代表默认模型已经可用。
