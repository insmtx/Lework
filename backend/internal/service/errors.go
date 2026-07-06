package service

import "errors"

// ErrNoDefaultAssistant 表示项目未配置 AI 队友，无法为 task session 分配 worker。
var ErrNoDefaultAssistant = errors.New("no default assistant in project")

// ErrNoDefaultAssistantInOrg 表示组织没有默认 AI 队友。
var ErrNoDefaultAssistantInOrg = errors.New("no default assistant in organization")

// ErrInvalidAssistantID 表示传入的助理 ID 无效。
var ErrInvalidAssistantID = errors.New("invalid assistant id")

// ErrDuplicateAssistant 表示传入的助理 ID 重复。
var ErrDuplicateAssistant = errors.New("duplicate assistant id")

// ErrCannotRemoveDefaultAssistant 表示不能移除默认 AI 队友。
var ErrCannotRemoveDefaultAssistant = errors.New("cannot remove default assistant")
