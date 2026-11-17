# Android 手机端（P1）

`frontend/apps/mobile` 是 Expo（React Native）实现的**纯客户端**：它不运行
`lework serve`、不运行 `agentd`、也不在手机上执行 OpenCode。所有会话事实来自
控制面背后的节点，App 只负责展示与提交审批。

本阶段只覆盖 Android 手机；仅以 APK 分发，不发布到 Google Play。

## 页面结构：任务优先

App 没有底部 tab bar，也没有常驻导航栏。首页只做一件事：**直接说任务**。

| 页面 | 路径 | 说明 |
|---|---|---|
| 首页 | `/` | 只有左上角 `☰` 和一个大输入框；没有标题、路径、会话列表或节点信息 |
| 项目 | `/projects` | 「我的」之外的其它项目 |
| 项目详情 | `/projects/[key]` | 跨节点会话、主节点选择、输入框 |
| 待审批 | `/pending` | 汇总所有 `pending_permissions > 0` 的会话 |
| 节点 | `/nodes` | 诊断用：在线状态、`workspace_roots` |
| 设置 | `/settings` | 扫码/粘贴配对、控制面地址、令牌、推送开关、版本检查 |

主导航是点 `☰` 从左侧滑出的侧边栏（自绘 Modal，不引入 drawer 依赖）：

```text
☰  当前项目/会话名称          🔔
＋ 新任务
▤ 查看全部
────────────────────
项目
  lework                 ← 项目按最近活动倒序
    重构登录页      2 小时前
    修上传超时      5 小时前
    加导出          昨天
    全部会话 ›            ← 超过 3 条时才出现
  api-server
────────────────────
用户                设置
```

- 每个项目只列最近 3 条会话，完整列表在项目页。
- 离线节点的会话不出现：控制面不缓存会话快照，列出来点进去只会失败。
- 「用户」只展示控制面地址与事件流连接状态；单用户产品没有账号体系。
- 定时任务、插件等尚不存在的能力不做占位。
- 会话页顶栏是 `☰ 标题 ⌄ 🔔`：点标题弹出同节点同项目的会话列表，就地切换；
  节点 ID 与在线状态不再上屏。

### 项目身份

项目身份直接采用 OpenCode 的 project id：

- git 仓库：**根 commit SHA**，同一仓库在不同机器上 id 相同，worktree 也归到
  同一个项目。
- 非 git 目录：canonical 路径的哈希。
- 无提交的 git 仓库：OpenCode 一律报 `"global"`，控制面改用
  `"global:" + canonical` 作为聚合键，避免多个空仓库混成一个项目。

同一项目的检出路径按节点区分：`/api/projects` 的顶层 `canonical` 只是聚合
展示值，`nodes[].canonical` 才是该机器上的真实路径。客户端新建会话必须使用
目标节点的路径，否则同一仓库在两台机器上路径不同时会发错目录。

会话按 `projectID` 归组，不按目录前缀过滤——会话可以在项目子目录里创建
（带 `subpath`），前缀过滤会漏。

### 「我的」与发送目标

「我的」不是 OpenCode 的特殊项目，而是一条用户偏好：某个节点上的目录。

- 默认取该节点 agent 的第一个 `workspace_roots`；可在设置里按节点改。
- 解析规则：在 `/api/projects` 结果里找 canonical 是「我的目录」路径前缀且
  最深的项目。目录在某个仓库内时，「我的」就是那个仓库。
- 首页不再展示「我的」的会话列表；它只是任务的默认归属。
- 项目已存在时，首页按项目选节点（主节点 → 最近在线节点），目录用**该节点**
  的 canonical；项目还没创建时才退回偏好目录，第一条消息把它创建出来。
- 草稿按任务 scope 保存，切换节点或项目不会清空正在输入的内容。

### 新任务页的显式选择

输入框上方有一行 chip：`[电脑 …] [项目 …]`。以后技能、快捷命令继续往右排，
弹层复用同一个通用组件。

- 默认不选，行为与自动定位一致；用户点选后只在本次停留有效，不写偏好。
- 电脑只列已关联节点：离线或缺 `session.create` 能力的置灰。
- 项目只列当前电脑上的项目，首项是「我的目录 · `<dir>`」。
- 切电脑时，原项目在新电脑上也有就保留，否则回落到那台电脑的「我的目录」。
- 选中的电脑离线、项目从节点消失时自动回退到默认目标，不会卡在无效目标上。
- 打开弹层不重新拉 `/api/projects`：该接口会等待所有在线节点，慢机器会拖住
  弹层；用 AppShell 的现有快照即可。


### 主节点与离线

- 新建会话的目标节点：项目主节点（在线时）→ 该项目最近在线节点 → 无。
- 主节点可在项目页点开修改；被选节点不必在线。
- 节点离线时项目保留显示（灰），但会话列表不可读、输入框禁用并说明原因。
  控制面不缓存会话快照，离线只保证项目元数据可见。
- 节点只是标签和诊断信息，不进入首页或侧边栏的主体结构。

## 服务端要求

- 控制面可从手机访问：`lework serve --listen 0.0.0.0:8787`（或反向代理到该端口）。
  非回环监听会让浏览器 API 要求令牌，见 `docs/operations/control-plane.md` 与
  设计稿 §7。
- 手机端用 `Authorization: Bearer <enrollment token>` 访问全部 API；SSE
  `/api/events` 同样只走 header，令牌不会出现在 URL、日志或通知负载中。
- 生产环境请在控制面前置 TLS（反向代理）。Android 默认禁止明文 HTTP；开发期
  若必须用 `http://`，需在 `app.json` 的 `android` 下显式开启 cleartext（见
  `expo-build-properties` 文档），并把它当作临时手段。

  当前 `frontend/apps/mobile/app.json` 已设置 `android.usesCleartextTraffic: true`，
  因为本项目的控制面入口（如 `http://dev.ckeyer.com`）还没有 TLS；**上线前应
  改成 HTTPS 并把这一项删掉**。诊断连接问题时先确认这一项还在，否则真机上会
  表现为 `Network request failed`，看不出是证书还是明文限制。
- 推送需要控制面能访问 Expo Push API；设备注册复用 P0 的 `/api/devices`。

## 本地开发

```bash
cd frontend
pnpm install --frozen-lockfile --ignore-scripts
pnpm --filter @lework/mobile start
```

在 Expo 开发客户端或调试版 APK 中打开，然后：

1. 打开 Settings，填写控制面地址（如 `https://cp.example.com`）与 enrollment token。
2. 点 Save：App 会先调用 `GET /api/nodes` 验证连通性，成功后才写入设置。
   令牌保存在 Android keystore（`expo-secure-store`），不写 AsyncStorage。
3. 回到首页：输入一句任务并发送，会自动落到「我的」对应项目和在线节点。
4. 点左上角 `☰` 打开侧边栏，进入项目、待审批或设置。

令牌轮换：在服务端更换 enrollment token 并重启后，手机端 Settings 里改成新
令牌重新保存即可；旧令牌立即失效，不需要卸载 App。

### 扫码配对（免手填）

控制面启动时会打印一个二维码，扫码即把地址与令牌带进 App：

```bash
# 安装版 APK：码里是 lework://connect?url=…&token=…
lework serve --public-url http://dev.ckeyer.com

# Expo Go 调试：码里是 exp://<metro>/--/connect?url=…&token=…
lework serve --public-url http://dev.ckeyer.com \
  --expo-url http://dev.ckeyer.com:8082
```

- `--public-url` 是**手机**访问控制面的地址。不给时退回本机局域网 IP + 监听
  端口，控制面只监听回环或手机不在同一网段时扫码会连不上。
- `--expo-url` 指向 Metro（`expo start` 的端口）。Metro 默认端口是 8081，被
  其它服务占用时用 `-p` 指定，例如 `expo start -p 8082`。
- 两个地址也可以写进统一配置文件 `lework.yaml` 的 `controlplane:` 段，省掉
  每次启动敲参数（flag 仍然优先）：

  ```yaml
  controlplane:
    public_url: "http://dev.ckeyer.com"
    expo_url: "http://dev.ckeyer.com:8082"
  ```
- 二维码只在 stdout 是终端时输出：链接含 enrollment token，重定向到文件或
  journal 时不打印，等同于不把令牌写进日志。需要关掉用 `--no-qr`。
- 扫码入口有两个：
  - **App 外**：手机相机或 Expo Go 直接扫，系统把深链递给 App，落在 `/connect`；
  - **App 内**：Settings →「扫码配对」→ 对准终端二维码（`expo-camera`，
    Expo Go 内置该模块）。识别后复用 Save 的验证与推送登记流程，成功后直接连上。
    相机权限被拒绝时会提示去系统设置，此时用下面的粘贴方式。
- 扫不动时把终端打印的那行链接粘到 Settings 的「配对链接」里，点「从配对链接
  填入」，再点 Save。链接两种 scheme 都认。

配对会先把候选配置拿去请求 `GET /api/nodes`，通过后才写入；扫到过期或错误的
码不会覆盖当前可用的配置。推送开关沿用原有偏好，deviceID 需要重新在设置页
保存时才登记。

## 构建 APK

发版走 GitHub Actions：推送 `vX.Y.Z` tag 后，`.github/workflows/release.yml`
会执行 `expo prebuild` + Gradle `assembleRelease`，把
`lework-android-X.Y.Z.apk` 与同名 zip 挂到该 tag 的 GitHub Release（完整流程见
[release.md](release.md)）。COS 只分发 zip——桶策略不允许匿名下载 `.apk`，
App 内置升级也改为下载 zip 后在本地解压。CI 使用 prebuild 模板自带的 debug
keystore 签名，不需要任何 secret，适合内部分发；debug 签名不能上架 Google Play。

本地复现同样的构建（需要 Java 17 与 Android SDK）：

```bash
cd frontend
pnpm install --frozen-lockfile --ignore-scripts
pnpm --filter @lework/mobile exec expo prebuild --platform android --no-install
cd apps/mobile/android && ./gradlew assembleRelease
# 产物：app/build/outputs/apk/release/app-release.apk
```

也可以改用 EAS 云构建（需要 Expo 账号；仓库目前没有 `extra.eas.projectId`）：

```bash
cd frontend/apps/mobile
npx eas-cli login
npx eas-cli init          # 写入 extra.eas.projectId 并创建/关联 EAS 项目
npx eas-cli build --profile preview --platform android
```

`eas.json` 的 `preview` 与 `production` 都配置为 `buildType: "apk"`、
`distribution: "internal"`，构建产物是可直接安装的 APK，不会走商店。

推送还需要 FCM：

- 在 Firebase 项目里添加 Android 应用，包名 `com.insmtx.lework`。
- 把 `google-services.json` 放到 `frontend/apps/mobile/`。
- 把 FCM 服务端密钥上传到 EAS（`eas credentials`）或使用 Expo 的推送服务。

缺少 `extra.eas.projectId` 或通知权限被拒绝时，Settings 仍会保存连接信息，
只提示推送未登记；App 本体可以正常轮询与手动刷新。

## 应用内升级

正式版 APK 内置应用内升级：检查、下载、安装全部围绕
`frontend/apps/mobile/src/update/` 的状态机（`idle → checking → available →
downloading → downloaded → installing`，失败进 `error` 可重试）运转。
更新源固定为构建期常量 `MOBILE_UPDATE_BASE_URL`（COS 发布目录，可用
`EXPO_PUBLIC_LEWORK_UPDATE_BASE_URL` 在构建时覆盖做本地联调），**绝不**从
Settings 里的控制面地址推导，防止控制面搬家把更新流量带偏。

只有 `expo-application` 的 `applicationId` 为 `com.insmtx.lework` 且能读到
`nativeApplicationVersion` 时才启用；Expo Go、开发宿主与 iOS 上检查完全禁用，
设置页「版本」行点按只会说明不支持。

### 检查时机

- 启动约 1 秒后首查（避开冷启动首屏）。
- App 在前台时每 30 分钟检查一次。
- 从后台回到前台，且距上次检查超过 5 分钟时补查一次。
- 设置 →「版本」行随时可手动检查；失败会弹可读的错误提示。

自动检查失败保持静默（回到 idle 等下一次时机），不打扰用户。流程是
`GET ${BASE}/LATEST.txt?t=<时间戳>` 取最新版本号、比较 semver，再对更新包
`android/lework-android-<version>.zip` 做 `HEAD` 确认已上传，二者齐备才算「有更新」。

### 下载与安装

发现新版本后立即在后台开始下载（进度弹窗显示「发现新版本 vX · 正在下载
xx%」），目标路径为缓存目录下的 `lework-update-<version>.zip`，下载前会删除
同名旧半成品。下载完成后解压出唯一的 APK（`lework-update-<version>.apk`）并
删掉 zip，再提示「更新已下载」。两个出口语义不同：「稍后」中止进行中的下载，
并在 5 分钟后重新检查、再次提示（暂缓窗口内的定时与回前台补查让路，不会提前
弹出）；「取消下载」只中止并收起提示，不安排任何自动重查，直到下一次常规检查
时机或用户手动检查。App 被杀后不续传，下次启动重新检查、重新下载。

下载完成提示「更新已下载」，点「立即安装」通过 `expo-intent-launcher` 调起
系统安装器（`android.intent.action.VIEW` + `application/vnd.android.package-archive`）。
安装完成后当前进程通常被系统结束，App 不监听安装结果，下次启动重新读取本机
版本即可。

### 首次安装与签名要求

- 首次从应用内升级需要在系统设置里允许本应用「安装未知应用」，这是一次性
  设置；被系统拦截时更新弹窗会给出可读提示与「重试」。`app.json` 已声明
  `REQUEST_INSTALL_PACKAGES` 权限，prebuild 会写入 Manifest。
- 覆盖安装依赖新旧 APK **签名一致**。CI 目前统一使用 prebuild 模板自带的
  debug keystore；一旦更换正式签名，旧签名安装的版本必须卸载重装，无法应用内
  覆盖升级。

### 手工验收步骤

自动测试只覆盖纯逻辑（版本比较、状态机、调度、文案、下载/安装适配器）；
以下需在 Android 真机或模拟器执行：

1. 安装旧版本 APK。
2. 把 `LATEST.txt` 与新版 APK 按 `<version>/android/...` 布局上传到测试前缀
   （构建时可用 `EXPO_PUBLIC_LEWORK_UPDATE_BASE_URL` 指向该前缀）。
3. 启动 App：出现更新 Modal，下载进度前进。
4. 点「立即安装」：系统安装器打开；允许安装未知应用后完成覆盖安装。
5. 重启 App：版本号更新，不再提示。
6. 关闭网络后在设置页手动检查：显示可读错误，不崩溃。

## 真机验收清单

自动验证（CI 或本地命令）：

- [ ] `pnpm --filter @lework/mobile test` 通过（纯逻辑：设置、事件归并、审批合并、深链、项目聚合、发送目标、侧边栏分组、草稿）。
- [ ] `pnpm --filter @lework/mobile typecheck` 通过。
- [ ] `cd frontend/apps/mobile && npx expo export --platform android` 能打出 Android bundle。

需人工执行（需要真机、FCM 与控制面）：

- [ ] 首次启动进入 Settings，错误地址/错误令牌给出可读错误，且不出现 token。
- [ ] 保存后回到首页：只有一个 `☰` 和大输入框，没有标题、路径或节点信息。
- [ ] 点 `☰` 侧边栏从左侧滑出：新任务 / 查看全部 / 按项目分组的最近 3 条会话 / 用户 / 设置。
- [ ] 项目按最近活动倒序；某个项目超过 3 条会话时出现「全部会话 ›」。
- [ ] 点侧边栏里的会话进入会话页；点标题 `⌄` 能切换同项目会话。
- [ ] 同一仓库挂两个节点时，项目只出现一次，会话合并成一个列表。
- [ ] 项目页可切换主节点；主节点离线时自动落到其它在线节点发送。
- [ ] 节点全部离线时输入框禁用并显示原因，项目卡标灰。
- [ ] 某会话请求审批时，锁屏收到通知；点通知直接落到该会话的审批卡片。
- [ ] 在审批卡片点 Allow once，桌面端会话继续；点 Reject 可填写原因。
- [ ] 深链指向已删除会话时显示「会话不存在」，可去待审批或回首页。
- [ ] 发送失败（例如节点掉线）后草稿仍在输入框里。
- [ ] 切后台超过一分钟再回前台：事件流自动重连，快照被重新拉取，不出现重复审批卡。
- [ ] 关闭通知开关后，控制面不再向该设备推送；重新开启会重新登记设备。

## 已知边界

- 仅 Android；iOS 未验证。
- 手机端只做会话读取、审批、一句话输入；终端、diff、文件浏览仍属平板阶段。
- 项目索引只缓存「id / canonical / vcs / 所属节点 / lastSeen」；会话、消息、
  diff 永远以 OpenCode 为准，离线时不显示缓存的会话快照。
- `/api/projects` 实时聚合在线节点，节点多时首屏会慢一点；离线节点用索引兜底。
- 不提供设备级令牌或多用户审计；共享令牌即最高权限。
