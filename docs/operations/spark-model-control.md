# Spark 本机 Qwen 模型启停

> 2026-11-02 启动兼容修复已部署并通过真实 GPU 启动与推理验收。实际 vLLM 在 `build_app` 返回前已构建中间件栈，包装器现改为在应用构造时安装准入控制。65 项 Python 测试通过。现场修复包 `/home/lework/leros-model-fix-20261102` 已执行，旧容器与失败记录保留；一次性修复脚本不可重跑。

本次现场验收结果：客户端启动流程约 **3 分 27 秒**，模型显示“已就绪”，容器重启次数为 0；健康检查返回 200。维护期间真实推理请求返回 503（`model_maintenance`）；通过客户端恢复服务后，后端与 Worker 已确认解除维护（`active=false, revision=14`），真实对话接口返回 200 和 `OK`，本次短请求耗时约 0.59 秒。操作占用已释放，客户端显示“新任务：允许提交”。这些时间仅为本次实测，不是启动时限或性能承诺；修复后的完整停止流程尚未验收。

本功能只控制安装时登记的一台宿主机上的一个 Docker Qwen 容器。启用 `model_workflow` 后，管理员从客户端发起启动或停止，后端持久记录操作并执行维护、空闲检查和模型控制；只有工作流自身进入的维护才会在启动成功后自动解除，停止成功后保持维护。高级维护入口仍可手动操作。停止等待请求排空，Docker 使用无限等待的正常停止方式，不升级为强制杀死。详见 [模型操作工作流](spark-model-workflow.md)。

现场原默认 DeepSeek 为双机模型，已在本轮工作前停止。2026-11-02 按用户要求，已将默认组织（org_id=1）的对话和翻译默认模型分别切换为本机 Qwen 配置（ID 1、2）；该组织所有账号的新请求均使用这些默认项，显式指定模型的请求除外。两个配置均通过真实短推理验证；未修改地址或密钥，DeepSeek 配置保留，另一台 Spark 未操作。变更备份位于 `/home/lework/leros-spark-default-model-20261102`。已提交的旧任务不会被自动重新路由。

## 安装绑定

后端原 `spark` 节点增加 `model_control: true`、`model_workflow: true`，`device_id` 使用发现服务持久 ID（`/var/lib/leros-discovery/id`），`device_name` 为设备显示名称，保留已有主机、组织、Worker、socket 与维护配置。采集器保留 `--enable-maintenance`，追加 `--enable-model-control`；必须保留 `KUBECONFIG=/tmp/kubeconfig` 及已有入口脚本、环境、权限和挂载。

`/etc/leros-spark/model.json` 由 root 管理，字段严格如下；容器及镜像必须使用完整 ID：

```json
{
  "version": 1,
  "host_id": "与现有维护配置一致的宿主机标识",
  "model_id": "qwen",
  "label": "Qwen3.6-35B-A3B",
  "scope": "local",
  "container_id": "完整的64位Docker容器ID",
  "image_id": "sha256:完整镜像摘要",
  "container_port": 8888,
  "served_model": "qwen3.6-35b-a3b"
}
```

控制器通过本机 Docker socket 核对精确容器身份，再经 `/run/leros-model/admission.sock` 查询模型准入和固定健康接口。模型侧仅代理 `GET /health`、`GET /v1/models`、`GET /metrics` 到容器内 `127.0.0.1:8888`，不接受任意地址或路径。这样不依赖当前 Wi-Fi/LAN IP，也不要求 Kubernetes Pod 直连 Docker bridge。跨主机和双机模型不属于此安装方式。

模型镜像在原实际镜像 ID 上增加 `model_entrypoint.py`、`model_admission.py`、`model_control.py`、`maintenance_state.py`。保留原 `/bin/bash /opt/nim/start_server.sh` 入口，将原 Cmd 包装为：

```text
python3 /usr/local/lib/leros-spark/model_entrypoint.py <完整原始 vllm serve 参数>
```

包装器强制单个 API 进程，应用内准入中间件安装完成后才发布 Unix 观察 socket。维护时拒绝新推理请求，流式响应结束前持续计入在途请求。不可绕过包装器启动已登记容器。

| 宿主机路径 | 模型内路径及权限 | 采集器内路径及权限 |
| --- | --- | --- |
| `/var/lib/leros-spark` | `/run/leros-spark-state`，只读 | 原路径，读写 |
| `/run/leros-model` | 原路径，读写 | 原路径，只读目录 |
| `/etc/leros-spark/model-admission` | `/etc/leros-spark`，只读目录 | 随配置目录只读 |
| `/etc/leros-spark` | 仅挂载上述 admission 子目录 | 原路径，只读目录 |

现有模型用户为 `nvs:1000`，保留该用户并补充组 `999`；`/run/leros-model` 为 `root:999 / 2770`。admission 子目录为 `root:1000 / 0750`；其中 `model-admission.json` 包含 `host_id`、`model_id`、新容器完整 `container_id` 和 `container_port: 8888`，权限为 `root:1000 / 0640`。它在新容器创建后、任何启动前原子发布；挂载目录以支持安全的文件替换。现有状态目录保持 `root:1000 / 2750`、维护文件 `0640`，不重新初始化。控制器运行时创建独立的 `model-operation.json`，不可删除它来清除失败或未知操作。

## 现场准备包与执行边界（2026-11-02）

准备包：`/home/lework/leros-model-control-20261102`。包内包含镜像、私有备份、带 UID 和旧值校验的补丁及 SHA256 清单；已通过下述续装流程完成升级，未启动或停止 Qwen。旧 Qwen 保持停止，原默认 DeepSeek 配置保持不变。

准备验证已完成：17 项部署及配置测试通过；真实 Qwen 镜像中的包装器、原参数和 vLLM 钩子检查通过。三个最终镜像中的文件与当前源码/后端二进制逐项 SHA256 一致，证据在包内 `image-source-proof.json`。33 个文件纳入校验清单，现场只读检查返回 `CHECK PASSED`；维护仍为 `active=false, revision=9`，原 Qwen 保持停止。此处表示准备完成，不表示升级或真实模型启停验收完成。

### 历史阶段：续装完成（2026-11-02，启动修复前）

续装后的只读核验已确认：`applied.json`、`resumed.json`、`continuation-complete.json` 均存在；后端及管理代理与准备补丁一致，Worker 配置未变，三项部署健康。维护恢复为 `active=false, revision=11`。Qwen3.6-35B-A3B 已登记，状态 `stopped`、`busy=false`，新容器保持 `created`，旧容器保持 `exited`。`GlobalConfig` 已返回匹配发现服务的设备 ID 和 `management=true`。本地客户端使用原登录配置重启后，设置页已显示设备“已连接”、`Qwen3.6-35B-A3B · 已停止`、“新任务：允许提交”，启动按钮可用，停止按钮禁用。本轮未执行真实模型启动、推理或停止验收。

以下保留首次中断及恢复过程供排查：

2026-11-02 首次执行已完成暂停、镜像导入、目录创建及新停止容器创建；在配置核验阶段因 Docker 将 `HostConfig.OomKillDisable` 的未设置值 `null` 规范化为 `false` 而中断。后端、采集器及后端配置仍为升级前版本，维护为 `active=true, revision=10`；旧模型已改名保留，新模型仍为 `created`，均未启动。原安装包不可重复执行。

独立续装包：`/home/lework/leros-model-control-20261102-recovery`。仅允许该字段的 `null → false` 等价转换，仍拒绝 `true` 及其他配置漂移；19 项测试通过。`continue_install.py --check` 检查普通用户可读的现场状态；root 私有记录和目录内容在 `--apply` 写入前再次核对。续装复用现有容器，从登记配置开始完成剩余步骤；保持独占管理窗口。当时使用的命令如下，已执行完成，不可重复运行：

```bash
sudo python3 -I /home/lework/leros-model-control-20261102-recovery/continue_install.py --apply
```

### 原始安装入口（仅用于理解首次安装流程；当前现场不可重跑）

现场运维执行入口（需在 Spark 上以 sudo 授权安装 root 管理文件）：

```bash
sudo python3 -I /home/lework/leros-model-control-20261102/upgrade.py --apply
```

包内 `upgrade.py --check` 读取最新维护状态、绑定 Worker 的十项计数、服务健康、容器精确身份及配置，并进行 Kubernetes 服务端补丁预检；不变更维护、Deployment 或容器。准备基线为维护 `active=false, revision=9`；如果现场已变化，脚本拒绝继续，需重新审查生成包，不能修改修订号后盲目执行。

安装期间应暂停新任务提交、Worker 创建/修改，以及所有管理员的模型启停和维护操作。维护锁不能阻止其他请求提前登记，须保持独占安装窗口。实际升级须由拥有 sudo 权限的运维执行准备包内命令。`--apply` 为一次性操作：

1. 再次验证基线，持有与管理 API 共用的维护排他锁，暂停准入；新鲜核对服务端和 Worker 均确认暂停且十项计数为零。
2. 向 k3s 导入离线后端、采集器镜像，安装 root 管理的模型绑定和 socket 目录。
3. 将旧 Qwen 重命名为保留备份；完整复制原环境、GPU、限制、端口、重启策略和实际卷身份，新建带准入包装器的 Qwen，状态为 **created / stopped**。
4. 以精确容器 ID 和镜像 ID 写入绑定，更新采集器和后端；Worker 不升级、不改配置。
5. 确认部署健康、配置无漂移、旧新模型均停止、采集器 `GET /model` 返回 `stopped` 且 `busy=false`、维护状态一致，然后恢复升级前的正常接单并复核。

包中 `apply-started.json` 防止重复执行。失败报告具体阶段并保留私有日志；恢复接单前的失败保持维护，不自动回退、删除容器或重放模型操作。若恢复接单后的复核失败，脚本在同一维护锁内重新暂停；报告的 `active` 为实际文件值。如果重新暂停本身失败，应视为状态未知，不能假定仍在维护。本包启用持久工作流，旧 `--resume`、`--rollback` 入口均拒绝执行，避免绕过数据库中的宿主机占用；异常须先审查持久操作记录再决定恢复步骤。

## 回退和异常处理

本包保留 Kubernetes 原配置、反向补丁、原停止容器和私有日志供恢复审查，但不提供绕过持久工作流的自动回退。发生异常后，先核对工作流数据库记录、维护文件、Kubernetes UID/配置和容器真实状态，再制定恢复步骤；不得强制覆盖配置、删除操作记录或盲目重试 `--apply`。

模型操作出现 `unknown`、`error` 或未确认结果时，不得把 HTTP 200 或请求已提交视作完成。启动、排空、停止以及未知结果使用持久记录；有未完成操作时不允许解除维护。停止结果未知将持续保留忙碌状态，需先核对 Docker 真实状态和操作记录。

`/run` 为临时目录，准备包在实际应用时同步安装 root 管理的 `/etc/tmpfiles.d/leros-model-control.conf`，由系统启动流程重建 `root:999 / 2770` 的 socket 目录。此配置随回退保留；不启动、重启任何主机服务。
