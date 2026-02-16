package page

import (
	"bytes"
	"errors"
	"html/template"

	"github.com/gofiber/fiber/v3"
	"github.com/rs/zerolog"

	"github.com/uncord-chat/uncord-server/internal/auth"
)

//nolint:misspell // CSS properties use American English spelling (color, center).
const defaultVerifyHTML = `<!DOCTYPE html>
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
padding:2.5rem 2rem;text-align:center;border-top:4px solid {{.AccentColour}}}
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
</html>`

var defaultVerifyTmpl = template.Must(template.New("verify").Parse(defaultVerifyHTML))

type verifyData struct {
	Title        string
	Heading      string
	Message      string
	AccentColour string
}

// VerifyHandler serves the browser-facing email verification page.
type VerifyHandler struct {
	auth       *auth.Service
	serverName string
	tmpl       *template.Template
	log        zerolog.Logger
}

// NewVerifyHandler creates a new VerifyHandler. The tmpl parameter is optional; when nil the compiled-in default
// template is used.
func NewVerifyHandler(authService *auth.Service, serverName string, tmpl *template.Template, logger zerolog.Logger) *VerifyHandler {
	if tmpl == nil {
		tmpl = defaultVerifyTmpl
	}
	return &VerifyHandler{auth: authService, serverName: serverName, tmpl: tmpl, log: logger}
}

// VerifyEmail handles GET /verify-email?token=... by consuming the verification token and rendering an HTML result page.
func (h *VerifyHandler) VerifyEmail(c fiber.Ctx) error {
	token := c.Query("token")
	if token == "" {
		return h.renderPage(c, fiber.StatusBadRequest, verifyData{
			Title:        h.serverName + " — Email Verification",
			Heading:      "Missing Token",
			Message:      "No verification token was provided. Please check the link in your email and try again.",
			AccentColour: "#e74c3c",
		})
	}

	if err := h.auth.VerifyEmail(c, token); err != nil {
		if errors.Is(err, auth.ErrInvalidToken) {
			return h.renderPage(c, fiber.StatusBadRequest, verifyData{
				Title:        h.serverName + " — Email Verification",
				Heading:      "Verification Failed",
				Message:      "This verification link is invalid or has expired. Please request a new verification email.",
				AccentColour: "#e74c3c",
			})
		}
		h.log.Error().Err(err).Msg("Unexpected error during email verification")
		return h.renderPage(c, fiber.StatusInternalServerError, verifyData{
			Title:        h.serverName + " — Email Verification",
			Heading:      "Something Went Wrong",
			Message:      "An unexpected error occurred while verifying your email. Please try again later.",
			AccentColour: "#e74c3c",
		})
	}

	return h.renderPage(c, fiber.StatusOK, verifyData{
		Title:        h.serverName + " — Email Verified",
		Heading:      "Email Verified",
		Message:      "Your email address has been verified. You can close this page and return to " + h.serverName + ".",
		AccentColour: "#2ecc71",
	})
}

// renderPage executes the verification template into a buffer and writes the complete HTML response. Buffering prevents
// partial writes if template execution fails.
func (h *VerifyHandler) renderPage(c fiber.Ctx, status int, data verifyData) error {
	var buf bytes.Buffer
	if err := h.tmpl.Execute(&buf, data); err != nil {
		h.log.Error().Err(err).Msg("Failed to render verification page template")
		return c.Status(fiber.StatusInternalServerError).SendString("internal server error")
	}
	c.Set("Content-Type", "text/html; charset=utf-8")
	return c.Status(status).Send(buf.Bytes())
}
