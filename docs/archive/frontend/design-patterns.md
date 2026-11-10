# 架构设计模式

本文档详细描述 Lework 前端的架构设计模式。

## 1. Slice 模式 (Zustand)

每个功能域拆分为独立 Slice，通过 `SliceCreator<T>` 类型约束，在 `appStore.ts` 中组合为单一 Store。扩展新功能域只需：

1. 创建 `slices/newSlice.ts` (State + ActionImpl)
2. 在 `appStore.ts` 中合并
3. 导出 `useNewStore` selector 辅助函数

## 2. ActionImpl 类模式

将 Action 逻辑封装为类实例，利用 `#私有字段` 隐藏 set/get 引用。`flattenActions` 将原型方法扁平化注入 Store。

优势：
- Action 方法可互相调用 (类内部组合)
- 私有字段保护状态修改入口
- IDE 类型推断友好

## 3. Reducer 模式 (topicSlice)

复杂状态更新使用 Reducer 模式分解：

```ts
#dispatchTopic = (action: TopicActionType) => {
  this.#set((state) => topicReducer(state, action));
};
```

Action 类型驱动状态变更，易于测试和追踪。

## 4. 乐观更新 + 回滚 (topicSlice)

- **创建**: 先插入临时数据 → 调用后端 → 失败则回滚删除
- **更新**: 缓存旧值 → 乐观覆盖 → 失败则回滚还原
- **删除**: 非乐观 — 先确认后端成功 → 再更新本地状态

## 5. 原语包装模式 (UI 组件)

所有 UI 组件遵循统一模式：

```ts
// 1. 引入 @base-ui/react 无样式原语
import { Button as ButtonPrimitive } from '@base-ui/react/button';

// 2. CVA 定义变体样式
const buttonVariants = cva('基础样式', { variants: { ... } });

// 3. 包装组件：合并 className + 传递 Props
function Button({ className, variant, size, ...props }) {
  return <ButtonPrimitive className={cn(buttonVariants({ variant, size, className }))} {...props} />;
}
```

## 6. 连接层抽象 (lib/)

HTTP / SSE / WebSocket 三种通信方式均封装为独立 Client 类：

- **统一接口**: `connect()` / `close()` / `reconnect()` / `getStatus()`
- **自动重连**: 可配置重试策略 (次数、间隔、是否重连)
- **Hook 包装**: 统一返回 `{ data, status, error, ... }`

## 新增 UI 组件扩展指引

遵循原语包装模式：引入 `@base-ui/react` 对应原语 → CVA 定义变体 → cn() 合并样式 → 导出。