package contract

import (
	"time"

	"github.com/insmtx/Leros/backend/types"
)

type Org struct {
	ID        uint      `json:"id"`
	Type      string    `json:"type"`
	Code      string    `json:"code"`
	Name      string    `json:"name"`
	Status    string    `json:"status"`
	CreatedAt time.Time `json:"created_at"`
	UpdatedAt time.Time `json:"updated_at"`
}

type CreateOrgRequest struct {
	Name   string `json:"name" binding:"required"`
	Code   string `json:"code" binding:"required"`
	Type   string `json:"type,omitempty"`
	Status string `json:"status,omitempty"`
}

type UpdateOrgRequest struct {
	Name   *string `json:"name,omitempty"`
	Type   *string `json:"type,omitempty"`
	Status *string `json:"status,omitempty"`
}

type ListOrgsRequest struct {
	Keyword *string `json:"keyword,omitempty"`
	Status  *string `json:"status,omitempty"`
	types.Pagination
}

type OrgList struct {
	Total  int64 `json:"total"`
	Offset int   `json:"offset"`
	Limit  int   `json:"limit"`
	Items  []Org `json:"items"`
}
