# Android 手机端（P1）

`frontend/apps/mobile` 是 Expo（React Native）实现的**纯客户端**：它不运行
`lework serve`、不运行 `agentd`、也不在手机上执行 OpenCode。所有会话事实来自
控制面背后的节点，App 只负责展示与提交审批。

本阶段只覆盖 Android 手机；仅以 APK 分发，不发布到 Google Play。

## 服务端要求

- 控制面可从手机访问：`lework serve --listen 0.0.0.0:8787`（或反向代理到该端口）。
  非回环监听会让浏览器 API 要求令牌，见 `docs/operations/control-plane.md` 与
  设计稿 §7。
- 手机端用 `Authorization: Bearer <enrollment token>` 访问全部 API；SSE
  `/api/events` 同样只走 header，令牌不会出现在 URL、日志或通知负载中。
- 生产环境请在控制面前置 TLS（反向代理）。Android 默认禁止明文 HTTP；开发期
  若必须用 `http://`，需在 `app.json` 的 `android` 下显式开启 cleartext（见
  `expo-build-properties` 文档），并把它当作临时手段。
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
3. 回到 Overview：应能看到所有节点的会话状态。

令牌轮换：在服务端更换 enrollment token 并重启后，手机端 Settings 里改成新
令牌重新保存即可；旧令牌立即失效，不需要卸载 App。

## 构建 APK

首次使用需要 EAS CLI 与 Expo 账号：

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

## 真机验收清单

自动验证（CI 或本地命令）：

- [ ] `pnpm --filter @lework/mobile test` 通过（纯逻辑：设置、事件归并、审批合并、深链）。
- [ ] `pnpm --filter @lework/mobile typecheck` 通过。
- [ ] `cd frontend/apps/mobile && npx expo export --platform android` 能打出 Android bundle。

需人工执行（需要真机、FCM 与控制面）：

- [ ] 首次启动进入 Settings，错误地址/错误令牌给出可读错误，且不出现 token。
- [ ] 保存后 Overview 列出所有节点；某节点上有会话在跑时状态为 Running。
- [ ] 某会话请求审批时，锁屏收到通知；点通知直接落到该会话的审批卡片。
- [ ] 在审批卡片点 Allow once，桌面端会话继续；点 Reject 可填写原因。
- [ ] 断开节点后，Overview 中该节点标记为离线且不影响其他节点。
- [ ] 切后台超过一分钟再回前台：事件流自动重连，快照被重新拉取，不出现重复审批卡。
- [ ] 关闭通知开关后，控制面不再向该设备推送；重新开启会重新登记设备。

## 已知边界

- 仅 Android；iOS 未验证。
- 手机端是只读会话视图 + 审批 + 一句话回复；新建会话、终端、diff、文件浏览属于平板阶段。
- 会话状态由控制面派生索引提供，索引重启后会短暂为空；回复始终以 OpenCode 的
  `permission_id` 为准，本地卡片只是提示。
- 不提供设备级令牌或多用户审计；共享令牌即最高权限。
