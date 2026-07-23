package account

// UserRef identifies a user by primary key or one non-key unique attribute.
// The first non-zero field determines the lookup strategy in the Resolve method.
type UserRef struct {
	ID       uint
	PublicID string
	Email    string
	Phone    string
}

// OrgRef identifies an organization.
type OrgRef struct {
	ID       uint
	PublicID string
	Code     string
}

// DepartmentRef identifies a department. When ID is set, OrgID is ignored.
// When Name is set, OrgID must be set too (name is unique within organization).
type DepartmentRef struct {
	ID    uint
	OrgID uint
	Name  string
}

// UserOrgRef identifies a user-org mapping. The combination (Uin, OrgID) is unique.
type UserOrgRef struct {
	ID          uint
	Uin         uint
	OrgID       uint
	ExternalUin uint // identity-platform UIN ID (enterprise edition)
}
