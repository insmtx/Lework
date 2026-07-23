//go:build !enterprise

package oss

// UserRef identifies a user by primary key or one non-key unique attribute.
// Mirrors account.UserRef to avoid an import cycle.
type UserRef struct {
	ID       uint
	PublicID string
	Email    string
	Phone    string
}

// OrgRef identifies an organization. Mirrors account.OrgRef.
type OrgRef struct {
	ID       uint
	PublicID string
	Code     string
}

// DepartmentRef identifies a department. Mirrors account.DepartmentRef.
type DepartmentRef struct {
	ID    uint
	OrgID uint
	Name  string
}

// UserOrgRef identifies a user-org mapping. Mirrors account.UserOrgRef.
type UserOrgRef struct {
	ID          uint
	Uin         uint
	OrgID       uint
	ExternalUin uint
}
