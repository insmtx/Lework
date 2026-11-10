# 排障

## 节点离线

- 检查 `lework agent` 进程与 `/api/agent/connect` 连接；控制面返回 `409 node_offline`。
- 检查 enrollment token 是否与控制面一致。

## OpenCode 未启动

- 查看 agent 日志；确认 `opencode` 在 PATH，端口未被占用。
- OpenCode 只应绑定 `127.0.0.1`。

## PTY / 文件失败

- 路径必须在 `workspace_roots` 内，且为绝对路径。
- 文本读取上限 512 KiB，超出请下载。

## Diff 为空

- 仅 git 工作区支持；非 git 目录返回空 diff。
