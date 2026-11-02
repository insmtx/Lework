# Spark 设备管理部署清单

本文用于在单台 Spark 上启用 Lework 的**设备状态查看和关机预检**。当前不启用模型启停、暂停接单或系统关机。

操作位置：Spark 宿主机。需要宿主机 sudo 权限和 Kubernetes 部署修改权限。后端配置生效时会重建后端 Pod，请安排在业务空闲时执行；无需重启整台设备。

## 1. 配置清单

以下为当前现场值，其他设备需按实际部署调整。

| 项目 | 配置要求 |
|---|---|
| 命名空间 / 后端 Deployment | `lework-prod` / `leros` |
| 后端 ConfigMap | `leros-server-config`，配置项 `config.yaml` |
| 管理服务运行用户 / 组 | `lework` / `lework`；当前组 ID 为 `1000`，以 `id -g lework` 为准 |
| Kubernetes 本机节点 | `master` |
| 默认组织 / 本机 Worker ID | 当前均为 `1`，安装前核对实际归属 |
| 主机标识 | 使用本机采集结果中的 `host_id`，不要复制其他设备的值 |
| 通信目录 / socket | `/run/leros-power` / `/run/leros-power/agent.sock` |
| 目录 / socket 权限 | `0750` / `0660`，由管理服务创建 |

前置条件：后端镜像包含 Spark 管理接口；宿主机已安装 Python 3、`kubectl` 和 Docker CLI；`lework` 用户可查询本机 Docker、Kubernetes 节点及 Pod。仅有版本号不能保证镜像包含该功能。

## 2. 备份与采集检查

以 `lework` 用户登录 Spark，并进入包含本功能的仓库目录。备份可能包含敏感配置，保存在仓库外：

```bash
umask 077
backup_dir=$(mktemp -d "$HOME/leros-spark-backup.XXXXXX")
kubectl get configmap leros-server-config -n lework-prod -o yaml > "$backup_dir/configmap.yaml"
kubectl get deployment leros -n lework-prod -o yaml > "$backup_dir/deployment.yaml"
python3 deployments/spark/power_agent.py --snapshot --node master
```

记录输出的 `host_id`，确认 `inventory_complete` 为 `true`。若为 `false`，先处理 Docker/Kubernetes 读取权限或节点绑定问题。

## 3. 安装宿主机管理服务

```bash
sudo install -d -m 0755 /usr/local/lib/leros-spark
sudo install -o root -g root -m 0644 deployments/spark/power_agent.py /usr/local/lib/leros-spark/power_agent.py
sudo install -o root -g root -m 0644 deployments/spark/leros-power-agent.service /etc/systemd/system/leros-power-agent.service
printf 'SPARK_NODE=master\n' | sudo tee /etc/leros-power-agent.conf >/dev/null
sudo chown root:root /etc/leros-power-agent.conf
sudo chmod 0644 /etc/leros-power-agent.conf
sudo systemctl daemon-reload
sudo systemctl enable --now leros-power-agent
```

服务必须能在非交互环境下执行查询。如果 `kubectl` 依赖终端中设置的 `KUBECONFIG`，在 `/etc/leros-power-agent.conf` 中补充实际文件的绝对路径，并确保 `lework` 可读。不要向普通用户授予额外 sudo 权限；Docker 访问权限本身也具有较高权限。

验证服务：

```bash
systemctl is-active leros-power-agent
curl --fail --unix-socket /run/leros-power/agent.sock http://localhost/status
```

应返回 `active` 和有效状态 JSON，且 `inventory_complete` 为 `true`。

## 4. 配置后端绑定与挂载

编辑 ConfigMap 中 `data.config.yaml` 的配置正文，在顶层增加以下内容，保留原有配置：

```yaml
spark:
  socket_path: /run/leros-power/agent.sock
  host_id: "<第 2 步获取的 host_id>"
  org_id: 1
  worker_ids: [1]
```

在 `leros` Deployment 的 Pod 模板中追加以下配置。**这是合并示例，不是完整 Deployment；不要覆盖已有容器、挂载、组权限或调度配置。**

```yaml
spec:
  template:
    spec:
      nodeSelector:
        kubernetes.io/hostname: master  # 核对本机节点的实际标签值
      securityContext:
        supplementalGroups: [1000]     # 替换为宿主机 lework 的组 ID
      containers:
        - name: leros
          volumeMounts:
            - name: spark-power
              mountPath: /run/leros-power
              readOnly: true
      volumes:
        - name: spark-power
          hostPath:
            path: /run/leros-power
            type: Directory
```

挂载整个通信目录，不能使用单文件 `subPath`；不要向后端挂载 Docker socket 或宿主机根目录。后端须运行在该 Spark 节点上。若部署由 Helm 管理，需同步到实际发布配置，避免后续升级覆盖。

修改 Pod 模板会自动触发滚动更新；若仅修改 ConfigMap，则手动重启后端：

```bash
kubectl rollout restart deployment/leros -n lework-prod
kubectl rollout status deployment/leros -n lework-prod
```

## 5. 验收与回退

- 使用设备所属组织的管理员登录 Lework，在“系统设置 → Spark 设备管理”点击“刷新状态”，确认设备和工作负载信息可见。
- 点击“关机预检”，确认能返回阻塞项。旧 Worker 可能显示状态未知或维护能力尚未接入，这不能按零任务处理。
- 三个模型启停/安全关机按钮仍应禁用；本次配置不会获得实际关机能力，也不要新增维护状态配置来尝试解锁按钮。
- 服务异常时查看 `journalctl -u leros-power-agent -n 50 --no-pager`，以及后端 Pod 日志，排查权限、挂载和绑定。
- 回退时按第 2 步备份还原本次修改的后端配置及 Pod 模板，等待后端恢复后执行 `sudo systemctl disable --now leros-power-agent`。无需修改数据库、IAM、模型或其他 Worker。

相关脚本及实现边界见 [Spark 部署说明](../../deployments/spark/README.md)。
