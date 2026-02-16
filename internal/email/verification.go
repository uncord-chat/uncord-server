package email

import (
	"bytes"
	"fmt"
	"html/template"
)

//nolint:misspell // CSS properties use American English spelling (color, center).
const defaultVerificationHTML = `<!DOCTYPE html>
<html lang="en">
<head>
<meta charset="utf-8">
<meta name="viewport" content="width=device-width, initial-scale=1">
<title>Verify Your Email</title>
</head>
<body style="margin:0;padding:0;background-color:#f4f5f7;font-family:-apple-system,BlinkMacSystemFont,'Segoe UI',Roboto,Helvetica,Arial,sans-serif;">
<table role="presentation" width="100%" cellpadding="0" cellspacing="0" style="background-color:#f4f5f7;padding:40px 20px;">
<tr>
<td align="center">
<table role="presentation" width="440" cellpadding="0" cellspacing="0" style="background-color:#ffffff;border-radius:8px;box-shadow:0 2px 8px rgba(0,0,0,0.08);border-top:4px solid #5865f2;max-width:440px;width:100%;">
<tr>
<td style="padding:40px 32px;text-align:center;">
<h1 style="font-size:20px;color:#1a1a2e;margin:0 0 16px;">Welcome to {{.ServerName}}!</h1>
<p style="font-size:15px;color:#555555;line-height:1.6;margin:0 0 24px;">Please verify your email address by clicking the button below.</p>
<table role="presentation" cellpadding="0" cellspacing="0" style="margin:0 auto 24px;">
<tr>
<td style="background-color:#5865f2;border-radius:6px;">
<a href="{{.VerifyURL}}" target="_blank" style="display:inline-block;padding:12px 32px;color:#ffffff;font-size:15px;font-weight:600;text-decoration:none;">Verify Email Address</a>
</td>
</tr>
</table>
<p style="font-size:13px;color:#888888;line-height:1.5;margin:0 0 16px;">If the button above does not work, copy and paste this link into your browser:</p>
<p style="font-size:13px;color:#5865f2;word-break:break-all;margin:0 0 24px;">{{.VerifyURL}}</p>
<p style="font-size:13px;color:#888888;line-height:1.5;margin:0;">This link expires in 24 hours. If you did not create an account, you can safely ignore this email.</p>
</td>
</tr>
</table>
</td>
</tr>
</table>
</body>
</html>`

var defaultVerificationTmpl = template.Must(template.New("verification").Parse(defaultVerificationHTML))

type verificationData struct {
	ServerName string
	VerifyURL  string
}

// renderVerification renders the HTML body for an email verification message using the provided template. If tmpl is nil
// the compiled-in default is used.
func renderVerification(tmpl *template.Template, serverName, serverURL, token string) (string, error) {
	if tmpl == nil {
		tmpl = defaultVerificationTmpl
	}
	data := verificationData{
		ServerName: serverName,
		VerifyURL:  fmt.Sprintf("%s/verify-email?token=%s", serverURL, token),
	}
	var buf bytes.Buffer
	if err := tmpl.Execute(&buf, data); err != nil {
		return "", fmt.Errorf("render verification template: %w", err)
	}
	return buf.String(), nil
}
