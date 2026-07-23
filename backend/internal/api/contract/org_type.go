package contract

import "github.com/insmtx/Leros/backend/internal/adapter/account"

type Org struct {
	account.Org
}

type CreateOrgRequest struct {
	account.CreateOrgInput
}

type UpdateOrgRequest struct {
	account.UpdateOrgInput
}

type ListOrgsRequest struct {
	account.ListOrgsInput
}

type OrgList struct {
	account.OrgList
}

type OrgMember struct {
	account.OrgMember
}

type OrgMemberDepartment struct {
	account.OrgMemberDepartment
}

type CreateOrgMemberRequest struct {
	account.CreateOrgMemberInput
}

type UpdateOrgMemberRequest struct {
	account.UpdateOrgMemberInput
}

type ListOrgMembersRequest struct {
	account.ListOrgMembersInput
}

type OrgMemberList struct {
	account.OrgMemberList
}
