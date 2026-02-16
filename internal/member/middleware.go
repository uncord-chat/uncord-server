package member

import (
	"errors"

	"github.com/gofiber/fiber/v3"
	"github.com/google/uuid"
	apierrors "github.com/uncord-chat/uncord-protocol/errors"
	"github.com/uncord-chat/uncord-protocol/models"

	"github.com/uncord-chat/uncord-server/internal/httputil"
)

// RequireActiveMember returns Fiber middleware that blocks users who are not active members of the server. A user with
// no member record or a pending member record is rejected. Must be placed after RequireAuth so that
// c.Locals("userID") is populated.
func RequireActiveMember(members Repository) fiber.Handler {
	return func(c fiber.Ctx) error {
		userID, ok := c.Locals("userID").(uuid.UUID)
		if !ok {
			return httputil.Fail(c, fiber.StatusUnauthorized, apierrors.Unauthorised, "Authentication required")
		}
		status, err := members.GetStatus(c, userID)
		if err != nil {
			if errors.Is(err, ErrNotFound) {
				return httputil.Fail(c, fiber.StatusForbidden, apierrors.MembershipRequired,
					"Server membership is required")
			}
			return httputil.Fail(c, fiber.StatusInternalServerError, apierrors.InternalError,
				"An internal error occurred")
		}
		if status == models.MemberStatusPending {
			return httputil.Fail(c, fiber.StatusForbidden, apierrors.MembershipRequired,
				"Onboarding must be completed first")
		}
		return c.Next()
	}
}
