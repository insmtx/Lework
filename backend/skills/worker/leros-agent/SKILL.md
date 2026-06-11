---
name: leros-agent
description: 配置、管理 Leros AI agent。
---

# leros CLI

Leros 命令行工具，用于管理 Leros AI agent 平台。当前支持 skill 的安装与搜索。

## Skill 管理

```
leros skill install <id>   安装 skill。支持三种标识符格式：
                           - 短名称（如 code-review）
                           - GitHub 路径（如 owner/repo/path）
                           - 直接 URL（如 https://.../SKILL.md）
leros skill search <query>  搜索远程 skill
```

| Flag      | 适用范围         | 说明                  |
| --------- | ---------------- | --------------------- |
| `--json`  | install / search | JSON 格式输出         |
| `--force` | install          | 覆盖已有 skill        |
| `--limit` | search           | 最大结果数（默认 10） |
