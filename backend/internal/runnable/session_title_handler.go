package runnable

import (
	"context"
	"encoding/json"

	"github.com/nats-io/nats.go"

	"github.com/insmtx/Leros/backend/internal/api/auth"
	"github.com/insmtx/Leros/backend/internal/api/contract"
	infradb "github.com/insmtx/Leros/backend/internal/infra/db"
	eventbus "github.com/insmtx/Leros/backend/internal/infra/mq"
	"github.com/insmtx/Leros/backend/pkg/dm"
	"github.com/insmtx/Leros/backend/types"
	"github.com/ygpkg/yg-go/logs"
	"gorm.io/gorm"
)

// StartSessionTitleHandler subscribes to session title update requests and dispatches to the service.
func StartSessionTitleHandler(ctx context.Context, service contract.SessionService, eb eventbus.EventBus, db *gorm.DB) {
	ctx = logs.WithContextFields(ctx, "runnable", "session_title_handler")
	topic := dm.SessionMessageRequestWildcardSubject()
	logs.InfoContextf(ctx, "starting session title handler runnable: %s", topic)

	Run(ctx, "session_title_handler", func(ctx context.Context) {
		if err := eb.Subscribe(ctx, topic, dm.SessionTitleConsumer(), func(msg *nats.Msg) {
			handleSessionTitleRequest(ctx, service, msg, db)
		}); err != nil {
			logs.ErrorContextf(ctx, "subscribe to %s failed: %v", topic, err)
		}
	})
}

func handleSessionTitleRequest(ctx context.Context, service contract.SessionService, msg *nats.Msg, db *gorm.DB) {
	var message types.SessionMessage
	if err := json.Unmarshal(msg.Data, &message); err != nil {
		logs.WarnContextf(ctx, "unmarshal session message: %v", err)
		return
	}
	if message.ID == 0 {
		logs.WarnContextf(ctx, "session message missing ID")
		return
	}
	session, err := infradb.GetSessionByID(ctx, db, message.SessionID)
	if err != nil || session == nil {
		logs.WarnContextf(ctx, "find session by id %d: %v", message.SessionID, err)
		return
	}
	ctx = auth.WithContext(ctx, auth.SystemIdentity(), nil)
	if err := service.HandleSessionTitleRequest(ctx, session.PublicID); err != nil {
		logs.WarnContextf(ctx, "handle session title request: %v", err)
	}
}
