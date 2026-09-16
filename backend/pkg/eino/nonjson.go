package eino

import (
	"bytes"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"
)

// maxUpstreamPeekSize 限制窥探上游响应体的字节数。
// 只用于判定响应是否为 HTML/XML 以及生成错误片段，不会读取整个响应。
const maxUpstreamPeekSize = 256

// maxSnippetRunes 限制错误信息里回显的响应片段长度，
// 避免把整页 HTML 带进日志、NodeEvent 或接口响应。
const maxSnippetRunes = 120

// ErrUpstreamNotJSON 表示上游返回了 HTTP 响应，但响应体不是 JSON，
// 典型场景是 base_url 指向了网关的 Web 首页（缺少 /v1 之类的 API 前缀）。
// 调用方可用 errors.Is 判定该类错误，并据此切换端点前缀重试。
var ErrUpstreamNotJSON = errors.New("upstream returned non-JSON content")

// UpstreamNotJSONError 描述一次"上游以非 JSON 内容冒充成功响应"的响应，
// 携带定位问题所需的 URL、状态码与响应片段。
type UpstreamNotJSONError struct {
	URL         string
	StatusCode  int
	ContentType string
	Snippet     string
}

// Error 输出可直接面向用户的可读错误，
// 替代原先由 JSON 解析器抛出的 "invalid character '<' looking for beginning of value"。
func (e *UpstreamNotJSONError) Error() string {
	contentType := e.ContentType
	if strings.TrimSpace(contentType) == "" {
		contentType = "unknown"
	}
	return fmt.Sprintf(
		"上游返回的不是 JSON 而是 HTML/XML 等页面内容（status %d, content-type %s, url %s, 响应开头 %q）；"+
			"请确认 base_url 指向 API 根地址，且必需的路径前缀（如 /v1）已补全",
		e.StatusCode, contentType, e.URL, truncateSnippet(e.Snippet),
	)
}

// Unwrap 暴露哨兵错误，便于调用方用 errors.Is 判定。
func (e *UpstreamNotJSONError) Unwrap() error { return ErrUpstreamNotJSON }

// IsUpstreamNotJSON 判断错误链中是否包含"上游返回非 JSON"这一类错误。
func IsUpstreamNotJSON(err error) bool { return errors.Is(err, ErrUpstreamNotJSON) }

// newJSONGuardingHTTPClient 构造一个带响应守卫的 HTTP 客户端。
// 上游若用 HTML 页面冒充 API 响应（常见于 base_url 少了 /v1 而打到网关首页），
// 这里会提前转成可读错误，而不是把页面文本一路带到 JSON 解析层再抛出难以理解的语法错误。
//
// 注意：eino-ext 在显式传入 HTTPClient 时不再使用其 Timeout 字段，
// 而本包此前也未设置 Timeout（等价于无限等待），因此这里不改变既有的超时语义。
func newJSONGuardingHTTPClient() *http.Client {
	return &http.Client{Transport: &jsonGuardTransport{base: http.DefaultTransport}}
}

// jsonGuardTransport 在传输层拦截以标记语言（'<' 开头）返回的响应体。
type jsonGuardTransport struct {
	base http.RoundTripper
}

func (t *jsonGuardTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	base := t.base
	if base == nil {
		base = http.DefaultTransport
	}

	resp, err := base.RoundTrip(req)
	if err != nil || resp == nil || resp.Body == nil {
		return resp, err
	}

	head, readErr := io.ReadAll(io.LimitReader(resp.Body, maxUpstreamPeekSize))
	if readErr != nil {
		// 窥探失败：把已读内容放回，交回原有链路处理。
		resp.Body = replayBody(head, resp.Body)
		return resp, nil
	}

	// JSON 以 '{' / '[' 开头，SSE 以 "data:" 开头，都不会以 '<' 开头，
	// 因此流式响应与正常 JSON 响应不受影响。
	if !looksLikeMarkup(head) {
		resp.Body = replayBody(head, resp.Body)
		return resp, nil
	}

	_ = resp.Body.Close()

	safe := safeURL(req)
	// http.Client 会拿 req.URL 构造外层的 *url.Error，
	// 而 base_url 可能带 ?key=xxx 之类的凭据，因此在这里清掉 query 与 userinfo。
	// 该请求已确定失败，且 SDK 重试时会从原始请求重新 clone，不影响后续尝试。
	req.URL.RawQuery = ""
	req.URL.User = nil

	return nil, &UpstreamNotJSONError{
		URL:         safe,
		StatusCode:  resp.StatusCode,
		ContentType: resp.Header.Get("Content-Type"),
		Snippet:     string(head),
	}
}

// looksLikeMarkup 判断响应体开头（跳过空白）是否为 HTML/XML 之类的标记内容。
func looksLikeMarkup(head []byte) bool {
	trimmed := bytes.TrimSpace(head)
	if len(trimmed) == 0 {
		return false
	}
	return trimmed[0] == '<'
}

// replayBody 把已窥探的字节放回响应流，保证调用方仍能读到完整响应体。
func replayBody(head []byte, rest io.ReadCloser) io.ReadCloser {
	return struct {
		io.Reader
		io.Closer
	}{io.MultiReader(bytes.NewReader(head), rest), rest}
}

// safeURL 只取 scheme/host/path，丢弃 query 与 userinfo，
// 避免把 base_url 里可能携带的 API Key 写进错误文本、日志或事件。
func safeURL(req *http.Request) string {
	if req == nil || req.URL == nil {
		return ""
	}
	return req.URL.Scheme + "://" + req.URL.Host + req.URL.Path
}

// truncateSnippet 压缩空白并按 rune 截断响应片段，保证错误信息是单行且长度可控。
func truncateSnippet(snippet string) string {
	flat := strings.Join(strings.Fields(snippet), " ")
	runes := []rune(flat)
	if len(runes) <= maxSnippetRunes {
		return flat
	}
	return string(runes[:maxSnippetRunes]) + "..."
}
