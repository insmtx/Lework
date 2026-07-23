package contract

import "github.com/insmtx/Leros/backend/internal/adapter/account"

type Department struct {
	account.Department
}

type CreateDepartmentRequest struct {
	account.CreateDepartmentInput
}

type UpdateDepartmentRequest struct {
	account.UpdateDepartmentInput
}

type ListDepartmentRequest struct {
	account.ListDepartmentInput
}

type DepartmentList struct {
	account.DepartmentList
}
