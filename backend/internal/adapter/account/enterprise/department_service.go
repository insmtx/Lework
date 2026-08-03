//go:build enterprise

package enterprise

import (
	"context"

	"github.com/insmtx/Leros/backend/internal/adapter/account"
)

type department struct {
	client *iamClient
}

func NewDepartment(client *iamClient) *department {
	return &department{client: client}
}

func (s *department) CreateDepartment(ctx context.Context, req *account.CreateDepartmentInput) (*account.Department, error) {
	var resp iamCreateDepartmentResp
	if err := s.client.callWithAuth(ctx, "account.CreateDepartment", &iamCreateDepartmentReq{
		Name:     req.Name,
		ParentID: req.ParentID,
		Sort:     req.Sort,
	}, &resp); err != nil {
		return nil, mapIAMError(err)
	}
	d := mapIAMDepartmentToContract(resp.Department)
	return &d, nil
}

func (s *department) GetDepartment(ctx context.Context, id uint) (*account.Department, error) {
	var resp iamGetDepartmentResp
	if err := s.client.callWithAuth(ctx, "account.GetDepartment", &iamGetDepartmentReq{
		ID: id,
	}, &resp); err != nil {
		return nil, mapIAMError(err)
	}
	d := mapIAMDepartmentToContract(resp.Department)
	return &d, nil
}

func (s *department) UpdateDepartment(ctx context.Context, id uint, req *account.UpdateDepartmentInput) (*account.Department, error) {
	iamReq := iamUpdateDepartmentReq{ID: id}
	if req.Name != nil {
		iamReq.Name = *req.Name
	}
	if req.ParentID != nil {
		iamReq.ParentID = *req.ParentID
	}
	if req.Sort != nil {
		iamReq.Sort = *req.Sort
	}

	var resp iamUpdateDepartmentResp
	if err := s.client.callWithAuth(ctx, "account.UpdateDepartment", &iamReq, &resp); err != nil {
		return nil, mapIAMError(err)
	}
	d := mapIAMDepartmentToContract(resp.Department)
	return &d, nil
}

func (s *department) DeleteDepartment(ctx context.Context, id uint) error {
	return s.client.callWithAuth(ctx, "account.DeleteDepartment", &iamDeleteDepartmentReq{ID: id}, nil)
}

func (s *department) ListDepartment(ctx context.Context, req *account.ListDepartmentInput) (*account.DepartmentList, error) {
	iamReq := iamListDepartmentReq{
		Offset: req.Offset,
		Limit:  req.Limit,
	}
	if req.Keyword != nil {
		iamReq.Keyword = *req.Keyword
	}

	var resp iamListDepartmentResp
	if err := s.client.callWithAuth(ctx, "account.ListDepartment", &iamReq, &resp); err != nil {
		return nil, mapIAMError(err)
	}

	items := make([]account.Department, 0, len(resp.Departments))
	for _, dept := range resp.Departments {
		items = append(items, mapListDepartmentToContract(dept))
	}

	return &account.DepartmentList{
		Total:  resp.Total,
		Offset: resp.Offset,
		Limit:  resp.Limit,
		Items:  items,
	}, nil
}
