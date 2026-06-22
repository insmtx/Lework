package modelrouter

import (
	"strings"

	"github.com/insmtx/Leros/backend/internal/worker/identity"
)

// WorkerProxyBaseURL returns the built-in worker model proxy BaseURL.
// The proxy is registered at identity.WorkerAddr() by RegisterRoutes.
// Requests sent to this address are transparently routed to the upstream provider
// according to the config previously registered in DefaultStore().
//
// Returns empty string when the worker address is not yet configured.
func WorkerProxyBaseURL() string {
	addr := strings.TrimSpace(identity.WorkerAddr())
	if addr == "" {
		return ""
	}
	addr = strings.TrimRight(addr, "/")
	if strings.HasPrefix(addr, "http://") || strings.HasPrefix(addr, "https://") {
		return ensureV1Suffix(addr)
	}
	if strings.HasPrefix(addr, ":") {
		return ensureV1Suffix("http://127.0.0.1" + addr)
	}
	return ensureV1Suffix("http://" + addr)
}

// ensureV1Suffix ensures the BaseURL ends with /v1 if needed.
func ensureV1Suffix(baseURL string) string {
	baseURL = strings.TrimRight(strings.TrimSpace(baseURL), "/")
	if baseURL == "" || strings.HasSuffix(baseURL, "/v1") {
		return baseURL
	}
	return baseURL + "/v1"
}
