// Package role provides domain types and validation for server roles. Roles carry a name, colour, display position,
// hoist flag, and a permission bitfield. The @everyone role cannot be deleted. Permission values are validated against
// the set of all defined permissions. The Repository interface defines CRUD operations backed by PostgreSQL.
package role
