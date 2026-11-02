package github

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"io"
	"net/http"

	"github.com/gin-gonic/gin"
	"github.com/google/go-github/v78/github"
	"github.com/ygpkg/yg-go/logs"

	"github.com/insmtx/Lework/backend/pkg/event"
)

const (
	signatureHeader  = "X-Hub-Signature-256"
	signaturePrefix  = "sha256="
	httpOK           = http.StatusOK
	httpBadRequest   = http.StatusBadRequest
	httpUnauthorized = http.StatusUnauthorized
)

// handleWebhook processes incoming GitHub webhook requests.
func (c *Connector) handleWebhook(ctx *gin.Context) {
	w := ctx.Writer
	r := ctx.Request

	payload, err := io.ReadAll(r.Body)
	if err != nil {
		logs.ErrorContextf(ctx, "Failed to read GitHub webhook payload: %v", err)
		http.Error(w, "bad request", httpBadRequest)
		return
	}

	// 只有配置了webhook_secret时才进行签名验证
	if c.cfg.WebhookSecret != "" {
		if !c.validateSignature(r, payload) {
			logs.WarnContextf(ctx, "Invalid GitHub webhook signature for request: %s %s", r.Method, r.URL.Path)
			http.Error(w, "invalid signature", httpUnauthorized)
			return
		}
	} else {
		logs.WarnContext(ctx, "GitHub webhook_secret not configured - skipping signature verification")
	}

	eventType := github.WebHookType(r)
	event, err := github.ParseWebHook(eventType, payload)
	if err != nil {
		logs.ErrorContextf(ctx, "Failed to parse GitHub webhook event (type: %s): %v", eventType, err)
		http.Error(w, "parse error", httpBadRequest)
		return
	}

	interactionEvent := c.convertEvent(eventType, event)
	if interactionEvent == nil {
		w.WriteHeader(httpOK)
		return
	}
	topicName := c.determineTopic(eventType)

	c.publisher.Publish(ctx, topicName, interactionEvent)
	w.WriteHeader(httpOK)
}

// validateSignature verifies the GitHub webhook signature.
func (c *Connector) validateSignature(r *http.Request, payload []byte) bool {
	if c.cfg.SkipWebhookVerify {
		logs.Warn("Webhook signature verification skipped - NOT FOR PRODUCTION USE!")
		return true
	}

	signature := r.Header.Get(signatureHeader)
	if signature == "" {
		return false
	}

	mac := hmac.New(sha256.New, []byte(c.cfg.WebhookSecret))
	mac.Write(payload)
	expected := signaturePrefix + hex.EncodeToString(mac.Sum(nil))

	return hmac.Equal([]byte(signature), []byte(expected))
}

// determineTopic maps GitHub event types to Lework topics.
func (c *Connector) determineTopic(eventType string) string {
	switch eventType {
	case "issue_comment":
		return event.TopicGithubIssueComment
	case "pull_request":
		return event.TopicGithubPullRequest
	case "push":
		return event.TopicGithubPush
	default:
		return event.TopicGithubIssueComment
	}
}
