// Package email provides an SMTP client for sending transactional emails. The Client handles STARTTLS negotiation,
// authentication, and message construction. SendVerification renders an HTML verification email with a confirmation
// link. The Client is safe for concurrent use.
package email
