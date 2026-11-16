# 统一 COS 发布与移动端应用内升级

## 背景

移动端（`frontend/apps/mobile`，Expo / React Native）目前只以 APK 内部分发，
没有版本检查，也没有任何升级入口；用户拿到旧 APK 后只能手动重新下载安装。

桌面端（Electron）已经有一套完整的 `electron-updater` 流程：每 30 分钟检查
更新源，后台下载，提示重启安装。但它的更新源、GitHub Release、Android APK
和服务端二进制分散在不同位置，移动端没有任何发现新版本的途径。

本次把发布统一到同一个 COS 前缀，并让移动端获得与桌面端一致的应用内升级体验。
GitLab 在本项目中只做代码同步，不参与构建与发布；所有发布相关改动都在
GitHub Actions 里完成。

已经装出去的旧客户端不做迁移兼容（明确决定）。

## 目标

1. 所有发布产物统一放在 COS `application/` 下，按版本号分目录。
2. `application/LATEST.txt` 记录最新稳定版本号，作为客户端发现新版本的唯一入口。
3. 移动端自动检查新版本、应用内提示、下载 APK，并拉起系统安装器完成升级。
4. 桌面端改用同一份 `LATEST.txt` 判断是否有新版本，再按版本目录设置更新源。
5. 桌面端、Android、服务端二进制由同一个 tag 触发、使用同一个版本号。

## 非目标

- iOS。当前只有 Android；iOS 将来只能跳 App Store / TestFlight。
- 静默安装。Android 不允许普通应用静默自装，用户必须在系统安装器里确认。
- JS 热更新（`expo-updates` / EAS Update）。本期只做整包 APK 升级。
- 旧客户端兼容。旧桌面端仍指向旧地址，旧移动端没有更新能力，均不处理。
- 安装包签名校验、SHA-256 客户端校验。依赖 HTTPS 与现有 APK 签名。
- 服务端下发的强制升级策略。移动端不依赖用户自填的控制面地址获取更新源。
- 私有化 / OEM 桌面打包。它不属于本仓库的 GitHub Release 流程。

## 约束

- 发布源固定为
  `https://leros-1395325824.cos.ap-beijing.myqcloud.com/application`，
  与用户填写的控制面地址无关。不能从 `settings.baseUrl` 推导更新源，
  否则任意自建控制面都能把用户引向任意 APK。
- COS 对象公开读；上传使用 `coscmd`。GitHub 的 Organization secrets 已有
  `COS_SECRET_ID`、`COS_SECRET_KEY`，workflow 直接引用；bucket / region 使用
  固定值 `leros-1395325824` / `ap-beijing`，或放到 repository variables。
- Android APK 当前由 prebuild 模板自带的 debug keystore 签名，所有构建共用同一
  把 key，覆盖安装可行。以后换正式 keystore 时必须保持签名一致，否则系统拒绝
  覆盖安装，用户只能卸载重装。
- 客户端只支持 Android 包名 `com.insmtx.lework`；Expo Go / 开发宿主里的
  `nativeApplicationVersion` 不是本应用版本，必须跳过检查。

## 发布布局

```text
application/
  LATEST.txt                                  # 纯文本，内容如 "0.8.0\n"
  0.8.0/
    SHA256SUMS.txt
    android/
      lework-android-0.8.0.apk
    server/
      lework_0.8.0_linux_amd64.tar.gz
      lework_0.8.0_linux_arm64.tar.gz
      lework_0.8.0_darwin_amd64.tar.gz
      lework_0.8.0_darwin_arm64.tar.gz
      lework_0.8.0_windows_amd64.zip
    darwin/
      arm64/
        latest-mac.yml
        Lework-0.8.0-mac-arm64.dmg
        Lework-0.8.0-mac-arm64.zip
        Lework-0.8.0-mac-arm64.dmg.blockmap
        Lework-0.8.0-mac-arm64.zip.blockmap
      x64/
        latest-mac.yml
        Lework-0.8.0-mac-x64.dmg
        Lework-0.8.0-mac-x64.zip
        Lework-0.8.0-mac-x64.dmg.blockmap
        Lework-0.8.0-mac-x64.zip.blockmap
    win32/
      x64/
        latest.yml
        Lework-0.8.0-win-x64.exe
        Lework-0.8.0-win-x64.exe.blockmap
    linux/
      x64/
        latest-linux.yml
        Lework-0.8.0-linux-amd64.deb
```

桌面端必须再分 `平台/架构` 一层：arm64 与 x64 都会生成同名的
`latest-mac.yml`，而 `electron-updater` 要求清单和安装包在同一目录，平铺会互相
覆盖。移动端和服务端产物没有这个约束，但仍放在版本目录下，保持同一个版本号
只出现一次。

当前 GitHub Actions 只构建 `mac-arm64`、`mac-x64`、`win-x64`、`linux-x64`。
Linux arm64 桌面产物本次不纳入，未来加入时按 `linux/arm64/` 扩展。

## 版本与发布顺序

- 版本号只来自 git tag `vX.Y.Z`。
  - Android：沿用现有 `app.json` 注入，`versionCode = major*10000 + minor*100 + patch`。
  - 服务端：沿用 `-ldflags "-X main.version=<version>"`。
  - 桌面端：沿用 `.github/workflows/desktop-release.yml` 已有的
    “Set desktop version from tag”，把 tag 版本写入
    `frontend/apps/desktop/package.json`，让安装包文件名与 `app.getVersion()` 一致。
- `LATEST.txt` 只记录稳定版本（tag 不带 `-`）。预发布产物仍然上传到
  `<version>/`，用于内测，但不更新 `LATEST.txt`。
- 桌面端、Android、服务端由同一个 tag 触发的同一个 workflow 构建。最终
  `publish` job 依赖全部构建 job，全部成功后才上传 COS 并写 `LATEST.txt`；
  任一平台失败都不会推进指针。
- `LATEST.txt` 只有一个写入者：`publish` job。写入时先读旧值，只有新版本号
  更大才覆盖，避免重跑旧 tag 把指针写回旧版本。
- 只有 tag 触发的运行允许写发布前缀和 `LATEST.txt`。`workflow_dispatch`
  只产出 workflow artifact，不上传 COS，不创建 Release。
- 客户端在提示前仍然用 `HEAD` 检查自己的产物是否存在（移动端查 APK，桌面端
  查 `latest*.yml`），404 时保持静默。它是防御性检查，不替代发布顺序约束。

## 移动端升级

### 版本来源

- 本机版本：`expo-application` 的 `nativeApplicationVersion`。
- 远端版本：`GET ${BASE}/LATEST.txt?t=<Date.now()>`，去掉空白后按 semver 比较。
  查询参数用于绕过 COS / CDN 缓存。
- `Application.applicationId !== "com.insmtx.lework"` 或版本号读不到时，完全
  禁用更新检查（Expo Go、开发宿主）。

### 检查时机

- 启动后首帧完成再检查（延迟约 1 秒，避免抢首屏）。
- App 在前台时每 30 分钟检查一次。
- 从后台回到前台，且距上次检查超过 5 分钟时补一次。
- 设置页「版本」行提供手动检查。

自动检查失败时静默；只有手动检查失败才在界面上显示错误。网络失败、`LATEST.txt`
解析失败、产物 404 都视为「本次没有更新」。

### 状态机

```text
idle → checking → available → downloading → downloaded → installing
                      ↑            │
                      └── error ←──┘
```

- 发现新版本后立即开始后台下载（与桌面端一致），同时弹出提示。
- 下载中提示「发现新版本 vX，正在下载 xx%」，提供「稍后」与「取消下载」。
  「稍后」在本次会话内 5 分钟后再次提示；重新启动 App 会立即再提示。
- 下载完成提示「更新已下载，点安装完成升级」，按钮「立即安装」。
- 下载失败显示错误与「重试」，停留在此状态，不静默丢弃。

### 下载

- 使用 `expo-file-system/legacy` 的 `createDownloadResumable`，目标路径
  `cacheDirectory + "lework-update-<version>.apk"`；开始前删除同目录旧文件。
- 进度回调节流到约 200ms 更新一次，避免高频 setState。
- 取消或失败时删除半成品文件。App 被杀掉后不保证续传，下次启动重新检查并
  重新下载。

### 安装

- `FileSystem.getContentUriAsync(fileUri)` 把 `file://` 转成 `content://`。
- `expo-intent-launcher` 调起系统安装器：

  ```ts
  IntentLauncher.startActivityAsync("android.intent.action.VIEW", {
    data: contentUri,
    type: "application/vnd.android.package-archive",
    flags: 1, // FLAG_GRANT_READ_URI_PERMISSION
  });
  ```

- `app.json` 增加 `android.permissions: ["android.permission.REQUEST_INSTALL_PACKAGES"]`，
  prebuild 写入 Manifest。
- 用户没开「安装未知应用」时，系统会拦截。捕获异常或调用返回后提示去系统设置
  开启；不尝试绕过系统限制。
- 安装完成后当前进程通常被系统结束，App 不需要监听安装结果；下次启动重新读取
  本机版本即可。

### 界面

- 新增全局 `<UpdateGate />`，挂在 `RootNavigator` 的 `AppShell` 内，用 Modal
  覆盖当前页面，保证任何页面都能提示更新。
- 设置首页新增「版本」行：显示本机版本；点按触发手动检查；右侧状态文案覆盖
  「检查中 / 已是最新 / 有新版本」。
- 文案、间距、颜色沿用现有 `theme` 与设置页组件。

### 权限与兼容

- 首次安装会要求用户允许「安装未知应用」，文案里说明这是一次性设置。
- APK 覆盖安装依赖签名一致；debug keystore 是当前内部分发的前提。
- 移动数据下也会自动下载（与桌面端一致），v1 不做仅 Wi-Fi 限制。

## 桌面端升级

- 更新基址改为 COS `application`。
- `checkDesktopUpdates()` 改成两段式：
  1. 拉 `LATEST.txt`，与本机 `app.getVersion()` 比较。没有新版本直接
     显示「当前已经是最新版本」，不访问版本目录。
  2. 有新版本时，macOS / Windows 先 `HEAD` 自己的 `latest*.yml`，存在则
     `updater.setFeedURL({ provider: "generic", url: <version>/<os>/<arch> })`，
     再 `checkForUpdates()`；原有自动下载、进度、重启安装事件不变。
  3. Linux 拉 `<version>/linux/x64/latest-linux.yml`，解析版本与 `.deb`
     文件名，提示手动安装；消息里给出下载地址，用现有 `openExternal` 打开。
- 平台产物 404 视为尚未发布，保持静默，不进入 `error`。
- 30 分钟定时检查、5 分钟 snooze、私有化构建禁用逻辑维持现状。
- 同步清理 `electron-builder.yml` 的 `publish.url` 与 `dev-app-update.yml` 里的
  旧域名，避免残留第二套更新源；运行时统一走 `setFeedURL`。
- 开发模式（`app.isPackaged === false`）保留手动检查能力，必要时允许用环境变量
  覆盖更新基址做测试。

## 发布端改动

### 合并桌面端发布

把 `.github/workflows/desktop-release.yml` 的桌面构建 matrix 并入
`.github/workflows/release.yml`（或改成 `workflow_call` 由 `release.yml` 调用），
保留现有的 macOS 签名 / notarize、Electron 缓存与重试逻辑，然后删除独立的
`desktop-release.yml` 触发，避免同一个 tag 触发两套重复构建与两套发布逻辑。

`release.yml` 的 job 变成：

```text
prepare → frontend ─┐
        → backend  ─┼→ publish
        → android  ─┤
        → desktop  ─┘
```

`publish` 依赖全部构建 job，负责：

1. 下载所有 artifact，按平台的 `latest*.yml` 与安装包保持原样。
2. 生成 `SHA256SUMS.txt`。
3. 上传到 COS `application/<version>/...`。
4. 写 `LATEST.txt`（稳定 tag 且版本更大时）。
5. 创建 / 更新 GitHub Release，行为与现在一致。

### COS 上传

- 新增共享脚本 `scripts/release/publish-cos.sh`，GitHub Actions 各发布 job 共用：
  - 输入：`COS_*`、`RELEASE_PREFIX`（默认 `application`）、`VERSION`、
    `PLATFORM_DIR`、`LOCAL_DIR`。
  - 上传 `LOCAL_DIR` 下文件到 `${RELEASE_PREFIX}/${VERSION}/${PLATFORM_DIR}/`。
  - `update-release-txt`：下载现有 `LATEST.txt`，仅当 `VERSION` 是稳定版且
    大于旧值时写入。
  - 用 `coscmd` 实现；凭据来自 Organization secrets。
- 也可以在 publish job 里直接调用 `coscmd`，但必须只有一处写
  `LATEST.txt` 的逻辑。

### 官网下载页

官网或外部下载页可能仍在读根目录的 `LATEST.txt` 与根目录安装包。删除前先确认；
无法确认时保留一次发版的兼容写入（`LATEST.txt` 内容与 `LATEST.txt` 一致），
确认后再移除。

## 测试

自动化：

- 移动端：版本比较、`LATEST.txt` 解析、状态机（mock fetch / 文件系统）、
  更新源不读取 `settings.baseUrl`。
- 桌面端：`LATEST.txt` 解析、按平台/架构拼 feed URL、404 静默、Linux 元数据
  解析。
- 发布脚本：版本比较单测（稳定/预发布、旧版本重跑）、上传路径快照测试；
  CI 上用测试前缀跑一次 dry-run 上传。
- workflow：`release.yml` 在 `workflow_dispatch` 下不写 COS / 不创建 Release；
  tag 下 `publish` 依赖全部平台 job。

人工：

- Android 旧版本安装 → 发布新版本 → 启动提示 → 下载进度 → 系统安装器 →
  重启后版本更新；相同版本不提示；产物 404 不提示；未开未知来源时提示可读。
- 发版前用 `apksigner verify --print-certs` 比对上一版与新版 APK 的签名证书，
  确认覆盖安装不会因签名不一致失败。
- 桌面端旧版本启动 → 发现新版本 → 自动下载 → 重启安装；Linux 只提示手动装
  `.deb`；无更新时显示「已是最新」。
- 断网、COS 不可达、`LATEST.txt` 内容非法时客户端不崩溃、不误报。

## 风险

- Android 无法静默安装：如果预期是「用户完全无感」，需要 MDM / device owner /
  root，当前方案做不到。
- debug keystore 只适合内部分发；换正式签名后必须保证签名一致才能覆盖升级。
  如果历史上有一部分 APK 来自 EAS 构建，它们与 CI 的 debug 签名不同，无法
  应用内覆盖升级；这类用户需要卸载重装。
- 把桌面端并入 `release.yml` 会拉长 tag 流水线（macOS runner 最慢），但换来
  一个原子发布和唯一 `LATEST.txt` 写入者。
- GitHub Actions 需要能读取 Organization secrets；fork 或未配置 secret 的环境
  发版会失败。
- 自动下载会消耗移动数据；v1 不限制网络类型。
- COS 缓存可能让 `LATEST.txt` 短暂滞后；客户端用时间戳查询参数规避。

## 留给构建者的空间

- `publish-cos.sh` 的具体参数命名与是否引入 Node 小脚本做 semver 比较。
- 桌面 matrix 是直接复制到 `release.yml`，还是抽成 `workflow_call` 可复用
  workflow；两者行为一致。
- 移动端更新 Modal 的具体视觉层次，按现有设置页/主题风格决定。
- 桌面端是否把 `downloadUrl` 加到 `DesktopUpdateState` 并用现成 `openExternal`
  打开；不改变主流程。
- 是否给 `LATEST.txt` 增加第二个字段（例如发布日期）；当前客户端只读第一行。
