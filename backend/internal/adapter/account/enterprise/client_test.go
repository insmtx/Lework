//go:build enterprise

package enterprise

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/insmtx/Leros/backend/config"
	"github.com/insmtx/Leros/backend/internal/adapter/account"
	localauth "github.com/insmtx/Leros/backend/internal/api/auth"
)

func TestUploadFileByMultipart_Success(t *testing.T) {
	var receivedPurpose, receivedFilename string
	var receivedBody []byte

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			t.Errorf("expected POST, got %s", r.Method)
		}
		if r.URL.Path != "/v5/account.UploadFile" {
			t.Errorf("expected /v5/account.UploadFile, got %s", r.URL.Path)
		}
		if r.Header.Get("Authorization") != "Bearer test-token" {
			t.Errorf("expected Bearer test-token, got %s", r.Header.Get("Authorization"))
		}

		contentType := r.Header.Get("Content-Type")
		if !strings.HasPrefix(contentType, "multipart/form-data") {
			t.Errorf("expected multipart/form-data, got %s", contentType)
		}

		if err := r.ParseMultipartForm(10 << 20); err != nil {
			t.Fatal(err)
		}
		receivedPurpose = r.FormValue("purpose")
		file, header, err := r.FormFile("file")
		if err != nil {
			t.Fatal(err)
		}
		defer file.Close()
		receivedFilename = header.Filename
		receivedBody, _ = io.ReadAll(file)

		resp := iamUploadFileResponse{
			Code:    0,
			Message: "success",
			Response: iamUploadFileResponseBody{
				FileID:      1,
				URL:         "https://iam.example.com/avatars/1.png",
				Filename:    "avatar.png",
				Size:        1024,
				ContentType: "image/png",
				FileExt:     ".png",
				StoragePath: "cu-image/20260721/1-abc.png",
			},
		}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(resp)
	}))
	defer srv.Close()

	client := newIAMClient(&config.IAMConfig{BaseURL: srv.URL, DomainName: "test"}, "test")

	ctx := localauth.WithBearerToken(context.Background(), "test-token")

	fileData := bytes.NewReader([]byte("fake-image-data"))
	url, err := client.UploadFileByMultipart(ctx, "cu-image", "avatar.png", fileData, 14)
	if err != nil {
		t.Fatalf("UploadFileByMultipart failed: %v", err)
	}

	if url != "https://iam.example.com/avatars/1.png" {
		t.Errorf("expected url https://iam.example.com/avatars/1.png, got %s", url)
	}
	if receivedPurpose != "cu-image" {
		t.Errorf("expected purpose cu-image, got %s", receivedPurpose)
	}
	if receivedFilename != "avatar.png" {
		t.Errorf("expected filename avatar.png, got %s", receivedFilename)
	}
	if string(receivedBody) != "fake-image-data" {
		t.Errorf("expected body fake-image-data, got %s", string(receivedBody))
	}
}

func TestCreateAPIKeyUsesCurrentBearerAndExpectedEnvelope(t *testing.T) {
	var received createAPIKeyRequest
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v5/account.CreateAPIKey" {
			t.Errorf("path = %q", r.URL.Path)
		}
		if r.Header.Get("Authorization") != "Bearer current-user-token" {
			t.Errorf("authorization = %q", r.Header.Get("Authorization"))
		}
		var envelope struct {
			Cmd     string              `json:"cmd"`
			Request createAPIKeyRequest `json:"request"`
		}
		if err := json.NewDecoder(r.Body).Decode(&envelope); err != nil {
			t.Fatal(err)
		}
		if envelope.Cmd != "account.CreateAPIKey" {
			t.Errorf("cmd = %q", envelope.Cmd)
		}
		received = envelope.Request
		_ = json.NewEncoder(w).Encode(iamResponse{
			Code:     0,
			Response: json.RawMessage(`{"api_key":"yg-created-key","id":12}`),
		})
	}))
	defer srv.Close()

	client := newIAMClient(&config.IAMConfig{BaseURL: srv.URL}, "test")
	ctx := localauth.WithBearerToken(context.Background(), "current-user-token")
	result, err := client.CreateAPIKey(ctx, account.CreateAPIKeyInput{
		Name:         "SingerOS CoreKG MCP",
		Purpose:      "mcp_connector",
		ResourceType: "mcp",
		ResourceID:   0,
		ExpireHours:  0,
	})
	if err != nil {
		t.Fatalf("CreateAPIKey() error = %v", err)
	}
	if result.APIKey != "yg-created-key" || result.ID != 12 {
		t.Fatalf("result = %#v", result)
	}
	if received.Name != "SingerOS CoreKG MCP" ||
		received.Purpose != "mcp_connector" ||
		received.ResourceType != "mcp" ||
		received.ResourceID != 0 ||
		received.ExpireHours != 0 {
		t.Fatalf("request = %#v", received)
	}
}

func TestUploadFileByMultipart_NoToken(t *testing.T) {
	client := newIAMClient(&config.IAMConfig{BaseURL: "http://localhost", DomainName: "test"}, "test")

	_, err := client.UploadFileByMultipart(context.Background(), "cu-image", "avatar.png", bytes.NewReader(nil), 0)
	if err == nil {
		t.Fatal("expected error for missing token, got nil")
	}
	if !strings.Contains(err.Error(), "auth token not found") {
		t.Errorf("expected auth token not found error, got %v", err)
	}
}

func TestUploadFileByMultipart_IAMError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		resp := iamUploadFileResponse{
			Code:    1001,
			Message: "invalid purpose",
		}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(resp)
	}))
	defer srv.Close()

	client := newIAMClient(&config.IAMConfig{BaseURL: srv.URL, DomainName: "test"}, "test")

	ctx := localauth.WithBearerToken(context.Background(), "test-token")
	_, err := client.UploadFileByMultipart(ctx, "invalid-purpose", "avatar.png", bytes.NewReader([]byte("data")), 4)
	if err == nil {
		t.Fatal("expected error for IAM error response, got nil")
	}
	var iamErr *iamError
	if !errors.As(err, &iamErr) {
		t.Errorf("expected iamError, got %T", err)
	}
	if iamErr.Code != 1001 {
		t.Errorf("expected code 1001, got %d", iamErr.Code)
	}
}
