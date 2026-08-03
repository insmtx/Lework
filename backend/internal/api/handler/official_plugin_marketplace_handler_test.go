package handler

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/gin-gonic/gin"

	"github.com/insmtx/Leros/backend/internal/api/auth"
	"github.com/insmtx/Leros/backend/internal/api/contract"
	"github.com/insmtx/Leros/backend/types"
)

type officialPluginMarketplaceHandlerTestService struct {
	installOrgID uint
	installUIN   uint
	installID    string
	listOrgID    uint
	detailOrgID  uint
	detailID     string
	latestKind   string
	latestCode   string
}

func (s *officialPluginMarketplaceHandlerTestService) ListOfficialPluginMarketplaceItems(
	_ context.Context,
	orgID uint,
	_ *contract.ListOfficialPluginMarketplaceItemsRequest,
) (*contract.ListOfficialPluginMarketplaceItemsResponse, error) {
	s.listOrgID = orgID
	return &contract.ListOfficialPluginMarketplaceItemsResponse{Items: []contract.OfficialPluginMarketplaceItemView{}}, nil
}

func (s *officialPluginMarketplaceHandlerTestService) GetOfficialPluginMarketplaceItem(
	_ context.Context,
	orgID uint,
	itemID string,
) (*contract.OfficialPluginMarketplaceItemView, error) {
	s.detailOrgID, s.detailID = orgID, itemID
	return &contract.OfficialPluginMarketplaceItemView{PublicID: itemID}, nil
}

func (s *officialPluginMarketplaceHandlerTestService) GetOfficialPluginLatestVersion(
	_ context.Context,
	req *contract.GetOfficialPluginLatestVersionRequest,
) (*contract.OfficialPluginLatestVersionResponse, error) {
	s.latestKind, s.latestCode = req.Kind, req.Code
	return &contract.OfficialPluginLatestVersionResponse{
		Kind: req.Kind, Code: req.Code,
	}, nil
}

func (s *officialPluginMarketplaceHandlerTestService) InstallOfficialPlugin(_ context.Context, orgID, uin uint, itemID string) (*contract.InstallOfficialPluginResponse, error) {
	s.installOrgID, s.installUIN, s.installID = orgID, uin, itemID
	return &contract.InstallOfficialPluginResponse{Operation: "installed", Plugin: contract.PluginView{}}, nil
}

func TestOfficialPluginMarketplaceInstallUsesCallerOrganization(t *testing.T) {
	gin.SetMode(gin.TestMode)
	service := &officialPluginMarketplaceHandlerTestService{}
	router := gin.New()
	router.Use(func(ctx *gin.Context) {
		auth.WithGinContext(ctx, &types.Caller{OrgID: 42, Uin: 7, State: types.AuthStateSucc}, nil, "")
		ctx.Next()
	})
	RegisterOfficialPluginMarketplaceRoutes(router, service)
	recorder := httptest.NewRecorder()
	router.ServeHTTP(recorder, httptest.NewRequest(http.MethodPost, "/plugin-marketplace/items/mkt_official/install", nil))
	if recorder.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d", recorder.Code, http.StatusOK)
	}
	if service.installOrgID != 42 || service.installUIN != 7 || service.installID != "mkt_official" {
		t.Fatalf("install caller = org=%d uin=%d item=%q", service.installOrgID, service.installUIN, service.installID)
	}
}

func TestOfficialPluginMarketplaceReadsUseCallerOrganization(t *testing.T) {
	gin.SetMode(gin.TestMode)
	service := &officialPluginMarketplaceHandlerTestService{}
	router := gin.New()
	router.Use(func(ctx *gin.Context) {
		auth.WithGinContext(ctx, &types.Caller{OrgID: 42, Uin: 7, State: types.AuthStateSucc}, nil, "")
		ctx.Next()
	})
	RegisterOfficialPluginMarketplaceRoutes(router, service)

	listRecorder := httptest.NewRecorder()
	router.ServeHTTP(
		listRecorder,
		httptest.NewRequest(http.MethodGet, "/plugin-marketplace/items?kind=skill", nil),
	)
	if listRecorder.Code != http.StatusOK || service.listOrgID != 42 {
		t.Fatalf("list status = %d, org = %d", listRecorder.Code, service.listOrgID)
	}

	detailRecorder := httptest.NewRecorder()
	router.ServeHTTP(
		detailRecorder,
		httptest.NewRequest(http.MethodGet, "/plugin-marketplace/items/mkt_official", nil),
	)
	if detailRecorder.Code != http.StatusOK ||
		service.detailOrgID != 42 ||
		service.detailID != "mkt_official" {
		t.Fatalf(
			"detail status = %d, org = %d, item = %q",
			detailRecorder.Code,
			service.detailOrgID,
			service.detailID,
		)
	}
}

func TestOfficialPluginLatestVersionUsesStaticRouteAndValidatesIdentity(t *testing.T) {
	gin.SetMode(gin.TestMode)
	service := &officialPluginMarketplaceHandlerTestService{}
	router := gin.New()
	RegisterOfficialPluginMarketplaceRoutes(router, service)

	recorder := httptest.NewRecorder()
	router.ServeHTTP(
		recorder,
		httptest.NewRequest(
			http.MethodGet,
			"/plugin-marketplace/items/latest-version?kind=skill&code=official-skill",
			nil,
		),
	)
	if recorder.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d", recorder.Code, http.StatusOK)
	}
	if service.latestKind != "skill" || service.latestCode != "official-skill" {
		t.Fatalf("latest version query = kind=%q code=%q", service.latestKind, service.latestCode)
	}

	missingRecorder := httptest.NewRecorder()
	router.ServeHTTP(
		missingRecorder,
		httptest.NewRequest(
			http.MethodGet,
			"/plugin-marketplace/items/latest-version?kind=skill",
			nil,
		),
	)
	if missingRecorder.Code != http.StatusBadRequest {
		t.Fatalf("missing code status = %d, want %d", missingRecorder.Code, http.StatusBadRequest)
	}
}
