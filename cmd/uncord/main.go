package main

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/signal"
	"runtime"
	"strings"
	"syscall"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/gofiber/fiber/v3"
	"github.com/gofiber/fiber/v3/middleware/cors"
	"github.com/gofiber/fiber/v3/middleware/limiter"
	"github.com/gofiber/fiber/v3/middleware/requestid"
	"github.com/redis/go-redis/v9"
	"github.com/rs/zerolog"
	"github.com/rs/zerolog/log"

	apierrors "github.com/uncord-chat/uncord-protocol/errors"

	"github.com/uncord-chat/uncord-server/internal/api"
	"github.com/uncord-chat/uncord-server/internal/attachment"
	"github.com/uncord-chat/uncord-server/internal/auth"
	"github.com/uncord-chat/uncord-server/internal/bootstrap"
	"github.com/uncord-chat/uncord-server/internal/category"
	"github.com/uncord-chat/uncord-server/internal/channel"
	"github.com/uncord-chat/uncord-server/internal/config"
	"github.com/uncord-chat/uncord-server/internal/disposable"
	"github.com/uncord-chat/uncord-server/internal/email"
	"github.com/uncord-chat/uncord-server/internal/gateway"
	"github.com/uncord-chat/uncord-server/internal/httputil"
	"github.com/uncord-chat/uncord-server/internal/invite"
	"github.com/uncord-chat/uncord-server/internal/media"
	"github.com/uncord-chat/uncord-server/internal/member"
	"github.com/uncord-chat/uncord-server/internal/message"
	"github.com/uncord-chat/uncord-server/internal/page"
	"github.com/uncord-chat/uncord-server/internal/permission"
	"github.com/uncord-chat/uncord-server/internal/postgres"
	"github.com/uncord-chat/uncord-server/internal/role"
	"github.com/uncord-chat/uncord-server/internal/search"
	servercfg "github.com/uncord-chat/uncord-server/internal/server"
	"github.com/uncord-chat/uncord-server/internal/typesense"
	"github.com/uncord-chat/uncord-server/internal/user"
	"github.com/uncord-chat/uncord-server/internal/valkey"

	"github.com/uncord-chat/uncord-protocol/permissions"
)

// Build metadata injected via ldflags at compile time.
var (
	version = "dev"
	commit  = "unknown"
	date    = "unknown"
)

// server holds the shared dependencies used by route handlers and middleware.
type server struct {
	cfg              *config.Config
	db               *pgxpool.Pool
	rdb              *redis.Client
	userRepo         user.Repository
	authService      *auth.Service
	serverRepo       servercfg.Repository
	channelRepo      channel.Repository
	categoryRepo     category.Repository
	roleRepo         role.Repository
	memberRepo       member.Repository
	inviteRepo       invite.Repository
	messageRepo      message.Repository
	attachmentRepo   attachment.Repository
	storage          media.StorageProvider
	permStore        permission.OverrideStore
	permReadStore    permission.Store
	permResolver     *permission.Resolver
	permPublisher    *permission.Publisher
	typesenseIndexer *typesense.Indexer
	gatewayPublisher *gateway.Publisher
	gatewayHub       *gateway.Hub
}

func main() {
	log.Logger = zerolog.New(os.Stderr).With().Timestamp().Logger()

	if err := run(); err != nil {
		log.Fatal().Err(err).Msg("Server stopped")
	}
}

func run() error {
	cfg, err := config.Load()
	if err != nil {
		return fmt.Errorf("load config: %w", err)
	}

	if cfg.IsDevelopment() {
		log.Logger = zerolog.New(zerolog.ConsoleWriter{Out: os.Stderr}).
			With().Timestamp().Logger()
	}

	log.Info().
		Str("version", version).
		Str("commit", commit).
		Str("built", date).
		Str("env", cfg.ServerEnv).
		Msg("Starting Uncord Server")

	if cfg.CORSAllowOrigins == "*" {
		log.Warn().Msg("CORS_ALLOW_ORIGINS is set to a wildcard. Set an explicit origin when in production.")
	}

	ctx := context.Background()

	// Connect PostgreSQL
	db, err := postgres.Connect(ctx, cfg.DatabaseURL, cfg.DatabaseMaxConn, cfg.DatabaseMinConn)
	if err != nil {
		return fmt.Errorf("connect postgres: %w", err)
	}
	defer db.Close()
	log.Info().Msg("PostgreSQL connected")

	// Run migrations
	if err := postgres.Migrate(cfg.DatabaseURL, log.Logger); err != nil {
		return fmt.Errorf("run migrations: %w", err)
	}
	log.Info().Msg("Database migrations complete")

	// Connect Valkey
	rdb, err := valkey.Connect(ctx, cfg.ValkeyURL, cfg.ValkeyDialTimeout)
	if err != nil {
		return fmt.Errorf("connect valkey: %w", err)
	}
	defer func() { _ = rdb.Close() }()
	log.Info().Msg("Valkey connected")

	// Check first-run and seed if needed
	firstRun, err := bootstrap.IsFirstRun(ctx, db)
	if err != nil {
		return fmt.Errorf("check first run: %w", err)
	}
	if firstRun {
		log.Info().Msg("First run detected, running initialization")
		if err := bootstrap.RunFirstInit(ctx, db, cfg, log.Logger); err != nil {
			return fmt.Errorf("first-run initialization: %w", err)
		}
		log.Info().Msg("First-run initialization complete")
	}

	// Typesense collection (best-effort)
	result, err := typesense.EnsureMessagesCollection(ctx, cfg.TypesenseURL, cfg.TypesenseAPIKey, cfg.TypesenseTimeout)
	if err != nil {
		log.Warn().Err(err).Msg("Typesense collection setup failed")
	} else {
		switch result {
		case typesense.ResultCreated:
			log.Info().Msg("Typesense messages collection created")
		case typesense.ResultRecreated:
			log.Warn().Msg("Typesense messages collection recreated due to schema change")
		case typesense.ResultUnchanged:
			log.Info().Msg("Typesense messages collection already exists")
		}
	}

	// Initialise disposable email blocklist with periodic refresh so newly added disposable domains are picked up
	// without requiring a server restart. Prefetch is called synchronously so the cache is warm before the server
	// begins accepting requests.
	blocklist := disposable.NewBlocklist(cfg.DisposableEmailBlocklistURL, cfg.DisposableEmailBlocklistEnabled, cfg.DisposableEmailBlocklistTimeout, log.Logger)
	blocklist.Prefetch(ctx)

	// Initialise permission engine
	permStore := permission.NewPGStore(db)
	permCache := permission.NewValkeyCache(rdb)
	permResolver := permission.NewResolver(permStore, permCache, log.Logger)
	permPublisher := permission.NewPublisher(rdb)

	// Initialise user repository early because the background purge goroutine needs it.
	userRepo := user.NewPGRepository(db, log.Logger)

	// Start background services with a shared cancellable context.
	subCtx, subCancel := context.WithCancel(ctx)

	go blocklist.Run(subCtx, cfg.DisposableEmailBlocklistRefreshInterval)

	// The purge goroutine is started below after the attachment repository is initialised, because orphan attachment
	// cleanup needs access to the repo and storage provider.
	startPurgeGoroutine := func(attachRepo *attachment.PGRepository, storage media.StorageProvider) {
		go func() {
			purgeExpiredData(subCtx, userRepo, attachRepo, storage, cfg)

			ticker := time.NewTicker(cfg.DataCleanupInterval)
			defer ticker.Stop()
			for {
				select {
				case <-subCtx.Done():
					return
				case <-ticker.C:
					purgeExpiredData(subCtx, userRepo, attachRepo, storage, cfg)
				}
			}
		}()
	}

	// Start permission cache invalidation subscriber with reconnection.
	defer subCancel()
	permSub := permission.NewSubscriber(permCache, rdb, log.Logger)
	go runWithBackoff(subCtx, "permission-cache-subscriber", permSub.Run)

	// SMTP client for transactional email (verification, password reset, etc.)
	var emailSender auth.Sender
	if cfg.SMTPConfigured() {
		emailClient := email.NewClient(cfg.SMTPHost, cfg.SMTPPort, cfg.SMTPUsername, cfg.SMTPPassword, cfg.SMTPFrom)
		if err := emailClient.Ping(ctx); err != nil {
			log.Warn().Err(err).Msg("SMTP connection test failed. Verification emails may not be delivered.")
		} else {
			log.Info().Str("host", cfg.SMTPHost).Int("port", cfg.SMTPPort).Msg("SMTP connection verified")
		}
		emailSender = emailClient
		if cfg.IsDevelopment() {
			log.Info().Msg("SMTP routed to Mailpit. View caught emails at http://localhost:8025")
		}
	} else {
		log.Warn().Msg("SMTP_HOST is not configured. Email verification will only work in development mode (token logged to console).")
	}

	// Initialise storage provider.
	var storage media.StorageProvider
	switch cfg.StorageBackend {
	case "local":
		storage = media.NewLocalStorage(cfg.StorageLocalPath, cfg.ServerURL)
		log.Info().Str("path", cfg.StorageLocalPath).Msg("Local file storage initialised")
	default:
		return fmt.Errorf("unsupported storage backend: %q", cfg.StorageBackend)
	}

	// Initialise remaining repositories and services
	serverRepo := servercfg.NewPGRepository(db, log.Logger)
	channelRepo := channel.NewPGRepository(db, log.Logger)
	categoryRepo := category.NewPGRepository(db, log.Logger)
	roleRepo := role.NewPGRepository(db, log.Logger)
	memberRepo := member.NewPGRepository(db, log.Logger)
	inviteRepo := invite.NewPGRepository(db, log.Logger)
	messageRepo := message.NewPGRepository(db, log.Logger)
	attachmentRepo := attachment.NewPGRepository(db, log.Logger)
	typesenseIndexer := typesense.NewIndexer(cfg.TypesenseURL, cfg.TypesenseAPIKey, cfg.TypesenseTimeout)
	gatewayPub := gateway.NewPublisher(rdb, log.Logger)
	startPurgeGoroutine(attachmentRepo, storage)

	// Start thumbnail worker with reconnection.
	thumbWorker := media.NewThumbnailWorker(rdb, storage, attachmentRepo, log.Logger)
	thumbWorker.EnsureStream(subCtx)
	go runWithBackoff(subCtx, "thumbnail-worker", thumbWorker.Run)
	authService, err := auth.NewService(userRepo, rdb, cfg, blocklist, emailSender, serverRepo, permPublisher, log.Logger)
	if err != nil {
		log.Fatal().Err(err).Msg("Failed to create auth service")
	}

	// Initialise gateway WebSocket hub and start the pub/sub subscriber with reconnection.
	sessionStore := gateway.NewSessionStore(rdb, cfg.GatewaySessionTTL, cfg.GatewayReplayBufferSize)
	gatewayHub := gateway.NewHub(rdb, cfg, sessionStore, permResolver, userRepo, serverRepo, channelRepo, roleRepo, memberRepo, log.Logger)
	go runWithBackoff(subCtx, "gateway-hub", gatewayHub.Run)

	// Create Fiber app
	app := fiber.New(fiber.Config{
		AppName:   "Uncord",
		BodyLimit: cfg.BodyLimitBytes(),
		// ErrorHandler catches errors returned by handlers that are not already mapped to structured API responses
		// (e.g. Fiber's built-in 404/405). errors.AsType is a generic helper added in Go 1.26.
		ErrorHandler: func(c fiber.Ctx, err error) error {
			status := fiber.StatusInternalServerError
			message := "An internal error occurred"
			apiCode := apierrors.InternalError
			if e, ok := errors.AsType[*fiber.Error](err); ok {
				status = e.Code
				message = e.Message
				apiCode = fiberStatusToAPICode(e.Code)
			} else {
				log.Error().Err(err).
					Str("method", c.Method()).
					Str("path", c.Path()).
					Msg("Unhandled error")
			}
			return c.Status(status).JSON(httputil.ErrorResponse{
				Error: httputil.ErrorBody{
					Code:    apiCode,
					Message: message,
				},
			})
		},
	})

	// Global middleware
	app.Use(requestid.New())
	if cfg.LogHealthRequests {
		app.Use(httputil.RequestLogger(log.Logger))
	} else {
		app.Use(httputil.RequestLogger(log.Logger, "/api/v1/health"))
	}
	app.Use(cors.New(cors.Config{
		AllowOrigins:  strings.Split(cfg.CORSAllowOrigins, ","),
		AllowMethods:  []string{"GET", "POST", "PUT", "PATCH", "DELETE", "OPTIONS"},
		AllowHeaders:  []string{"Origin", "Content-Type", "Accept", "Authorization"},
		ExposeHeaders: []string{"X-Request-ID"},
	}))

	// Global API rate limiter
	app.Use(limiter.New(limiter.Config{
		Max:        cfg.RateLimitAPIRequests,
		Expiration: time.Duration(cfg.RateLimitAPIWindowSeconds) * time.Second,
	}))

	// Register routes
	srv := &server{
		cfg:              cfg,
		db:               db,
		rdb:              rdb,
		userRepo:         userRepo,
		serverRepo:       serverRepo,
		channelRepo:      channelRepo,
		categoryRepo:     categoryRepo,
		roleRepo:         roleRepo,
		memberRepo:       memberRepo,
		inviteRepo:       inviteRepo,
		messageRepo:      messageRepo,
		attachmentRepo:   attachmentRepo,
		storage:          storage,
		authService:      authService,
		permStore:        permStore,
		permReadStore:    permStore,
		permResolver:     permResolver,
		permPublisher:    permPublisher,
		typesenseIndexer: typesenseIndexer,
		gatewayPublisher: gatewayPub,
		gatewayHub:       gatewayHub,
	}
	srv.registerRoutes(app)

	// Graceful shutdown
	quit := make(chan os.Signal, 1)
	signal.Notify(quit, syscall.SIGINT, syscall.SIGTERM)

	go func() {
		<-quit
		log.Info().Msg("Shutting down server")
		gatewayHub.Shutdown()
		subCancel()
		shutdownCtx, shutdownCancel := context.WithTimeout(context.Background(), 15*time.Second)
		defer shutdownCancel()
		if err := app.ShutdownWithContext(shutdownCtx); err != nil {
			log.Error().Err(err).Msg("Server shutdown error")
		}
	}()

	// Listen
	addr := fmt.Sprintf(":%d", cfg.ServerPort)
	log.Info().Str("addr", addr).Msg("Server listening")

	var mem runtime.MemStats
	runtime.ReadMemStats(&mem)
	log.Debug().
		Uint64("alloc_mb", mem.Alloc/1024/1024).
		Uint64("sys_mb", mem.Sys/1024/1024).
		Uint64("heap_inuse_mb", mem.HeapInuse/1024/1024).
		Uint64("stack_inuse_mb", mem.StackInuse/1024/1024).
		Uint32("num_gc", mem.NumGC).
		Msg("Runtime memory stats")

	if err := app.Listen(addr, fiber.ListenConfig{DisableStartupMessage: true}); err != nil {
		return fmt.Errorf("server error: %w", err)
	}

	return nil
}

func (s *server) registerRoutes(app *fiber.App) {
	requireAuth := auth.RequireAuth(s.cfg.JWTSecret, s.cfg.ServerURL)
	requireVerified := auth.RequireVerifiedEmail(s.userRepo)
	requireActive := member.RequireActiveMember(s.memberRepo)

	// Browser-facing email verification page (outside /api/v1/ because users click this link directly from email)
	verifyHandler := page.NewVerifyHandler(s.authService, s.cfg.ServerName, log.Logger)
	app.Get("/verify-email", limiter.New(limiter.Config{
		Max:        s.cfg.RateLimitAuthCount,
		Expiration: time.Duration(s.cfg.RateLimitAuthWindowSeconds) * time.Second,
	}), verifyHandler.VerifyEmail)

	health := api.NewHealthHandler(s.db, redisPinger{client: s.rdb})
	app.Get("/api/v1/health", health.Health)

	authHandler := api.NewAuthHandler(s.authService, log.Logger)

	// Auth routes with stricter rate limiting (public, no email/member checks)
	authGroup := app.Group("/api/v1/auth")
	authGroup.Use(limiter.New(limiter.Config{
		Max:        s.cfg.RateLimitAuthCount,
		Expiration: time.Duration(s.cfg.RateLimitAuthWindowSeconds) * time.Second,
	}))
	authGroup.Post("/register", authHandler.Register)
	authGroup.Post("/login", authHandler.Login)
	authGroup.Post("/refresh", authHandler.Refresh)
	authGroup.Post("/verify-email", authHandler.VerifyEmail)
	authGroup.Post("/mfa/verify", authHandler.MFAVerify)
	authGroup.Post("/verify-password", requireAuth, authHandler.VerifyPassword)

	// User profile routes (authenticated + verified email, no member check required)
	userHandler := api.NewUserHandler(s.userRepo, s.authService, log.Logger)
	userGroup := app.Group("/api/v1/users", requireAuth, requireVerified)
	userGroup.Get("/@me", userHandler.GetMe)
	userGroup.Patch("/@me", userHandler.UpdateMe)
	userGroup.Delete("/@me", userHandler.DeleteMe)

	// MFA management routes (authenticated + verified email)
	mfaHandler := api.NewMFAHandler(s.authService, log.Logger)
	mfaGroup := userGroup.Group("/@me/mfa")
	mfaGroup.Post("/enable", mfaHandler.Enable)
	mfaGroup.Post("/confirm", mfaHandler.Confirm)
	mfaGroup.Post("/disable", mfaHandler.Disable)
	mfaGroup.Post("/recovery-codes", mfaHandler.RegenerateCodes)

	// Server config routes (authenticated + verified email)
	serverHandler := api.NewServerHandler(s.serverRepo, log.Logger)
	app.Get("/api/v1/server/info", serverHandler.GetPublicInfo)
	serverGroup := app.Group("/api/v1/server", requireAuth, requireVerified)
	serverGroup.Get("/", serverHandler.Get)
	serverGroup.Patch("/", requireActive,
		permission.RequireServerPermission(s.permResolver, permissions.ManageServer), serverHandler.Update)

	// Channel routes (server group: list is open to pending, create requires active)
	channelHandler := api.NewChannelHandler(s.channelRepo, s.memberRepo, s.inviteRepo, s.permResolver, s.cfg.MaxChannels, log.Logger)
	serverGroup.Get("/channels", channelHandler.ListChannels)
	serverGroup.Post("/channels", requireActive,
		permission.RequireServerPermission(s.permResolver, permissions.ManageChannels),
		channelHandler.CreateChannel)

	// Channel routes (standalone group: all routes require active membership)
	channelGroup := app.Group("/api/v1/channels", requireAuth, requireVerified, requireActive)
	channelGroup.Get("/:channelID",
		permission.RequirePermission(s.permResolver, permissions.ViewChannels),
		channelHandler.GetChannel)
	channelGroup.Patch("/:channelID",
		permission.RequirePermission(s.permResolver, permissions.ManageChannels),
		channelHandler.UpdateChannel)
	channelGroup.Delete("/:channelID",
		permission.RequirePermission(s.permResolver, permissions.ManageChannels),
		channelHandler.DeleteChannel)

	// Permission override routes
	permHandler := api.NewPermissionHandler(s.permStore, s.permResolver, s.permPublisher, log.Logger)
	channelGroup.Put("/:channelID/overrides/:targetID",
		permission.RequireServerPermission(s.permResolver, permissions.ManageRoles),
		permHandler.SetOverride)
	channelGroup.Delete("/:channelID/overrides/:targetID",
		permission.RequireServerPermission(s.permResolver, permissions.ManageRoles),
		permHandler.DeleteOverride)
	channelGroup.Get("/:channelID/permissions/@me",
		permHandler.GetMyPermissions)

	// Attachment upload route (nested under channels, inherits active requirement)
	attachmentHandler := api.NewAttachmentHandler(
		s.attachmentRepo, s.storage, s.rdb, s.cfg.MaxUploadSizeBytes(), log.Logger)
	channelGroup.Post("/:channelID/attachments",
		limiter.New(limiter.Config{
			Max:        s.cfg.RateLimitUploadCount,
			Expiration: time.Duration(s.cfg.RateLimitUploadWindowSeconds) * time.Second,
		}),
		permission.RequirePermission(s.permResolver, permissions.AttachFiles),
		attachmentHandler.Upload)

	// Message routes (nested under channels for list and create, inherits active requirement)
	messageHandler := api.NewMessageHandler(
		s.messageRepo, s.attachmentRepo, s.storage, s.permResolver, s.typesenseIndexer, s.gatewayPublisher,
		s.cfg.MaxMessageLength, s.cfg.MaxAttachmentsPerMessage, log.Logger)
	channelGroup.Get("/:channelID/messages",
		permission.RequirePermission(s.permResolver, permissions.ViewChannels|permissions.ReadMessageHistory),
		messageHandler.ListMessages)
	channelGroup.Post("/:channelID/messages",
		permission.RequirePermission(s.permResolver, permissions.SendMessages),
		messageHandler.CreateMessage)

	// Message routes (standalone for edit and delete, require active membership)
	messageGroup := app.Group("/api/v1/messages", requireAuth, requireVerified, requireActive)
	messageGroup.Patch("/:messageID", messageHandler.EditMessage)
	messageGroup.Delete("/:messageID", messageHandler.DeleteMessage)

	// Search routes (require active membership)
	searchSearcher := search.NewTypesenseSearcher(s.cfg.TypesenseURL, s.cfg.TypesenseAPIKey, s.cfg.TypesenseTimeout)
	searchService := search.NewService(s.channelRepo, s.permResolver, searchSearcher, log.Logger)
	searchHandler := api.NewSearchHandler(searchService, log.Logger)
	app.Get("/api/v1/search/messages", requireAuth, requireVerified, requireActive,
		searchHandler.SearchMessages)

	// Category routes (server group routes need per-route active, standalone group requires active)
	categoryHandler := api.NewCategoryHandler(s.categoryRepo, s.cfg.MaxCategories, log.Logger)
	serverGroup.Get("/categories", requireActive, categoryHandler.ListCategories)
	serverGroup.Post("/categories", requireActive,
		permission.RequireServerPermission(s.permResolver, permissions.ManageCategories),
		categoryHandler.CreateCategory)

	categoryGroup := app.Group("/api/v1/categories", requireAuth, requireVerified, requireActive)
	categoryGroup.Patch("/:categoryID",
		permission.RequireServerPermission(s.permResolver, permissions.ManageCategories),
		categoryHandler.UpdateCategory)
	categoryGroup.Delete("/:categoryID",
		permission.RequireServerPermission(s.permResolver, permissions.ManageCategories),
		categoryHandler.DeleteCategory)

	// Role routes (all require active membership)
	roleHandler := api.NewRoleHandler(s.roleRepo, s.permPublisher, s.cfg.MaxRoles, log.Logger)
	serverGroup.Get("/roles", requireActive, roleHandler.ListRoles)
	serverGroup.Post("/roles", requireActive,
		permission.RequireServerPermission(s.permResolver, permissions.ManageRoles),
		roleHandler.CreateRole)
	serverGroup.Patch("/roles/:roleID", requireActive,
		permission.RequireServerPermission(s.permResolver, permissions.ManageRoles),
		roleHandler.UpdateRole)
	serverGroup.Delete("/roles/:roleID", requireActive,
		permission.RequireServerPermission(s.permResolver, permissions.ManageRoles),
		roleHandler.DeleteRole)

	// Invite management routes (under /api/v1/server, require active membership)
	inviteHandler := api.NewInviteHandler(s.inviteRepo, s.memberRepo, s.userRepo, log.Logger)
	serverGroup.Post("/invites", requireActive,
		permission.RequireServerPermission(s.permResolver, permissions.CreateInvites),
		inviteHandler.CreateInvite)
	serverGroup.Get("/invites", requireActive,
		permission.RequireServerPermission(s.permResolver, permissions.ManageInvites),
		inviteHandler.ListInvites)

	// Invite action routes (under /api/v1/invites, authenticated + verified email)
	inviteGroup := app.Group("/api/v1/invites", requireAuth, requireVerified)
	inviteGroup.Delete("/:code", requireActive,
		permission.RequireServerPermission(s.permResolver, permissions.ManageInvites),
		inviteHandler.DeleteInvite)
	inviteGroup.Post("/:code/join", inviteHandler.JoinViaInvite)

	// Onboarding route (authenticated + verified email, no active member check)
	app.Post("/api/v1/onboarding/accept", requireAuth, requireVerified,
		inviteHandler.AcceptOnboarding)

	// Member routes (mixed: some require active, some do not)
	memberHandler := api.NewMemberHandler(s.memberRepo, s.roleRepo, s.permReadStore, s.permPublisher, log.Logger)
	memberGroup := serverGroup.Group("/members")
	memberGroup.Get("/", requireActive, memberHandler.ListMembers)
	memberGroup.Get("/@me", memberHandler.GetSelf)
	memberGroup.Patch("/@me", requireActive,
		permission.RequireServerPermission(s.permResolver, permissions.ChangeNicknames),
		memberHandler.UpdateSelf)
	memberGroup.Delete("/@me", memberHandler.Leave)
	memberGroup.Get("/:userID", requireActive, memberHandler.GetMember)
	memberGroup.Patch("/:userID", requireActive,
		permission.RequireServerPermission(s.permResolver, permissions.ManageNicknames),
		memberHandler.UpdateMember)
	memberGroup.Delete("/:userID", requireActive,
		permission.RequireServerPermission(s.permResolver, permissions.KickMembers),
		memberHandler.KickMember)
	memberGroup.Put("/:userID/timeout", requireActive,
		permission.RequireServerPermission(s.permResolver, permissions.TimeoutMembers),
		memberHandler.SetTimeout)
	memberGroup.Delete("/:userID/timeout", requireActive,
		permission.RequireServerPermission(s.permResolver, permissions.TimeoutMembers),
		memberHandler.ClearTimeout)
	memberGroup.Put("/:userID/roles/:roleID", requireActive,
		permission.RequireServerPermission(s.permResolver, permissions.AssignRoles),
		memberHandler.AssignRole)
	memberGroup.Delete("/:userID/roles/:roleID", requireActive,
		permission.RequireServerPermission(s.permResolver, permissions.AssignRoles),
		memberHandler.RemoveRole)

	// Ban routes (require active membership)
	banGroup := serverGroup.Group("/bans", requireActive,
		permission.RequireServerPermission(s.permResolver, permissions.BanMembers))
	banGroup.Get("/", memberHandler.ListBans)
	banGroup.Put("/:userID", memberHandler.BanMember)
	banGroup.Delete("/:userID", memberHandler.UnbanMember)

	// Public media file serving (outside /api/v1/, no auth required). The UUID component of each storage key provides
	// sufficient entropy to prevent guessing. Directory traversal is prevented by Fiber's path parameter sanitisation.
	if _, ok := s.storage.(*media.LocalStorage); ok {
		app.Get("/media/*", func(c fiber.Ctx) error {
			key := c.Params("*")
			if key == "" || strings.Contains(key, "..") {
				return fiber.ErrNotFound
			}
			rc, err := s.storage.Get(c.Context(), key)
			if err != nil {
				return fiber.ErrNotFound
			}
			defer func() { _ = rc.Close() }()

			// Set a long cache header since attachment URLs include a unique UUID.
			c.Set("Cache-Control", "public, max-age=31536000, immutable")
			return c.SendStream(rc)
		})
	}

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

// redisPinger adapts *redis.Client to the api.Pinger interface.
type redisPinger struct{ client *redis.Client }

func (p redisPinger) Ping(ctx context.Context) error { return p.client.Ping(ctx).Err() }

// purgeExpiredData deletes stale login attempts, deletion tombstones, and orphaned attachments. Each call logs the
// outcome so operators can monitor retention enforcement.
func purgeExpiredData(ctx context.Context, repo *user.PGRepository, attachRepo *attachment.PGRepository, storage media.StorageProvider, cfg *config.Config) {
	deleted, err := repo.PurgeLoginAttempts(ctx, time.Now().Add(-cfg.LoginAttemptRetention))
	if err != nil {
		log.Warn().Err(err).Msg("Failed to purge expired login attempts")
	} else if deleted > 0 {
		log.Info().Int64("deleted", deleted).Dur("retention", cfg.LoginAttemptRetention).Msg("Purged expired login attempts")
	}

	if cfg.DeletionTombstoneRetention > 0 {
		deleted, err := repo.PurgeTombstones(ctx, time.Now().Add(-cfg.DeletionTombstoneRetention))
		if err != nil {
			log.Warn().Err(err).Msg("Failed to purge expired deletion tombstones")
		} else if deleted > 0 {
			log.Info().Int64("deleted", deleted).Dur("retention", cfg.DeletionTombstoneRetention).
				Msg("Purged expired deletion tombstones")
		}
	}

	// Purge orphaned attachments (uploaded but never linked to a message).
	orphanKeys, err := attachRepo.PurgeOrphans(ctx, time.Now().Add(-cfg.AttachmentOrphanTTL))
	if err != nil {
		log.Warn().Err(err).Msg("Failed to purge orphaned attachments")
	} else if len(orphanKeys) > 0 {
		for _, key := range orphanKeys {
			if delErr := storage.Delete(ctx, key); delErr != nil {
				log.Warn().Err(delErr).Str("key", key).Msg("Failed to delete orphaned attachment file")
			}
		}
		log.Info().Int("deleted", len(orphanKeys)).Dur("ttl", cfg.AttachmentOrphanTTL).
			Msg("Purged orphaned attachment files")
	}
}

// runWithBackoff runs fn in a loop, restarting with exponential backoff when it returns a non-nil, non-cancelled error.
// If fn returns nil or context.Canceled the goroutine exits. The delay starts at 1 second and doubles on each
// consecutive failure up to a 2-minute cap.
func runWithBackoff(ctx context.Context, name string, fn func(context.Context) error) {
	const (
		initialDelay = time.Second
		maxDelay     = 2 * time.Minute
	)
	delay := initialDelay
	for {
		if err := fn(ctx); err != nil {
			if errors.Is(err, context.Canceled) {
				return
			}
			log.Error().Err(err).Str("service", name).Dur("retry_in", delay).
				Msg("Background service stopped, restarting after delay")
			select {
			case <-ctx.Done():
				return
			case <-time.After(delay):
			}
			delay = min(delay*2, maxDelay)
			continue
		}
		return
	}
}

// fiberStatusToAPICode maps an HTTP status code from Fiber's built-in errors (404, 405, etc.) to the closest protocol
// error code.
func fiberStatusToAPICode(status int) apierrors.Code {
	switch status {
	case fiber.StatusNotFound:
		return apierrors.NotFound
	case fiber.StatusMethodNotAllowed:
		return apierrors.ValidationError
	case fiber.StatusTooManyRequests:
		return apierrors.RateLimited
	case fiber.StatusRequestEntityTooLarge:
		return apierrors.PayloadTooLarge
	case fiber.StatusServiceUnavailable:
		return apierrors.ServiceUnavailable
	default:
		if status >= 400 && status < 500 {
			return apierrors.ValidationError
		}
		return apierrors.InternalError
	}
}
