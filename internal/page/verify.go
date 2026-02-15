package page

import (
	"bytes"
	_ "embed"
	"errors"
	"html/template"

	"github.com/gofiber/fiber/v3"
	"github.com/rs/zerolog"

	"github.com/uncord-chat/uncord-server/internal/auth"
)

//go:embed templates/verify.html
var verifyHTML string

var verifyTmpl = template.Must(template.New("verify").Parse(verifyHTML))

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
	log        zerolog.Logger
}

// NewVerifyHandler creates a new VerifyHandler.
func NewVerifyHandler(authService *auth.Service, serverName string, logger zerolog.Logger) *VerifyHandler {
	return &VerifyHandler{auth: authService, serverName: serverName, log: logger}
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
	if err := verifyTmpl.Execute(&buf, data); err != nil {
		h.log.Error().Err(err).Msg("Failed to render verification page template")
		return c.Status(fiber.StatusInternalServerError).SendString("internal server error")
	}
	c.Set("Content-Type", "text/html; charset=utf-8")
	return c.Status(status).Send(buf.Bytes())
}
