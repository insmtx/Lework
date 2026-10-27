package acp

import (
	"context"
	"fmt"
	"strings"
	"sync/atomic"

	acpsdk "github.com/coder/acp-go-sdk"
)

// Stats 是传输层计数器；由 opencode 在收尾时读取并打日志，
// 不要在 opencode 包里另建同名计数器（设计文档 §11 / 计划 W2-2）。
type Stats struct {
	ResumeFallbacks atomic.Int64
	UnknownUpdates  atomic.Int64
	DroppedEvents   atomic.Int64
	ProtocolErrors  atomic.Int64
	FormDeclines    atomic.Int64
}

// StatsSnapshot 是计数器的只读快照。
type StatsSnapshot struct {
	ResumeFallbacks int64
	UnknownUpdates  int64
	DroppedEvents   int64
	ProtocolErrors  int64
	FormDeclines    int64
}

// Snapshot 返回当前计数。
func (s *Stats) Snapshot() StatsSnapshot {
	if s == nil {
		return StatsSnapshot{}
	}
	return StatsSnapshot{
		ResumeFallbacks: s.ResumeFallbacks.Load(),
		UnknownUpdates:  s.UnknownUpdates.Load(),
		DroppedEvents:   s.DroppedEvents.Load(),
		ProtocolErrors:  s.ProtocolErrors.Load(),
		FormDeclines:    s.FormDeclines.Load(),
	}
}

// ConfigMismatchError 表示注入的配置没有在 agent 侧生效。
// opencode 把它映射为 runtime_version_mismatch 类错误码并快速失败（计划 W2-2）。
type ConfigMismatchError struct {
	Option string
	Want   string
	Got    string
	Reason string
}

func (e *ConfigMismatchError) Error() string {
	if e.Reason != "" {
		return fmt.Sprintf("acp config mismatch (%s): %s", e.Option, e.Reason)
	}
	return fmt.Sprintf("acp config mismatch (%s): got %q, want %q", e.Option, e.Got, e.Want)
}

// Session 是 ACP 会话的最小句柄。
type Session struct {
	conn    *Conn
	ID      acpsdk.SessionId
	Cwd     string
	Options []acpsdk.SessionConfigOption
}

// ResumeOrNewSession 优先 resume 已有会话；失败时降级新建并累加 ResumeFallbacks。
// 返回值 resumed 表示是否真的续上了旧会话（false 表示已降级）。
func (c *Conn) ResumeOrNewSession(
	ctx context.Context,
	sessionID string,
	cwd string,
	mcp []acpsdk.McpServer,
) (sess *Session, resumed bool, err error) {
	sessionID = strings.TrimSpace(sessionID)
	if sessionID != "" {
		resumedSess, resumeErr := c.ResumeSession(ctx, acpsdk.SessionId(sessionID), cwd)
		if resumeErr == nil {
			return resumedSess, true, nil
		}
		// 旧 v1 会话 id 在 v2 库里不存在、或 cwd 不一致，都属预期降级。
		c.stats.ResumeFallbacks.Add(1)
	}
	newSess, err := c.NewSession(ctx, cwd, mcp)
	if err != nil {
		return nil, false, err
	}
	return newSess, false, nil
}

// ResumeSession 续上已有会话；调用方负责在失败时决定是否降级。
func (c *Conn) ResumeSession(ctx context.Context, id acpsdk.SessionId, cwd string) (*Session, error) {
	if id == "" {
		return nil, fmt.Errorf("acp session/resume: sessionId is required")
	}
	if strings.TrimSpace(cwd) == "" {
		return nil, fmt.Errorf("acp session/resume: cwd is required")
	}
	resp, err := c.sdk.ResumeSession(ctx, acpsdk.ResumeSessionRequest{
		SessionId: id,
		Cwd:       cwd,
	})
	if err != nil {
		return nil, fmt.Errorf("acp session/resume: %w", err)
	}
	return &Session{conn: c, ID: id, Cwd: cwd, Options: resp.ConfigOptions}, nil
}

// NewSession 在 cwd 下新建会话。
// 探针不传 AdditionalDirectories：它是 external_directory 的权限旁路，
// 安全评审已否决（设计文档 §4、D-10）。
func (c *Conn) NewSession(ctx context.Context, cwd string, mcp []acpsdk.McpServer) (*Session, error) {
	if strings.TrimSpace(cwd) == "" {
		return nil, fmt.Errorf("acp session cwd is required")
	}
	if mcp == nil {
		mcp = []acpsdk.McpServer{}
	}
	resp, err := c.sdk.NewSession(ctx, acpsdk.NewSessionRequest{
		Cwd:        cwd,
		McpServers: mcp,
	})
	if err != nil {
		return nil, fmt.Errorf("acp session/new: %w", err)
	}
	if resp.SessionId == "" {
		return nil, fmt.Errorf("acp session/new returned empty sessionId")
	}
	return &Session{
		conn:    c,
		ID:      resp.SessionId,
		Cwd:     cwd,
		Options: resp.ConfigOptions,
	}, nil
}

// ModelCurrentValue 返回 configOptions 中 model 的 currentValue。
// 这是「配置注入是否生效」的启动期自检依据（设计文档 §4、R-1）。
func (s *Session) ModelCurrentValue() (string, bool) {
	return configOptionValue(s.Options, "model")
}

// ModeCurrentValue 返回 configOptions 中 mode 的 currentValue（探针日志用）。
func (s *Session) ModeCurrentValue() (string, bool) {
	return configOptionValue(s.Options, "mode")
}

func configOptionValue(options []acpsdk.SessionConfigOption, id string) (string, bool) {
	for _, option := range options {
		selectOption := option.Select
		if selectOption == nil || string(selectOption.Id) != id {
			continue
		}
		return string(selectOption.CurrentValue), true
	}
	return "", false
}

// ConfigOptionValues 返回某个 select 型 configOption 的全部候选值，
// 用于验证自定义 agent 是否真的注册进了 mode 列表。
func (s *Session) ConfigOptionValues(id string) []string {
	for _, option := range s.Options {
		selectOption := option.Select
		if selectOption == nil || string(selectOption.Id) != id {
			continue
		}
		values := make([]string, 0, 8)
		if ungrouped := selectOption.Options.Ungrouped; ungrouped != nil {
			for _, item := range *ungrouped {
				values = append(values, string(item.Value))
			}
		}
		if grouped := selectOption.Options.Grouped; grouped != nil {
			for _, group := range *grouped {
				for _, item := range group.Options {
					values = append(values, string(item.Value))
				}
			}
		}
		return values
	}
	return nil
}

// RequireModel 校验注入的模型确实生效。
// 上游对配置错误只回 provider.invalid-request（探针 README 的坑 1），
// 因此必须在建会话后立即比对，而不是等 Prompt 失败。
func (s *Session) RequireModel(want string) error {
	want = strings.TrimSpace(want)
	if want == "" {
		return nil
	}
	got, ok := s.ModelCurrentValue()
	if !ok {
		return &ConfigMismatchError{Option: "model", Reason: "configOptions has no model option"}
	}
	if got != want {
		return &ConfigMismatchError{Option: "model", Want: want, Got: got}
	}
	return nil
}

// CloseSession 释放 agent 侧会话；失败不影响进程回收，调用方可只记日志。
func (c *Conn) CloseSession(ctx context.Context, id acpsdk.SessionId) error {
	if id == "" {
		return nil
	}
	if _, err := c.sdk.CloseSession(ctx, acpsdk.CloseSessionRequest{SessionId: id}); err != nil {
		return fmt.Errorf("acp session/close: %w", err)
	}
	return nil
}
