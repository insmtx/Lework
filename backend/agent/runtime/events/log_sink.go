package events

import (
	"context"
	"encoding/json"

	"github.com/insmtx/Leros/backend/agent"
	"github.com/ygpkg/yg-go/logs"
)

type logSink struct{}

// NewLogSink 返回一个将运行事件写入调试日志的接收器。
func NewLogSink() Sink {
	return logSink{}
}

func (logSink) Emit(ctx context.Context, event *agent.Event) error {
	if event == nil {
		return nil
	}

	encoded, err := json.Marshal(event)
	if err != nil {
		logs.DebugContextf(ctx, "runtime event: type=%s run_id=%s seq=%d marshal_error=%v", event.Type, event.RunID, event.Seq, err)
		return nil
	}

	logs.DebugContextf(ctx, "runtime event: %s", string(encoded))
	return nil
}
