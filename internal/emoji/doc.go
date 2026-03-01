// Package emoji provides domain types and validation for custom server emoji. Emoji names must be 2 to 32 alphanumeric
// or underscore characters. Each emoji tracks its uploader, creation time, and an optional animation flag. The
// Repository interface defines CRUD operations backed by PostgreSQL.
package emoji
