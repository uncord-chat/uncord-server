package page

import (
	"bytes"
	"errors"
	"html/template"

	"github.com/gofiber/fiber/v3"
	"github.com/rs/zerolog/log"

	"github.com/uncord-chat/uncord-server/internal/auth"
)

//nolint:misspell // CSS property names (color, center) are not misspellings.
var verifyTmpl = template.Must(template.New("verify").Parse(`<!DOCTYPE html>
<html lang="en">
<head>
<meta charset="utf-8">
<meta name="viewport" content="width=device-width, initial-scale=1">
<title>{{.Title}}</title>
<style>
*{margin:0;padding:0;box-sizing:border-box}
body{font-family:-apple-system,BlinkMacSystemFont,"Segoe UI",Roboto,Helvetica,Arial,sans-serif;
background:#f4f5f7;display:flex;align-items:center;justify-content:center;min-height:100vh;padding:1rem}
.card{background:#fff;border-radius:8px;box-shadow:0 2px 8px rgba(0,0,0,.08);max-width:440px;width:100%;
padding:2.5rem 2rem;text-align:center;border-top:4px solid {{.AccentColor}}}
h1{font-size:1.25rem;color:#1a1a2e;margin-bottom:.75rem}
p{font-size:.95rem;color:#555;line-height:1.5}
</style>
</head>
<body>
<div class="card">
<h1>{{.Heading}}</h1>
<p>{{.Message}}</p>
</div>
</body>
</html>`))

type verifyData struct {
	Title       string
	Heading     string
	Message     string
	AccentColor string
}

// VerifyHandler serves the browser-facing email verification page.
type VerifyHandler struct {
	auth       *auth.Service
	serverName string
}

// NewVerifyHandler creates a new VerifyHandler.
func NewVerifyHandler(authService *auth.Service, serverName string) *VerifyHandler {
	return &VerifyHandler{auth: authService, serverName: serverName}
}

// VerifyEmail handles GET /verify-email?token=... by consuming the verification token and rendering an HTML result page.
func (h *VerifyHandler) VerifyEmail(c fiber.Ctx) error {
	token := c.Query("token")
	if token == "" {
		return renderPage(c, fiber.StatusBadRequest, verifyData{
			Title:       h.serverName + " — Email Verification",
			Heading:     "Missing Token",
			Message:     "No verification token was provided. Please check the link in your email and try again.",
			AccentColor: "#e74c3c",
		})
	}

	if err := h.auth.VerifyEmail(c, token); err != nil {
		if errors.Is(err, auth.ErrInvalidToken) {
			return renderPage(c, fiber.StatusBadRequest, verifyData{
				Title:       h.serverName + " — Email Verification",
				Heading:     "Verification Failed",
				Message:     "This verification link is invalid or has expired. Please request a new verification email.",
				AccentColor: "#e74c3c",
			})
		}
		log.Error().Err(err).Msg("Unexpected error during email verification")
		return renderPage(c, fiber.StatusInternalServerError, verifyData{
			Title:       h.serverName + " — Email Verification",
			Heading:     "Something Went Wrong",
			Message:     "An unexpected error occurred while verifying your email. Please try again later.",
			AccentColor: "#e74c3c",
		})
	}

	return renderPage(c, fiber.StatusOK, verifyData{
		Title:       h.serverName + " — Email Verified",
		Heading:     "Email Verified",
		Message:     "Your email address has been verified. You can close this page and return to " + h.serverName + ".",
		AccentColor: "#2ecc71",
	})
}

// renderPage executes the verification template into a buffer and writes the complete HTML response. Buffering prevents
// partial writes if template execution fails.
func renderPage(c fiber.Ctx, status int, data verifyData) error {
	var buf bytes.Buffer
	if err := verifyTmpl.Execute(&buf, data); err != nil {
		log.Error().Err(err).Msg("Failed to render verification page template")
		return c.Status(fiber.StatusInternalServerError).SendString("internal server error")
	}
	c.Set("Content-Type", "text/html; charset=utf-8")
	return c.Status(status).Send(buf.Bytes())
}
