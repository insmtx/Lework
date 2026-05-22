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
	ygauth "github.com/ygpkg/yg-go/apis/runtime/auth"
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

func setupTestUserOrg(t *testing.T, database *gorm.DB, uin uint, orgID uint) {
	t.Helper()
	userOrg := &types.UserOrg{
		Uin:       uin,
		OrgID:     orgID,
		IsDefault: true,
	}
	if err := db.CreateUserOrg(context.Background(), database, userOrg); err != nil {
		t.Fatalf("failed to create user org: %v", err)
	}
}

func generateTestJWT(uin uint, issuer string) (string, error) {
	claims := &ygauth.UserClaims{
		Uin:       uin,
		Issuer:    issuer,
		IssuedAt:  time.Now().Unix(),
		ExpiresAt: time.Now().Add(time.Hour).Unix(),
	}
	token := jwt.NewWithClaims(jwt.SigningMethodHS256, claims)
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

func TestCallerMiddleware_ValidToken(t *testing.T) {
	testUin := uint(12345)
	testOrgID := uint(1)
	database := setupTestDB(t)
	setupTestUserOrg(t, database, testUin, testOrgID)

	token, err := generateTestJWT(testUin, "test-issuer")
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
	token, _ := generateTestJWT(12345, "test-issuer")

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
	token, _ := generateTestJWT(0, "test-issuer")

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
		t.Errorf("expected State AuthStateFailed for zero Uin, got %v", caller.State)
	}
}
