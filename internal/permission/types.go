package permission

// TargetType identifies whether a permission override applies to a channel or category.
type TargetType string

const (
	TargetChannel  TargetType = "channel"
	TargetCategory TargetType = "category"
)

// PrincipalType identifies whether a permission override is for a role or user.
type PrincipalType string

const (
	PrincipalRole PrincipalType = "role"
	PrincipalUser PrincipalType = "user"
)
