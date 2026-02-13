package email

import "fmt"

// verificationBody returns the plain text body for an email verification message.
func verificationBody(serverName, serverURL, token string) string {
	return fmt.Sprintf(
		"Welcome to %s!\n\n"+
			"Please verify your email address by visiting the link below:\n\n"+
			"%s/verify-email?token=%s\n\n"+
			"This link expires in 24 hours. If you did not create an account, you can safely ignore this email.\n",
		serverName, serverURL, token,
	)
}
