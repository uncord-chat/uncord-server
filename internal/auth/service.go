package auth

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"time"

	"github.com/google/uuid"
	"github.com/redis/go-redis/v9"
	"github.com/rs/zerolog/log"

	"github.com/uncord-chat/uncord-server/internal/config"
	"github.com/uncord-chat/uncord-server/internal/disposable"
	"github.com/uncord-chat/uncord-server/internal/user"
)

// Service implements authentication business logic, keeping HTTP handlers
// thin and focused on request parsing / response formatting.
type Service struct {
	users     user.Repository
	redis     *redis.Client
	config    *config.Config
	blocklist *disposable.Blocklist
}

// NewService creates a new authentication service.
func NewService(users user.Repository, rdb *redis.Client, cfg *config.Config, bl *disposable.Blocklist) *Service {
	return &Service{
		users:     users,
		redis:     rdb,
		config:    cfg,
		blocklist: bl,
	}
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

// AuthResult is the output for Register and Login.
type AuthResult struct {
	User         UserInfo
	AccessToken  string
	RefreshToken string
}

// UserInfo is a safe-to-return subset of the user record.
type UserInfo struct {
	ID            string
	Email         string
	Username      string
	EmailVerified bool
}

// TokenPair is the output for Refresh.
type TokenPair struct {
	AccessToken  string
	RefreshToken string
}

// Register validates inputs, creates the user with an email verification
// token in a single transaction, and returns auth tokens.
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

	blocked, err := s.blocklist.IsBlocked(domain)
	if err != nil {
		log.Warn().Err(err).Msg("Disposable email check failed")
	}
	if blocked {
		return nil, ErrDisposableEmail
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

	verifyToken, err := generateSecureToken(32)
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
		log.Info().
			Str("user_id", userID.String()).
			Str("token", verifyToken).
			Msg("Email verification token (dev mode)")
	}

	tokens, err := s.issueTokens(ctx, userID)
	if err != nil {
		return nil, err
	}

	return &AuthResult{
		User: UserInfo{
			ID:            userID.String(),
			Email:         email,
			Username:      req.Username,
			EmailVerified: false,
		},
		AccessToken:  tokens.AccessToken,
		RefreshToken: tokens.RefreshToken,
	}, nil
}

// Login verifies credentials, records the attempt, and returns auth tokens.
func (s *Service) Login(ctx context.Context, req LoginRequest) (*AuthResult, error) {
	email, _, err := ValidateEmail(req.Email)
	if err != nil {
		return nil, err
	}

	u, err := s.users.GetByEmail(ctx, email)
	if err != nil {
		if errors.Is(err, user.ErrNotFound) {
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
		return nil, ErrMFARequired
	}

	s.recordLoginAttempt(ctx, email, req.IP, true)

	tokens, err := s.issueTokens(ctx, u.ID)
	if err != nil {
		return nil, err
	}

	return &AuthResult{
		User: UserInfo{
			ID:            u.ID.String(),
			Email:         email,
			Username:      u.Username,
			EmailVerified: u.EmailVerified,
		},
		AccessToken:  tokens.AccessToken,
		RefreshToken: tokens.RefreshToken,
	}, nil
}

// Refresh rotates a refresh token and issues a new access token.
func (s *Service) Refresh(ctx context.Context, oldToken string) (*TokenPair, error) {
	refreshTTL := time.Duration(s.config.JWTRefreshTTL) * time.Second
	accessTTL := time.Duration(s.config.JWTAccessTTL) * time.Second

	newRefresh, userID, err := RotateRefreshToken(ctx, s.redis, oldToken, refreshTTL)
	if err != nil {
		return nil, err // ErrRefreshTokenReused passes through
	}

	accessToken, err := NewAccessToken(userID, s.config.JWTSecret, accessTTL, s.config.ServerURL)
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
	_, err := s.users.VerifyEmail(ctx, token)
	if err != nil {
		if errors.Is(err, user.ErrInvalidToken) {
			return ErrInvalidToken
		}
		return fmt.Errorf("verify email: %w", err)
	}
	return nil
}

func (s *Service) issueTokens(ctx context.Context, userID uuid.UUID) (*TokenPair, error) {
	accessTTL := time.Duration(s.config.JWTAccessTTL) * time.Second
	refreshTTL := time.Duration(s.config.JWTRefreshTTL) * time.Second

	accessToken, err := NewAccessToken(userID, s.config.JWTSecret, accessTTL, s.config.ServerURL)
	if err != nil {
		return nil, fmt.Errorf("create access token: %w", err)
	}

	refreshToken, err := CreateRefreshToken(ctx, s.redis, userID, refreshTTL)
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
		log.Warn().Err(err).Msg("Failed to record login attempt")
	}
}

func generateSecureToken(n int) (string, error) {
	b := make([]byte, n)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	return hex.EncodeToString(b), nil
}
