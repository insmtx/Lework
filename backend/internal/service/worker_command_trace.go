package service

import (
	"context"

	"github.com/insmtx/Lework/backend/internal/api/auth"
	"github.com/insmtx/Lework/backend/pkg/messaging"
)

func withRequestTrace(ctx context.Context, cmd messaging.WorkerCommand) messaging.WorkerCommand {
	_, trace := auth.FromContext(ctx)
	if trace == nil {
		return cmd
	}
	cmd.Trace.ReqID = trace.RequestID
	return cmd
}
