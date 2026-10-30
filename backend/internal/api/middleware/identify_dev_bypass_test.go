package middleware

import (
	"net/http/httptest"
	"testing"

	"github.com/gin-gonic/gin"

	"github.com/insmtx/Leros/backend/types"
)

func newDevBypassTestContext(t *testing.T, remoteAddr string) *gin.Context {
	t.Helper()
	ctx, _ := gin.CreateTestContext(httptest.NewRecorder())
	ctx.Request = httptest.NewRequest("GET", "/v1/ListUser", nil)
	ctx.Request.RemoteAddr = remoteAddr
	return ctx
}

func TestParseCallerDevBypassRequiresExplicitFlagLoopbackAndNonProduction(t *testing.T) {
	gin.SetMode(gin.TestMode)

	cases := []struct {
		name       string
		dev        string
		allow      string
		env        string
		remote     string
		wantBypass bool
	}{
		{
			name:       "LEROS_DEV alone no longer bypasses auth",
			dev:        "true",
			remote:     "127.0.0.1:1234",
			wantBypass: false,
		},
		{
			name:       "explicit flag from loopback bypasses auth",
			allow:      "true",
			remote:     "127.0.0.1:1234",
			wantBypass: true,
		},
		{
			name:       "explicit flag from IPv6 loopback bypasses auth",
			allow:      "true",
			remote:     "[::1]:1234",
			wantBypass: true,
		},
		{
			name:       "explicit flag from non-loopback is rejected",
			allow:      "true",
			remote:     "192.0.2.10:1234",
			wantBypass: false,
		},
		{
			name:       "production always rejects the bypass",
			allow:      "true",
			env:        "production",
			remote:     "127.0.0.1:1234",
			wantBypass: false,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Setenv("LEROS_DEV", tc.dev)
			t.Setenv("LEROS_DEV_ALLOW_UNAUTH", tc.allow)
			t.Setenv("LEROS_ENV", tc.env)

			caller, _ := parseCallerFromRequest(newDevBypassTestContext(t, tc.remote), nil)
			if caller == nil {
				t.Fatal("parseCallerFromRequest returned nil caller")
			}

			bypass := caller.State == types.AuthStateSucc && caller.Uin == 1 && caller.OrgID == 1
			if bypass != tc.wantBypass {
				t.Fatalf("bypass = %v, want %v (caller=%+v)", bypass, tc.wantBypass, caller)
			}
		})
	}
}
