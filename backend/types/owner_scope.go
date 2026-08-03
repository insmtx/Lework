package types

// OwnerScope identifies whether a resource belongs to one organization or to
// the Leros system namespace.
type OwnerScope string

const (
	OwnerScopeOrganization OwnerScope = "organization"
	OwnerScopeSystem       OwnerScope = "system"
)

// NormalizeOwnerScope applies the default for organization-owned callers.
func NormalizeOwnerScope(scope OwnerScope) OwnerScope {
	if scope == "" {
		return OwnerScopeOrganization
	}
	return scope
}

// ValidateOwnerScope checks the scope and organization identity invariant.
func ValidateOwnerScope(scope OwnerScope, orgID uint) bool {
	switch NormalizeOwnerScope(scope) {
	case OwnerScopeOrganization:
		return orgID > 0
	case OwnerScopeSystem:
		return orgID == 0
	default:
		return false
	}
}
