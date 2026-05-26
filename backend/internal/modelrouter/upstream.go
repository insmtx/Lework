package modelrouter

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"
)

func upstreamAPIPath(proto Protocol, hasV1 bool) string {
	switch proto {
	case ProtocolOpenAIChat:
		if hasV1 {
			return "/v1/chat/completions"
		}
		return "/chat/completions"
	case ProtocolOpenAIResponses:
		if hasV1 {
			return "/v1/responses"
		}
		return "/responses"
	case ProtocolAnthropicMessages:
		if hasV1 {
			return "/v1/messages"
		}
		return "/messages"
	default:
		if hasV1 {
			return "/v1/chat/completions"
		}
		return "/chat/completions"
	}
}

func setUpstreamRequest(ctx context.Context, cfg *UpstreamConfig, body []byte) (*http.Request, error) {
	baseURL := strings.TrimRight(cfg.BaseURL, "/")
	apiPath := upstreamAPIPath(cfg.Protocol, cfg.BaseURLHasV1)
	url := baseURL + apiPath

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(body))
	if err != nil {
		return nil, fmt.Errorf("create upstream request: %w", err)
	}

	req.Header.Set("Content-Type", "application/json")

	switch cfg.Protocol {
	case ProtocolAnthropicMessages:
		req.Header.Set("x-api-key", cfg.APIKey)
		req.Header.Set("anthropic-version", "2023-06-01")
	default:
		req.Header.Set("Authorization", "Bearer "+cfg.APIKey)
	}

	return req, nil
}

func doUpstreamCall(ctx context.Context, cfg *UpstreamConfig, body []byte) ([]byte, error) {
	timeout := time.Duration(cfg.TimeoutSec) * time.Second
	if timeout <= 0 {
		timeout = 120 * time.Second
	}

	client := &http.Client{Timeout: timeout}
	req, err := setUpstreamRequest(ctx, cfg, body)
	if err != nil {
		return nil, err
	}

	resp, err := client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("upstream request failed: %w", err)
	}
	defer resp.Body.Close()

	respBody, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("read upstream response: %w", err)
	}

	if resp.StatusCode >= 400 {
		return nil, &upstreamError{
			StatusCode: resp.StatusCode,
			Body:       respBody,
		}
	}

	return respBody, nil
}

func doUpstreamStreamCall(ctx context.Context, cfg *UpstreamConfig, body []byte) (io.ReadCloser, error) {
	timeout := time.Duration(cfg.TimeoutSec) * time.Second
	if timeout <= 0 {
		timeout = 180 * time.Second
	}

	client := &http.Client{Timeout: timeout}
	req, err := setUpstreamRequest(ctx, cfg, body)
	if err != nil {
		return nil, err
	}

	resp, err := client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("upstream stream request failed: %w", err)
	}

	if resp.StatusCode >= 400 {
		defer resp.Body.Close()
		respBody, _ := io.ReadAll(resp.Body)
		return nil, &upstreamError{
			StatusCode: resp.StatusCode,
			Body:       respBody,
		}
	}

	return resp.Body, nil
}

type upstreamError struct {
	StatusCode int
	Body       []byte
}

func (e *upstreamError) Error() string {
	return fmt.Sprintf("upstream returned status %d: %s", e.StatusCode, string(e.Body))
}
