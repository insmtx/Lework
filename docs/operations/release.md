# 发布流程（GitHub Actions）

`.github/workflows/release.yml` 在推送 `vX.Y.Z` 标签时构建并发布：

| 产物 | 内容 | 命名 |
|---|---|---|
| 后端 | 单文件 `lework` 二进制，**已内嵌控制面界面**，同时包含 `lework serve` 与 `lework agent` | `lework_<version>_<os>_<arch>.tar.gz`（Windows 为 `.zip`） |
| Android | 可直接安装的 APK | `lework-android-<version>.apk` |
| 桌面端 | Electron 安装包与更新元数据（mac arm64/x64、win x64、linux x64） | `Lework-<version>-<platform>-<arch>.<ext>` |
| 校验 | 上述产物的 SHA-256 | `SHA256SUMS-release.txt` |

一个 tag 只触发一条流水线：`prepare → frontend/backend/android/desktop → publish`，
所有产物挂到同一个 GitHub Release（预发布 tag 含 `-` 时标记为 prerelease）。
前端不再单独发包：`frontend/apps/control-plane` 的静态导出会被拷进
`backend/internal/controlplane/webui/dist/`，由 `//go:embed all:dist` 嵌进二进制，
`lework serve` 直接提供界面。

## COS 发布（客户端更新源）

`publish` job 在创建 GitHub Release 之前，会把全部产物按版本目录上传到 COS，
并推进指针文件：

```text
application/
  LATEST.txt                    # 最新稳定版本，桌面端/移动端/下载页共用
  Lework-<version>-<platform>-<arch>.<ext>   # 根目录安装包，兼容旧下载页
  <version>/
    SHA256SUMS.txt
    server/
      lework_<version>_<os>_<arch>.tar.gz    # Windows 为 .zip
    android/
      lework-android-<version>.apk
    darwin/
      arm64/  latest-mac.yml + dmg/zip/blockmap
      x64/    latest-mac.yml + dmg/zip/blockmap
    win32/
      x64/    latest.yml + exe/blockmap
    linux/
      x64/    latest-linux.yml + AppImage/deb
```

- `LATEST.txt` 沿用旧下载页的指针文件，内容是单行版本号；只在稳定 tag（不含 `-`）
  且新版本大于现值时更新，重跑旧 tag 不会把版本写回去。桌面端和移动端都读它。
- 根目录安装包按旧下载页的文件名约定平铺，文件名带版本号，不会互相覆盖；
  Linux `.deb` 额外生成 `-linux-amd64.deb` 别名，兼容旧链接。
- bucket / region 默认 `leros-1395325824` / `ap-beijing`，可用仓库变量
  `COS_BUCKET` / `COS_REGION` 覆盖；凭据来自 secrets `COS_SECRET_ID` /
  `COS_SECRET_KEY`，缺失时 publish job 直接失败。
- 上传逻辑在 `scripts/release/publish-cos.sh`。本地可用
  `DRY_RUN=1 bash scripts/release/publish-cos.sh <dir> <version>` 预览上传路径，
  `bash scripts/release/publish-cos.test.sh` 跑 dry-run 用例。
- 桌面端 macOS 签名/公证校验失败时只告警、不阻断发布，产物仍会进入 GitHub
  Release 与 COS；正式对外分发前需要确认公证材料配置正确。

支持平台：`linux/amd64`、`linux/arm64`、`darwin/amd64`、`darwin/arm64`、
`windows/amd64`。二进制以 `CGO_ENABLED=0` 静态构建，未做代码签名：macOS 首次
运行需要 `xattr -d com.apple.quarantine ./lework`（或右键打开），Windows 会弹
SmartScreen 提示。

## 触发方式

- `push` tag：`v0.7.1`、`v0.8.0-rc.1` 等，匹配 `v*.*.*`。
- 手动 `workflow_dispatch`：只构建并上传 workflow artifact，不创建 Release，
  不写 COS，用来验证流水线。

整条链首次约 25 分钟（桌面端 macOS 与 Android 最久），之后靠 pnpm / Go / Gradle
缓存约 15 分钟。仓库是公开的，Actions 分钟数不计费；要换自建 runner 只需改各
job 的 `runs-on`。

## 版本规则

- 版本号来自 tag，去掉前缀 `v`：`v0.7.1` → `0.7.1`。
- 后端通过 `-ldflags "-X main.version=<version>"` 注入，`lework --version` 输出
  `lework <version>`；workflow 会在 `linux/amd64` 上真实执行一次做校验。
- Android 写进 `app.json`：`expo.version=<version>`，
  `android.versionCode = major*10000 + minor*100 + patch`（`0.7.1` → `701`）。
  这两处只在 CI 里临时修改，不提交回仓库。
- 桌面端把 tag 版本写进 `frontend/apps/desktop/package.json`，保证安装包文件名与
  `app.getVersion()` 一致；同样不提交回仓库。

## 桌面端签名

macOS 构建使用 secrets `MACOS_CERTIFICATE_P12_BASE64` /
`MACOS_CERTIFICATE_PASSWORD` 与 `APPLE_API_KEY_BASE64` / `APPLE_API_KEY_ID` /
`APPLE_API_ISSUER`。签名/公证校验失败只告警，不阻断 GitHub Release 与 COS 上传。

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

# 桌面端安装包（按平台执行）
cd frontend
pnpm --filter @lework/desktop typecheck
pnpm run dist:desktop:mac:arm64   # 或 mac:x64 / win:x64 / linux:x64
```
