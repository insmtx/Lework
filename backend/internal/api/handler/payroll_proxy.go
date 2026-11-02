package handler

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"mime"
	"mime/multipart"
	"net/http"
	"net/url"
	"path"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/insmtx/Lework/backend/internal/api/dto"
	"github.com/insmtx/Lework/backend/internal/service"
)

const (
	payrollCredentialHandler = "payroll"
	payrollCredentialCode    = "payroll"
	payrollCredentialName    = "Go Payroll"
	payrollCredentialPurpose = "http_client"
	payrollMaxRequestBytes   = 32 << 20
	payrollMaxResponseBytes  = 32 << 20
)

var payrollProxyActions = map[string]struct{}{
	"PageRoster": {}, "PageHisSalary": {}, "ImportRoster": {}, "ImportHisSalary": {},
	"UpdateRoster": {}, "UpdateHisSalary": {}, "DeleteRoster": {}, "DeleteHisSalary": {},
	"ListRosterChangeHistory": {}, "ListHisSalaryChangeHistory": {},
}

type payrollCredentialResolver interface {
	Resolve(context.Context, string, service.ManagedCredentialRequest) (map[string]string, error)
}

// PayrollProxyHandler forwards only the fixed Payroll API actions.
type PayrollProxyHandler struct {
	endpoint    string
	credentials payrollCredentialResolver
	client      *http.Client
}

// NewPayrollProxyHandler creates a proxy using a trusted server-side endpoint.
func NewPayrollProxyHandler(endpoint string, credentials payrollCredentialResolver) *PayrollProxyHandler {
	return &PayrollProxyHandler{
		endpoint: strings.TrimSpace(endpoint), credentials: credentials,
		client: &http.Client{
			Timeout:       30 * time.Second,
			CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse },
		},
	}
}

// RegisterPayrollProxyRoutes registers the fixed Payroll proxy path on an authenticated route group.
func RegisterPayrollProxyRoutes(r gin.IRouter, proxy *PayrollProxyHandler) {
	r.POST("/PayrollProxy/:action", proxy.forward)
}

func (h *PayrollProxyHandler) forward(ctx *gin.Context) {
	action := ctx.Param("action")
	if _, ok := payrollProxyActions[action]; !ok {
		ctx.JSON(http.StatusNotFound, dto.Error(dto.CodeNotFound, "payroll action not found"))
		return
	}
	base, err := url.Parse(h.endpoint)
	if err != nil || (base.Scheme != "http" && base.Scheme != "https") || base.Host == "" ||
		base.User != nil || (base.Path != "" && base.Path != "/") || base.RawQuery != "" || base.Fragment != "" {
		ctx.JSON(http.StatusServiceUnavailable, dto.Error(dto.CodeInternalError, "payroll service is not configured"))
		return
	}
	if h.credentials == nil {
		ctx.JSON(http.StatusServiceUnavailable, dto.Error(dto.CodeInternalError, "payroll credentials are unavailable"))
		return
	}
	body, contentType, err := readPayrollRequest(ctx)
	if err != nil {
		ctx.JSON(http.StatusBadRequest, dto.Error(dto.CodeInvalidParams, "invalid payroll request"))
		return
	}
	credentials, err := h.credentials.Resolve(ctx.Request.Context(), payrollCredentialHandler, service.ManagedCredentialRequest{
		ConnectorCode: payrollCredentialCode,
		ConnectorName: payrollCredentialName,
		Purpose:       payrollCredentialPurpose,
	})
	if err != nil || strings.TrimSpace(credentials["api_key"]) == "" {
		ctx.JSON(http.StatusServiceUnavailable, dto.Error(dto.CodeInternalError, "payroll credentials are unavailable"))
		return
	}

	upstreamURL := *base
	upstreamURL.Path = path.Join("/v1", "payroll."+action)
	request, err := http.NewRequestWithContext(ctx.Request.Context(), http.MethodPost, upstreamURL.String(), bytes.NewReader(body))
	if err != nil {
		ctx.JSON(http.StatusBadGateway, dto.Error(dto.CodeInternalError, "payroll service is unavailable"))
		return
	}
	request.Header.Set("Authorization", "Bearer "+credentials["api_key"])
	request.Header.Set("Accept", ctx.GetHeader("Accept"))
	request.Header.Set("Content-Type", contentType)
	response, err := h.client.Do(request)
	if err != nil {
		ctx.JSON(http.StatusBadGateway, dto.Error(dto.CodeInternalError, "payroll service is unavailable"))
		return
	}
	defer response.Body.Close()

	if response.StatusCode == http.StatusUnauthorized || response.StatusCode == http.StatusForbidden {
		ctx.JSON(http.StatusBadGateway, dto.Error(dto.CodeInternalError, "payroll service authentication failed"))
		return
	}
	if response.StatusCode >= http.StatusInternalServerError {
		ctx.JSON(http.StatusBadGateway, dto.Error(dto.CodeInternalError, "payroll service is unavailable"))
		return
	}
	responseBody, err := io.ReadAll(io.LimitReader(response.Body, payrollMaxResponseBytes+1))
	if err != nil || len(responseBody) > payrollMaxResponseBytes {
		ctx.JSON(http.StatusBadGateway, dto.Error(dto.CodeInternalError, "payroll service returned an invalid response"))
		return
	}
	ctx.Header("Content-Type", response.Header.Get("Content-Type"))
	ctx.Header("Cache-Control", "no-store")
	ctx.Status(response.StatusCode)
	_, _ = ctx.Writer.Write(responseBody)
}

func readPayrollRequest(ctx *gin.Context) ([]byte, string, error) {
	if ctx.Request.ContentLength > payrollMaxRequestBytes {
		return nil, "", io.ErrUnexpectedEOF
	}
	body, err := io.ReadAll(io.LimitReader(ctx.Request.Body, payrollMaxRequestBytes+1))
	if err != nil || len(body) > payrollMaxRequestBytes {
		return nil, "", io.ErrUnexpectedEOF
	}
	contentType := ctx.GetHeader("Content-Type")
	mediaType, params, err := mime.ParseMediaType(contentType)
	if err != nil {
		return nil, "", err
	}
	switch mediaType {
	case "application/json":
		if !json.Valid(body) {
			return nil, "", io.ErrUnexpectedEOF
		}
	case "multipart/form-data":
		boundary := params["boundary"]
		if boundary == "" {
			return nil, "", io.ErrUnexpectedEOF
		}
		reader := multipart.NewReader(bytes.NewReader(body), boundary)
		found := false
		for {
			part, nextErr := reader.NextPart()
			if nextErr == io.EOF {
				break
			}
			if nextErr != nil {
				return nil, "", nextErr
			}
			if part.FormName() == "file" {
				if !strings.HasSuffix(strings.ToLower(part.FileName()), ".xlsx") || found {
					_ = part.Close()
					return nil, "", io.ErrUnexpectedEOF
				}
				found = true
			}
			_, copyErr := io.Copy(io.Discard, part)
			_ = part.Close()
			if copyErr != nil {
				return nil, "", copyErr
			}
		}
		if !found {
			return nil, "", io.ErrUnexpectedEOF
		}
	default:
		return nil, "", io.ErrUnexpectedEOF
	}
	return body, contentType, nil
}
