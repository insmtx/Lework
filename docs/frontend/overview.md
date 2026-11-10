# 控制面前端

> 更新：2026-11-10 ｜ 目录：`frontend/apps/control-plane/`

## 职责

- 浏览器只访问控制面，不直连节点，不保存节点 token。
- 展示节点列表、Session、消息/工具/审批、Diff、文件、PTY。

## 结构

- 页面：节点总览、Session 详情、Diff 视图、文件浏览、终端。
- 通信：HTTP + SSE（`/api/events`）+ 同源 WebSocket（PTY）。
- 状态：本地只存 UI 偏好；执行状态以控制面与 OpenCode 为准。

## 约束

- 不引入旧 Web 主界面的项目/任务/组织模型。
- 所有请求走同源相对路径。
