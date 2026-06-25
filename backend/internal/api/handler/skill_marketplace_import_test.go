package handler

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/gin-gonic/gin"

	"github.com/insmtx/Leros/backend/internal/api/contract"
)

type skillMarketplaceImportTestService struct {
	importErr       error
	importGitHubErr error
}

func (s *skillMarketplaceImportTestService) SearchSkillMarketplace(context.Context, *contract.SearchSkillMarketplaceRequest) (*contract.SearchSkillMarketplaceResponse, error) {
	return nil, nil
}

func (s *skillMarketplaceImportTestService) DownloadBuiltinSkill(context.Context, string) (*contract.SkillPackageDownload, error) {
	return nil, nil
}

func (s *skillMarketplaceImportTestService) DownloadSkillPackage(context.Context, *contract.DownloadSkillRequest) (*contract.SkillPackageDownload, error) {
	return &contract.SkillPackageDownload{Reader: io.NopCloser(bytes.NewReader(nil)), FileName: "skill.zip"}, nil
}

func (s *skillMarketplaceImportTestService) InstallSkill(context.Context, *contract.InstallSkillRequest) (*contract.InstallSkillResponse, error) {
	return nil, nil
}

func (s *skillMarketplaceImportTestService) InstalledSkills(context.Context, *contract.InstalledSkillsRequest) (*contract.InstalledSkillsResponse, error) {
	return nil, nil
}

func (s *skillMarketplaceImportTestService) UninstallSkill(context.Context, *contract.UninstallSkillRequest) (*contract.UninstallSkillResponse, error) {
	return nil, nil
}

func (s *skillMarketplaceImportTestService) GetSkillDetail(context.Context, *contract.SkillDetailRequest) (*contract.SkillDetailResponse, error) {
	return nil, nil
}

func (s *skillMarketplaceImportTestService) ImportSkill(context.Context, *contract.ImportSkillRequest) (*contract.ImportSkillResponse, error) {
	if s.importErr != nil {
		return nil, s.importErr
	}
	return &contract.ImportSkillResponse{Status: "imported", Message: "skill imported"}, nil
}

func (s *skillMarketplaceImportTestService) ImportSkillFromGitHub(context.Context, *contract.ImportSkillFromGitHubRequest) (*contract.ImportSkillResponse, error) {
	if s.importGitHubErr != nil {
		return nil, s.importGitHubErr
	}
	return &contract.ImportSkillResponse{Status: "imported", Message: "github skill imported"}, nil
}

func TestImportSkillReturnsBusinessFailureOnServiceError(t *testing.T) {
	router := newSkillMarketplaceImportTestRouter(&skillMarketplaceImportTestService{
		importErr: errors.New("技能包文件损坏，请重新导出或重新下载后再试"),
	})

	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/skill-marketplace/import", bytes.NewReader([]byte(`{"file_upload_id":"file_demo"}`)))
	req.Header.Set("Content-Type", "application/json")
	router.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusOK)
	}
	var body struct {
		Code int `json:"code"`
		Data struct {
			Status  string `json:"status"`
			Message string `json:"message"`
		} `json:"data"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if body.Data.Status != "failed" {
		t.Fatalf("data.status = %q, want failed", body.Data.Status)
	}
	if body.Data.Message != "技能包文件损坏，请重新导出或重新下载后再试" {
		t.Fatalf("data.message = %q", body.Data.Message)
	}
}

func TestImportSkillFromGitHubReturnsBusinessFailureOnServiceError(t *testing.T) {
	router := newSkillMarketplaceImportTestRouter(&skillMarketplaceImportTestService{
		importGitHubErr: errors.New("GitHub 链接必须指向 SKILL.md 文件"),
	})

	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/skill-marketplace/import/github", bytes.NewReader([]byte(`{"github_url":"https://github.com/owner/repo/blob/main/README.md"}`)))
	req.Header.Set("Content-Type", "application/json")
	router.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusOK)
	}
	var body struct {
		Data struct {
			Status  string `json:"status"`
			Message string `json:"message"`
		} `json:"data"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if body.Data.Status != "failed" {
		t.Fatalf("data.status = %q, want failed", body.Data.Status)
	}
	if body.Data.Message != "GitHub 链接必须指向 SKILL.md 文件" {
		t.Fatalf("data.message = %q", body.Data.Message)
	}
}

func TestImportSkillRequiredParamsStillReturnBadRequest(t *testing.T) {
	router := newSkillMarketplaceImportTestRouter(&skillMarketplaceImportTestService{})

	for _, tc := range []struct {
		name string
		path string
		body string
	}{
		{name: "file_upload_id", path: "/skill-marketplace/import", body: `{}`},
		{name: "github_url", path: "/skill-marketplace/import/github", body: `{}`},
	} {
		t.Run(tc.name, func(t *testing.T) {
			rec := httptest.NewRecorder()
			req := httptest.NewRequest(http.MethodPost, tc.path, bytes.NewReader([]byte(tc.body)))
			req.Header.Set("Content-Type", "application/json")
			router.ServeHTTP(rec, req)
			if rec.Code != http.StatusBadRequest {
				t.Fatalf("status = %d, want %d", rec.Code, http.StatusBadRequest)
			}
		})
	}
}

func TestImportSkillUnauthenticatedStillReturnsUnauthorized(t *testing.T) {
	router := newSkillMarketplaceImportTestRouter(&skillMarketplaceImportTestService{
		importErr: errors.New("user not authenticated or org not set"),
	})

	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/skill-marketplace/import", bytes.NewReader([]byte(`{"file_upload_id":"file_demo"}`)))
	req.Header.Set("Content-Type", "application/json")
	router.ServeHTTP(rec, req)

	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusUnauthorized)
	}
}

func newSkillMarketplaceImportTestRouter(service contract.SkillMarketplaceService) *gin.Engine {
	gin.SetMode(gin.TestMode)
	router := gin.New()
	RegisterSkillMarketplaceRoutes(router, service)
	return router
}
