package service

import (
	"context"
	"errors"
	"testing"

	"github.com/insmtx/Leros/backend/internal/api/auth"
	"github.com/insmtx/Leros/backend/types"
)

func TestAutomationCallerDelegatesWorkerToTargetUserIDWithoutMembershipLookup(t *testing.T) {
	worker := &types.Caller{OrgID: 7, WorkerID: 9, Kind: types.CallerKindWorker, State: types.AuthStateSucc}
	ctx := WithAutomationTargetUserID(auth.WithContext(context.Background(), worker, nil), 42)
	caller, err := (&automationService{}).automationCaller(ctx)
	if err != nil {
		t.Fatalf("automationCaller() error = %v", err)
	}
	if caller.Uin != 42 || caller.OrgID != 7 || caller.Kind != types.CallerKindUser || caller.WorkerID != 0 {
		t.Fatalf("delegated caller = %#v", caller)
	}
}

func TestAutomationCallerRejectsWorkerWithoutTargetUserID(t *testing.T) {
	worker := &types.Caller{OrgID: 7, WorkerID: 9, Kind: types.CallerKindWorker, State: types.AuthStateSucc}
	base := auth.WithContext(context.Background(), worker, nil)

	if _, err := (&automationService{}).automationCaller(base); !errors.Is(err, ErrAutomationTargetUserIDRequired) {
		t.Fatalf("missing target error = %v, want %v", err, ErrAutomationTargetUserIDRequired)
	}
	ctx := WithAutomationTargetUserID(base, 99)
	if caller, err := (&automationService{}).automationCaller(ctx); err != nil || caller.Uin != 99 {
		t.Fatalf("target outside stored membership should still delegate, caller=%#v err=%v", caller, err)
	}
}

func TestAutomationCallerDoesNotAllowUserToSwitchUserID(t *testing.T) {
	user := &types.Caller{Uin: 7, OrgID: 7, Kind: types.CallerKindUser, State: types.AuthStateSucc}
	ctx := WithAutomationTargetUserID(auth.WithContext(context.Background(), user, nil), 42)
	if _, err := (&automationService{}).automationCaller(ctx); !errors.Is(err, ErrAutomationForbidden) {
		t.Fatalf("user switch error = %v, want %v", err, ErrAutomationForbidden)
	}
}
