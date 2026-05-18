package runnable

import (
	"context"
	"encoding/json"

	"github.com/nats-io/nats.go"

	"github.com/insmtx/Leros/backend/internal/api/auth"
	"github.com/insmtx/Leros/backend/internal/api/contract"
	eventbus "github.com/insmtx/Leros/backend/internal/infra/mq"
	"github.com/insmtx/Leros/backend/pkg/dm"
	"github.com/ygpkg/yg-go/logs"
)

// StartSessionTitleHandler subscribes to session title update requests and dispatches to the service.
func StartSessionTitleHandler(ctx context.Context, service contract.SessionService, eb eventbus.EventBus) {
	ctx = logs.WithContextFields(ctx, "runnable", "session_title_handler")
	topic := dm.SessionMessageRequestWildcardSubject()
	logs.InfoContextf(ctx, "starting session title handler runnable: %s", topic)

	Run(ctx, "session_title_handler", func(ctx context.Context) {
		if err := eb.SubscribeFrom(ctx, topic, 0, func(msg *nats.Msg) {
			handleSessionTitleRequest(ctx, service, msg)
		}); err != nil {
			logs.ErrorContextf(ctx, "subscribe to %s failed: %v", topic, err)
		}
	})
}

func handleSessionTitleRequest(ctx context.Context, service contract.SessionService, msg *nats.Msg) {
	var req contract.SessionTitleRequest
	if err := json.Unmarshal(msg.Data, &req); err != nil {
		logs.WarnContextf(ctx, "unmarshal session title request: %v", err)
		return
	}
	if req.SessionID == "" {
		logs.WarnContextf(ctx, "session title request missing session ID")
		return
	}
	ctx = auth.WithContext(ctx, auth.SystemIdentity(), nil)
	if err := service.HandleSessionTitleRequest(ctx, &req); err != nil {
		logs.WarnContextf(ctx, "handle session title request: %v", err)
	}
}
