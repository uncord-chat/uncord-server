// Package bootstrap handles first-run database seeding. When the server starts against an empty database, RunFirstInit
// creates the owner user, the @everyone role with default permissions, default channels, and initial onboarding
// configuration in a single transaction. IsFirstRun detects whether seeding has already occurred by checking for
// existing users.
package bootstrap
