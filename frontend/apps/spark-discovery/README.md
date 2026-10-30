# Spark 发现原型

用途：验证“自有代码发现真实 Spark → 解析服务地址 → 请求 Lework GlobalConfig”路径。
独立原型，不是正式桌面端功能，不提供登录、设备记忆或自动切换。

在 frontend 目录安装依赖后运行：

```sh
pnpm --filter @leros/spark-discovery dev
```

打开 http://127.0.0.1:8787。手填地址始终可用；原型监听回环地址，仅允许本机页面调用测试接口。

Spark 已有 Avahi 和 python3-dbus 时，从仓库根目录执行：

```sh
ssh lework@172.16.0.64 'python3 -u - --interface wlP9s9 --port 38081' < frontend/apps/spark-discovery/publish.py
```

接口和端口按部署实际情况替换。保持 SSH 连接，默认 30 分钟后自动撤销广播；Ctrl+C 可提前退出。
这不会创建常驻服务或修改 Avahi 配置。每次生成临时 ID，不用于持久设备身份。

```sh
pnpm --filter @leros/spark-discovery test
```

边界：只发现 `_lework._tcp`、`v=1`、`path=/v1` 广播；无网段扫描。搜索由 Node 进程完成，网页仅展示结果。
候选主机名需要系统可解析 `.local`。全平台打包权限、TTL 异常清理、可靠部署身份、单播隔离环境、生产常驻发布均留给后续正式实现。
“发现”不等于“可用”，必须点击测试连接。连接测试不携带认证信息，也不跟随 HTTP 重定向。

## 本次实测（2026-10-29）

- macOS 笔记本通过 bonjour-service 自动发现 Spark 的 Wi-Fi 广播，未在搜索代码里写死 IP。
- 页面显示 `Lework Spark - Prototype`，解析为 `http://spark.local:38081/v1`，地址记录包含 `172.16.0.64`。
- 点击设备的“测试连接”，GlobalConfig 返回 enterprise，页面显示连接成功。
- 手动输入 `http://172.16.0.64:38081` 后测试同样成功。
- 3 项 Node 测试通过，覆盖 URL 校验、广播解析、无凭据请求、非 Lework 响应和重定向拒绝。
- Windows/Linux 客户端及正式 Electron 集成尚未验证。临时广播到期后需重新运行发布脚本。
