# 核心机制详解

本文档详细描述 Lework 前端的核心机制实现。

## 1. 路由与鉴权

### 路由结构

- `/login` — BlankLayout，登录页

- `/` — BasicLayout，所有业务页面（重定向至 `/overview`）
- `/exception/{403,404,500}` — 异常页面
- `/chat` — Shell 三栏布局，AI 聊天交互

### 动态路由机制

- 7 个内置模块各自提供 `moduleConfig.ts` 配置（routes + menus）
- 第三方插件通过后端 `/platform/ui-extensions` API 获取菜单和组件映射
- 首次导航触发 `registerPluginRoutes()` 动态注入路由

### 路由守卫流程

```
路由切换前守卫:
  ├─ 设置 document.title
  ├─ 已登录 + 访问 login → 重定向至 "/"
  ├─ 未登录 + 非 login → 重定向至 login
  ├─ 插件未加载 → fetchExtensions() + registerPluginRoutes()
  │   ├─ 修复重定向后的 404误判：重新 resolve 目标路由
  │   └─ replace 重入当前路由以触发新动态路由生效
  └─ 检查 requiredRole → 无权限重定向至 /chat
```

### 鉴权状态管理

- userSlice 存储 Token、用户信息、角色、权限
- 401 响应自动 logout + 跳转 `/login`
- 演示账号 403 响应: `message.warning("演示账号无操作权限")`

## 2. AI 交互系统

三层 AI 能力集成：

| 层级 | 实现 | 用途 |
|------|------|------|
| AI 浮动助手 | `components/ai-float/AiFloat.tsx` + `useAiFloat` | 全局侧边 AI 助手，Prompt 跨组件传递 |
| 内联 AI | `hooks/useInlineAi` | 表单/页面内 AI 流式交互（SSE EventSource） |
| 聊天页面 | `pages/chat/` + Shell 三栏 | 专用全屏 AI 助手界面 |
| AI 辅助按钮 | `components/business/AiAssistButton.tsx` | 操作按钮触发 AI 辅助 |

### SSE 流式交互流程 (`useInlineAi`)

1. `createChatSession` → 获取 `session_id`
2. `sendChatMessage` → 获取 `message_id`
3. `getChatStreamUrl(messageId, token)` → 建立 EventSource
4. 监听 `text/thinking/tool_use/tool_result/done/server_error/interrupted` 事件
5. 实时更新 `text → html`（Markdown 渲染）
6. 支持缓存（`cacheKey`）、中断（`abort`）、重置（`reset`）

## 3. 业务 CRUD 模式

### 通用 CRUD 分页 (`hooks/useCrudPage`)

- 传入 API 函数 + 删除 API → 自动管理列表/分页/筛选/排序/选择
- 提供: `listData, loading, total, filterParams, selectedIds, loadData, deleteRow`
- 支持响应式 API 函数（useMemo），适用于动态切换的 API

### 通用对话框表单 (`hooks/useDialogForm`)

- 标准化 新增/编辑/详情 模式
- 支持 Zod 表单验证、自动加载详情、create/update 操作映射
- 提供: `loading, formState, isEdit, isReadonly, onSubmit, fetchDetail`

### 脏检查 (`hooks/useDirtyCheck`)

- JSON 序列化快照比对
- 关闭前确认对话框（`dialog.confirm`）

## 4. 插件化架构

### 内置模块声明 (各 `pages/` 的 `moduleConfig.ts`)

- 每个 module 导出 `{ routes: RouteRecord[], menus: MenuConfig[] }`
- 路由和菜单通过 `moduleConfigs.flatMap(c => c.routes)` 和 `...moduleConfig.menus` 集成

### 第三方插件动态注册 (`store/slices/pluginSlice.ts` + `router/pluginRoutes.ts`)

1. 首次导航 → `pluginStore.fetchExtensions()` 从后端获取扩展声明
2. `registerPluginRoutes()` 动态注入第三方路由
3. `pluginStore.pluginMenus()` 向侧边栏注入第三方菜单
4. 组件映射 `pluginViews` 预注册第三方页面组件

## 5. 通知系统

三层通知 (`hooks/useNotification`)：

| 层级 | 实现 | 触发场景 |
|------|------|---------|
| Toast | `notification.info/error` | WebSocket 消息/任务完成 |
| 桌面通知 | `Notification API` | 页面不可见时 |
| 声音通知 | `AudioContext + decodeAudioData` | 用户交互后初始化 |

- 标签页标题闪烁: `(N) Lework - xxx`
- 偏好持久化: `localStorage notification-prefs`

## 6. 页面权限模型

路由 meta 权限控制：

```
路由 meta:
  requiredRole: "admin" → 仅 admin/demo 角色可访问

管理员页面:
  contacts · plugins · skills · marketplace · channels
  logs · webhooks · agents · ai-config · upgrade · ai-employees · remote-nodes

普通用户页面:
  chat · overview · profile · notifications
  jenkins · knowledge · reports · schedules
```