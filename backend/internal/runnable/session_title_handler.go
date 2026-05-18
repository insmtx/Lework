package runnable

import (
	"context"
	"encoding/json"

	"github.com/nats-io/nats.go"

	"github.com/insmtx/Leros/backend/internal/api/contract"
	eventbus "github.com/insmtx/Leros/backend/internal/infra/mq"
	"github.com/insmtx/Leros/backend/pkg/dm"
	"github.com/ygpkg/yg-go/logs"
)

// StartSessionTitleHandler subscribes to session title update requests and dispatches to the service.
func StartSessionTitleHandler(ctx context.Context, service contract.SessionService, eb eventbus.EventBus) {
	topic := dm.SessionMessageRequestWildcardSubject()
	logs.InfoContextf(ctx, "starting session title handler runnable: %s", topic)

	Run(ctx, "session_title_handler", func(ctx context.Context) {
		if err := eb.Subscribe(ctx, topic, func(msg *nats.Msg) {
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
		return
	}
	if err := service.HandleSessionTitleRequest(ctx, &req); err != nil {
		logs.WarnContextf(ctx, "handle session title request: %v", err)
	}
}
