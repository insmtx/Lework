# Lework 故障排除指南

## 常见问题

### 问题: "Failed to initialize database: unsupported database driver: "

**原因**: 数据库配置中的driver字段为空或未配置。

**解决方案**:
1. 如果你不需要数据库功能，可以忽略此警告 - 系统会继续运行，只是数据库功能不可用。
2. 如果你需要数据库，请确保配置文件中有完整的数据库配置：

```yaml
database:
  driver: postgres
  url: "host=localhost user=lework password=lework dbname=lework port=5432 sslmode=disable"
  debug: false
```

**注意**: GitHub认证功能可以在没有数据库的情况下运行，只是不会持久化用户数据。

## 运行模式

### 最小模式 (无需数据库)
使用 `minimal-config.yaml` 可以在没有PostgreSQL的情况下启动服务：

```bash
./lework --config minimal-config.yaml
```

### 完整模式 (使用数据库)
使用完整配置启动，包括数据库：

```bash
./lework --config example-config.yaml
```

## 启动检查清单

- [ ] 配置文件路径正确
- [ ] 如果使用数据库，PostgreSQL已启动且连接信息正确
- [ ] 如果使用NATS，NATS已启动
- [ ] GitHub配置正确（如果使用GitHub功能）
- [ ] 所有端口可用 (默认8080)