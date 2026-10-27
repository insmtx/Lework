package acp

import (
	"context"
	"fmt"
	"strings"

	acpsdk "github.com/coder/acp-go-sdk"
)

// Session 是 ACP 会话的最小句柄。
type Session struct {
	conn    *Conn
	ID      acpsdk.SessionId
	Cwd     string
	Options []acpsdk.SessionConfigOption
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
		return fmt.Errorf("acp config injection lost: configOptions has no model option")
	}
	if got != want {
		return fmt.Errorf("acp config injection mismatch: model currentValue=%q, want %q", got, want)
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
