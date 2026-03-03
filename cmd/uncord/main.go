package main

import (
	"context"
	"errors"
	"fmt"
	"html/template"
	"io/fs"
	"mime"
	"os"
	"os/signal"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"syscall"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/gofiber/fiber/v3"
	"github.com/gofiber/fiber/v3/middleware/cors"
	"github.com/gofiber/fiber/v3/middleware/limiter"
	"github.com/gofiber/fiber/v3/middleware/requestid"
	"github.com/gofiber/fiber/v3/middleware/timeout"
	"github.com/redis/go-redis/v9"
	"github.com/rs/zerolog"
	"github.com/rs/zerolog/log"

	apierrors "github.com/uncord-chat/uncord-protocol/errors"

	"github.com/uncord-chat/uncord-server/internal/attachment"
	"github.com/uncord-chat/uncord-server/internal/audit"
	"github.com/uncord-chat/uncord-server/internal/auth"
	"github.com/uncord-chat/uncord-server/internal/bootstrap"
	"github.com/uncord-chat/uncord-server/internal/category"
	"github.com/uncord-chat/uncord-server/internal/channel"
	"github.com/uncord-chat/uncord-server/internal/config"
	"github.com/uncord-chat/uncord-server/internal/disposable"
	"github.com/uncord-chat/uncord-server/internal/dm"
	"github.com/uncord-chat/uncord-server/internal/e2ee"
	"github.com/uncord-chat/uncord-server/internal/email"
	"github.com/uncord-chat/uncord-server/internal/emoji"
	"github.com/uncord-chat/uncord-server/internal/gateway"
	"github.com/uncord-chat/uncord-server/internal/httputil"
	"github.com/uncord-chat/uncord-server/internal/invite"
	"github.com/uncord-chat/uncord-server/internal/media"
	"github.com/uncord-chat/uncord-server/internal/member"
	"github.com/uncord-chat/uncord-server/internal/message"
	"github.com/uncord-chat/uncord-server/internal/onboarding"
	"github.com/uncord-chat/uncord-server/internal/permission"
	"github.com/uncord-chat/uncord-server/internal/postgres"
	"github.com/uncord-chat/uncord-server/internal/presence"
	"github.com/uncord-chat/uncord-server/internal/reaction"
	"github.com/uncord-chat/uncord-server/internal/readstate"
	"github.com/uncord-chat/uncord-server/internal/role"
	servercfg "github.com/uncord-chat/uncord-server/internal/server"
	"github.com/uncord-chat/uncord-server/internal/thread"
	"github.com/uncord-chat/uncord-server/internal/typesense"
	"github.com/uncord-chat/uncord-server/internal/user"
	"github.com/uncord-chat/uncord-server/internal/valkey"
)

// Build metadata injected via ldflags at compile time.
var (
	version = "dev"
	commit  = "unknown"
	date    = "unknown"
)

// server holds the shared dependencies used by route handlers and middleware. All fields are injected during startup in
// run() and remain read-only for the lifetime of the process.
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
	onboardingRepo   onboarding.Repository
	documentStore    *onboarding.DocumentStore
	messageRepo      message.Repository
	threadRepo       thread.Repository
	attachmentRepo   attachment.Repository
	emojiRepo        emoji.Repository
	reactionRepo     reaction.Repository
	readStateRepo    readstate.Repository
	dmRepo           dm.Repository
	e2eeRepo         e2ee.Repository
	storage          media.StorageProvider
	permStore        *permission.PGStore
	permResolver     *permission.Resolver
	permPublisher    *permission.Publisher
	typesenseIndexer *typesense.Indexer
	gatewayPublisher *gateway.Publisher
	gatewayHub       *gateway.Hub
	presenceStore    *presence.Store
	auditRepo        audit.Repository
	auditLogger      *audit.Logger
	verifyPageTmpl   *template.Template
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
	db, err := postgres.Connect(ctx, cfg.DatabaseURL.Expose(), cfg.DatabaseMaxConn, cfg.DatabaseMinConn)
	if err != nil {
		return fmt.Errorf("connect postgres: %w", err)
	}
	defer db.Close()
	log.Info().Msg("PostgreSQL connected")

	// Run migrations
	if err := postgres.Migrate(cfg.DatabaseURL.Expose(), log.Logger); err != nil {
		return fmt.Errorf("run migrations: %w", err)
	}
	log.Info().Msg("Database migrations complete")

	// Connect Valkey
	rdb, err := valkey.Connect(ctx, cfg.ValkeyURL.Expose(), cfg.ValkeyDialTimeout)
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

	// Typesense collection (best-effort). Search is non-essential; the server runs without it but message search will
	// be unavailable. The searchAvailable flag is logged in the startup summary so operators immediately see whether
	// search is degraded.
	var searchAvailable bool
	result, err := typesense.EnsureMessagesCollection(ctx, cfg.TypesenseURL, cfg.TypesenseAPIKey.Expose(), cfg.TypesenseTimeout)
	if err != nil {
		log.Warn().Err(err).Msg("Typesense collection setup failed; message search will be unavailable")
	} else {
		searchAvailable = true
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
	// begins accepting requests. A 30-second timeout prevents a slow or unreachable upstream from blocking startup
	// indefinitely; if the fetch fails, IsBlocked retries lazily on first use.
	blocklist := disposable.NewBlocklist(cfg.DisposableEmailBlocklistURL, cfg.DisposableEmailBlocklistEnabled, cfg.DisposableEmailBlocklistTimeout, log.Logger)
	prefetchCtx, prefetchCancel := context.WithTimeout(ctx, 30*time.Second)
	defer prefetchCancel()
	blocklist.Prefetch(prefetchCtx)

	// Initialise permission engine
	permStore := permission.NewPGStore(db)
	permCache := permission.NewValkeyCache(rdb, log.Logger)
	permResolver := permission.NewResolver(permStore, permCache, log.Logger)
	permPublisher := permission.NewPublisher(rdb)

	// Initialise user repository early because the background purge goroutine needs it.
	userRepo := user.NewPGRepository(db)

	// Start background services with a shared cancellable context. The WaitGroup ensures all goroutines have returned
	// before the process exits.
	subCtx, subCancel := context.WithCancel(ctx)
	defer subCancel()
	var wg sync.WaitGroup

	safeGo(&wg, func() {
		blocklist.Run(subCtx, cfg.DisposableEmailBlocklistRefreshInterval)
	})

	// The purge goroutine is started below after the attachment repository is initialised, because orphan attachment
	// cleanup needs access to the repo and storage provider.
	startPurgeGoroutine := func(attachRepo *attachment.PGRepository, storage media.StorageProvider) {
		safeGo(&wg, func() {
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
		})
	}

	// Start permission cache invalidation subscriber with reconnection.
	permSub := permission.NewSubscriber(permCache, rdb, log.Logger)
	safeGo(&wg, func() {
		runWithBackoff(subCtx, "permission-cache-subscriber", permSub.Run)
	})

	// Load external templates from DATA_DIR (nil means use compiled-in defaults).
	verificationTmpl, verifyPageTmpl, err := loadTemplates(cfg.DataDir)
	if err != nil {
		return fmt.Errorf("load templates: %w", err)
	}

	// Load onboarding documents from DATA_DIR.
	var documentStore *onboarding.DocumentStore
	if cfg.DataDir != "" {
		documentStore, err = onboarding.LoadDocuments(filepath.Join(cfg.DataDir, "onboarding"))
		if err != nil {
			return fmt.Errorf("load onboarding documents: %w", err)
		}
		log.Info().Int("count", len(documentStore.Documents())).Msg("Loaded onboarding documents")
	} else {
		documentStore = onboarding.EmptyDocumentStore()
	}

	// SMTP client for transactional email (verification, password reset, etc.)
	var emailSender auth.Sender
	if cfg.SMTPConfigured() {
		emailClient := email.NewClient(cfg.SMTPHost, cfg.SMTPPort, cfg.SMTPUsername, cfg.SMTPPassword.Expose(), cfg.SMTPFrom, verificationTmpl)
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
		localStorage, storageErr := media.NewLocalStorage(cfg.StorageLocalPath, cfg.ServerURL)
		if storageErr != nil {
			return fmt.Errorf("initialise local storage: %w", storageErr)
		}
		defer func() { _ = localStorage.Close() }()
		storage = localStorage
		log.Info().Str("path", cfg.StorageLocalPath).Msg("Local file storage initialised")
	default:
		return fmt.Errorf("unsupported storage backend: %q", cfg.StorageBackend)
	}

	// Initialise remaining repositories and services
	serverRepo := servercfg.NewPGRepository(db)
	channelRepo := channel.NewPGRepository(db)
	categoryRepo := category.NewPGRepository(db)
	roleRepo := role.NewPGRepository(db)
	memberRepo := member.NewPGRepository(db)
	inviteRepo := invite.NewPGRepository(db)
	onboardingRepo := onboarding.NewPGRepository(db)
	messageRepo := message.NewPGRepository(db)
	threadRepo := thread.NewPGRepository(db)
	attachmentRepo := attachment.NewPGRepository(db)
	emojiRepo := emoji.NewPGRepository(db)
	reactionRepo := reaction.NewPGRepository(db)
	readStateRepo := readstate.NewPGRepository(db)
	dmRepo := dm.NewPGRepository(db)
	e2eeRepo := e2ee.NewPGRepository(db, cfg.E2EEMaxDevicesPerUser)
	typesenseIndexer := typesense.NewIndexer(cfg.TypesenseURL, cfg.TypesenseAPIKey.Expose(), cfg.TypesenseTimeout)
	gatewayPub := gateway.NewPublisher(rdb, log.Logger, cfg.GatewayPublishWorkers, cfg.GatewayPublishQueueSize, cfg.GatewayPublishTimeout)
	presenceStore := presence.NewStore(rdb)
	auditRepo := audit.NewPGRepository(db)
	auditLogger := audit.NewLogger(auditRepo, log.Logger)
	startPurgeGoroutine(attachmentRepo, storage)

	// Start thumbnail worker with reconnection.
	thumbWorker := media.NewThumbnailWorker(rdb, storage, attachmentRepo, log.Logger)
	thumbWorker.EnsureStream(subCtx)
	safeGo(&wg, func() {
		runWithBackoff(subCtx, "thumbnail-worker", thumbWorker.Run)
	})
	safeGo(&wg, func() {
		runWithBackoff(subCtx, "gateway-publisher", gatewayPub.Run)
	})
	authService, err := auth.NewService(userRepo, rdb, cfg, blocklist, emailSender, serverRepo, permPublisher, log.Logger)
	if err != nil {
		return fmt.Errorf("create auth service: %w", err)
	}

	// Initialise gateway WebSocket hub and start the pub/sub subscriber with reconnection.
	sessionStore := gateway.NewSessionStore(rdb, log.Logger, cfg.GatewaySessionTTL, cfg.GatewayReplayBufferSize)
	gatewayHub := gateway.NewHub(gateway.HubDeps{
		RDB:            rdb,
		Cfg:            cfg,
		Sessions:       sessionStore,
		Resolver:       permResolver,
		Users:          userRepo,
		Server:         serverRepo,
		Channels:       channelRepo,
		Roles:          roleRepo,
		Members:        memberRepo,
		ReadStates:     readStateRepo,
		Presence:       presenceStore,
		Publisher:      gatewayPub,
		OnboardingRepo: onboardingRepo,
		DocumentStore:  documentStore,
		Logger:         log.Logger,
	})
	safeGo(&wg, func() {
		runWithBackoff(subCtx, "gateway-hub", gatewayHub.Run)
	})

	app := newFiberApp(cfg)

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
		onboardingRepo:   onboardingRepo,
		documentStore:    documentStore,
		messageRepo:      messageRepo,
		threadRepo:       threadRepo,
		attachmentRepo:   attachmentRepo,
		emojiRepo:        emojiRepo,
		reactionRepo:     reactionRepo,
		readStateRepo:    readStateRepo,
		dmRepo:           dmRepo,
		e2eeRepo:         e2eeRepo,
		storage:          storage,
		authService:      authService,
		permStore:        permStore,
		permResolver:     permResolver,
		permPublisher:    permPublisher,
		typesenseIndexer: typesenseIndexer,
		gatewayPublisher: gatewayPub,
		gatewayHub:       gatewayHub,
		presenceStore:    presenceStore,
		auditRepo:        auditRepo,
		auditLogger:      auditLogger,
		verifyPageTmpl:   verifyPageTmpl,
	}
	srv.registerRoutes(app)

	// Graceful shutdown: the signal goroutine drains in-flight HTTP requests via app.ShutdownWithContext. A sync.Once
	// ensures the shutdown path executes exactly once regardless of whether a signal or a Listen error triggers it
	// first. All remaining cleanup happens sequentially in the main goroutine after Listen returns, eliminating races
	// between the signal handler and post-Listen teardown.
	quit := make(chan os.Signal, 1)
	signal.Notify(quit, syscall.SIGINT, syscall.SIGTERM)
	defer signal.Stop(quit)

	var shutdownOnce sync.Once
	shutdownApp := func() {
		shutdownCtx, shutdownCancel := context.WithTimeout(context.Background(), cfg.ShutdownTimeout)
		defer shutdownCancel()
		if err := app.ShutdownWithContext(shutdownCtx); err != nil {
			log.Error().Err(err).Msg("Server shutdown error")
		}
	}

	go func() {
		<-quit
		log.Info().Msg("Shutting down server")
		shutdownOnce.Do(shutdownApp)
	}()

	// Startup summary: log the status of optional services so operators can immediately see what is degraded.
	log.Info().
		Bool("search", searchAvailable).
		Bool("email", cfg.SMTPConfigured()).
		Str("storage", cfg.StorageBackend).
		Msg("Service status")

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

	listenErr := app.Listen(addr, fiber.ListenConfig{DisableStartupMessage: true})

	// Ensure the Fiber app is shut down even if Listen returned due to a startup error (e.g. port binding) rather
	// than a signal. The sync.Once guarantees this is a no-op if the signal handler already triggered shutdown.
	shutdownOnce.Do(shutdownApp)

	// Shutdown sequence: all steps run in the main goroutine after Listen returns, regardless of whether the return
	// was triggered by a signal or a startup failure (e.g. port binding). This eliminates races between the signal
	// handler and post-Listen teardown, and ensures shared resources (storage, database) are not closed until all
	// background goroutines have stopped.
	//
	// 1. Shut down the gateway hub (sends Reconnect frames to connected clients).
	// 2. Cancel the background service context so goroutines begin exiting.
	// 3. Wait for all goroutines to stop (with a grace timeout).
	// 4. run() returns and deferred Close calls release the database pool, Valkey client, and storage provider.
	gatewayHub.Shutdown()
	subCancel()

	waitDone := make(chan struct{})
	go func() { wg.Wait(); close(waitDone) }()
	select {
	case <-waitDone:
	case <-time.After(cfg.ShutdownGraceTimeout):
		log.Warn().Msg("Timed out waiting for background goroutines to stop")
	}

	if listenErr != nil {
		return fmt.Errorf("server error: %w", listenErr)
	}

	return nil
}

// newFiberApp creates the Fiber application with the global error handler and middleware stack.
func newFiberApp(cfg *config.Config) *fiber.App {
	app := fiber.New(fiber.Config{
		AppName:   "Uncord",
		BodyLimit: cfg.BodyLimitBytes(),
		// ErrorHandler catches errors returned by handlers that are not already mapped to structured API responses
		// (e.g. Fiber's built-in 404/405). errors.AsType is a generic helper added in Go 1.26.
		ErrorHandler: func(c fiber.Ctx, err error) error {
			status := fiber.StatusInternalServerError
			msg := "An internal error occurred"
			apiCode := apierrors.InternalError
			if e, ok := errors.AsType[*fiber.Error](err); ok {
				status = e.Code
				msg = e.Message
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
					Message: msg,
				},
			})
		},
	})

	app.Use(requestid.New())
	if cfg.LogHealthRequests {
		app.Use(httputil.RequestLogger(log.Logger))
	} else {
		app.Use(httputil.RequestLogger(log.Logger, "/api/v1/health"))
	}
	// CORS runs before the timeout so that preflight OPTIONS responses are not subject to the request deadline.
	corsConfig := cors.Config{
		AllowMethods:     []string{"GET", "POST", "PUT", "PATCH", "DELETE", "OPTIONS"},
		AllowHeaders:     []string{"Origin", "Content-Type", "Accept", "Authorization", "X-CSRF-Token"},
		ExposeHeaders:    []string{"X-Request-ID"},
		AllowCredentials: true,
	}
	if cfg.IsDevelopment() && cfg.CORSAllowOrigins == "*" {
		// AllowCredentials with a literal "*" origin causes Fiber to panic. In development, dynamically allow all
		// origins by echoing the request's Origin header.
		corsConfig.AllowOriginsFunc = func(string) bool { return true }
	} else {
		origins := strings.Split(cfg.CORSAllowOrigins, ",")
		for i := range origins {
			origins[i] = strings.TrimSpace(origins[i])
		}
		corsConfig.AllowOrigins = origins
	}
	app.Use(cors.New(corsConfig))

	// Enforce a per-request timeout on all REST handlers. WebSocket upgrade requests are excluded because the gateway
	// manages its own connection lifecycle. The timeout middleware runs the remaining handler chain in a goroutine and
	// returns 408 if processing exceeds the configured duration.
	app.Use(timeout.New(func(c fiber.Ctx) error { return c.Next() }, timeout.Config{
		Timeout: cfg.RequestTimeout,
		Next: func(c fiber.Ctx) bool {
			return strings.EqualFold(c.Get(fiber.HeaderUpgrade), "websocket")
		},
		OnTimeout: func(c fiber.Ctx) error {
			return c.Status(fiber.StatusRequestTimeout).JSON(httputil.ErrorResponse{
				Error: httputil.ErrorBody{
					Code:    apierrors.InternalError,
					Message: "Request timed out",
				},
			})
		},
	}))

	// Restrict non-upload request bodies to a sensible size for JSON payloads. Multipart (file upload) requests are
	// exempt because they are protected by the global Fiber body limit and per-handler size checks instead.
	app.Use(jsonBodyLimit(cfg.BodyLimitJSONBytes()))

	app.Use(limiter.New(limiter.Config{
		Max:          cfg.RateLimitAPIRequests,
		Expiration:   time.Duration(cfg.RateLimitAPIWindowSeconds) * time.Second,
		LimitReached: rateLimitReached,
	}))

	return app
}

// loadTemplates loads optional email and page templates from the data directory. Returns nil templates when dataDir is
// empty, which causes the application to use compiled-in defaults. Templates are trusted input from the server operator;
// html/template auto-escapes rendered values but does not guard against arbitrary actions in the template definitions.
func loadTemplates(dataDir string) (verification *template.Template, verifyPage *template.Template, err error) {
	if dataDir == "" {
		return nil, nil, nil
	}

	emailTmplPath := filepath.Join(dataDir, "templates", "email", "verification.html")
	if data, readErr := os.ReadFile(emailTmplPath); readErr == nil {
		verification, err = template.New("verification").Parse(string(data))
		if err != nil {
			return nil, nil, fmt.Errorf("parse email verification template: %w", err)
		}
		log.Info().Str("path", emailTmplPath).Msg("Loaded email verification template from data directory")
	} else if !errors.Is(readErr, fs.ErrNotExist) {
		return nil, nil, fmt.Errorf("read email verification template: %w", readErr)
	}

	pageTmplPath := filepath.Join(dataDir, "templates", "pages", "verify.html")
	if data, readErr := os.ReadFile(pageTmplPath); readErr == nil {
		verifyPage, err = template.New("verify").Parse(string(data))
		if err != nil {
			return nil, nil, fmt.Errorf("parse verify page template: %w", err)
		}
		log.Info().Str("path", pageTmplPath).Msg("Loaded verify page template from data directory")
	} else if !errors.Is(readErr, fs.ErrNotExist) {
		return nil, nil, fmt.Errorf("read verify page template: %w", readErr)
	}

	return verification, verifyPage, nil
}

// redisPinger adapts *redis.Client to the api.Pinger interface.
type redisPinger struct{ client *redis.Client }

func (p redisPinger) Ping(ctx context.Context) error { return p.client.Ping(ctx).Err() }

// purgeExpiredData deletes stale login attempts, deletion tombstones, and orphaned attachments. Each call logs the
// outcome so operators can monitor retention enforcement.
func purgeExpiredData(ctx context.Context, repo user.Repository, attachRepo attachment.Repository, storage media.StorageProvider, cfg *config.Config) {
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

// safeGo starts a goroutine via wg.Go with panic recovery. If the goroutine panics, the panic value and a stack trace
// are logged so operators can diagnose the failure. The goroutine returns normally so the WaitGroup can drain during
// shutdown instead of crashing the process.
func safeGo(wg *sync.WaitGroup, fn func()) {
	wg.Go(func() {
		defer func() {
			if r := recover(); r != nil {
				buf := make([]byte, 4096)
				n := runtime.Stack(buf, false)
				log.Error().Interface("panic", r).Str("stack", string(buf[:n])).Msg("Background goroutine panicked")
			}
		}()
		fn()
	})
}

// runWithBackoff runs fn in a loop, restarting with exponential backoff when it returns a non-nil, non-cancelled error.
// If fn returns nil or context.Canceled the goroutine exits. The delay starts at 1 second and doubles on each
// consecutive failure up to a 2-minute cap. If fn runs successfully for at least maxDelay before failing, the backoff
// resets to initialDelay so that a transient error after a long healthy run does not inherit a stale elevated delay.
func runWithBackoff(ctx context.Context, name string, fn func(context.Context) error) {
	const (
		initialDelay = time.Second
		maxDelay     = 2 * time.Minute
	)
	delay := initialDelay
	for {
		started := time.Now()
		if err := fn(ctx); err != nil {
			if errors.Is(err, context.Canceled) {
				return
			}
			if time.Since(started) >= maxDelay {
				delay = initialDelay
			}
			log.Error().Err(err).Str("service", name).Dur("retry_in", delay).
				Msg("Background service stopped, restarting after delay")
			timer := time.NewTimer(delay)
			select {
			case <-ctx.Done():
				timer.Stop()
				return
			case <-timer.C:
			}
			delay = min(delay*2, maxDelay)
			continue
		}
		return
	}
}

// jsonBodyLimit returns middleware that rejects non-multipart requests whose Content-Length exceeds maxBytes. Multipart
// requests (file uploads) are exempt because they are governed by the global Fiber body limit and per-handler size
// validation. Requests without a Content-Length header are allowed through; Fiber's global limit still applies when
// reading the body.
func jsonBodyLimit(maxBytes int) fiber.Handler {
	return func(c fiber.Ctx) error {
		ct := c.Get("Content-Type")
		if strings.HasPrefix(ct, "multipart/") {
			return c.Next()
		}
		if cl := c.Request().Header.ContentLength(); cl > 0 && cl > maxBytes {
			return httputil.Fail(c, fiber.StatusRequestEntityTooLarge, apierrors.PayloadTooLarge, "Request body too large")
		}
		return c.Next()
	}
}

// rateLimitReached returns a structured JSON error response when a rate limit is exceeded. Without this handler,
// Fiber's limiter middleware sends a plain text "Too Many Requests" body that clients cannot parse as JSON.
func rateLimitReached(c fiber.Ctx) error {
	return httputil.Fail(c, fiber.StatusTooManyRequests, apierrors.RateLimited, "Too many requests. Please try again later.")
}

// userKeyGenerator returns the authenticated user's ID as the rate limiter key. This ensures per-user rate limiting
// regardless of proxy topology or shared IP addresses. Falls back to client IP for unauthenticated requests.
func userKeyGenerator(c fiber.Ctx) string {
	if userID, ok := c.Locals("userID").(uuid.UUID); ok {
		return "user:" + userID.String()
	}
	return c.IP()
}

// userChannelKeyGenerator returns a composite key of user ID and channel ID, giving each user an independent rate limit
// per channel. Falls back to client IP for unauthenticated requests.
func userChannelKeyGenerator(c fiber.Ctx) string {
	if userID, ok := c.Locals("userID").(uuid.UUID); ok {
		return "user:" + userID.String() + ":ch:" + c.Params("channelID")
	}
	return c.IP()
}

// fiberStatusToAPICode maps an HTTP status code from Fiber's built-in errors (404, 405, etc.) to the closest protocol
// error code.
func fiberStatusToAPICode(status int) apierrors.Code {
	switch status {
	case fiber.StatusUnauthorized:
		return apierrors.Unauthorised
	case fiber.StatusNotFound:
		return apierrors.NotFound
	case fiber.StatusMethodNotAllowed:
		return apierrors.ValidationError
	case fiber.StatusRequestTimeout:
		return apierrors.InternalError
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

// serveMediaFile returns a Fiber handler that serves stored files by storage key. Content type is derived from the
// storage key extension, falling back to application/octet-stream for unrecognised extensions.
func serveMediaFile(storage media.StorageProvider) fiber.Handler {
	return func(c fiber.Ctx) error {
		key := c.Params("*")
		if key == "" {
			return fiber.ErrNotFound
		}
		rc, err := storage.Get(c.Context(), key)
		if err != nil {
			return fiber.ErrNotFound
		}

		contentType := "application/octet-stream"
		if ext := filepath.Ext(key); ext != "" {
			if mt := mime.TypeByExtension(ext); mt != "" {
				contentType = mt
			}
		}
		c.Set("Content-Type", contentType)
		c.Set("X-Content-Type-Options", "nosniff")
		c.Set("Cache-Control", "public, max-age=31536000, immutable")
		return c.SendStream(rc)
	}
}
