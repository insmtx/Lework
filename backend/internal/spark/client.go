package spark

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net"
	"net/http"
	"strings"
	"time"

	"github.com/insmtx/Leros/backend/types"
)

const maxResponseBytes = 1024 * 1024

// ErrBadResponse prevents unverified host observations from being treated as safe.
var ErrBadResponse = errors.New("spark agent response cannot be verified")

// ErrUnavailable describes missing observations without exposing local paths or logs.
var ErrUnavailable = errors.New("spark agent unavailable")

func readSnapshot(ctx context.Context, socketPath, hostID string) (types.SparkSnapshot, error) {
	var result types.SparkSnapshot
	transport := &http.Transport{
		Proxy: nil,
		DialContext: func(ctx context.Context, _, _ string) (net.Conn, error) {
			return (&net.Dialer{Timeout: 2 * time.Second}).DialContext(ctx, "unix", socketPath)
		},
		DisableKeepAlives:      true,
		MaxResponseHeaderBytes: 8192,
	}
	defer transport.CloseIdleConnections()
	client := &http.Client{Transport: transport, Timeout: 15 * time.Second,
		CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, "http://spark-agent/status", nil)
	if err != nil {
		return result, ErrUnavailable
	}
	resp, err := client.Do(req)
	if err != nil {
		return result, ErrUnavailable
	}
	defer resp.Body.Close()
	if resp.StatusCode >= 300 && resp.StatusCode < 400 {
		return result, ErrBadResponse
	}
	if resp.StatusCode != http.StatusOK {
		return result, ErrUnavailable
	}
	body, err := io.ReadAll(io.LimitReader(resp.Body, maxResponseBytes+1))
	if err != nil {
		return result, ErrUnavailable
	}
	if len(body) > maxResponseBytes || json.Unmarshal(body, &result) != nil {
		return result, ErrBadResponse
	}
	if result.ProtocolVersion != 1 || result.HostID != hostID || !shortText(result.BootID) ||
		!shortText(result.Hostname) || result.Mode != "read_only" || result.ModelState != "unconfigured" ||
		!fresh(result.ObservedAt) || result.Components == nil || result.Checks == nil ||
		len(result.Components) > 256 || len(result.Checks) > 64 {
		return types.SparkSnapshot{}, ErrBadResponse
	}
	for _, c := range result.Components {
		if (c.Kind != "docker" && c.Kind != "kubernetes") || !shortText(c.ID) || !shortText(c.Name) || !shortText(c.State) {
			return types.SparkSnapshot{}, ErrBadResponse
		}
	}
	for _, c := range result.Checks {
		if !shortText(c.Code) || !shortText(c.Message) {
			return types.SparkSnapshot{}, ErrBadResponse
		}
	}
	return result, nil
}
func shortText(s string) bool { return len(s) > 0 && len(s) <= 2048 && strings.TrimSpace(s) != "" }
func fresh(stamp int64) bool {
	age := time.Now().Unix() - stamp
	return stamp > 0 && age >= -5 && age <= 30
}
