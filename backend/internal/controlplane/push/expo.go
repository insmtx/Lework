// Package push 把控制面事件转成手机推送。它只做两件事：把审批请求变成
// Expo 推送消息，以及在没有设备或推送失败时安静地退让。推送是旁路能力，
// 任何失败都不能影响事件转发本身。
package push

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"time"
)

// defaultExpoEndpoint 是 Expo 推送服务的发送入口。
const defaultExpoEndpoint = "https://exp.host/--/api/v2/push/send"

// sendTimeout 限制一次推送请求的耗时；移动端推送不值得让 goroutine 长等。
const sendTimeout = 10 * time.Second

// Message 是一条推送。字段名遵循 Expo Push 的线格式，Tag 用来让同一条
// 会话的后续通知覆盖前一条（Android 上按 tag 折叠）。
type Message struct {
	To        string            `json:"to"`
	Title     string            `json:"title"`
	Body      string            `json:"body"`
	Data      map[string]string `json:"data,omitempty"`
	Tag       string            `json:"tag,omitempty"`
	ChannelID string            `json:"channelId,omitempty"`
	Priority  string            `json:"priority,omitempty"`
}

// Sender 抽象推送出口，测试与替换服务端时只需实现这一个方法。
type Sender interface {
	Send(ctx context.Context, msgs []Message) error
}

// ExpoSender 通过 Expo Push API 发送消息。
type ExpoSender struct {
	endpoint string
	client   *http.Client
}

// NewExpoSender 创建指向 Expo 官方入口的发送器。client 为 nil 时使用带
// 超时的默认客户端，避免共享一个没有超时的 http.Client。
func NewExpoSender(client *http.Client) *ExpoSender {
	return NewExpoSenderWithEndpoint(defaultExpoEndpoint, client)
}

// NewExpoSenderWithEndpoint 允许测试与私有部署替换入口地址。
func NewExpoSenderWithEndpoint(endpoint string, client *http.Client) *ExpoSender {
	if client == nil {
		client = &http.Client{Timeout: sendTimeout}
	}
	return &ExpoSender{endpoint: endpoint, client: client}
}

// expoResponse 是 Expo 推送票据响应；status 为 error 的票据代表该条
// 消息被服务端拒绝（例如 DeviceNotRegistered）。
type expoResponse struct {
	Data []struct {
		Status  string `json:"status"`
		Message string `json:"message"`
		Details struct {
			Error string `json:"error"`
		} `json:"details"`
	} `json:"data"`
}

// Send 批量提交推送。空批次直接返回，避免无意义的网络请求。
func (s *ExpoSender) Send(ctx context.Context, msgs []Message) error {
	if len(msgs) == 0 {
		return nil
	}

	body, err := json.Marshal(msgs)
	if err != nil {
		return fmt.Errorf("encode push messages: %w", err)
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, s.endpoint, bytes.NewReader(body))
	if err != nil {
		return fmt.Errorf("build push request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "application/json")

	resp, err := s.client.Do(req)
	if err != nil {
		return fmt.Errorf("send push request: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		// 不回显响应体：其中可能带服务端回传的推送令牌。
		return fmt.Errorf("expo push: unexpected status %d", resp.StatusCode)
	}

	var decoded expoResponse
	if err := json.NewDecoder(resp.Body).Decode(&decoded); err != nil {
		return fmt.Errorf("decode push response: %w", err)
	}
	for _, ticket := range decoded.Data {
		if ticket.Status == "error" {
			reason := ticket.Details.Error
			if reason == "" {
				reason = ticket.Message
			}
			return fmt.Errorf("expo push ticket rejected: %s", reason)
		}
	}
	return nil
}
