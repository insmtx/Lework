# 发布流程（GitHub Actions）

`.github/workflows/release.yml` 在推送 `vX.Y.Z` 标签时构建并发布：

| 产物 | 内容 | 命名 |
|---|---|---|
| 后端 | 单文件 `lework` 二进制，**已内嵌控制面界面**，同时包含 `lework serve` 与 `lework agent` | `lework_<version>_<os>_<arch>.tar.gz`（Windows 为 `.zip`） |
| Android | 可直接安装的 APK | `lework-android-<version>.apk` |
| 校验 | 上述产物的 SHA-256 | `SHA256SUMS-release.txt` |

所有产物挂到同一个 GitHub Release（预发布 tag 含 `-` 时标记为 prerelease）。
前端不再单独发包：`frontend/apps/control-plane` 的静态导出会被拷进
`backend/internal/controlplane/webui/dist/`，由 `//go:embed all:dist` 嵌进二进制，
`lework serve` 直接提供界面。

支持平台：`linux/amd64`、`linux/arm64`、`darwin/amd64`、`darwin/arm64`、
`windows/amd64`。二进制以 `CGO_ENABLED=0` 静态构建，未做代码签名：macOS 首次
运行需要 `xattr -d com.apple.quarantine ./lework`（或右键打开），Windows 会弹
SmartScreen 提示。

## 触发方式

- `push` tag：`v0.7.1`、`v0.8.0-rc.1` 等，匹配 `v*.*.*`。
- 手动 `workflow_dispatch`：只构建并上传 workflow artifact，不创建 Release，
  用来验证流水线。

整条链首次约 15 分钟（Android 最久），之后靠 pnpm / Go / Gradle 缓存约 8–10 分钟。
仓库是公开的，Actions 分钟数不计费；要换自建 runner 只需改各 job 的 `runs-on`。

## 版本规则

- 版本号来自 tag，去掉前缀 `v`：`v0.7.1` → `0.7.1`。
- 后端通过 `-ldflags "-X main.version=<version>"` 注入，`lework --version` 输出
  `lework <version>`；workflow 会在 `linux/amd64` 上真实执行一次做校验。
- Android 写进 `app.json`：`expo.version=<version>`，
  `android.versionCode = major*10000 + minor*100 + patch`（`0.7.1` → `701`）。
  这两处只在 CI 里临时修改，不提交回仓库。

## Android 签名

当前**没有任何签名密钥**：prebuild 生成的模板里 `release` 构建复用
`android/app/debug.keystore`，所以 CI 不需要 secret 就能产出可安装的 APK。
每次构建用的都是同一把模板 debug key，覆盖升级（同包名）不会冲突。

限制：

- debug 签名不能上架 Google Play，本项目也只做内部分发。
- APK 能安装、能连控制面，但推送仍依赖 FCM 的 `google-services.json`，
  仓库里目前没有，推送注册会失败；这不影响构建。

以后要换正式 keystore：把 keystore 以 base64 存进 Actions secret，在 prebuild
之后、`assembleRelease` 之前解码并改写 `android/app/build.gradle` 的
`signingConfigs.release`（`storeFile` / `storePassword` / `keyAlias` /
`keyPassword`），把 `buildTypes.release.signingConfig` 指向它即可。

## 本地等价命令

```bash
# 控制面界面 + 内嵌它的 lework 二进制
make build

# 只验证二进制（界面不内嵌时启动会显示构建提示页）
go build -o ./bundles/lework ./backend/cmd/lework/

# Android APK（需要 Java 17 与 Android SDK）
cd frontend
pnpm install --frozen-lockfile --ignore-scripts
pnpm --filter @lework/mobile exec expo prebuild --platform android --no-install
cd apps/mobile/android && ./gradlew assembleRelease
```

## 与桌面端的关系

桌面端（Electron）仍由 `.github/workflows/desktop-release.yml` 独立发布，同样在
`v*.*.*` tag 触发。两个 workflow 会写同一个 Release：`release.yml` 用
`gh release view || gh release create` 加失败重试，避免同时创建时撞车。
桌面端将来并入本工作流后，可把 `SHA256SUMS-release.txt` 与它的
`SHA256SUMS.txt` 合并成一个文件。
