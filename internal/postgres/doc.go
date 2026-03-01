// Package postgres manages PostgreSQL connectivity, migrations, and transaction helpers. Connect creates a pgxpool
// connection pool with configurable minimum and maximum connections. Migrate runs embedded goose migrations on startup.
// WithTx executes a function within a database transaction and handles commit or rollback. IsUniqueViolation detects
// PostgreSQL unique constraint violations by error code 23505.
package postgres
