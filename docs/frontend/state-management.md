# 状态管理架构

本文档详细描述 Lework 前端的 Zustand 状态管理架构。

## Slice 模式 + ActionImpl 类架构

采用 **Slice 模式 + ActionImpl 类** 的 Zustand 架构：

```
appStore.ts
├── layoutSlice (LayoutActionImpl)
│   ├── State: workspaces, conversations, activeConversationId, ...
│   ├── Actions: toggleLeftRail, switchConversation, createConversation, ...
│   └── 模式: 类实例方法 (私有 #set)
├── topicSlice (TopicActionImpl)
│   ├── State: activeTopicId, topicMaps, topicIds, topicLoadingIds, ...
│   ├── Actions: createTopic, updateTopic, deleteTopic, switchTopic, ...
│   ├── 内部: Reducer 模式 (topicReducer + #dispatchTopic)
│   └── 模式: 乐观更新 + 回滚 + 异步 Action
├── userSlice
│   ├── State: Token, 用户信息, 角色, 权限, mustChangePassword
│   ├── Actions: login, logout, fetchUserInfo, setMustChangePassword
│   └── 持久化: localStorage (zustand/middleware persist)
├── appSettingsSlice
│   ├── State: 侧边栏折叠, 标签页显示, 移动端侧边栏状态
│   ├── Actions: toggleSidebar, setMobileSidebarOpen
│   └── 持久化: localStorage
├── menuSlice
│   ├── State: 菜单列表, 当前激活菜单
│   ├── Actions: setMenus, setActiveMenu
├── tabsSlice
│   ├── State: 页面标签页数据, 激活标签
│   ├── Actions: addTab, removeTab, switchTab
├── configSlice
│   ├── State: 系统配置 (pageSize 等)
│   ├── Actions: fetchConfig, setConfig
├── pluginSlice
│   ├── State: 第三方插件扩展列表, 动态菜单
│   ├── Actions: fetchExtensions, pluginMenus
├── notificationSlice
│   ├── State: 各会话未读计数, InsFlow 未读数
│   ├── Actions: setUnreadCount, clearUnread
└── activeTasksSlice
    ├── State: 正在执行的任务列表
    ├── Actions: addTask, removeTask, setTaskStatus
```

## Store 合成

```ts
import { createWithEqualityFn } from 'zustand/traditional';
import { subscribeWithSelector } from 'zustand/middleware/subscribeWithSelector';
import { devtools } from 'zustand/middleware/devtools';

const createStore: SliceCreator<AppStore> = (...params) => ({
  ...layoutSlice(...params),
  ...topicSlice(...params),
  ...userSlice(...params),
  ...appSettingsSlice(...params),
  ...menuSlice(...params),
  ...tabsSlice(...params),
  ...configSlice(...params),
  ...pluginSlice(...params),
  ...notificationSlice(...params),
  ...activeTasksSlice(...params),
});

export const useAppStore = createWithEqualityFn<AppStore>()(
  subscribeWithSelector(devtools(createStore)),
  Object.is,
);
```

## Slice 持久化与职责

| Slice | 持久化 | 核心职责 |
|-------|--------|---------|
| user | ✅ localStorage | Token、用户信息、角色判断（admin/demo）、强制改密码标志 |
| appSettings | ✅ localStorage | 侧边栏折叠、标签页显示、移动端侧边栏状态 |
| layout | ❌ | 工作区/会话列表、活跃会话、LeftRail/RightRail 状态 |
| topic | ❌ | 话题列表、活跃话题、乐观更新 + 回滚 |
| plugin | ❌ | 第三方插件扩展列表、动态菜单生成 |
| notification | ❌ | 各会话未读计数、InsFlow 未读数 |
| tabs | ❌ | 页面标签页数据 |
| config | ❌ | 系统配置（pageSize 等） |

| activeTasks | ❌ | 正在执行的任务列表 |

## Slice 访问辅助

```ts
export const useLayoutStore = <T>(
  selector: (state: LayoutStore & LayoutAction) => T,
): T => useAppStore(selector);

export const useTopicStore = <T>(
  selector: (state: TopicStore & TopicAction) => T,
): T => useAppStore(selector);

export const useUserStore = <T>(
  selector: (state: UserStore & UserAction) => T,
): T => useAppStore(selector);

export const usePluginStore = <T>(
  selector: (state: PluginStore & PluginAction) => T,
): T => useAppStore(selector);

// ... 其他 Slice 的 selector 辅助函数
```

## flattenActions 工具

`flattenActions` 将 ActionImpl 类实例的方法从原型链提取并扁平化为普通对象，以适配 Zustand 的 flat state 要求。遍历原型链收集所有方法名，绑定实例上下文后合并。

## topicSlice Reducer 模式

Topic 切片采用类似 Redux 的 Reducer 模式，通过 `#dispatchTopic` 调用 `topicReducer` 处理以下 Action 类型：

- `addTopic` — 乐观创建 (临时 ID + loading 状态 + 回滚)
- `updateTopic` — 乐观更新 (缓存旧值 + 错误回滚)
- `removeTopic` — 非乐观删除 (先调用后端再更新状态)
- `setTopics` — 批量覆盖

## 私有字段约定

ActionImpl 类使用 JavaScript `#私有字段` 模式 (`#set`, `#get`, `#dispatchTopic`)，确保状态修改方法不可外部访问。

## 新增 Slice 扩展指引

1. 在 `store/slices/` 创建 `newSlice.ts`：
   - 定义 `NewState` / `NewAction` / `NewStore` 类型
   - 实现 `NewActionImpl` 类
   - 导出 `newSlice: SliceCreator<NewStore>`

2. 在 `appStore.ts` 合成：
   ```ts
   export type AppStore = LayoutStore & TopicStore & NewStore;
   export type AppAction = LayoutAction & TopicAction & NewAction;

   const createStore: SliceCreator<AppStore> = (...params) => ({
     ...layoutSlice(...params),
     ...topicSlice(...params),
     ...newSlice(...params),
   });
   ```

3. 导出 selector 辅助：
   ```ts
   export const useNewStore = <T>(
     selector: (state: NewStore & NewAction) => T,
   ): T => useAppStore(selector);
   ```