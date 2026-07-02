# 通信层架构

本文档详细描述 Lework 前端的 HTTP、SSE、WebSocket 通信层实现。

## HttpClient (`lib/request.ts`)

基于原生 `fetch` 的 HTTP 客户端：

- 请求/响应拦截器 (`useRequestInterceptor` / `useResponseInterceptor`)
- 超时控制 (`AbortController`)
- 重试机制 (仅 5xx 错误重试，指数递增延迟)
- 默认导出 `http` 实例 + `createHttpClient` 工厂函数

### 使用方式

```ts
import { http } from '@/lib/request';

const response = await http.get<DataType>('/api/endpoint');
// response.data, response.status
```

### 配置 baseURL

```ts
const apiClient = createHttpClient('http://localhost:8080/api', {
  'Authorization': 'Bearer xxx',
});
```

### 添加请求拦截器

```ts
http.useRequestInterceptor((config) => ({
  ...config,
  headers: { ...config.headers, Authorization: `Bearer ${token}` },
}));
```

## SSEClient (`lib/sse.ts`)

EventSource 包装，支持自定义事件监听：

- 自动重连 (`retryCount` + `retryInterval` + `maxRetries`)
- 状态管理 (connecting / open / closed)
- Header 通过 URL query 参数传递 (EventSource API 限制)
- 支持自定义事件监听 (`on/off`)

## WSClient (`lib/websocket.ts`)

WebSocket 包装，支持完整生命周期管理：

- 自动重连 (指数退避，最大 30s)
- 心跳检测 (`heartbeatInterval` + `heartbeatMessage`)
- 消息队列 (离线时缓存消息，连接后 flush)
- 协议支持 (`protocols` 参数)

## Hook 层 (`hooks/`)

### useSSE<T>(url, options)

- 自动连接/断开 (url 变化触发 reconnect)
- 返回 `{ data, status, error, reconnect }`
- JSON 自动解析，失败则返回原始数据

### useWebSocket<T>(url, options)

- 自动连接/断开 + 消息队列
- 返回 `{ data, status, error, send, reconnect }`
- 附加 `useWebSocketMessage<T>` 变体 (返回 WSMessage 包装类型)

## 数据流

### SSE 实时推送数据流

```
后端 SSE 事件流
    ↓
SSEClient (lib/sse.ts)
    ↓
useSSE Hook (hooks/useSSE.ts)
    ↓
组件消费 { data, status, error, reconnect }
```

### WebSocket 双向通信数据流

```
前端发送: useWebSocket.send(data) → WSClient.send() → WebSocket
后端推送: WebSocket.onmessage → WSClient → useWebSocket { data }
    ↓
自动重连 (指数退避) + 心跳检测
```

### HTTP 请求数据流

```
HttpClient.request<T>(url, options)
    ↓ 请求拦截器
    ↓ 超时控制 (AbortController)
    ↓ fetch
    ↓ 响应拦截器
    ↓ 错误处理 (5xx 重试)
    ↓
ApiResponse<T> / ApiError
```

## 新增通信方式扩展指引

在 `lib/` 创建新 Client 类 + `types/api.ts` 定义类型 + `hooks/` 创建对应 Hook。

统一接口规范：
- `connect()` — 建立连接
- `close()` — 关闭连接
- `reconnect()` — 重连
- `getStatus()` — 获取连接状态
- Hook 统一返回 `{ data, status, error, ... }` 格式