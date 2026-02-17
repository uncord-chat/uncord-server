package auth

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/pquerna/otp/totp"
	"github.com/redis/go-redis/v9"
	"github.com/rs/zerolog"
	"github.com/uncord-chat/uncord-protocol/models"

	"github.com/uncord-chat/uncord-server/internal/config"
	"github.com/uncord-chat/uncord-server/internal/disposable"
	"github.com/uncord-chat/uncord-server/internal/permission"
	"github.com/uncord-chat/uncord-server/internal/server"
	"github.com/uncord-chat/uncord-server/internal/user"
)

// verifyTokenBytes is the number of random bytes used to generate email verification tokens. 32 bytes yields 64 hex
// characters and 256 bits of entropy.
const verifyTokenBytes = 32

// Sender sends transactional emails such as verification messages. Implementations must be safe for concurrent use.
type Sender interface {
	SendVerification(ctx context.Context, to, token, serverURL, serverName string) error
}

// Service implements authentication business logic, keeping HTTP handlers thin and focused on request parsing /
// response formatting.
type Service struct {
	users         user.Repository
	redis         *redis.Client
	config        *config.Config
	blocklist     *disposable.Blocklist
	sender        Sender
	serverRepo    server.Repository
	permPublisher *permission.Publisher
	log           zerolog.Logger
	// dummyHash is a precomputed Argon2id hash used to keep login timing constant when a user is not found,
	// preventing email enumeration via response-time analysis.
	dummyHash string
}

// NewService creates a new authentication service. The sender parameter may be nil when SMTP is not configured; in that
// case, verification emails are silently skipped. It returns an error if the Argon2id configuration is invalid, since
// password hashing is fundamental to every auth operation.
func NewService(users user.Repository, rdb *redis.Client, cfg *config.Config, bl *disposable.Blocklist, sender Sender, serverRepo server.Repository, permPub *permission.Publisher, logger zerolog.Logger) (*Service, error) {
	// Generate a dummy hash at startup so VerifyPassword always runs against a real Argon2id hash even when the user
	// does not exist. A failure here means the Argon2 parameters are broken and no password operation will succeed.
	dummy, err := HashPassword("uncord-dummy-password", cfg.Argon2Memory, cfg.Argon2Iterations, cfg.Argon2Parallelism, cfg.Argon2SaltLength, cfg.Argon2KeyLength)
	if err != nil {
		return nil, fmt.Errorf("generate dummy hash: %w", err)
	}
	return &Service{
		users:         users,
		redis:         rdb,
		config:        cfg,
		blocklist:     bl,
		sender:        sender,
		serverRepo:    serverRepo,
		permPublisher: permPub,
		log:           logger,
		dummyHash:     dummy,
	}, nil
}

// RegisterRequest is the input for Service.Register.
type RegisterRequest struct {
	Email    string
	Username string
	Password string
}

// LoginRequest is the input for Service.Login.
type LoginRequest struct {
	Email    string
	Password string
	IP       string
}

// AuthResult is the output for Register and successful logins (with or without MFA).
type AuthResult struct {
	User         models.User
	AccessToken  string
	RefreshToken string
}

// LoginResult is the output for Login. When MFARequired is true, the Auth field is nil and Ticket contains the
// single-use ticket that the client must present to the MFA verify endpoint.
type LoginResult struct {
	MFARequired bool
	Ticket      string
	Auth        *AuthResult
}

// MFASetupResult is the output for BeginMFASetup, containing the TOTP provisioning data the client needs to register
// the secret in an authenticator app.
type MFASetupResult struct {
	Secret string
	URI    string
}

// TokenPair is the output for Refresh.
type TokenPair struct {
	AccessToken  string
	RefreshToken string
}

// Register validates inputs, creates the user with an email verification token in a single transaction, and returns
// auth tokens.
func (s *Service) Register(ctx context.Context, req RegisterRequest) (*AuthResult, error) {
	email, domain, err := ValidateEmail(req.Email)
	if err != nil {
		return nil, err
	}
	if err := ValidateUsername(req.Username); err != nil {
		return nil, err
	}
	if err := ValidatePassword(req.Password); err != nil {
		return nil, err
	}

	blocked, err := s.blocklist.IsBlocked(ctx, domain)
	if err != nil {
		s.log.Warn().Err(err).Msg("Disposable email check failed")
	}
	if blocked {
		return nil, ErrDisposableEmail
	}

	emailHMAC, err := HMACIdentifier(email, s.config.ServerSecret)
	if err != nil {
		return nil, fmt.Errorf("compute email HMAC: %w", err)
	}
	if tombstoned, err := s.users.CheckTombstone(ctx, user.TombstoneEmail, emailHMAC); err != nil {
		return nil, fmt.Errorf("check email tombstone: %w", err)
	} else if tombstoned {
		return nil, ErrAccountTombstoned
	}

	usernameHMAC, err := HMACIdentifier(strings.ToLower(req.Username), s.config.ServerSecret)
	if err != nil {
		return nil, fmt.Errorf("compute username HMAC: %w", err)
	}
	if tombstoned, err := s.users.CheckTombstone(ctx, user.TombstoneUsername, usernameHMAC); err != nil {
		return nil, fmt.Errorf("check username tombstone: %w", err)
	} else if tombstoned {
		return nil, ErrAccountTombstoned
	}

	hash, err := HashPassword(
		req.Password,
		s.config.Argon2Memory,
		s.config.Argon2Iterations,
		s.config.Argon2Parallelism,
		s.config.Argon2SaltLength,
		s.config.Argon2KeyLength,
	)
	if err != nil {
		return nil, fmt.Errorf("hash password: %w", err)
	}

	verifyToken, err := generateSecureToken(verifyTokenBytes)
	if err != nil {
		return nil, fmt.Errorf("generate verification token: %w", err)
	}

	userID, err := s.users.Create(ctx, user.CreateParams{
		Email:        email,
		Username:     req.Username,
		PasswordHash: hash,
		VerifyToken:  verifyToken,
		VerifyExpiry: time.Now().Add(24 * time.Hour),
	})
	if err != nil {
		if errors.Is(err, user.ErrAlreadyExists) {
			return nil, ErrEmailAlreadyTaken
		}
		return nil, fmt.Errorf("create user: %w", err)
	}

	if s.config.IsDevelopment() {
		s.log.Info().
			Str("user_id", userID.String()).
			Str("token", verifyToken).
			Msg("Email verification token (dev mode)")
	}

	if s.sender != nil {
		if err := s.sender.SendVerification(ctx, email, verifyToken, s.config.ServerURL, s.config.ServerName); err != nil {
			s.log.Error().Err(err).Str("user_id", userID.String()).Msg("Failed to send verification email")
		}
	}

	s.log.Debug().Str("user_id", userID.String()).Msg("User registered")

	tokens, err := s.issueTokens(ctx, userID)
	if err != nil {
		return nil, err
	}

	return &AuthResult{
		User: models.User{
			ID:            userID.String(),
			Email:         email,
			Username:      req.Username,
			MFAEnabled:    false,
			EmailVerified: false,
		},
		AccessToken:  tokens.AccessToken,
		RefreshToken: tokens.RefreshToken,
	}, nil
}

// Login verifies credentials, records the attempt, and returns either auth tokens (for non-MFA users) or an MFA ticket
// (for MFA-enabled users).
func (s *Service) Login(ctx context.Context, req LoginRequest) (*LoginResult, error) {
	email, _, err := ValidateEmail(req.Email)
	if err != nil {
		return nil, err
	}

	u, err := s.users.GetByEmail(ctx, email)
	if err != nil {
		if errors.Is(err, user.ErrNotFound) {
			// Hash against a dummy value to prevent timing-based email enumeration. Without this, "user not found" returns
			// faster than "wrong password" because Argon2id is skipped.
			_, _ = VerifyPassword(req.Password, s.dummyHash)
			s.recordLoginAttempt(ctx, email, req.IP, false)
			return nil, ErrInvalidCredentials
		}
		return nil, fmt.Errorf("get user: %w", err)
	}

	match, err := VerifyPassword(req.Password, u.PasswordHash)
	if err != nil {
		return nil, fmt.Errorf("verify password: %w", err)
	}
	if !match {
		s.recordLoginAttempt(ctx, email, req.IP, false)
		return nil, ErrInvalidCredentials
	}

	if u.MFAEnabled {
		ticket, err := CreateMFATicket(ctx, s.redis, u.ID, s.config.MFATicketTTL)
		if err != nil {
			return nil, fmt.Errorf("create MFA ticket: %w", err)
		}
		s.recordLoginAttempt(ctx, email, req.IP, true)
		return &LoginResult{MFARequired: true, Ticket: ticket}, nil
	}

	// Lazy hash rotation: rehash with current parameters if the stored hash was generated with older settings.
	needsRehash, rehashErr := NeedsRehash(u.PasswordHash, s.config.Argon2Memory, s.config.Argon2Iterations, s.config.Argon2Parallelism, s.config.Argon2SaltLength, s.config.Argon2KeyLength)
	if rehashErr != nil {
		s.log.Warn().Err(rehashErr).Str("user_id", u.ID.String()).Msg("Password hash decode failed during rehash check")
	}
	if needsRehash {
		if newHash, hashErr := HashPassword(req.Password, s.config.Argon2Memory, s.config.Argon2Iterations, s.config.Argon2Parallelism, s.config.Argon2SaltLength, s.config.Argon2KeyLength); hashErr == nil {
			if updateErr := s.users.UpdatePasswordHash(ctx, u.ID, newHash); updateErr != nil {
				s.log.Warn().Err(updateErr).Str("user_id", u.ID.String()).Msg("Failed to rotate password hash")
			} else {
				s.log.Debug().Str("user_id", u.ID.String()).Msg("Password hash rotated to current parameters")
			}
		}
	}

	s.recordLoginAttempt(ctx, email, req.IP, true)

	tokens, err := s.issueTokens(ctx, u.ID)
	if err != nil {
		return nil, err
	}

	return &LoginResult{
		Auth: &AuthResult{
			User:         u.ToModel(),
			AccessToken:  tokens.AccessToken,
			RefreshToken: tokens.RefreshToken,
		},
	}, nil
}

// Refresh rotates a refresh token and issues a new access token.
func (s *Service) Refresh(ctx context.Context, oldToken string) (*TokenPair, error) {
	newRefresh, userID, err := RotateRefreshToken(ctx, s.redis, oldToken, s.config.JWTRefreshTTL)
	if err != nil {
		return nil, err // ErrRefreshTokenReused passes through
	}

	accessToken, err := NewAccessToken(userID, s.config.JWTSecret, s.config.JWTAccessTTL, s.config.ServerURL)
	if err != nil {
		return nil, fmt.Errorf("create access token: %w", err)
	}

	return &TokenPair{
		AccessToken:  accessToken,
		RefreshToken: newRefresh,
	}, nil
}

// VerifyEmail consumes a verification token and marks the user as verified.
func (s *Service) VerifyEmail(ctx context.Context, token string) error {
	userID, err := s.users.VerifyEmail(ctx, token)
	if err != nil {
		if errors.Is(err, user.ErrInvalidToken) {
			return ErrInvalidToken
		}
		return fmt.Errorf("verify email: %w", err)
	}
	s.log.Debug().Str("user_id", userID.String()).Msg("User email verified")
	return nil
}

// ResendVerification generates a new email verification token and sends a verification email. Returns
// ErrEmailAlreadyVerified if the user is already verified, or propagates ErrVerificationCooldown from the repository if
// the user has requested a token too recently.
func (s *Service) ResendVerification(ctx context.Context, userID uuid.UUID) error {
	u, err := s.users.GetByID(ctx, userID)
	if err != nil {
		return fmt.Errorf("get user for resend verification: %w", err)
	}
	if u.EmailVerified {
		return ErrEmailAlreadyVerified
	}

	verifyToken, err := generateSecureToken(verifyTokenBytes)
	if err != nil {
		return fmt.Errorf("generate verification token: %w", err)
	}

	err = s.users.ReplaceVerificationToken(ctx, userID, verifyToken, time.Now().Add(24*time.Hour), 60*time.Second)
	if err != nil {
		if errors.Is(err, user.ErrVerificationCooldown) {
			return ErrVerificationCooldown
		}
		return fmt.Errorf("replace verification token: %w", err)
	}

	if s.config.IsDevelopment() {
		s.log.Info().
			Str("user_id", userID.String()).
			Str("token", verifyToken).
			Msg("Email verification token (dev mode)")
	}

	if s.sender != nil {
		if err := s.sender.SendVerification(ctx, u.Email, verifyToken, s.config.ServerURL, s.config.ServerName); err != nil {
			s.log.Error().Err(err).Str("user_id", userID.String()).Msg("Failed to send verification email")
		}
	}

	s.log.Debug().Str("user_id", userID.String()).Msg("Verification email resent")
	return nil
}

// VerifyMFA consumes an MFA ticket, loads the user's credentials, validates the provided TOTP or recovery code, and
// issues auth tokens on success.
func (s *Service) VerifyMFA(ctx context.Context, ticket, code string) (*AuthResult, error) {
	userID, err := ConsumeMFATicket(ctx, s.redis, ticket)
	if err != nil {
		return nil, err
	}

	creds, err := s.users.GetCredentialsByID(ctx, userID)
	if err != nil {
		return nil, fmt.Errorf("get credentials for MFA verify: %w", err)
	}

	if creds.MFASecret == nil {
		return nil, ErrMFANotEnabled
	}

	if !s.config.MFAConfigured() {
		return nil, ErrMFANotConfigured
	}

	secret, err := DecryptTOTPSecret(*creds.MFASecret, s.config.MFAEncryptionKey)
	if err != nil {
		return nil, fmt.Errorf("decrypt MFA secret: %w", err)
	}

	if totp.Validate(code, secret) {
		return s.completeMFALogin(ctx, creds)
	}

	// The code did not match TOTP; try recovery codes.
	if err := s.tryRecoveryCode(ctx, userID, code); err != nil {
		if errors.Is(err, ErrInvalidMFACode) {
			return nil, ErrInvalidMFACode
		}
		return nil, fmt.Errorf("try recovery code during login: %w", err)
	}

	return s.completeMFALogin(ctx, creds)
}

// BeginMFASetup verifies the user's password, generates a new TOTP key, encrypts and stores it in Valkey as a pending
// secret, and returns the provisioning data. The caller must present a valid TOTP code via ConfirmMFASetup to activate
// MFA.
func (s *Service) BeginMFASetup(ctx context.Context, userID uuid.UUID, password string) (*MFASetupResult, error) {
	if !s.config.MFAConfigured() {
		return nil, ErrMFANotConfigured
	}

	creds, err := s.users.GetCredentialsByID(ctx, userID)
	if err != nil {
		return nil, fmt.Errorf("get credentials for MFA setup: %w", err)
	}

	match, err := VerifyPassword(password, creds.PasswordHash)
	if err != nil {
		return nil, fmt.Errorf("verify password for MFA setup: %w", err)
	}
	if !match {
		return nil, ErrInvalidCredentials
	}

	if creds.MFAEnabled {
		return nil, ErrMFAAlreadyEnabled
	}

	key, err := totp.Generate(totp.GenerateOpts{
		Issuer:      s.config.ServerName,
		AccountName: creds.Email,
	})
	if err != nil {
		return nil, fmt.Errorf("generate TOTP key: %w", err)
	}

	encrypted, err := EncryptTOTPSecret(key.Secret(), s.config.MFAEncryptionKey)
	if err != nil {
		return nil, fmt.Errorf("encrypt TOTP secret: %w", err)
	}

	if err := StorePendingMFASecret(ctx, s.redis, userID, encrypted); err != nil {
		return nil, err
	}
	if err := ResetMFASetupAttempts(ctx, s.redis, userID); err != nil {
		s.log.Warn().Err(err).Str("user_id", userID.String()).Msg("Failed to reset MFA setup attempts")
	}

	return &MFASetupResult{
		Secret: key.Secret(),
		URI:    key.URL(),
	}, nil
}

// ConfirmMFASetup consumes the pending TOTP secret, validates the provided code against it, generates recovery codes,
// and persists everything to enable MFA. If the code is invalid, the pending secret is re-stored so the user can retry
// without restarting the setup flow.
func (s *Service) ConfirmMFASetup(ctx context.Context, userID uuid.UUID, code string) ([]string, error) {
	if !s.config.MFAConfigured() {
		return nil, ErrMFANotConfigured
	}

	encrypted, err := ConsumePendingMFASecret(ctx, s.redis, userID)
	if err != nil {
		return nil, err
	}

	secret, err := DecryptTOTPSecret(encrypted, s.config.MFAEncryptionKey)
	if err != nil {
		return nil, fmt.Errorf("decrypt pending MFA secret: %w", err)
	}

	if !totp.Validate(code, secret) {
		attempts, err := IncrementMFASetupAttempts(ctx, s.redis, userID)
		if err != nil {
			s.log.Error().Err(err).Str("user_id", userID.String()).Msg("Failed to increment MFA setup attempts")
		}

		if attempts >= maxMFASetupAttempts {
			return nil, ErrMFASetupLocked
		}

		// Re-store so the user can retry.
		if storeErr := StorePendingMFASecret(ctx, s.redis, userID, encrypted); storeErr != nil {
			s.log.Error().Err(storeErr).Str("user_id", userID.String()).Msg("Failed to re-store pending MFA secret")
		}
		return nil, ErrInvalidMFACode
	}

	codes, err := GenerateRecoveryCodes()
	if err != nil {
		return nil, fmt.Errorf("generate recovery codes: %w", err)
	}

	hashes := make([]string, len(codes))
	for i, c := range codes {
		h, err := HashRecoveryCode(c, s.config.Argon2Memory, s.config.Argon2Iterations, s.config.Argon2Parallelism, s.config.Argon2SaltLength, s.config.Argon2KeyLength)
		if err != nil {
			return nil, fmt.Errorf("hash recovery code: %w", err)
		}
		hashes[i] = h
	}

	if err := s.users.EnableMFA(ctx, userID, encrypted, hashes); err != nil {
		return nil, fmt.Errorf("enable MFA: %w", err)
	}

	s.log.Debug().Str("user_id", userID.String()).Msg("MFA enabled")
	return codes, nil
}

// DisableMFA verifies the user's password and MFA code (TOTP or recovery code), disables MFA, and revokes all refresh
// tokens so the user must re-authenticate.
func (s *Service) DisableMFA(ctx context.Context, userID uuid.UUID, password, code string) error {
	creds, err := s.users.GetCredentialsByID(ctx, userID)
	if err != nil {
		return fmt.Errorf("get credentials for MFA disable: %w", err)
	}

	match, err := VerifyPassword(password, creds.PasswordHash)
	if err != nil {
		return fmt.Errorf("verify password for MFA disable: %w", err)
	}
	if !match {
		return ErrInvalidCredentials
	}

	if !creds.MFAEnabled || creds.MFASecret == nil {
		return ErrMFANotEnabled
	}

	if !s.config.MFAConfigured() {
		return ErrMFANotConfigured
	}

	secret, err := DecryptTOTPSecret(*creds.MFASecret, s.config.MFAEncryptionKey)
	if err != nil {
		return fmt.Errorf("decrypt MFA secret: %w", err)
	}

	if !totp.Validate(code, secret) {
		// The code did not match TOTP; try recovery codes.
		if err := s.tryRecoveryCode(ctx, userID, code); err != nil {
			if errors.Is(err, ErrInvalidMFACode) {
				return ErrInvalidMFACode
			}
			return fmt.Errorf("try recovery code during MFA disable: %w", err)
		}
	}

	if err := s.users.DisableMFA(ctx, userID); err != nil {
		return fmt.Errorf("disable MFA: %w", err)
	}

	if err := RevokeAllRefreshTokens(ctx, s.redis, userID); err != nil {
		s.log.Warn().Err(err).Str("user_id", userID.String()).Msg("Failed to revoke refresh tokens after MFA disable")
	}

	s.log.Debug().Str("user_id", userID.String()).Msg("MFA disabled")
	return nil
}

// RegenerateRecoveryCodes verifies the user's password, generates a new set of recovery codes, and replaces the
// existing ones in the database.
func (s *Service) RegenerateRecoveryCodes(ctx context.Context, userID uuid.UUID, password string) ([]string, error) {
	creds, err := s.users.GetCredentialsByID(ctx, userID)
	if err != nil {
		return nil, fmt.Errorf("get credentials for recovery code regeneration: %w", err)
	}

	match, err := VerifyPassword(password, creds.PasswordHash)
	if err != nil {
		return nil, fmt.Errorf("verify password for recovery code regeneration: %w", err)
	}
	if !match {
		return nil, ErrInvalidCredentials
	}

	if !creds.MFAEnabled {
		return nil, ErrMFANotEnabled
	}

	codes, err := GenerateRecoveryCodes()
	if err != nil {
		return nil, fmt.Errorf("generate recovery codes: %w", err)
	}

	hashes := make([]string, len(codes))
	for i, c := range codes {
		h, err := HashRecoveryCode(c, s.config.Argon2Memory, s.config.Argon2Iterations, s.config.Argon2Parallelism, s.config.Argon2SaltLength, s.config.Argon2KeyLength)
		if err != nil {
			return nil, fmt.Errorf("hash recovery code: %w", err)
		}
		hashes[i] = h
	}

	if err := s.users.ReplaceRecoveryCodes(ctx, userID, hashes); err != nil {
		return nil, fmt.Errorf("replace recovery codes: %w", err)
	}

	s.log.Debug().Str("user_id", userID.String()).Msg("Recovery codes regenerated")
	return codes, nil
}

// VerifyUserPassword confirms that the provided password matches the stored hash for the given user. It is used by the
// verify-password endpoint to let clients gate sensitive workflows behind a password prompt without performing any
// mutation.
func (s *Service) VerifyUserPassword(ctx context.Context, userID uuid.UUID, password string) error {
	creds, err := s.users.GetCredentialsByID(ctx, userID)
	if err != nil {
		return fmt.Errorf("get credentials for password verification: %w", err)
	}

	match, err := VerifyPassword(password, creds.PasswordHash)
	if err != nil {
		return fmt.Errorf("verify password: %w", err)
	}
	if !match {
		return ErrInvalidCredentials
	}

	return nil
}

// DeleteAccount verifies the user's password, checks that the user is not the server owner, computes HMAC tombstones
// for the email (always) and optionally the username, atomically deletes the user and inserts tombstones, and performs
// best-effort cleanup of refresh tokens and permission caches.
func (s *Service) DeleteAccount(ctx context.Context, userID uuid.UUID, password string) error {
	creds, err := s.users.GetCredentialsByID(ctx, userID)
	if err != nil {
		return fmt.Errorf("get credentials for account deletion: %w", err)
	}

	match, err := VerifyPassword(password, creds.PasswordHash)
	if err != nil {
		return fmt.Errorf("verify password for account deletion: %w", err)
	}
	if !match {
		return ErrInvalidCredentials
	}

	serverCfg, err := s.serverRepo.Get(ctx)
	if err != nil {
		return fmt.Errorf("get server config: %w", err)
	}
	if serverCfg.OwnerID == userID {
		return ErrServerOwner
	}

	tombstones := make([]user.Tombstone, 0, 2)

	emailHMAC, err := HMACIdentifier(creds.Email, s.config.ServerSecret)
	if err != nil {
		return fmt.Errorf("compute email HMAC: %w", err)
	}
	tombstones = append(tombstones, user.Tombstone{
		IdentifierType: user.TombstoneEmail,
		HMACHash:       emailHMAC,
	})

	if s.config.DeletionTombstoneUsernames {
		usernameHMAC, err := HMACIdentifier(strings.ToLower(creds.Username), s.config.ServerSecret)
		if err != nil {
			return fmt.Errorf("compute username HMAC: %w", err)
		}
		tombstones = append(tombstones, user.Tombstone{
			IdentifierType: user.TombstoneUsername,
			HMACHash:       usernameHMAC,
		})
	}

	if err := s.users.DeleteWithTombstones(ctx, userID, tombstones); err != nil {
		return fmt.Errorf("delete user with tombstones: %w", err)
	}

	if err := RevokeAllRefreshTokens(ctx, s.redis, userID); err != nil {
		s.log.Warn().Err(err).Str("user_id", userID.String()).Msg("Failed to revoke refresh tokens after account deletion")
	}

	if err := s.permPublisher.InvalidateUser(ctx, userID); err != nil {
		s.log.Warn().Err(err).Str("user_id", userID.String()).Msg("Failed to invalidate permission cache after account deletion")
	}

	s.log.Info().Str("user_id", userID.String()).Msg("Account deleted")
	return nil
}

// completeMFALogin issues tokens and builds an AuthResult with MFAEnabled set to true.
func (s *Service) completeMFALogin(ctx context.Context, creds *user.Credentials) (*AuthResult, error) {
	tokens, err := s.issueTokens(ctx, creds.ID)
	if err != nil {
		return nil, err
	}

	return &AuthResult{
		User:         creds.ToModel(),
		AccessToken:  tokens.AccessToken,
		RefreshToken: tokens.RefreshToken,
	}, nil
}

// tryRecoveryCode checks the provided code against all unused recovery codes for the user. If a match is found, the
// code is marked as used and nil is returned. Otherwise, ErrInvalidMFACode is returned. The function always evaluates
// every stored code to prevent timing side channels from revealing how many unused codes remain.
func (s *Service) tryRecoveryCode(ctx context.Context, userID uuid.UUID, code string) error {
	codes, err := s.users.GetUnusedRecoveryCodes(ctx, userID)
	if err != nil {
		return fmt.Errorf("get recovery codes: %w", err)
	}

	var matched *user.MFARecoveryCode
	for _, rc := range codes {
		match, err := VerifyRecoveryCode(code, rc.CodeHash)
		if err != nil {
			s.log.Warn().Err(err).Str("user_id", userID.String()).Msg("Recovery code verification error")
			continue
		}
		if match && matched == nil {
			matched = &rc
		}
	}

	if matched != nil {
		if err := s.users.UseRecoveryCode(ctx, matched.ID); err != nil {
			return fmt.Errorf("mark recovery code used: %w", err)
		}
		return nil
	}

	return ErrInvalidMFACode
}

func (s *Service) issueTokens(ctx context.Context, userID uuid.UUID) (*TokenPair, error) {
	accessToken, err := NewAccessToken(userID, s.config.JWTSecret, s.config.JWTAccessTTL, s.config.ServerURL)
	if err != nil {
		return nil, fmt.Errorf("create access token: %w", err)
	}

	refreshToken, err := CreateRefreshToken(ctx, s.redis, userID, s.config.JWTRefreshTTL)
	if err != nil {
		return nil, fmt.Errorf("create refresh token: %w", err)
	}

	return &TokenPair{
		AccessToken:  accessToken,
		RefreshToken: refreshToken,
	}, nil
}

func (s *Service) recordLoginAttempt(ctx context.Context, email, ip string, success bool) {
	if err := s.users.RecordLoginAttempt(ctx, email, ip, success); err != nil {
		s.log.Warn().Err(err).Msg("Failed to record login attempt")
	}
}

func generateSecureToken(n int) (string, error) {
	b := make([]byte, n)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	return hex.EncodeToString(b), nil
}
