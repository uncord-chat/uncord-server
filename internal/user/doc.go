// Package user provides domain types, validation, and repository operations for user identity records. The User type
// carries public profile fields (display name, pronouns, about, theme colours, avatar, and banner). The Credentials
// type extends User with the password hash for authentication. Tombstone records track deleted accounts. The Repository
// interface defines CRUD operations backed by PostgreSQL with transactional user creation.
package user
