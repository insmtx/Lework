package cli

import (
	"context"
	"strconv"

	"github.com/insmtx/Leros/backend/internal/api/contract"
)

// ListAutomations 调用服务端 ListAutomations API 并返回自动化列表。
func ListAutomations(ctx context.Context, serverAddr, authToken string, req *contract.ListAutomationsRequest, targetUserID ...*uint) (*contract.AutomationList, error) {
	var result contract.AutomationList
	if err := doListRequestWithHeaders(ctx, serverAddr, authToken, "ListAutomations", req, &result, automationTargetHeaders(optionalTargetUserID(targetUserID))); err != nil {
		return nil, err
	}
	return &result, nil
}

// GetAutomation 调用服务端 GetAutomation API 并返回自动化详情。
func GetAutomation(ctx context.Context, serverAddr, authToken, publicID string, targetUserID ...*uint) (*contract.Automation, error) {
	var result contract.Automation
	if err := doPostRequestWithHeaders(ctx, serverAddr, authToken, "GetAutomation",
		&contract.GetAutomationRequest{PublicID: publicID}, &result, automationTargetHeaders(optionalTargetUserID(targetUserID))); err != nil {
		return nil, err
	}
	return &result, nil
}

// CreateAutomation 调用服务端 CreateAutomation API 并返回新建自动化。
func CreateAutomation(ctx context.Context, serverAddr, authToken string, req *contract.CreateAutomationRequest, targetUserID ...*uint) (*contract.Automation, error) {
	var result contract.Automation
	if err := doPostRequestWithHeaders(ctx, serverAddr, authToken, "CreateAutomation", req, &result, automationTargetHeaders(optionalTargetUserID(targetUserID))); err != nil {
		return nil, err
	}
	return &result, nil
}

// UpdateAutomation 调用服务端 UpdateAutomation API 并返回更新后的自动化。
func UpdateAutomation(ctx context.Context, serverAddr, authToken, publicID string, req *contract.UpdateAutomationRequest, targetUserID ...*uint) (*contract.Automation, error) {
	var result contract.Automation
	body := struct {
		PublicID string `json:"public_id"`
		contract.UpdateAutomationRequest
	}{
		PublicID:                publicID,
		UpdateAutomationRequest: *req,
	}
	if err := doPostRequestWithHeaders(ctx, serverAddr, authToken, "UpdateAutomation", &body, &result, automationTargetHeaders(optionalTargetUserID(targetUserID))); err != nil {
		return nil, err
	}
	return &result, nil
}

// DeleteAutomation 调用服务端 DeleteAutomation API。

func DeleteAutomation(ctx context.Context, serverAddr, authToken, publicID string, targetUserID ...*uint) error {
	return doPostRequestWithHeaders(ctx, serverAddr, authToken, "DeleteAutomation",
		&contract.DeleteAutomationRequest{PublicID: publicID}, nil, automationTargetHeaders(optionalTargetUserID(targetUserID)))
}

func optionalTargetUserID(values []*uint) *uint {
	if len(values) == 0 {
		return nil
	}
	return values[0]
}

func automationTargetHeaders(targetUserID *uint) map[string]string {
	if targetUserID == nil || *targetUserID == 0 {
		return nil
	}
	return map[string]string{
		"X-Leros-Target-User-Id": strconv.FormatUint(uint64(*targetUserID), 10),
	}
}
