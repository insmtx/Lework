package middleware

import (
	"context"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/dgrijalva/jwt-go"
	"github.com/gin-gonic/gin"
	localauth "github.com/insmtx/Leros/backend/internal/api/auth"
	"github.com/insmtx/Leros/backend/internal/infra/db"
	"github.com/insmtx/Leros/backend/types"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
)

const testJWTSecret = "test-secret-key"

func setupTestDB(t *testing.T) *gorm.DB {
	t.Helper()
	database, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	if err != nil {
		t.Fatalf("failed to open test database: %v", err)
	}
	if err := database.AutoMigrate(&types.UserOrg{}); err != nil {
		t.Fatalf("failed to migrate test database: %v", err)
	}
	return database
}

func generateTestUserJWT(uin uint) (string, error) {
	token, _, err := localauth.GenerateUserToken(localauth.UserClaims{
		Uin: uin,
	}, testJWTSecret, time.Hour)
	return token, err
}

func generateRawUserJWT(claims localauth.UserClaims) (string, error) {
	now := time.Now()
	claims.StandardClaims = jwt.StandardClaims{
		Subject:   "test-user",
		Issuer:    localauth.UserTokenIssuer,
		Audience:  localauth.UserTokenAudience,
		IssuedAt:  now.Unix(),
		ExpiresAt: now.Add(time.Hour).Unix(),
	}
	token := jwt.NewWithClaims(jwt.SigningMethodHS256, &claims)
	return token.SignedString([]byte(testJWTSecret))
}

func setupTestContext() (*gin.Context, *httptest.ResponseRecorder) {
	w := httptest.NewRecorder()
	ctx, _ := gin.CreateTestContext(w)
	return ctx, w
}

func TestCallerMiddleware_NoAuthHeader(t *testing.T) {
	database := setupTestDB(t)
	ctx, _ := setupTestContext()
	ctx.Request = httptest.NewRequest("GET", "/", nil)

	middleware := CallerMiddleware(testJWTSecret, database)
	middleware(ctx)

	caller, _ := localauth.FromGinContext(ctx)
	if caller == nil {
		t.Fatal("caller should not be nil")
	}
	if caller.Uin != 0 {
		t.Errorf("expected Uin 0, got %d", caller.Uin)
	}
	if caller.State != types.AuthStateNil {
		t.Errorf("expected State AuthStateNil, got %v", caller.State)
	}
}

func TestCallerMiddleware_EmptyAuthHeader(t *testing.T) {
	database := setupTestDB(t)
	ctx, _ := setupTestContext()
	req := httptest.NewRequest("GET", "/", nil)
	req.Header.Set("Authorization", "")
	ctx.Request = req

	middleware := CallerMiddleware(testJWTSecret, database)
	middleware(ctx)

	caller, _ := localauth.FromGinContext(ctx)
	if caller == nil {
		t.Fatal("caller should not be nil")
	}
	if caller.State != types.AuthStateNil {
		t.Errorf("expected State AuthStateNil, got %v", caller.State)
	}
}

func TestCallerMiddleware_InvalidToken(t *testing.T) {
	database := setupTestDB(t)
	ctx, _ := setupTestContext()
	req := httptest.NewRequest("GET", "/", nil)
	req.Header.Set("Authorization", "Bearer invalid-token")
	ctx.Request = req

	middleware := CallerMiddleware(testJWTSecret, database)
	middleware(ctx)

	caller, _ := localauth.FromGinContext(ctx)
	if caller == nil {
		t.Fatal("caller should not be nil")
	}
	if caller.State != types.AuthStateFailed {
		t.Errorf("expected State AuthStateFailed, got %v", caller.State)
	}
}

func TestCallerMiddleware_ValidV2UserToken(t *testing.T) {
	testUserID := uint(8)
	testUin := uint(12345)
	testOrgID := uint(2)
	database := setupTestDB(t)
	userOrg := &types.UserOrg{
		Uin:       testUin,
		UserID:    testUserID,
		OrgID:     testOrgID,
		IsDefault: true,
	}
	if err := db.CreateUserOrg(context.Background(), database, userOrg); err != nil {
		t.Fatalf("failed to create user org: %v", err)
	}

	token, err := generateTestUserJWT(testUin)
	if err != nil {
		t.Fatalf("failed to generate token: %v", err)
	}

	ctx, _ := setupTestContext()
	req := httptest.NewRequest("GET", "/", nil)
	req.Header.Set("Authorization", "Bearer "+token)
	ctx.Request = req

	middleware := CallerMiddleware(testJWTSecret, database)
	middleware(ctx)

	caller, _ := localauth.FromGinContext(ctx)
	if caller == nil {
		t.Fatal("caller should not be nil")
	}
	if caller.Uin != testUin {
		t.Errorf("expected Uin %d, got %d", testUin, caller.Uin)
	}
	if caller.OrgID != testOrgID {
		t.Errorf("expected OrgID %d, got %d", testOrgID, caller.OrgID)
	}
	if caller.State != types.AuthStateSucc {
		t.Errorf("expected State AuthStateSucc, got %v", caller.State)
	}
	if caller.Kind != types.CallerKindUser {
		t.Errorf("expected Kind user, got %q", caller.Kind)
	}
}

func TestCallerMiddleware_WorkerToken(t *testing.T) {
	database := setupTestDB(t)
	token, _, err := localauth.GenerateWorkerToken(3, 7, testJWTSecret, time.Hour)
	if err != nil {
		t.Fatalf("failed to generate worker token: %v", err)
	}

	ctx, _ := setupTestContext()
	req := httptest.NewRequest("GET", "/", nil)
	req.Header.Set("Authorization", "Bearer "+token)
	ctx.Request = req

	middleware := CallerMiddleware(testJWTSecret, database)
	middleware(ctx)

	caller, _ := localauth.FromGinContext(ctx)
	if caller == nil {
		t.Fatal("caller should not be nil")
	}
	if caller.Kind != types.CallerKindWorker {
		t.Fatalf("expected Kind worker, got %q", caller.Kind)
	}
	if caller.OrgID != 3 || caller.WorkerID != 7 {
		t.Fatalf("caller identity = org %d worker %d, want org 3 worker 7", caller.OrgID, caller.WorkerID)
	}
	if caller.Uin != 0 {
		t.Fatalf("worker caller Uin = %d, want 0", caller.Uin)
	}
	if caller.State != types.AuthStateSucc {
		t.Fatalf("worker caller state = %v, want success", caller.State)
	}
}

func TestCallerMiddleware_RequestIDAndTraceID(t *testing.T) {
	database := setupTestDB(t)
	ctx, _ := setupTestContext()
	req := httptest.NewRequest("GET", "/", nil)
	req.Header.Set(headerKeyRequestID, "test-request-id")
	req.Header.Set(headerKeyTraceID, "test-trace-id")
	ctx.Request = req

	middleware := CallerMiddleware(testJWTSecret, database)
	middleware(ctx)

	_, trace := localauth.FromGinContext(ctx)
	if trace == nil {
		t.Fatal("trace should not be nil")
	}
	if trace.RequestID != "test-request-id" {
		t.Errorf("expected RequestID test-request-id, got %s", trace.RequestID)
	}
	if trace.TraceID != "test-trace-id" {
		t.Errorf("expected TraceID test-trace-id, got %s", trace.TraceID)
	}
}

func TestCallerMiddleware_AutoGenerateRequestID(t *testing.T) {
	database := setupTestDB(t)
	ctx, _ := setupTestContext()
	ctx.Request = httptest.NewRequest("GET", "/", nil)

	middleware := CallerMiddleware(testJWTSecret, database)
	middleware(ctx)

	_, trace := localauth.FromGinContext(ctx)
	if trace == nil {
		t.Fatal("trace should not be nil")
	}
	if trace.RequestID == "" {
		t.Error("RequestID should be auto-generated when not provided")
	}
	if trace.TraceID != trace.RequestID {
		t.Error("TraceID should equal RequestID when not provided")
	}
}

func TestExtractTokenFromHeader(t *testing.T) {
	tests := []struct {
		name       string
		authHeader string
		want       string
	}{
		{
			name:       "Bearer token",
			authHeader: "Bearer my-token",
			want:       "my-token",
		},
		{
			name:       "Bearer token with spaces",
			authHeader: "Bearer   my-token  ",
			want:       "my-token",
		},
		{
			name:       "Direct token",
			authHeader: "my-direct-token",
			want:       "my-direct-token",
		},
		{
			name:       "Empty header",
			authHeader: "",
			want:       "",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := extractTokenFromHeader(tt.authHeader)
			if got != tt.want {
				t.Errorf("extractTokenFromHeader() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestCallerMiddleware_WrongSecret(t *testing.T) {
	database := setupTestDB(t)
	token, _ := generateTestUserJWT(12345)

	ctx, _ := setupTestContext()
	req := httptest.NewRequest("GET", "/", nil)
	req.Header.Set("Authorization", "Bearer "+token)
	ctx.Request = req

	middleware := CallerMiddleware("wrong-secret", database)
	middleware(ctx)

	caller, _ := localauth.FromGinContext(ctx)
	if caller == nil {
		t.Fatal("caller should not be nil")
	}
	if caller.State != types.AuthStateFailed {
		t.Errorf("expected State AuthStateFailed with wrong secret, got %v", caller.State)
	}
}

func TestCallerMiddleware_ZeroUin(t *testing.T) {
	database := setupTestDB(t)
	token, err := generateRawUserJWT(localauth.UserClaims{
		Uin: 0,
	})
	if err != nil {
		t.Fatalf("failed to generate raw token: %v", err)
	}

	ctx, _ := setupTestContext()
	req := httptest.NewRequest("GET", "/", nil)
	req.Header.Set("Authorization", "Bearer "+token)
	ctx.Request = req

	middleware := CallerMiddleware(testJWTSecret, database)
	middleware(ctx)

	caller, _ := localauth.FromGinContext(ctx)
	if caller == nil {
		t.Fatal("caller should not be nil")
	}
	if caller.State != types.AuthStateFailed {
		t.Errorf("expected State AuthStateFailed for zero uin, got %v", caller.State)
	}
}
