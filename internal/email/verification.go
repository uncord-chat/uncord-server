package email

import (
	"bytes"
	_ "embed"
	"fmt"
	"html/template"
)

//go:embed templates/verification.html
var verificationHTML string

var verificationTmpl = template.Must(template.New("verification").Parse(verificationHTML))

type verificationData struct {
	ServerName string
	VerifyURL  string
}

// verificationBody renders the HTML body for an email verification message.
func verificationBody(serverName, serverURL, token string) (string, error) {
	data := verificationData{
		ServerName: serverName,
		VerifyURL:  fmt.Sprintf("%s/verify-email?token=%s", serverURL, token),
	}
	var buf bytes.Buffer
	if err := verificationTmpl.Execute(&buf, data); err != nil {
		return "", fmt.Errorf("render verification template: %w", err)
	}
	return buf.String(), nil
}
