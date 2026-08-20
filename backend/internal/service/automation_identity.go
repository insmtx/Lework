package service

import (
	"context"
	"errors"

	"github.com/insmtx/Leros/backend/types"
	"github.com/ygpkg/yg-go/logs"
)

var (
	// ErrAutomationTargetUserIDRequired indicates that a worker request did not
	// identify the user whose automations should be operated on.
	ErrAutomationTargetUserIDRequired = errors.New("automation target user id is required")
)

type automationTargetUserIDContextKey struct{}

// WithAutomationTargetUserID binds the target user for an Automation request.
// Worker authentication supplies the organization scope; the target user ID
// selects the owner's automation records within that scope.
func WithAutomationTargetUserID(ctx context.Context, userID uint) context.Context {
	return context.WithValue(ctx, automationTargetUserIDContextKey{}, userID)
}

func automationTargetUserID(ctx context.Context) (uint, bool) {
	if ctx == nil {
		return 0, false
	}
	userID, ok := ctx.Value(automationTargetUserIDContextKey{}).(uint)
	return userID, ok
}

// automationCaller resolves the effective user for an Automation operation.
// Normal user requests remain self-scoped. Worker requests may delegate to a
// target user ID supplied by the Agent CLI.
func (s *automationService) automationCaller(ctx context.Context) (*types.Caller, error) {
	caller, err := requireCallerOrg(ctx)
	if err != nil {
		return nil, err
	}

	targetUserID, hasTarget := automationTargetUserID(ctx)
	if !hasTarget {
		if caller.Kind == types.CallerKindWorker {
			return nil, ErrAutomationTargetUserIDRequired
		}
		return caller, nil
	}
	if targetUserID == 0 {
		return nil, ErrAutomationTargetUserIDRequired
	}

	if caller.Kind == types.CallerKindUser {
		if caller.Uin != targetUserID {
			return nil, ErrAutomationForbidden
		}
		return caller, nil
	}
	if caller.Kind != types.CallerKindWorker {
		return nil, ErrAutomationForbidden
	}

	delegated := *caller
	delegated.Uin = targetUserID
	delegated.WorkerID = 0
	delegated.Kind = types.CallerKindUser
	logs.InfoContextw(ctx, "automation request delegated to target user",
		"org_id", caller.OrgID, "worker_id", caller.WorkerID, "target_user_id", targetUserID)
	return &delegated, nil
}

// automationCallerFromContext is kept separate from the handler so all
// Automation operations, including execution queries, share the same scope.
func (s *automationService) automationCallerFromContext(ctx context.Context) (*types.Caller, error) {
	return s.automationCaller(ctx)
}
