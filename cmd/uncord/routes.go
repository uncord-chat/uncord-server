package main

import (
	"time"

	"github.com/gofiber/fiber/v3"
	"github.com/gofiber/fiber/v3/middleware/limiter"
	"github.com/rs/zerolog/log"

	"github.com/uncord-chat/uncord-protocol/permissions"

	"github.com/uncord-chat/uncord-server/internal/api"
	"github.com/uncord-chat/uncord-server/internal/auth"
	"github.com/uncord-chat/uncord-server/internal/media"
	"github.com/uncord-chat/uncord-server/internal/member"
	"github.com/uncord-chat/uncord-server/internal/page"
	"github.com/uncord-chat/uncord-server/internal/permission"
	"github.com/uncord-chat/uncord-server/internal/search"
)

// authLimiter returns a rate limiter scoped to authentication endpoints.
func (s *server) authLimiter() fiber.Handler {
	return limiter.New(limiter.Config{
		Max:          s.cfg.RateLimitAuthCount,
		Expiration:   time.Duration(s.cfg.RateLimitAuthWindowSeconds) * time.Second,
		LimitReached: rateLimitReached,
	})
}

// uploadLimiter returns a per-user rate limiter scoped to file upload endpoints.
func (s *server) uploadLimiter() fiber.Handler {
	return limiter.New(limiter.Config{
		Max:          s.cfg.RateLimitUploadCount,
		Expiration:   time.Duration(s.cfg.RateLimitUploadWindowSeconds) * time.Second,
		KeyGenerator: userKeyGenerator,
		LimitReached: rateLimitReached,
	})
}

// channelMsgLimiter returns a per-user-per-channel rate limiter for message creation.
func (s *server) channelMsgLimiter() fiber.Handler {
	return limiter.New(limiter.Config{
		Max:          s.cfg.RateLimitMsgCount,
		Expiration:   time.Duration(s.cfg.RateLimitMsgWindowSeconds) * time.Second,
		KeyGenerator: userChannelKeyGenerator,
		LimitReached: rateLimitReached,
	})
}

// globalMsgLimiter returns a per-user rate limiter for message creation across all channels.
func (s *server) globalMsgLimiter() fiber.Handler {
	return limiter.New(limiter.Config{
		Max:          s.cfg.RateLimitMsgGlobalCount,
		Expiration:   time.Duration(s.cfg.RateLimitMsgGlobalWindowSeconds) * time.Second,
		KeyGenerator: userKeyGenerator,
		LimitReached: rateLimitReached,
	})
}

// registerRoutes sets up all HTTP routes, middleware chains, and handler bindings on the Fiber application. Routes are
// grouped by resource and documented with section comments to aid navigation.
func (s *server) registerRoutes(app *fiber.App) {
	requireAuth := auth.RequireAuth(s.cfg.JWTSecret.Expose(), s.cfg.ServerURL, s.cfg)
	requireCSRF := auth.RequireCSRF(s.cfg)
	requireVerifiedEmail := auth.RequireVerifiedEmail()
	requireActiveMember := member.RequireActiveMember(s.memberRepo)

	// === EMAIL VERIFICATION PAGE ===

	// Browser-facing email verification page (outside /api/v1/ because users click this link directly from email)
	verifyHandler := page.NewVerifyHandler(s.authService, s.cfg.ServerName, s.verifyPageTmpl, log.Logger)
	app.Get("/verify-email", s.authLimiter(), verifyHandler.VerifyEmail)

	// === HEALTH CHECK ===

	health := api.NewHealthHandler(s.db, redisPinger{client: s.rdb})
	app.Get("/api/v1/health", health.Health)

	// === AUTH ROUTES ===

	authHandler := api.NewAuthHandler(s.authService, s.cfg, log.Logger)

	// Auth routes with stricter rate limiting (public, no email/member checks)
	authGroup := app.Group("/api/v1/auth")
	authGroup.Use(s.authLimiter())
	authGroup.Post("/register", authHandler.Register)
	authGroup.Post("/login", authHandler.Login)
	authGroup.Post("/refresh", authHandler.Refresh)
	authGroup.Post("/verify-email", authHandler.VerifyEmail)
	authGroup.Post("/resend-verification", requireAuth, requireCSRF, authHandler.ResendVerification)
	authGroup.Post("/mfa/verify", authHandler.MFAVerify)
	authGroup.Post("/verify-password", requireAuth, requireCSRF, authHandler.VerifyPassword)
	authGroup.Post("/logout", requireAuth, requireCSRF, authHandler.Logout)
	authGroup.Post("/gateway-ticket", requireAuth, requireCSRF, authHandler.GatewayTicket)

	// === USER PROFILE ROUTES ===

	// User profile routes (authenticated + verified email, no member check required)
	userHandler := api.NewUserHandler(s.userRepo, s.authService, log.Logger)
	userGroup := app.Group("/api/v1/users", requireAuth, requireCSRF, requireVerifiedEmail)
	userGroup.Get("/@me", userHandler.GetMe)
	userGroup.Patch("/@me", userHandler.UpdateMe)
	userGroup.Delete("/@me", userHandler.DeleteMe)
	userGroup.Get("/:userID", userHandler.GetProfile)

	// User image upload/delete routes (authenticated + verified email)
	imageHandler := api.NewImageUploadHandler(
		s.userRepo, s.serverRepo, s.storage,
		s.cfg.MaxAvatarSizeBytes(), s.cfg.MaxAvatarDimension,
		s.cfg.MaxBannerWidth, s.cfg.MaxBannerHeight, log.Logger)
	userUploadLimiter := s.uploadLimiter()
	userGroup.Put("/@me/avatar", userUploadLimiter, imageHandler.UploadUserAvatar)
	userGroup.Delete("/@me/avatar", imageHandler.DeleteUserAvatar)
	userGroup.Put("/@me/banner", userUploadLimiter, imageHandler.UploadUserBanner)
	userGroup.Delete("/@me/banner", imageHandler.DeleteUserBanner)

	// === MFA ROUTES ===

	// MFA management routes (authenticated + verified email)
	mfaHandler := api.NewMFAHandler(s.authService, log.Logger)
	mfaGroup := userGroup.Group("/@me/mfa")
	mfaGroup.Post("/enable", mfaHandler.Enable)
	mfaGroup.Post("/confirm", mfaHandler.Confirm)
	mfaGroup.Post("/disable", mfaHandler.Disable)
	mfaGroup.Post("/recovery-codes", mfaHandler.RegenerateCodes)

	// === E2EE DEVICE AND KEY ROUTES ===

	// E2EE device and key management (under /api/v1/users, inherits auth + verified)
	e2eeHandler := api.NewE2EEHandler(s.e2eeRepo, s.dmRepo, s.gatewayPublisher, s.cfg.E2EEOPKLowThreshold, log.Logger)
	userGroup.Post("/@me/devices", e2eeHandler.RegisterDevice)
	userGroup.Get("/@me/devices", e2eeHandler.ListDevices)
	userGroup.Delete("/@me/devices/:deviceID", e2eeHandler.RemoveDevice)
	userGroup.Put("/@me/devices/:deviceID/identity-key", e2eeHandler.UpdateIdentityKey)
	userGroup.Put("/@me/devices/:deviceID/signed-pre-key", e2eeHandler.UploadSignedPreKey)
	userGroup.Post("/@me/devices/:deviceID/one-time-pre-keys", e2eeHandler.UploadOneTimePreKeys)
	userGroup.Get("/@me/devices/:deviceID/one-time-pre-keys/count", e2eeHandler.GetKeyCount)
	userGroup.Get("/:userID/keys", e2eeHandler.FetchKeyBundle)

	// === DIRECT MESSAGE ROUTES ===

	// DM channel management (under /api/v1/users/@me/channels)
	dmHandler := api.NewDMHandler(s.dmRepo, s.messageRepo, s.e2eeRepo, s.gatewayPublisher, s.cfg.MaxMessageLength, log.Logger)
	userGroup.Post("/@me/channels", dmHandler.CreateDMChannel)
	userGroup.Get("/@me/channels", dmHandler.ListDMChannels)

	// DM channel routes (under /api/v1/dm, auth + verified + participant check). Active server membership is
	// intentionally not required because DMs are independent of the server; access control is enforced per channel via
	// RequireParticipant instead.
	dmGroup := app.Group("/api/v1/dm", requireAuth, requireCSRF, requireVerifiedEmail)
	dmGroup.Get("/:channelID", dmHandler.RequireParticipant, dmHandler.GetDMChannel)
	dmGroup.Post("/:channelID/participants", dmHandler.RequireParticipant, dmHandler.AddParticipant)
	dmGroup.Delete("/:channelID/participants/:userID", dmHandler.RequireParticipant, dmHandler.RemoveParticipant)
	dmGroup.Get("/:channelID/messages", dmHandler.RequireParticipant, dmHandler.ListMessages)
	dmGroup.Post("/:channelID/messages", dmHandler.RequireParticipant, dmHandler.SendMessage)

	// === SERVER CONFIG ROUTES ===

	// Server config routes (authenticated + verified email)
	serverHandler := api.NewServerHandler(s.serverRepo, s.auditLogger, log.Logger)
	app.Get("/api/v1/server/info", serverHandler.GetPublicInfo)
	serverUploadLimiter := s.uploadLimiter()
	serverGroup := app.Group("/api/v1/server", requireAuth, requireCSRF, requireVerifiedEmail)
	serverGroup.Get("/", serverHandler.Get)
	serverGroup.Patch("/", requireActiveMember,
		permission.RequireServerPermission(s.permResolver, permissions.ManageServer), serverHandler.Update)
	serverGroup.Put("/icon", requireActiveMember,
		permission.RequireServerPermission(s.permResolver, permissions.ManageServer),
		serverUploadLimiter, imageHandler.UploadServerIcon)
	serverGroup.Delete("/icon", requireActiveMember,
		permission.RequireServerPermission(s.permResolver, permissions.ManageServer),
		imageHandler.DeleteServerIcon)
	serverGroup.Put("/banner", requireActiveMember,
		permission.RequireServerPermission(s.permResolver, permissions.ManageServer),
		serverUploadLimiter, imageHandler.UploadServerBanner)
	serverGroup.Delete("/banner", requireActiveMember,
		permission.RequireServerPermission(s.permResolver, permissions.ManageServer),
		imageHandler.DeleteServerBanner)

	// === AUDIT LOG ROUTES ===

	// Audit log route (requires active membership and ViewAuditLog permission)
	auditHandler := api.NewAuditHandler(s.auditRepo, log.Logger)
	serverGroup.Get("/audit-log", requireActiveMember,
		permission.RequireServerPermission(s.permResolver, permissions.ViewAuditLog),
		auditHandler.List)

	// === EMOJI ROUTES ===

	// Emoji routes (under /api/v1/server/emoji, all require active membership)
	emojiHandler := api.NewEmojiHandler(s.emojiRepo, s.storage, s.gatewayPublisher,
		s.cfg.MaxEmojiSizeBytes(), 128, s.cfg.MaxEmojiPerServer, s.auditLogger, log.Logger)
	serverGroup.Get("/emoji", requireActiveMember, emojiHandler.ListEmoji)
	serverGroup.Post("/emoji", requireActiveMember,
		permission.RequireServerPermission(s.permResolver, permissions.ManageEmoji),
		serverUploadLimiter, emojiHandler.CreateEmoji)
	serverGroup.Patch("/emoji/:emojiID", requireActiveMember,
		permission.RequireServerPermission(s.permResolver, permissions.ManageEmoji),
		emojiHandler.UpdateEmoji)
	serverGroup.Delete("/emoji/:emojiID", requireActiveMember,
		permission.RequireServerPermission(s.permResolver, permissions.ManageEmoji),
		emojiHandler.DeleteEmoji)

	// === CHANNEL ROUTES ===

	// Channel routes (server group: list is open to pending, create requires active)
	channelHandler := api.NewChannelHandler(s.channelRepo, s.memberRepo, s.onboardingRepo, s.permResolver, s.gatewayPublisher, s.cfg.MaxChannels, s.auditLogger, log.Logger)
	serverGroup.Get("/channels", channelHandler.ListChannels)
	serverGroup.Post("/channels", requireActiveMember,
		permission.RequireServerPermission(s.permResolver, permissions.ManageChannels),
		channelHandler.CreateChannel)

	// Channel routes (standalone group: all routes require active membership)
	channelGroup := app.Group("/api/v1/channels", requireAuth, requireCSRF, requireVerifiedEmail, requireActiveMember)
	channelGroup.Get("/:channelID",
		permission.RequirePermission(s.permResolver, permissions.ViewChannels),
		channelHandler.GetChannel)
	channelGroup.Patch("/:channelID",
		permission.RequirePermission(s.permResolver, permissions.ManageChannels),
		channelHandler.UpdateChannel)
	channelGroup.Delete("/:channelID",
		permission.RequirePermission(s.permResolver, permissions.ManageChannels),
		channelHandler.DeleteChannel)

	// === READ STATE ROUTES ===

	// Read state routes (acknowledge read position in a channel)
	readStateHandler := api.NewReadStateHandler(s.readStateRepo, s.gatewayPublisher, log.Logger)
	channelGroup.Post("/:channelID/ack",
		permission.RequirePermission(s.permResolver, permissions.ViewChannels),
		readStateHandler.Ack)

	// === PERMISSION OVERRIDE ROUTES ===

	// Permission override routes
	permHandler := api.NewPermissionHandler(s.permStore, s.permResolver, s.permPublisher, s.auditLogger, log.Logger)
	channelGroup.Put("/:channelID/overrides/:targetID",
		permission.RequireServerPermission(s.permResolver, permissions.ManageRoles),
		permHandler.SetOverride)
	channelGroup.Delete("/:channelID/overrides/:targetID",
		permission.RequireServerPermission(s.permResolver, permissions.ManageRoles),
		permHandler.DeleteOverride)
	channelGroup.Get("/:channelID/permissions/@me",
		permHandler.GetMyPermissions)

	// === ATTACHMENT ROUTES ===

	// Attachment upload route (nested under channels, inherits active requirement)
	attachmentHandler := api.NewAttachmentHandler(
		s.attachmentRepo, s.storage, s.rdb, s.cfg.MaxUploadSizeBytes(), log.Logger)
	channelGroup.Post("/:channelID/attachments",
		s.uploadLimiter(),
		permission.RequirePermission(s.permResolver, permissions.AttachFiles),
		attachmentHandler.Upload)

	// === MESSAGE ROUTES ===

	// Message routes (nested under channels for list and create, inherits active requirement)
	messageHandler := api.NewMessageHandler(
		s.messageRepo, s.attachmentRepo, s.reactionRepo, s.storage, s.permResolver, s.typesenseIndexer,
		s.gatewayPublisher, s.presenceStore, s.cfg.MaxMessageLength, s.cfg.MaxAttachmentsPerMessage, s.auditLogger, log.Logger)
	channelGroup.Get("/:channelID/messages",
		permission.RequirePermission(s.permResolver, permissions.ViewChannels|permissions.ReadMessageHistory),
		messageHandler.ListMessages)
	channelGroup.Post("/:channelID/messages",
		s.channelMsgLimiter(), s.globalMsgLimiter(),
		permission.RequirePermission(s.permResolver, permissions.SendMessages),
		messageHandler.CreateMessage)

	// === TYPING INDICATOR ROUTES ===

	// Typing indicator routes
	typingHandler := api.NewTypingHandler(s.presenceStore, s.gatewayPublisher, log.Logger)
	channelGroup.Post("/:channelID/typing",
		permission.RequirePermission(s.permResolver, permissions.SendMessages),
		typingHandler.StartTyping)
	channelGroup.Delete("/:channelID/typing",
		permission.RequirePermission(s.permResolver, permissions.SendMessages),
		typingHandler.StopTyping)

	// === REACTION ROUTES ===

	// Reaction routes (nested under channels, inherits active requirement)
	reactionHandler := api.NewReactionHandler(s.reactionRepo, s.messageRepo, s.emojiRepo,
		s.gatewayPublisher, log.Logger)
	channelGroup.Put("/:channelID/messages/:messageID/reactions/:emoji",
		permission.RequirePermission(s.permResolver, permissions.AddReactions),
		reactionHandler.AddReaction)
	channelGroup.Delete("/:channelID/messages/:messageID/reactions/:emoji",
		reactionHandler.RemoveReaction)
	channelGroup.Get("/:channelID/messages/:messageID/reactions",
		permission.RequirePermission(s.permResolver, permissions.ViewChannels),
		reactionHandler.ListReactions)
	channelGroup.Get("/:channelID/messages/:messageID/reactions/:emoji",
		permission.RequirePermission(s.permResolver, permissions.ViewChannels),
		reactionHandler.ListReactionUsers)

	// Message routes (standalone for edit and delete, require active membership)
	messageGroup := app.Group("/api/v1/messages", requireAuth, requireCSRF, requireVerifiedEmail, requireActiveMember)
	messageGroup.Patch("/:messageID", messageHandler.EditMessage)
	messageGroup.Delete("/:messageID", messageHandler.DeleteMessage)

	// === THREAD ROUTES ===

	// Thread routes
	threadHandler := api.NewThreadHandler(
		s.threadRepo, s.messageRepo, s.channelRepo, s.attachmentRepo, s.reactionRepo, s.storage,
		s.permResolver, s.gatewayPublisher, s.presenceStore,
		s.cfg.MaxMessageLength, s.cfg.MaxAttachmentsPerMessage, log.Logger)

	// Thread creation (under /api/v1/messages, inherits auth/verified/active)
	messageGroup.Post("/:messageID/threads", threadHandler.CreateThread)

	// Thread listing (under /api/v1/channels, ViewChannels via middleware)
	channelGroup.Get("/:channelID/threads",
		permission.RequirePermission(s.permResolver, permissions.ViewChannels),
		threadHandler.ListThreads)

	// === PIN ROUTES ===

	// Pinned messages (under /api/v1/channels, ViewChannels via middleware)
	pinHandler := api.NewPinHandler(
		s.messageRepo, s.attachmentRepo, s.reactionRepo, s.storage,
		s.permResolver, s.gatewayPublisher, s.auditLogger, log.Logger)
	channelGroup.Get("/:channelID/pins",
		permission.RequirePermission(s.permResolver, permissions.ViewChannels),
		pinHandler.ListPins)

	// Thread-scoped routes (standalone group)
	threadGroup := app.Group("/api/v1/threads", requireAuth, requireCSRF, requireVerifiedEmail, requireActiveMember)
	threadGroup.Get("/:threadID", threadHandler.GetThread)
	threadGroup.Patch("/:threadID", threadHandler.UpdateThread)
	threadGroup.Get("/:threadID/messages", threadHandler.ListThreadMessages)
	threadGroup.Post("/:threadID/messages", s.channelMsgLimiter(), threadHandler.CreateThreadMessage)

	// Pin/unpin (under /api/v1/messages, inherits auth/verified/active)
	messageGroup.Put("/:messageID/pin", pinHandler.PinMessage)
	messageGroup.Delete("/:messageID/pin", pinHandler.UnpinMessage)

	// === SEARCH ROUTES ===

	// Search routes (require active membership)
	searcher := search.NewTypesenseSearcher(s.cfg.TypesenseURL, s.cfg.TypesenseAPIKey.Expose(), s.cfg.TypesenseTimeout)
	searchService := search.NewService(s.channelRepo, s.permResolver, searcher, log.Logger)
	searchHandler := api.NewSearchHandler(searchService, log.Logger)
	app.Get("/api/v1/search/messages", requireAuth, requireCSRF, requireVerifiedEmail, requireActiveMember,
		searchHandler.SearchMessages)

	// === CATEGORY ROUTES ===

	// Category routes (server group routes need per-route active, standalone group requires active)
	categoryHandler := api.NewCategoryHandler(s.categoryRepo, s.cfg.MaxCategories, s.auditLogger, log.Logger)
	serverGroup.Get("/categories", requireActiveMember, categoryHandler.ListCategories)
	serverGroup.Post("/categories", requireActiveMember,
		permission.RequireServerPermission(s.permResolver, permissions.ManageCategories),
		categoryHandler.CreateCategory)

	categoryGroup := app.Group("/api/v1/categories", requireAuth, requireCSRF, requireVerifiedEmail, requireActiveMember)
	categoryGroup.Patch("/:categoryID",
		permission.RequireServerPermission(s.permResolver, permissions.ManageCategories),
		categoryHandler.UpdateCategory)
	categoryGroup.Delete("/:categoryID",
		permission.RequireServerPermission(s.permResolver, permissions.ManageCategories),
		categoryHandler.DeleteCategory)

	// === ROLE ROUTES ===

	// Role routes (all require active membership)
	roleHandler := api.NewRoleHandler(s.roleRepo, s.permPublisher, s.gatewayPublisher, s.cfg.MaxRoles, s.auditLogger, log.Logger)
	serverGroup.Get("/roles", requireActiveMember, roleHandler.ListRoles)
	serverGroup.Post("/roles", requireActiveMember,
		permission.RequireServerPermission(s.permResolver, permissions.ManageRoles),
		roleHandler.CreateRole)
	serverGroup.Patch("/roles/:roleID", requireActiveMember,
		permission.RequireServerPermission(s.permResolver, permissions.ManageRoles),
		roleHandler.UpdateRole)
	serverGroup.Delete("/roles/:roleID", requireActiveMember,
		permission.RequireServerPermission(s.permResolver, permissions.ManageRoles),
		roleHandler.DeleteRole)

	// === INVITE ROUTES ===

	// Invite management routes (under /api/v1/server, require active membership)
	inviteHandler := api.NewInviteHandler(s.inviteRepo, s.onboardingRepo, s.memberRepo, s.userRepo, s.auditLogger, log.Logger)
	serverGroup.Post("/invites", requireActiveMember,
		permission.RequireServerPermission(s.permResolver, permissions.CreateInvites),
		inviteHandler.CreateInvite)
	serverGroup.Get("/invites", requireActiveMember,
		permission.RequireServerPermission(s.permResolver, permissions.ManageInvites),
		inviteHandler.ListInvites)

	// Invite action routes (under /api/v1/invites, authenticated). The join endpoint handles email verification
	// after invite validation so that clients can distinguish invalid codes from unverified accounts.
	inviteGroup := app.Group("/api/v1/invites", requireAuth, requireCSRF)
	inviteGroup.Delete("/:code", requireVerifiedEmail, requireActiveMember,
		permission.RequireServerPermission(s.permResolver, permissions.ManageInvites),
		inviteHandler.DeleteInvite)
	inviteGroup.Post("/:code/join", inviteHandler.JoinViaInvite)

	// === ONBOARDING ROUTES ===

	// Onboarding routes
	onboardingHandler := api.NewOnboardingHandler(s.onboardingRepo, s.documentStore, s.memberRepo, s.userRepo, s.serverRepo, s.gatewayPublisher, s.auditLogger, log.Logger)
	app.Get("/api/v1/onboarding/status", requireAuth, requireCSRF, onboardingHandler.GetOnboardingStatus)
	app.Get("/api/v1/onboarding/acceptance", requireAuth, requireCSRF, onboardingHandler.GetAcceptance)
	app.Get("/api/v1/onboarding", requireAuth, requireCSRF, onboardingHandler.GetOnboarding)
	app.Patch("/api/v1/onboarding", requireAuth, requireCSRF, requireVerifiedEmail, requireActiveMember, onboardingHandler.UpdateOnboarding)
	app.Post("/api/v1/onboarding/accept", requireAuth, requireCSRF, requireVerifiedEmail, onboardingHandler.AcceptOnboarding)
	serverGroup.Post("/join", onboardingHandler.JoinServer)

	// === MEMBER ROUTES ===

	// Member routes (mixed: some require active, some do not)
	memberHandler := api.NewMemberHandler(s.memberRepo, s.roleRepo, s.permStore, s.permResolver, s.permPublisher, s.gatewayPublisher, s.auditLogger, log.Logger)

	// Channel member listing (which server members can see this channel)
	channelGroup.Get("/:channelID/members",
		permission.RequirePermission(s.permResolver, permissions.ViewChannels),
		memberHandler.ListChannelMembers)

	memberGroup := serverGroup.Group("/members")
	memberGroup.Get("/", requireActiveMember, memberHandler.ListMembers)
	memberGroup.Get("/@me", memberHandler.GetSelf)
	memberGroup.Patch("/@me", requireActiveMember,
		permission.RequireServerPermission(s.permResolver, permissions.ChangeNicknames),
		memberHandler.UpdateSelf)
	memberGroup.Delete("/@me", memberHandler.Leave)
	memberGroup.Get("/:userID", requireActiveMember, memberHandler.GetMember)
	memberGroup.Patch("/:userID", requireActiveMember,
		permission.RequireServerPermission(s.permResolver, permissions.ManageNicknames),
		memberHandler.UpdateMember)
	memberGroup.Delete("/:userID", requireActiveMember,
		permission.RequireServerPermission(s.permResolver, permissions.KickMembers),
		memberHandler.KickMember)
	memberGroup.Put("/:userID/timeout", requireActiveMember,
		permission.RequireServerPermission(s.permResolver, permissions.TimeoutMembers),
		memberHandler.SetTimeout)
	memberGroup.Delete("/:userID/timeout", requireActiveMember,
		permission.RequireServerPermission(s.permResolver, permissions.TimeoutMembers),
		memberHandler.ClearTimeout)
	memberGroup.Put("/:userID/roles/:roleID", requireActiveMember,
		permission.RequireServerPermission(s.permResolver, permissions.AssignRoles),
		memberHandler.AssignRole)
	memberGroup.Delete("/:userID/roles/:roleID", requireActiveMember,
		permission.RequireServerPermission(s.permResolver, permissions.AssignRoles),
		memberHandler.RemoveRole)

	// === BAN ROUTES ===

	// Ban routes (require active membership)
	banGroup := serverGroup.Group("/bans", requireActiveMember,
		permission.RequireServerPermission(s.permResolver, permissions.BanMembers))
	banGroup.Get("/", memberHandler.ListBans)
	banGroup.Put("/:userID", memberHandler.BanMember)
	banGroup.Delete("/:userID", memberHandler.UnbanMember)

	// === MEDIA SERVING ===

	// Public media file serving (outside /api/v1/, no auth required). The UUID component of each storage key provides
	// sufficient entropy to prevent guessing. Path traversal is handled by os.Root inside LocalStorage, which rejects
	// any key that would escape the storage directory (including via symbolic links).
	if _, ok := s.storage.(*media.LocalStorage); ok {
		app.Get("/media/*", serveMediaFile(s.storage))
	}

	// === GATEWAY ===

	// Gateway WebSocket endpoint (unauthenticated; authentication happens inside the WebSocket via Identify/Resume).
	gatewayHandler := api.NewGatewayHandler(s.gatewayHub)
	app.Get("/api/v1/gateway", gatewayHandler.Upgrade)

	// Catch-all handler returns 404 for any request that does not match a defined route. Fiber v3 treats app.Use()
	// middleware as route matches, so without this terminal handler the router considers unmatched requests "handled"
	// and returns the default 200 status with an empty body.
	app.Use(func(_ fiber.Ctx) error {
		return fiber.ErrNotFound
	})
}
