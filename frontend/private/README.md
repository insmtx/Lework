# 私有化静态资源

本目录存放客户私有化打包素材，**不要把客户文件提交到开源仓库**。

```
frontend/private/
  README.md
  logo/
    {mode}/          # gitignore，客户标识目录
      logo.svg       # 优先
      logo.png       # 无 svg 时使用
```

## 规则

- 环境变量 `LEROS_DEPLOY_MODE` 只用来选择 `logo/{mode}/`。
- **未设置 mode**：不读本目录，安装包使用默认 Lework Logo。
- **设置了 mode，但 `logo/{mode}/` 目录不存在**：仍打私有化包，使用默认 Lework Logo。
- **设置了 mode，且目录存在并含 `logo.svg` 或 `logo.png`**：把该 Logo 打进安装包。
- **目录存在但没有 Logo 文件**：打包失败（避免空品牌包）。
- `LEROS_DEPLOY_APP_NAME` 可选；不填则品牌名仍为 Lework。

本地示例：

```bash
# 私有化，沿用官方 Logo
LEROS_DEPLOY_MODE=acme pnpm dist:desktop:private:win:x64

# 私有化并替换 Logo（先放好 frontend/private/logo/acme/logo.svg）
LEROS_DEPLOY_MODE=acme pnpm dist:desktop:private:win:x64

# 同时替换品牌名
LEROS_DEPLOY_MODE=acme LEROS_DEPLOY_APP_NAME=AcmeAI pnpm dist:desktop:private:win:x64
```
