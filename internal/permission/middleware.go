package permission

import (
	"github.com/gofiber/fiber/v2"
	"github.com/google/uuid"
	"github.com/uncord-chat/uncord-protocol/permissions"

	apierrors "github.com/uncord-chat/uncord-protocol/errors"

	"github.com/uncord-chat/uncord-server/internal/httputil"
)

// RequirePermission returns Fiber middleware that checks whether the
// authenticated user has the given permission in the channel specified by
// the "channelID" route parameter.
func RequirePermission(resolver *Resolver, perm permissions.Permission) fiber.Handler {
	return func(c *fiber.Ctx) error {
		userIDVal := c.Locals("userID")
		if userIDVal == nil {
			return httputil.Fail(c, fiber.StatusUnauthorized, apierrors.Unauthorized, "Authentication required")
		}

		userID, ok := userIDVal.(uuid.UUID)
		if !ok {
			return httputil.Fail(c, fiber.StatusUnauthorized, apierrors.Unauthorized, "Invalid user identity")
		}

		channelIDStr := c.Params("channelID")
		if channelIDStr == "" {
			return httputil.Fail(c, fiber.StatusBadRequest, apierrors.MissingChannelID, "Channel ID is required")
		}

		channelID, err := uuid.Parse(channelIDStr)
		if err != nil {
			return httputil.Fail(c, fiber.StatusBadRequest, apierrors.InvalidChannelID, "Invalid channel ID format")
		}

		allowed, err := resolver.HasPermission(c.Context(), userID, channelID, perm)
		if err != nil {
			return httputil.Fail(c, fiber.StatusInternalServerError, apierrors.InternalError, "Failed to check permissions")
		}

		if !allowed {
			return httputil.Fail(c, fiber.StatusForbidden, apierrors.MissingPermissions, "You do not have the required permissions")
		}

		return c.Next()
	}
}
