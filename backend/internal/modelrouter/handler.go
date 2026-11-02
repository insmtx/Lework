package modelrouter

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"os"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/bytedance/sonic"
	"github.com/gin-gonic/gin"

	"github.com/insmtx/Lework/backend/internal/llm"
	"github.com/insmtx/Lework/backend/pkg/cache"
	"github.com/insmtx/Lework/backend/pkg/llmprotocol"
)

const defaultBizTTL = 1 * time.Hour

// ━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━
// ModelStore — minimal in-handler model config resolution
// ━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━

// ModelStore holds UpstreamConfig and business ID entries keyed by model name.
// It is safe for concurrent use.
type ModelStore struct {
	configs  map[string]*UpstreamConfig
	bizCache *cache.Cache[BusinessKeys]
	caller   llm.Caller
	orgID    uint
	mu       sync.RWMutex
}

// BusinessKeys holds business primary key IDs for a single run.
type BusinessKeys struct {
	ProjectID   uint
	SessionID   uint
	MessageID   uint
	AssistantID uint
	Uin         uint
}

// NewModelStore creates an isolated model routing store.
func NewModelStore() *ModelStore {
	return &ModelStore{
		configs:  make(map[string]*UpstreamConfig),
		bizCache: cache.New[BusinessKeys](defaultBizTTL),
	}
}

// SetCaller sets the llm.Caller used for upstream LLM calls.
func (s *ModelStore) SetCaller(caller llm.Caller) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.caller = caller
}

// SetOrgID sets the org ID used for call recording.
func (s *ModelStore) SetOrgID(orgID uint) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.orgID = orgID
}

// Put registers an upstream configuration for a model.
// If cfg.Protocol is not explicitly set and cfg.Provider is non-empty,
// the protocol is inferred from the provider.
func (s *ModelStore) Put(cfg UpstreamConfig) {
	if cfg.Protocol == "" && cfg.Provider != "" {
		cfg.Protocol = protocolForProvider(cfg.Provider)
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.configs == nil {
		s.configs = make(map[string]*UpstreamConfig)
	}
	cp := cfg
	s.configs[cfg.ModelName] = &cp
}

// Resolve returns the UpstreamConfig for the given model name.
func (s *ModelStore) Resolve(model string) (*UpstreamConfig, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	cfg, ok := s.configs[model]
	if !ok {
		return nil, fmt.Errorf("modelrouter: no upstream config for model %q", model)
	}
	cp := *cfg
	return &cp, nil
}

// PutBiz stores business identifiers keyed by proxy model name (modelName:runID).
func (s *ModelStore) PutBiz(modelKey string, biz BusinessKeys) {
	if s.bizCache != nil {
		s.bizCache.Put(modelKey, biz)
	}
}

// GetBiz returns business identifiers for the given proxy model key, or nil.
func (s *ModelStore) GetBiz(modelKey string) *BusinessKeys {
	if s.bizCache == nil {
		return nil
	}
	return s.bizCache.Get(modelKey)
}

// RemoveBiz removes business identifiers for the given proxy model key.
func (s *ModelStore) RemoveBiz(modelKey string) {
	if s.bizCache != nil {
		s.bizCache.Remove(modelKey)
	}
}

// SplitProxyModel separates the proxy model name into the real model name and run ID.
// "gpt-4o:run_abc" → "gpt-4o", "run_abc". Returns original model and empty runID if no colon.
func SplitProxyModel(model string) (realModelName, runID string) {
	idx := strings.LastIndex(model, ":")
	if idx < 0 {
		return model, ""
	}
	return model[:idx], model[idx+1:]
}

func (s *ModelStore) getCaller() llm.Caller {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.caller
}

func (s *ModelStore) getOrgID() uint {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.orgID
}

// ━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━
// Route Registration
// ━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━

// RegisterRoutes registers model routing endpoints on the given Gin router.
// Each endpoint supports all entry protocols and transparently converts
// between protocols when upstream targets a different protocol.
func RegisterRoutes(r gin.IRouter, store *ModelStore) {
	if store == nil {
		return
	}
	// NOTE: caller (worker/router) already mounts under /v1/ prefix.
	// Register directly on the given router to avoid double-wrapping.
	r.POST("/chat/completions", handleModelRoute(store, llmprotocol.ProtocolOpenAIChat))
	r.POST("/messages", handleModelRoute(store, llmprotocol.ProtocolAnthropicMessages))
	r.POST("/responses", handleModelRoute(store, llmprotocol.ProtocolOpenAIResponses))
	// Gemini: use wildcard because Gin cannot handle ":model:generateContent" in one segment
	r.POST("/models/*modelAction", handleModelRoute(store, llmprotocol.ProtocolGemini))
}

// handleModelRoute returns a Gin handler that routes model requests through protocol conversion.
// Upstream LLM calls are delegated to llm.Caller for unified accounting.
func handleModelRoute(store *ModelStore, entryProtocol llmprotocol.Protocol) gin.HandlerFunc {
	return func(c *gin.Context) {
		body, err := io.ReadAll(c.Request.Body)
		if err != nil {
			c.JSON(http.StatusBadRequest, newEntryError(entryProtocol, "failed to read request body"))
			return
		}

		caller := store.getCaller()
		if caller == nil {
			c.JSON(http.StatusInternalServerError, newEntryError(entryProtocol, "llm caller not configured"))
			return
		}

		debugEnabled := os.Getenv("LEWORK_MODELROUTER_DEBUG") == "true"
		dl := NewDebugLogger(debugEnabled)
		defer dl.Close()

		dl.LogOriginalRequest(body)

		model := extractModelField(body)
		if model == "" && entryProtocol == llmprotocol.ProtocolGemini {
			model = extractGeminiModelFromPath(c.Param("modelAction"))
		}
		if model == "" {
			c.JSON(http.StatusBadRequest, newEntryError(entryProtocol, "model field is required"))
			return
		}

		cfg, err := store.Resolve(model)
		if err != nil {
			c.JSON(http.StatusBadRequest, newEntryError(entryProtocol, err.Error()))
			return
		}

		realModelName, _ := SplitProxyModel(model)
		if realModelName != "" {
			cfg.ModelName = realModelName
		}

		isStream := isStreamRequest(body)
		dl.LogRequestMeta(entryProtocol, cfg.Protocol, model, isStream)

		var raw map[string]interface{}
		if err := sonic.Unmarshal(body, &raw); err != nil {
			c.JSON(http.StatusBadRequest, newEntryError(entryProtocol, "invalid JSON request body"))
			return
		}

		entryAdapter, err := llmprotocol.GetAdapter(entryProtocol)
		if err != nil {
			c.JSON(http.StatusInternalServerError, newEntryError(entryProtocol, "entry protocol adapter not available"))
			return
		}

		ir, err := entryAdapter.DecodeRequest(raw)
		if err != nil {
			c.JSON(http.StatusBadRequest, newEntryError(entryProtocol, fmt.Sprintf("decode request: %v", err)))
			return
		}
		dl.LogIRDecoded(ir)

		upstreamProtocol := cfg.Protocol
		targetCaps := llmprotocol.CapabilitiesForProtocol(upstreamProtocol)
		normalizedIR, _, err := llmprotocol.NormalizeRequest(ir, targetCaps)
		if err != nil {
			c.JSON(http.StatusBadRequest, newEntryError(entryProtocol, fmt.Sprintf("request incompatible with target protocol: %v", err)))
			return
		}
		dl.LogIRNormalized(normalizedIR)

		normalizedIR.Model = cfg.ModelName

		upstreamAdapter, err := llmprotocol.GetAdapter(upstreamProtocol)
		if err != nil {
			c.JSON(http.StatusInternalServerError, newEntryError(entryProtocol, "upstream protocol adapter not available"))
			return
		}

		upstreamBody, err := upstreamAdapter.EncodeRequest(normalizedIR)
		if err != nil {
			c.JSON(http.StatusInternalServerError, newEntryError(entryProtocol, fmt.Sprintf("encode upstream request: %v", err)))
			return
		}

		upstreamBodyBytes, err := marshalJSON(upstreamBody)
		if err != nil {
			c.JSON(http.StatusInternalServerError, newEntryError(entryProtocol, "marshal upstream body failed"))
			return
		}
		dl.LogUpstreamRequest(upstreamBodyBytes)

		orgID := store.getOrgID()
		modelCfg := cfg.ToModelConfig()

		c.Request = c.Request.WithContext(injectBusinessIDs(c.Request.Context(), c))

		biz := store.GetBiz(model)
		if biz != nil {
			ctx := c.Request.Context()
			ctx = llm.WithCtxUint(ctx, llm.CtxProjectID, biz.ProjectID)
			ctx = llm.WithCtxUint(ctx, llm.CtxSessionID, biz.SessionID)
			ctx = llm.WithCtxUint(ctx, llm.CtxMessageID, biz.MessageID)
			ctx = llm.WithCtxUint(ctx, llm.CtxAssistantID, biz.AssistantID)
			ctx = llm.WithCtxUint(ctx, llm.CtxUin, biz.Uin)
			ctx = llm.WithCtxString(ctx, llm.CtxCallerType, llm.CallerTypeWorker)
			c.Request = c.Request.WithContext(ctx)
		}

		if isStream {
			handleStreamResponse(c, caller, orgID, modelCfg, upstreamBodyBytes, entryProtocol, upstreamProtocol, entryAdapter, upstreamAdapter, dl)
		} else {
			handleNonStreamResponse(c, caller, orgID, modelCfg, upstreamBodyBytes, entryProtocol, upstreamProtocol, entryAdapter, upstreamAdapter, dl)
		}
	}
}

// ━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━
// Request parsing helpers
// ━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━

func extractModelField(body []byte) string {
	var raw struct {
		Model string `json:"model"`
	}
	if err := sonic.Unmarshal(body, &raw); err != nil {
		return ""
	}
	return strings.TrimSpace(raw.Model)
}

// extractGeminiModelFromPath extracts the model name from a Gemini URL action parameter.
// e.g., "/gemini-2.0-flash:generateContent" -> "gemini-2.0-flash"
func extractGeminiModelFromPath(action string) string {
	action = strings.TrimPrefix(action, "/")
	colonIdx := strings.LastIndex(action, ":")
	if colonIdx < 0 {
		return ""
	}
	return action[:colonIdx]
}

func isStreamRequest(body []byte) bool {
	var raw struct {
		Stream bool `json:"stream"`
	}
	if err := sonic.Unmarshal(body, &raw); err != nil {
		return false
	}
	return raw.Stream
}

func marshalJSON(v interface{}) ([]byte, error) {
	return sonic.ConfigStd.Marshal(v)
}

// injectBusinessIDs 从 HTTP 请求头中提取业务 ID，注入到 context 中。
// 客户端通过 X-Lework-Project-Id / X-Lework-Session-Id / X-Lework-Message-Id /
// X-Lework-Assistant-Id / X-Lework-Uin 请求头传递业务 ID。
func injectBusinessIDs(ctx context.Context, c *gin.Context) context.Context {
	if v := parseHeaderUint(c, "X-Lework-Project-Id"); v > 0 {
		ctx = llm.WithCtxUint(ctx, llm.CtxProjectID, v)
	}
	if v := parseHeaderUint(c, "X-Lework-Session-Id"); v > 0 {
		ctx = llm.WithCtxUint(ctx, llm.CtxSessionID, v)
	}
	if v := parseHeaderUint(c, "X-Lework-Message-Id"); v > 0 {
		ctx = llm.WithCtxUint(ctx, llm.CtxMessageID, v)
	}
	if v := parseHeaderUint(c, "X-Lework-Assistant-Id"); v > 0 {
		ctx = llm.WithCtxUint(ctx, llm.CtxAssistantID, v)
	}
	if v := parseHeaderUint(c, "X-Lework-Uin"); v > 0 {
		ctx = llm.WithCtxUint(ctx, llm.CtxUin, v)
	}
	if clientIP := c.ClientIP(); clientIP != "" {
		ctx = llm.WithCtxString(ctx, llm.CtxClientIP, clientIP)
	}
	return ctx
}

func parseHeaderUint(c *gin.Context, key string) uint {
	val := strings.TrimSpace(c.GetHeader(key))
	if val == "" {
		return 0
	}
	n, err := strconv.ParseUint(val, 10, 64)
	if err != nil {
		return 0
	}
	return uint(n)
}
