package llm

import (
	"context"
	"testing"

	"github.com/insmtx/Leros/backend/internal/api/auth"
	"github.com/insmtx/Leros/backend/types"
)

// stubManager 实现 Manager 空桩，供 ContractAdapter 测试使用。
type stubManager struct {
	Manager
}

// stubOrgCreator 实现 orgCreatorChecker，IsOrgCreator 由字段注入。
type stubOrgCreator struct {
	creator bool
}

func (s *stubOrgCreator) IsOrgCreator(ctx context.Context, orgID, uin uint) (bool, error) {
	return s.creator, nil
}

func TestContractAdapter_requireOrgCreator(t *testing.T) {
	tests := []struct {
		name    string
		creator bool
		caller  *types.Caller
		wantErr bool
	}{
		{"creator passes", true, &types.Caller{Uin: 1, OrgID: 100, State: types.AuthStateSucc}, false},
		{"non-creator denied", false, &types.Caller{Uin: 1, OrgID: 100, State: types.AuthStateSucc}, true},
		{"no caller denied", false, nil, true},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			a := &ContractAdapter{manager: &stubManager{}, orgCreatorCheck: &stubOrgCreator{creator: tc.creator}}
			ctx := context.Background()
			if tc.caller != nil {
				ctx = auth.WithContext(ctx, tc.caller, nil)
			}
			orgID := uint(100)
			if tc.caller == nil {
				orgID = 0
			}
			err := a.requireOrgCreator(ctx, orgID)
			if (err != nil) != tc.wantErr {
				t.Fatalf("requireOrgCreator error = %v, wantErr %v", err, tc.wantErr)
			}
		})
	}
}
