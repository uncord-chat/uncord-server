package api

import (
	"context"
	"errors"
	"time"

	"github.com/gofiber/fiber/v3"
	"github.com/google/uuid"
	"github.com/rs/zerolog"
	apierrors "github.com/uncord-chat/uncord-protocol/errors"
	"github.com/uncord-chat/uncord-protocol/events"
	"github.com/uncord-chat/uncord-protocol/models"

	"github.com/uncord-chat/uncord-server/internal/gateway"
	"github.com/uncord-chat/uncord-server/internal/httputil"
	"github.com/uncord-chat/uncord-server/internal/member"
	"github.com/uncord-chat/uncord-server/internal/onboarding"
	"github.com/uncord-chat/uncord-server/internal/server"
	"github.com/uncord-chat/uncord-server/internal/user"
)

// OnboardingHandler serves onboarding endpoints.
type OnboardingHandler struct {
	onboarding onboarding.Repository
	documents  *onboarding.DocumentStore
	members    member.Repository
	users      user.Repository
	servers    server.Repository
	gateway    *gateway.Publisher
	log        zerolog.Logger
}

// NewOnboardingHandler creates a new onboarding handler.
func NewOnboardingHandler(
	onboardingRepo onboarding.Repository,
	documents *onboarding.DocumentStore,
	members member.Repository,
	users user.Repository,
	servers server.Repository,
	gw *gateway.Publisher,
	logger zerolog.Logger,
) *OnboardingHandler {
	return &OnboardingHandler{
		onboarding: onboardingRepo,
		documents:  documents,
		members:    members,
		users:      users,
		servers:    servers,
		gateway:    gw,
		log:        logger,
	}
}

// GetOnboarding handles GET /api/v1/onboarding. Returns the onboarding config and all documents. No verified or active
// member check is required so clients can discover the onboarding requirements before completing them.
func (h *OnboardingHandler) GetOnboarding(c fiber.Ctx) error {
	cfg, err := h.onboarding.Get(c)
	if err != nil {
		h.log.Error().Err(err).Str("handler", "onboarding").Msg("get onboarding config failed")
		return httputil.Fail(c, fiber.StatusInternalServerError, apierrors.InternalError, "An internal error occurred")
	}

	return httputil.Success(c, cfg.ToModel(h.documents.ToModels()))
}

// UpdateOnboarding handles PATCH /api/v1/onboarding. Only the server owner may update the onboarding configuration.
// Documents are managed on the filesystem, not via this endpoint.
func (h *OnboardingHandler) UpdateOnboarding(c fiber.Ctx) error {
	userID, ok := c.Locals("userID").(uuid.UUID)
	if !ok {
		return httputil.Fail(c, fiber.StatusUnauthorized, apierrors.Unauthorised, "Missing user identity")
	}

	srv, err := h.servers.Get(c)
	if err != nil {
		h.log.Error().Err(err).Str("handler", "onboarding").Msg("get server config failed")
		return httputil.Fail(c, fiber.StatusInternalServerError, apierrors.InternalError, "An internal error occurred")
	}
	if srv.OwnerID != userID {
		return httputil.Fail(c, fiber.StatusForbidden, apierrors.OwnerOnly, "Only the server owner can update onboarding settings")
	}

	var body models.UpdateOnboardingConfigRequest
	if err := c.Bind().Body(&body); err != nil {
		return httputil.Fail(c, fiber.StatusBadRequest, apierrors.InvalidBody, "Invalid request body")
	}

	params := onboarding.UpdateParams{
		RequireEmailVerification: body.RequireEmailVerification,
		OpenJoin:                 body.OpenJoin,
		MinAccountAgeSeconds:     body.MinAccountAgeSeconds,
	}

	if body.WelcomeChannelID != nil {
		if *body.WelcomeChannelID == "" {
			params.SetWelcomeChannelNull = true
		} else {
			parsed, err := uuid.Parse(*body.WelcomeChannelID)
			if err != nil {
				return httputil.Fail(c, fiber.StatusBadRequest, apierrors.ValidationError, "Invalid welcome channel ID format")
			}
			params.WelcomeChannelID = &parsed
		}
	}

	if body.AutoRoles != nil {
		params.SetAutoRoles = true
		params.AutoRoles = make([]uuid.UUID, len(body.AutoRoles))
		for i, s := range body.AutoRoles {
			parsed, err := uuid.Parse(s)
			if err != nil {
				return httputil.Fail(c, fiber.StatusBadRequest, apierrors.ValidationError, "Invalid auto role ID format")
			}
			params.AutoRoles[i] = parsed
		}
	}

	cfg, err := h.onboarding.Update(c, params)
	if err != nil {
		return h.mapOnboardingError(c, err)
	}

	return httputil.Success(c, cfg.ToModel(h.documents.ToModels()))
}

// AcceptOnboarding handles POST /api/v1/onboarding/accept. Validates that the client has accepted all required
// documents, then activates the pending member.
func (h *OnboardingHandler) AcceptOnboarding(c fiber.Ctx) error {
	userID, ok := c.Locals("userID").(uuid.UUID)
	if !ok {
		return httputil.Fail(c, fiber.StatusUnauthorized, apierrors.Unauthorised, "Missing user identity")
	}

	var body models.AcceptOnboardingRequest
	if err := c.Bind().Body(&body); err != nil {
		return httputil.Fail(c, fiber.StatusBadRequest, apierrors.InvalidBody, "Invalid request body")
	}

	cfg, err := h.onboarding.Get(c)
	if err != nil {
		h.log.Error().Err(err).Str("handler", "onboarding").Msg("get onboarding config failed")
		return httputil.Fail(c, fiber.StatusInternalServerError, apierrors.InternalError, "An internal error occurred")
	}

	if cfg.RequireEmailVerification {
		u, err := h.users.GetByID(c, userID)
		if err != nil {
			h.log.Error().Err(err).Str("handler", "onboarding").Msg("get user for email check failed")
			return httputil.Fail(c, fiber.StatusInternalServerError, apierrors.InternalError, "An internal error occurred")
		}
		if !u.EmailVerified {
			return httputil.Fail(c, fiber.StatusBadRequest, apierrors.EmailNotVerified,
				"You must verify your email address before joining")
		}
	}

	if cfg.RequirePhone {
		return httputil.Fail(c, fiber.StatusBadRequest, apierrors.ValidationError,
			"Phone verification is not yet supported")
	}

	if cfg.RequireCaptcha {
		return httputil.Fail(c, fiber.StatusBadRequest, apierrors.ValidationError,
			"Captcha verification is not yet supported")
	}

	// Verify that all required document slugs have been accepted.
	required := h.documents.RequiredSlugs()
	if len(required) > 0 {
		accepted := make(map[string]struct{}, len(body.AcceptedDocumentSlugs))
		for _, slug := range body.AcceptedDocumentSlugs {
			accepted[slug] = struct{}{}
		}
		for slug := range required {
			if _, ok := accepted[slug]; !ok {
				return httputil.Fail(c, fiber.StatusBadRequest, apierrors.ValidationError,
					"You must accept all required documents to continue")
			}
		}
	}

	m, err := h.members.Activate(c, userID, cfg.AutoRoles)
	if err != nil {
		return h.mapOnboardingError(c, err)
	}

	result := m.ToModel()
	if h.gateway != nil {
		go func() {
			if err := h.gateway.Publish(context.Background(), events.MemberAdd, result); err != nil {
				h.log.Warn().Err(err).Str("user_id", userID.String()).Msg("Gateway publish failed")
			}
		}()
	}

	return httputil.Success(c, result)
}

// JoinServer handles POST /api/v1/server/join. Allows users to join the server without an invite when open_join is
// enabled.
func (h *OnboardingHandler) JoinServer(c fiber.Ctx) error {
	userID, ok := c.Locals("userID").(uuid.UUID)
	if !ok {
		return httputil.Fail(c, fiber.StatusUnauthorized, apierrors.Unauthorised, "Missing user identity")
	}

	cfg, err := h.onboarding.Get(c)
	if err != nil {
		h.log.Error().Err(err).Str("handler", "onboarding").Msg("get onboarding config failed")
		return httputil.Fail(c, fiber.StatusInternalServerError, apierrors.InternalError, "An internal error occurred")
	}

	if !cfg.OpenJoin {
		return httputil.Fail(c, fiber.StatusForbidden, apierrors.OpenJoinDisabled, "Open server joining is not enabled")
	}

	banned, err := h.members.IsBanned(c, userID)
	if err != nil {
		h.log.Error().Err(err).Str("handler", "onboarding").Msg("ban check failed")
		return httputil.Fail(c, fiber.StatusInternalServerError, apierrors.InternalError, "An internal error occurred")
	}
	if banned {
		return httputil.Fail(c, fiber.StatusForbidden, apierrors.Banned, "You are banned from this server")
	}

	if cfg.MinAccountAgeSeconds > 0 {
		u, err := h.users.GetByID(c, userID)
		if err != nil {
			h.log.Error().Err(err).Str("handler", "onboarding").Msg("get user failed")
			return httputil.Fail(c, fiber.StatusInternalServerError, apierrors.InternalError, "An internal error occurred")
		}
		accountAge := time.Since(u.CreatedAt)
		if accountAge < time.Duration(cfg.MinAccountAgeSeconds)*time.Second {
			return httputil.Fail(c, fiber.StatusBadRequest, apierrors.ValidationError,
				"Your account is too new to join this server")
		}
	}

	m, err := h.members.CreatePending(c, userID)
	if err != nil {
		return h.mapOnboardingError(c, err)
	}

	return httputil.Success(c, m.ToModel())
}

// GetOnboardingStatus handles GET /api/v1/onboarding/status. Returns the next step the authenticated user must complete
// in the onboarding flow as a single computed string. Only requires authentication so it works at any onboarding stage.
func (h *OnboardingHandler) GetOnboardingStatus(c fiber.Ctx) error {
	userID, ok := c.Locals("userID").(uuid.UUID)
	if !ok {
		return httputil.Fail(c, fiber.StatusUnauthorized, apierrors.Unauthorised, "Missing user identity")
	}

	status, err := h.members.GetStatus(c, userID)
	if err != nil {
		if !errors.Is(err, member.ErrNotFound) {
			h.log.Error().Err(err).Str("handler", "onboarding").Msg("get member status failed")
			return httputil.Fail(c, fiber.StatusInternalServerError, apierrors.InternalError, "An internal error occurred")
		}
		return httputil.Success(c, models.OnboardingStatusResponse{Step: models.OnboardingStepJoinServer})
	}

	if status != models.MemberStatusPending {
		return httputil.Success(c, models.OnboardingStatusResponse{Step: models.OnboardingStepComplete})
	}

	cfg, err := h.onboarding.Get(c)
	if err != nil {
		h.log.Error().Err(err).Str("handler", "onboarding").Msg("get onboarding config for status failed")
		return httputil.Fail(c, fiber.StatusInternalServerError, apierrors.InternalError, "An internal error occurred")
	}

	if cfg.RequireEmailVerification {
		u, err := h.users.GetByID(c, userID)
		if err != nil {
			h.log.Error().Err(err).Str("handler", "onboarding").Msg("get user for status email check failed")
			return httputil.Fail(c, fiber.StatusInternalServerError, apierrors.InternalError, "An internal error occurred")
		}
		if !u.EmailVerified {
			return httputil.Success(c, models.OnboardingStatusResponse{Step: models.OnboardingStepVerifyEmail})
		}
	}

	return httputil.Success(c, models.OnboardingStatusResponse{Step: models.OnboardingStepAcceptDocuments})
}

// mapOnboardingError converts onboarding and member layer errors to appropriate HTTP responses.
func (h *OnboardingHandler) mapOnboardingError(c fiber.Ctx, err error) error {
	switch {
	case errors.Is(err, onboarding.ErrNotFound):
		return httputil.Fail(c, fiber.StatusNotFound, apierrors.NotFound, "Onboarding configuration not found")
	case errors.Is(err, onboarding.ErrOpenJoinDisabled):
		return httputil.Fail(c, fiber.StatusForbidden, apierrors.OpenJoinDisabled, "Open server joining is not enabled")
	case errors.Is(err, onboarding.ErrDocumentsIncomplete):
		return httputil.Fail(c, fiber.StatusBadRequest, apierrors.ValidationError, "You must accept all required documents")
	case errors.Is(err, member.ErrAlreadyMember):
		return httputil.Fail(c, fiber.StatusConflict, apierrors.AlreadyMember, "You are already a member of this server")
	case errors.Is(err, member.ErrNotPending):
		return httputil.Fail(c, fiber.StatusBadRequest, apierrors.ValidationError, "Not in pending status")
	default:
		h.log.Error().Err(err).Str("handler", "onboarding").Msg("unhandled onboarding service error")
		return httputil.Fail(c, fiber.StatusInternalServerError, apierrors.InternalError, "An internal error occurred")
	}
}
