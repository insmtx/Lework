# Leros k3s Deployment

`server.config.example.yaml` is the k3s-oriented server configuration template.
The service deployment updates only `scheduler.worker_image` in the server
ConfigMap, then restarts the server Deployment so the reconciler can converge
existing worker Deployments to the desired worker image. The GitLab desktop
release job updates only `client_update.desktop.latest_version` after
publishing the desktop packages.

Expected ConfigMap layout:

```bash
kubectl -n insmtx-test create configmap leros-server-config \
  --from-file=config.yaml=deployments/k3s/server.config.example.yaml
```

Useful CI variables:

| Variable | Default | Meaning |
| --- | --- | --- |
| `UPDATE_WORKER_IMAGE` | `true` | Whether a `leros` server release also builds the worker image and updates `scheduler.worker_image`. |
| `K3S_NAMESPACE_TEST` | `insmtx-test` | Test namespace containing the server Deployment and server ConfigMap. |
| `K3S_NAMESPACE_PROD` | required for prod | Production namespace containing the server Deployment and server ConfigMap. |
| `K3S_SERVER_DEPLOYMENT` | `leros` | Server Deployment name. |
| `K3S_SERVER_CONTAINER` | `leros` | Server container name inside the Deployment. |
| `K3S_SERVER_CONFIGMAP` | `leros-server-config` | ConfigMap that contains the server config file. |
| `K3S_SERVER_CONFIG_KEY` | `config.yaml` | ConfigMap data key for the server config file. |

The GitLab desktop release job reads the new desktop latest version from
`frontend/apps/desktop/package.json`, writes that value to
`client_update.desktop.latest_version`, and restarts the server Deployment so
the server reloads the ConfigMap.

For your current test cluster, the repository default is:

```bash
K3S_NAMESPACE_TEST=insmtx-test
K3S_SERVER_CONFIGMAP=leros-server-config
K3S_SERVER_CONFIG_KEY=config.yaml
```

For production, add a protected GitLab CI/CD variable:

```bash
K3S_NAMESPACE_PROD=insmtx-prod
```

When you want to deploy only the server image and keep the current worker image,
run the pipeline with:

```bash
UPDATE_WORKER_IMAGE=false
```

The worker runtime settings below are intentionally kept in the server config
and are preserved by `update-leros-images.sh`:

```yaml
scheduler:
  mode: k8s
  kubernetes_connection: in_cluster
  namespace: insmtx-test
  server_addr: leros:8080
  workspace_init_image: registry.cn-beijing.aliyuncs.com/yygu/corekg:busybox_1.36.1
  config_map: leros-worker-config
  secret: leros-secret
  image_pull_secret: insmtx-registry
  workspace_host_path_root: /data/leros-workspaces
  workspace_mount_root: /leros-workspaces
  storage_host_path: /data/leros-storage
  storage_mount_path: /leros-storage
  node_selector:
    node-role: worker
    kubernetes.io/hostname: t30
  env:
    LEROS_STORAGE_LOCAL_DIR: /leros-storage
```
