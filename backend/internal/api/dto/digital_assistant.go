package dto

import "github.com/insmtx/Lework/backend/internal/api/contract"

// CreateDigitalAssistantResponse 创建数字助手响应
type CreateDigitalAssistantResponse struct {
	Code    int                        `json:"code"`
	Message string                     `json:"message"`
	Data    *contract.DigitalAssistant `json:"data"`
}

// NewCreateDigitalAssistantResponse 创建成功响应
func NewCreateDigitalAssistantResponse(data *contract.DigitalAssistant) *CreateDigitalAssistantResponse {
	return &CreateDigitalAssistantResponse{
		Code:    CodeSuccess,
		Message: "success",
		Data:    data,
	}
}
