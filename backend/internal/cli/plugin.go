package cli

import (
	"context"
	"net/url"
	"strconv"
	"strings"

	"github.com/insmtx/Leros/backend/internal/api/contract"
)

// ListOrganizationPlugins lists organization-owned plugins through the typed REST API.
func ListOrganizationPlugins(
	ctx context.Context,
	serverAddr, authToken string,
	req *contract.ListPluginsRequest,
) (*contract.ListPluginsResponse, error) {
	values := url.Values{}
	if req != nil {
		values.Set("kind", strings.TrimSpace(req.Kind))
		values.Set("status", strings.TrimSpace(req.Status))
		values.Set("keyword", strings.TrimSpace(req.Keyword))
		if req.Offset > 0 {
			values.Set("offset", strconv.Itoa(req.Offset))
		}
		if req.Limit > 0 {
			values.Set("limit", strconv.Itoa(req.Limit))
		}
	}
	path := "plugins"
	if query := values.Encode(); query != "" {
		path += "?" + query
	}
	var result contract.ListPluginsResponse
	if err := doGetRequest(ctx, serverAddr, authToken, path, &result); err != nil {
		return nil, err
	}
	return &result, nil
}

// ListProjectPlugins lists plugins currently associated with a project.
func ListProjectPlugins(
	ctx context.Context,
	serverAddr, authToken string,
	req *contract.ListProjectPluginsRequest,
) ([]contract.ProjectPlugin, error) {
	var result []contract.ProjectPlugin
	if err := doPostRequest(ctx, serverAddr, authToken, "ListProjectPlugins", req, &result); err != nil {
		return nil, err
	}
	return result, nil
}

// AddProjectPlugin associates one organization plugin with a project.
func AddProjectPlugin(
	ctx context.Context,
	serverAddr, authToken string,
	req *contract.UpdateProjectPluginRequest,
) (*contract.ProjectPluginMutationResult, error) {
	var result contract.ProjectPluginMutationResult
	if err := doPostRequest(ctx, serverAddr, authToken, "AddProjectPlugin", req, &result); err != nil {
		return nil, err
	}
	return &result, nil
}

// RemoveProjectPlugin removes one project plugin association.
func RemoveProjectPlugin(
	ctx context.Context,
	serverAddr, authToken string,
	req *contract.UpdateProjectPluginRequest,
) (*contract.ProjectPluginMutationResult, error) {
	var result contract.ProjectPluginMutationResult
	if err := doPostRequest(ctx, serverAddr, authToken, "RemoveProjectPlugin", req, &result); err != nil {
		return nil, err
	}
	return &result, nil
}
