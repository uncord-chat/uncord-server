package auth

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/pquerna/otp/totp"
	"github.com/rs/zerolog"

	"github.com/uncord-chat/uncord-server/internal/config"
	"github.com/uncord-chat/uncord-server/internal/disposable"
	"github.com/uncord-chat/uncord-server/internal/permission"
	"github.com/uncord-chat/uncord-server/internal/server"
	"github.com/uncord-chat/uncord-server/internal/user"
)

// fakeRepository implements user.Repository for unit tests.
type fakeRepository struct {
	users         map[string]*user.Credentials // keyed by email
	recoveryCodes map[uuid.UUID][]user.MFARecoveryCode
	tombstones    map[string]bool // keyed by "type:hash"
	createErr     error
	getByEmailErr error
	verifyErr     error
	loginAttempts []loginAttempt
}

type loginAttempt struct {
	Email   string
	IP      string
	Success bool
}

func newFakeRepository() *fakeRepository {
	return &fakeRepository{
		users:         make(map[string]*user.Credentials),
		recoveryCodes: make(map[uuid.UUID][]user.MFARecoveryCode),
		tombstones:    make(map[string]bool),
	}
}

func (r *fakeRepository) Create(_ context.Context, params user.CreateParams) (uuid.UUID, error) {
	if r.createErr != nil {
		return uuid.Nil, r.createErr
	}
	if _, exists := r.users[params.Email]; exists {
		return uuid.Nil, user.ErrAlreadyExists
	}
	id := uuid.New()
	r.users[params.Email] = &user.Credentials{
		User: user.User{
			ID:       id,
			Email:    params.Email,
			Username: params.Username,
		},
		PasswordHash: params.PasswordHash,
	}
	return id, nil
}

func (r *fakeRepository) GetByEmail(_ context.Context, email string) (*user.Credentials, error) {
	if r.getByEmailErr != nil {
		return nil, r.getByEmailErr
	}
	c, ok := r.users[email]
	if !ok {
		return nil, user.ErrNotFound
	}
	return c, nil
}

func (r *fakeRepository) VerifyEmail(_ context.Context, token string) (uuid.UUID, error) {
	if r.verifyErr != nil {
		return uuid.Nil, r.verifyErr
	}
	if token == "valid-token" {
		return uuid.New(), nil
	}
	return uuid.Nil, user.ErrInvalidToken
}

func (r *fakeRepository) RecordLoginAttempt(_ context.Context, email, ip string, success bool) error {
	r.loginAttempts = append(r.loginAttempts, loginAttempt{Email: email, IP: ip, Success: success})
	return nil
}

func (r *fakeRepository) GetByID(_ context.Context, id uuid.UUID) (*user.User, error) {
	for _, c := range r.users {
		if c.ID == id {
			cpy := c.User
			return &cpy, nil
		}
	}
	return nil, user.ErrNotFound
}

func (r *fakeRepository) Update(_ context.Context, id uuid.UUID, params user.UpdateParams) (*user.User, error) {
	for _, c := range r.users {
		if c.ID == id {
			if params.DisplayName != nil {
				c.DisplayName = params.DisplayName
			}
			if params.AvatarKey != nil {
				c.AvatarKey = params.AvatarKey
			}
			if params.Pronouns != nil {
				c.Pronouns = params.Pronouns
			}
			if params.BannerKey != nil {
				c.BannerKey = params.BannerKey
			}
			if params.About != nil {
				c.About = params.About
			}
			if params.ThemeColourPrimary != nil {
				c.ThemeColourPrimary = params.ThemeColourPrimary
			}
			if params.ThemeColourSecondary != nil {
				c.ThemeColourSecondary = params.ThemeColourSecondary
			}
			cpy := c.User
			return &cpy, nil
		}
	}
	return nil, user.ErrNotFound
}

func (r *fakeRepository) UpdatePasswordHash(_ context.Context, userID uuid.UUID, hash string) error {
	for _, c := range r.users {
		if c.ID == userID {
			c.PasswordHash = hash
			return nil
		}
	}
	return user.ErrNotFound
}

func (r *fakeRepository) GetCredentialsByID(_ context.Context, id uuid.UUID) (*user.Credentials, error) {
	for _, c := range r.users {
		if c.ID == id {
			return c, nil
		}
	}
	return nil, user.ErrNotFound
}

func (r *fakeRepository) EnableMFA(_ context.Context, userID uuid.UUID, encryptedSecret string, codeHashes []string) error {
	for _, c := range r.users {
		if c.ID == userID {
			c.MFAEnabled = true
			c.MFASecret = &encryptedSecret
			codes := make([]user.MFARecoveryCode, len(codeHashes))
			for i, h := range codeHashes {
				codes[i] = user.MFARecoveryCode{ID: uuid.New(), CodeHash: h}
			}
			r.recoveryCodes[userID] = codes
			return nil
		}
	}
	return user.ErrNotFound
}

func (r *fakeRepository) DisableMFA(_ context.Context, userID uuid.UUID) error {
	for _, c := range r.users {
		if c.ID == userID {
			c.MFAEnabled = false
			c.MFASecret = nil
			delete(r.recoveryCodes, userID)
			return nil
		}
	}
	return user.ErrNotFound
}

func (r *fakeRepository) GetUnusedRecoveryCodes(_ context.Context, userID uuid.UUID) ([]user.MFARecoveryCode, error) {
	return r.recoveryCodes[userID], nil
}

func (r *fakeRepository) UseRecoveryCode(_ context.Context, codeID uuid.UUID) error {
	for uid, codes := range r.recoveryCodes {
		for i, c := range codes {
			if c.ID == codeID {
				r.recoveryCodes[uid] = append(codes[:i], codes[i+1:]...)
				return nil
			}
		}
	}
	return nil
}

func (r *fakeRepository) ReplaceRecoveryCodes(_ context.Context, userID uuid.UUID, codeHashes []string) error {
	codes := make([]user.MFARecoveryCode, len(codeHashes))
	for i, h := range codeHashes {
		codes[i] = user.MFARecoveryCode{ID: uuid.New(), CodeHash: h}
	}
	r.recoveryCodes[userID] = codes
	return nil
}

func (r *fakeRepository) DeleteWithTombstones(_ context.Context, id uuid.UUID, tombstones []user.Tombstone) error {
	var found string
	for email, c := range r.users {
		if c.ID == id {
			found = email
			break
		}
	}
	if found == "" {
		return user.ErrNotFound
	}

	for _, t := range tombstones {
		r.tombstones[string(t.IdentifierType)+":"+t.HMACHash] = true
	}
	delete(r.users, found)
	delete(r.recoveryCodes, id)
	return nil
}

func (r *fakeRepository) CheckTombstone(_ context.Context, identifierType user.TombstoneType, hmacHash string) (bool, error) {
	return r.tombstones[string(identifierType)+":"+hmacHash], nil
}

// fakeServerRepo implements server.Repository for unit tests.
type fakeServerRepo struct {
	ownerID uuid.UUID
}

func (r *fakeServerRepo) Get(_ context.Context) (*server.Config, error) {
	return &server.Config{
		ID:      uuid.New(),
		Name:    "Test Server",
		OwnerID: r.ownerID,
	}, nil
}

func (r *fakeServerRepo) Update(_ context.Context, _ server.UpdateParams) (*server.Config, error) {
	return nil, fmt.Errorf("not implemented")
}

func testConfig() *config.Config {
	return &config.Config{
		ServerName:                 "Test Server",
		ServerURL:                  "https://test.example.com",
		ServerEnv:                  "production",
		JWTSecret:                  "test-secret-at-least-32-chars-long!!",
		JWTAccessTTL:               15 * time.Minute,
		JWTRefreshTTL:              7 * 24 * time.Hour,
		Argon2Memory:               64 * 1024,
		Argon2Iterations:           1, // fast for tests
		Argon2Parallelism:          1,
		Argon2SaltLength:           16,
		Argon2KeyLength:            32,
		MFAEncryptionKey:           testEncryptionKey,
		MFATicketTTL:               5 * time.Minute,
		ServerSecret:               testEncryptionKey, // reuse same hex key for test convenience
		DeletionTombstoneUsernames: true,
	}
}

func newTestService(t *testing.T, repo *fakeRepository) *Service {
	t.Helper()
	_, rdb := setupMiniredis(t)
	bl := disposable.NewBlocklist("", false, 10*time.Second, zerolog.Nop())
	serverRepo := &fakeServerRepo{ownerID: uuid.New()}
	permPub := permission.NewPublisher(rdb)
	svc, err := NewService(repo, rdb, testConfig(), bl, nil, serverRepo, permPub, zerolog.Nop())
	if err != nil {
		t.Fatalf("NewService() error = %v", err)
	}
	return svc
}

// --- Register tests ---

func TestServiceRegisterSuccess(t *testing.T) {
	t.Parallel()
	repo := newFakeRepository()
	svc := newTestService(t, repo)
	ctx := context.Background()

	result, err := svc.Register(ctx, RegisterRequest{
		Email:    "alice@example.com",
		Username: "alice",
		Password: "strongpassword",
	})
	if err != nil {
		t.Fatalf("Register() error = %v", err)
	}
	if result.User.Email != "alice@example.com" {
		t.Errorf("Register() email = %q, want %q", result.User.Email, "alice@example.com")
	}
	if result.User.Username != "alice" {
		t.Errorf("Register() username = %q, want %q", result.User.Username, "alice")
	}
	if result.User.EmailVerified {
		t.Error("Register() emailVerified = true, want false")
	}
	if result.AccessToken == "" {
		t.Error("Register() returned empty access token")
	}
	if result.RefreshToken == "" {
		t.Error("Register() returned empty refresh token")
	}
}

func TestServiceRegisterInvalidEmail(t *testing.T) {
	t.Parallel()
	repo := newFakeRepository()
	svc := newTestService(t, repo)

	_, err := svc.Register(context.Background(), RegisterRequest{
		Email:    "not-an-email",
		Username: "alice",
		Password: "strongpassword",
	})
	if !errors.Is(err, ErrInvalidEmail) {
		t.Errorf("Register() error = %v, want ErrInvalidEmail", err)
	}
}

func TestServiceRegisterInvalidUsername(t *testing.T) {
	t.Parallel()
	repo := newFakeRepository()
	svc := newTestService(t, repo)

	_, err := svc.Register(context.Background(), RegisterRequest{
		Email:    "alice@example.com",
		Username: "a",
		Password: "strongpassword",
	})
	if !errors.Is(err, ErrUsernameLength) {
		t.Errorf("Register() error = %v, want ErrUsernameLength", err)
	}
}

func TestServiceRegisterInvalidPassword(t *testing.T) {
	t.Parallel()
	repo := newFakeRepository()
	svc := newTestService(t, repo)

	_, err := svc.Register(context.Background(), RegisterRequest{
		Email:    "alice@example.com",
		Username: "alice",
		Password: "short",
	})
	if !errors.Is(err, ErrPasswordTooShort) {
		t.Errorf("Register() error = %v, want ErrPasswordTooShort", err)
	}
}

func TestServiceRegisterDuplicateEmail(t *testing.T) {
	t.Parallel()
	repo := newFakeRepository()
	svc := newTestService(t, repo)
	ctx := context.Background()

	_, err := svc.Register(ctx, RegisterRequest{
		Email:    "alice@example.com",
		Username: "alice",
		Password: "strongpassword",
	})
	if err != nil {
		t.Fatalf("first Register() error = %v", err)
	}

	_, err = svc.Register(ctx, RegisterRequest{
		Email:    "alice@example.com",
		Username: "alice2",
		Password: "strongpassword",
	})
	if !errors.Is(err, ErrEmailAlreadyTaken) {
		t.Errorf("second Register() error = %v, want ErrEmailAlreadyTaken", err)
	}
}

func TestServiceRegisterDisposableEmailBlocked(t *testing.T) {
	t.Parallel()
	repo := newFakeRepository()
	_, rdb := setupMiniredis(t)

	// Serve a blocklist containing "throwaway.email" so the blocklist can load it.
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = fmt.Fprintln(w, "throwaway.email")
		_, _ = fmt.Fprintln(w, "fakeinbox.com")
	}))
	t.Cleanup(srv.Close)

	bl := disposable.NewBlocklist(srv.URL, true, 10*time.Second, zerolog.Nop())
	serverRepo := &fakeServerRepo{ownerID: uuid.New()}
	permPub := permission.NewPublisher(rdb)
	svc, err := NewService(repo, rdb, testConfig(), bl, nil, serverRepo, permPub, zerolog.Nop())
	if err != nil {
		t.Fatalf("NewService() error = %v", err)
	}

	_, err = svc.Register(context.Background(), RegisterRequest{
		Email:    "alice@throwaway.email",
		Username: "alice",
		Password: "strongpassword",
	})
	if !errors.Is(err, ErrDisposableEmail) {
		t.Errorf("Register() with disposable domain error = %v, want ErrDisposableEmail", err)
	}

	// Non-disposable domain should still succeed.
	result, err := svc.Register(context.Background(), RegisterRequest{
		Email:    "alice@example.com",
		Username: "alice",
		Password: "strongpassword",
	})
	if err != nil {
		t.Fatalf("Register() with non-disposable domain error = %v", err)
	}
	if result == nil {
		t.Fatal("Register() returned nil result")
	}
}

func TestServiceRegisterCreateFails(t *testing.T) {
	t.Parallel()
	repo := newFakeRepository()
	repo.createErr = errors.New("database is down")
	svc := newTestService(t, repo)

	_, err := svc.Register(context.Background(), RegisterRequest{
		Email:    "alice@example.com",
		Username: "alice",
		Password: "strongpassword",
	})
	if err == nil {
		t.Fatal("Register() should fail when repo.Create fails")
	}
	if errors.Is(err, ErrEmailAlreadyTaken) {
		t.Error("Register() should not return ErrEmailAlreadyTaken for generic create error")
	}
}

// --- Login tests ---

func TestServiceLoginSuccess(t *testing.T) {
	t.Parallel()
	repo := newFakeRepository()
	svc := newTestService(t, repo)
	ctx := context.Background()

	// Register a user first
	_, err := svc.Register(ctx, RegisterRequest{
		Email:    "alice@example.com",
		Username: "alice",
		Password: "strongpassword",
	})
	if err != nil {
		t.Fatalf("Register() error = %v", err)
	}

	result, err := svc.Login(ctx, LoginRequest{
		Email:    "alice@example.com",
		Password: "strongpassword",
		IP:       "127.0.0.1",
	})
	if err != nil {
		t.Fatalf("Login() error = %v", err)
	}
	if result.MFARequired {
		t.Error("Login() MFARequired = true, want false")
	}
	if result.Auth == nil {
		t.Fatal("Login() Auth is nil")
	}
	if result.Auth.User.Email != "alice@example.com" {
		t.Errorf("Login() email = %q, want %q", result.Auth.User.Email, "alice@example.com")
	}
	if result.Auth.AccessToken == "" {
		t.Error("Login() returned empty access token")
	}
	if result.Auth.RefreshToken == "" {
		t.Error("Login() returned empty refresh token")
	}

	// Verify login attempt was recorded
	if len(repo.loginAttempts) == 0 {
		t.Fatal("Login() did not record login attempt")
	}
	last := repo.loginAttempts[len(repo.loginAttempts)-1]
	if !last.Success {
		t.Error("Login() recorded unsuccessful attempt for valid login")
	}
}

func TestServiceLoginInvalidEmail(t *testing.T) {
	t.Parallel()
	repo := newFakeRepository()
	svc := newTestService(t, repo)

	_, err := svc.Login(context.Background(), LoginRequest{
		Email:    "bad",
		Password: "strongpassword",
		IP:       "127.0.0.1",
	})
	if !errors.Is(err, ErrInvalidEmail) {
		t.Errorf("Login() error = %v, want ErrInvalidEmail", err)
	}
}

func TestServiceLoginUserNotFound(t *testing.T) {
	t.Parallel()
	repo := newFakeRepository()
	svc := newTestService(t, repo)

	_, err := svc.Login(context.Background(), LoginRequest{
		Email:    "nobody@example.com",
		Password: "strongpassword",
		IP:       "127.0.0.1",
	})
	if !errors.Is(err, ErrInvalidCredentials) {
		t.Errorf("Login() error = %v, want ErrInvalidCredentials", err)
	}

	// Should record a failed attempt
	if len(repo.loginAttempts) != 1 || repo.loginAttempts[0].Success {
		t.Error("Login() should record failed attempt for unknown user")
	}
}

func TestServiceLoginWrongPassword(t *testing.T) {
	t.Parallel()
	repo := newFakeRepository()
	svc := newTestService(t, repo)
	ctx := context.Background()

	_, err := svc.Register(ctx, RegisterRequest{
		Email:    "alice@example.com",
		Username: "alice",
		Password: "strongpassword",
	})
	if err != nil {
		t.Fatalf("Register() error = %v", err)
	}

	_, err = svc.Login(ctx, LoginRequest{
		Email:    "alice@example.com",
		Password: "wrongpassword",
		IP:       "127.0.0.1",
	})
	if !errors.Is(err, ErrInvalidCredentials) {
		t.Errorf("Login() error = %v, want ErrInvalidCredentials", err)
	}

	// Last attempt should be failed
	found := false
	for _, a := range repo.loginAttempts {
		if a.Email == "alice@example.com" && !a.Success {
			found = true
		}
	}
	if !found {
		t.Error("Login() should record failed attempt for wrong password")
	}
}

func TestServiceLoginMFARequired(t *testing.T) {
	t.Parallel()
	repo := newFakeRepository()
	svc := newTestService(t, repo)
	ctx := context.Background()

	// Register, then enable MFA on the user record
	_, err := svc.Register(ctx, RegisterRequest{
		Email:    "alice@example.com",
		Username: "alice",
		Password: "strongpassword",
	})
	if err != nil {
		t.Fatalf("Register() error = %v", err)
	}
	repo.users["alice@example.com"].MFAEnabled = true

	result, err := svc.Login(ctx, LoginRequest{
		Email:    "alice@example.com",
		Password: "strongpassword",
		IP:       "127.0.0.1",
	})
	if err != nil {
		t.Fatalf("Login() error = %v", err)
	}
	if !result.MFARequired {
		t.Error("Login() MFARequired = false, want true")
	}
	if result.Ticket == "" {
		t.Error("Login() returned empty ticket")
	}
	if result.Auth != nil {
		t.Error("Login() Auth should be nil when MFA is required")
	}
}

func TestServiceLoginGetByEmailFails(t *testing.T) {
	t.Parallel()
	repo := newFakeRepository()
	repo.getByEmailErr = errors.New("database timeout")
	svc := newTestService(t, repo)

	_, err := svc.Login(context.Background(), LoginRequest{
		Email:    "alice@example.com",
		Password: "strongpassword",
		IP:       "127.0.0.1",
	})
	if err == nil {
		t.Fatal("Login() should fail when GetByEmail fails")
	}
	if errors.Is(err, ErrInvalidCredentials) {
		t.Error("Login() should not return ErrInvalidCredentials for database error")
	}
}

// --- Refresh tests ---

func TestServiceRefreshSuccess(t *testing.T) {
	t.Parallel()
	repo := newFakeRepository()
	svc := newTestService(t, repo)
	ctx := context.Background()

	// Register to get tokens
	result, err := svc.Register(ctx, RegisterRequest{
		Email:    "alice@example.com",
		Username: "alice",
		Password: "strongpassword",
	})
	if err != nil {
		t.Fatalf("Register() error = %v", err)
	}

	tokens, err := svc.Refresh(ctx, result.RefreshToken)
	if err != nil {
		t.Fatalf("Refresh() error = %v", err)
	}
	if tokens.AccessToken == "" {
		t.Error("Refresh() returned empty access token")
	}
	if tokens.RefreshToken == "" {
		t.Error("Refresh() returned empty refresh token")
	}
	if tokens.RefreshToken == result.RefreshToken {
		t.Error("Refresh() returned same refresh token (should rotate)")
	}
}

func TestServiceRefreshTokenReused(t *testing.T) {
	t.Parallel()
	repo := newFakeRepository()
	svc := newTestService(t, repo)
	ctx := context.Background()

	result, err := svc.Register(ctx, RegisterRequest{
		Email:    "alice@example.com",
		Username: "alice",
		Password: "strongpassword",
	})
	if err != nil {
		t.Fatalf("Register() error = %v", err)
	}

	// First refresh succeeds
	_, err = svc.Refresh(ctx, result.RefreshToken)
	if err != nil {
		t.Fatalf("first Refresh() error = %v", err)
	}

	// Second refresh with same token should fail
	_, err = svc.Refresh(ctx, result.RefreshToken)
	if !errors.Is(err, ErrRefreshTokenReused) {
		t.Errorf("second Refresh() error = %v, want ErrRefreshTokenReused", err)
	}
}

func TestServiceRefreshInvalidToken(t *testing.T) {
	t.Parallel()
	repo := newFakeRepository()
	svc := newTestService(t, repo)

	_, err := svc.Refresh(context.Background(), "nonexistent-token")
	if err == nil {
		t.Fatal("Refresh() with invalid token should return error")
	}
}

// --- VerifyEmail tests ---

func TestServiceVerifyEmailSuccess(t *testing.T) {
	t.Parallel()
	repo := newFakeRepository()
	svc := newTestService(t, repo)

	err := svc.VerifyEmail(context.Background(), "valid-token")
	if err != nil {
		t.Fatalf("VerifyEmail() error = %v", err)
	}
}

func TestServiceVerifyEmailInvalidToken(t *testing.T) {
	t.Parallel()
	repo := newFakeRepository()
	svc := newTestService(t, repo)

	err := svc.VerifyEmail(context.Background(), "invalid-token")
	if !errors.Is(err, ErrInvalidToken) {
		t.Errorf("VerifyEmail() error = %v, want ErrInvalidToken", err)
	}
}

func TestServiceVerifyEmailRepoError(t *testing.T) {
	t.Parallel()
	repo := newFakeRepository()
	repo.verifyErr = errors.New("database error")
	svc := newTestService(t, repo)

	err := svc.VerifyEmail(context.Background(), "any-token")
	if err == nil {
		t.Fatal("VerifyEmail() should fail when repo errors")
	}
	if errors.Is(err, ErrInvalidToken) {
		t.Error("VerifyEmail() should not return ErrInvalidToken for database error")
	}
}

// --- Token issuance integration ---

func TestServiceRegisterTokensAreValid(t *testing.T) {
	t.Parallel()
	repo := newFakeRepository()
	svc := newTestService(t, repo)
	ctx := context.Background()

	result, err := svc.Register(ctx, RegisterRequest{
		Email:    "alice@example.com",
		Username: "alice",
		Password: "strongpassword",
	})
	if err != nil {
		t.Fatalf("Register() error = %v", err)
	}

	// Access token should be valid
	claims, err := ValidateAccessToken(result.AccessToken, svc.config.JWTSecret, svc.config.ServerURL)
	if err != nil {
		t.Fatalf("ValidateAccessToken() error = %v", err)
	}
	if claims.Subject != result.User.ID {
		t.Errorf("access token subject = %q, want %q", claims.Subject, result.User.ID)
	}

	// Refresh token should be valid
	userID, err := ValidateRefreshToken(ctx, svc.redis, result.RefreshToken)
	if err != nil {
		t.Fatalf("ValidateRefreshToken() error = %v", err)
	}
	if userID.String() != result.User.ID {
		t.Errorf("refresh token userID = %q, want %q", userID.String(), result.User.ID)
	}
}

func TestServiceLoginRecordsSuccessAttempt(t *testing.T) {
	t.Parallel()
	repo := newFakeRepository()
	svc := newTestService(t, repo)
	ctx := context.Background()

	_, err := svc.Register(ctx, RegisterRequest{
		Email:    "alice@example.com",
		Username: "alice",
		Password: "strongpassword",
	})
	if err != nil {
		t.Fatalf("Register() error = %v", err)
	}

	_, err = svc.Login(ctx, LoginRequest{
		Email:    "alice@example.com",
		Password: "strongpassword",
		IP:       "10.0.0.1",
	})
	if err != nil {
		t.Fatalf("Login() error = %v", err)
	}

	found := false
	for _, a := range repo.loginAttempts {
		if a.Email == "alice@example.com" && a.IP == "10.0.0.1" && a.Success {
			found = true
		}
	}
	if !found {
		t.Error("Login() did not record successful login attempt with correct IP")
	}
}

func TestServiceRegisterNormalizesEmail(t *testing.T) {
	t.Parallel()
	repo := newFakeRepository()
	svc := newTestService(t, repo)
	ctx := context.Background()

	result, err := svc.Register(ctx, RegisterRequest{
		Email:    "Alice@Example.COM",
		Username: "alice",
		Password: "strongpassword",
	})
	if err != nil {
		t.Fatalf("Register() error = %v", err)
	}
	if result.User.Email != "alice@example.com" {
		t.Errorf("Register() email = %q, want normalised %q", result.User.Email, "alice@example.com")
	}
}

func TestServiceRefreshIssuesNewAccessToken(t *testing.T) {
	t.Parallel()
	repo := newFakeRepository()
	svc := newTestService(t, repo)
	ctx := context.Background()

	result, err := svc.Register(ctx, RegisterRequest{
		Email:    "alice@example.com",
		Username: "alice",
		Password: "strongpassword",
	})
	if err != nil {
		t.Fatalf("Register() error = %v", err)
	}

	tokens, err := svc.Refresh(ctx, result.RefreshToken)
	if err != nil {
		t.Fatalf("Refresh() error = %v", err)
	}

	// New access token should be valid
	claims, err := ValidateAccessToken(tokens.AccessToken, svc.config.JWTSecret, svc.config.ServerURL)
	if err != nil {
		t.Fatalf("ValidateAccessToken() on refreshed token error = %v", err)
	}
	if claims.Subject != result.User.ID {
		t.Errorf("refreshed access token subject = %q, want %q", claims.Subject, result.User.ID)
	}

	// Refresh token should be rotated
	if tokens.RefreshToken == result.RefreshToken {
		t.Error("Refresh() returned same refresh token (should rotate)")
	}
}

// --- Verification email tests ---

type fakeSender struct {
	calls []fakeSendCall
	err   error
}

type fakeSendCall struct {
	To         string
	Token      string
	ServerURL  string
	ServerName string
}

func (f *fakeSender) SendVerification(_ context.Context, to, token, serverURL, serverName string) error {
	f.calls = append(f.calls, fakeSendCall{To: to, Token: token, ServerURL: serverURL, ServerName: serverName})
	return f.err
}

func newTestServiceWithSender(t *testing.T, repo *fakeRepository, sender Sender) *Service {
	t.Helper()
	_, rdb := setupMiniredis(t)
	bl := disposable.NewBlocklist("", false, 10*time.Second, zerolog.Nop())
	serverRepo := &fakeServerRepo{ownerID: uuid.New()}
	permPub := permission.NewPublisher(rdb)
	svc, err := NewService(repo, rdb, testConfig(), bl, sender, serverRepo, permPub, zerolog.Nop())
	if err != nil {
		t.Fatalf("NewService() error = %v", err)
	}
	return svc
}

func TestServiceRegisterSendsVerificationEmail(t *testing.T) {
	t.Parallel()
	repo := newFakeRepository()
	sender := &fakeSender{}
	svc := newTestServiceWithSender(t, repo, sender)

	_, err := svc.Register(context.Background(), RegisterRequest{
		Email:    "alice@example.com",
		Username: "alice",
		Password: "strongpassword",
	})
	if err != nil {
		t.Fatalf("Register() error = %v", err)
	}
	if len(sender.calls) != 1 {
		t.Fatalf("sender called %d times, want 1", len(sender.calls))
	}
	call := sender.calls[0]
	if call.To != "alice@example.com" {
		t.Errorf("SendVerification to = %q, want %q", call.To, "alice@example.com")
	}
	if call.Token == "" {
		t.Error("SendVerification token is empty")
	}
	if call.ServerURL != "https://test.example.com" {
		t.Errorf("SendVerification serverURL = %q, want %q", call.ServerURL, "https://test.example.com")
	}
}

func TestServiceRegisterContinuesWhenEmailFails(t *testing.T) {
	t.Parallel()
	repo := newFakeRepository()
	sender := &fakeSender{err: fmt.Errorf("SMTP connection refused")}
	svc := newTestServiceWithSender(t, repo, sender)

	result, err := svc.Register(context.Background(), RegisterRequest{
		Email:    "alice@example.com",
		Username: "alice",
		Password: "strongpassword",
	})
	if err != nil {
		t.Fatalf("Register() should succeed even when email fails, got error = %v", err)
	}
	if result.User.Email != "alice@example.com" {
		t.Errorf("Register() email = %q, want %q", result.User.Email, "alice@example.com")
	}
}

func TestServiceRegisterNilSenderSkipsEmail(t *testing.T) {
	t.Parallel()
	repo := newFakeRepository()
	svc := newTestServiceWithSender(t, repo, nil)

	result, err := svc.Register(context.Background(), RegisterRequest{
		Email:    "alice@example.com",
		Username: "alice",
		Password: "strongpassword",
	})
	if err != nil {
		t.Fatalf("Register() error = %v", err)
	}
	if result.User.Email != "alice@example.com" {
		t.Errorf("Register() email = %q, want %q", result.User.Email, "alice@example.com")
	}
}

// --- MFA tests ---

// registerMFAUser is a test helper that registers a user and runs the full MFA setup flow, returning the TOTP secret
// for generating valid codes in tests.
func registerMFAUser(t *testing.T, svc *Service, repo *fakeRepository, email, password string) string {
	t.Helper()
	ctx := context.Background()

	_, err := svc.Register(ctx, RegisterRequest{
		Email:    email,
		Username: "mfauser",
		Password: password,
	})
	if err != nil {
		t.Fatalf("Register() error = %v", err)
	}

	userID := repo.users[email].ID
	setup, err := svc.BeginMFASetup(ctx, userID, password)
	if err != nil {
		t.Fatalf("BeginMFASetup() error = %v", err)
	}

	code, err := totp.GenerateCode(setup.Secret, time.Now())
	if err != nil {
		t.Fatalf("totp.GenerateCode() error = %v", err)
	}

	_, err = svc.ConfirmMFASetup(ctx, userID, code)
	if err != nil {
		t.Fatalf("ConfirmMFASetup() error = %v", err)
	}

	return setup.Secret
}

func TestServiceBeginMFASetupSuccess(t *testing.T) {
	t.Parallel()
	repo := newFakeRepository()
	svc := newTestService(t, repo)
	ctx := context.Background()

	_, err := svc.Register(ctx, RegisterRequest{
		Email:    "alice@example.com",
		Username: "alice",
		Password: "strongpassword",
	})
	if err != nil {
		t.Fatalf("Register() error = %v", err)
	}

	userID := repo.users["alice@example.com"].ID
	result, err := svc.BeginMFASetup(ctx, userID, "strongpassword")
	if err != nil {
		t.Fatalf("BeginMFASetup() error = %v", err)
	}
	if result.Secret == "" {
		t.Error("BeginMFASetup() returned empty secret")
	}
	if result.URI == "" {
		t.Error("BeginMFASetup() returned empty URI")
	}
}

func TestServiceBeginMFASetupWrongPassword(t *testing.T) {
	t.Parallel()
	repo := newFakeRepository()
	svc := newTestService(t, repo)
	ctx := context.Background()

	_, err := svc.Register(ctx, RegisterRequest{
		Email:    "alice@example.com",
		Username: "alice",
		Password: "strongpassword",
	})
	if err != nil {
		t.Fatalf("Register() error = %v", err)
	}

	userID := repo.users["alice@example.com"].ID
	_, err = svc.BeginMFASetup(ctx, userID, "wrongpassword")
	if !errors.Is(err, ErrInvalidCredentials) {
		t.Errorf("BeginMFASetup() error = %v, want ErrInvalidCredentials", err)
	}
}

func TestServiceBeginMFASetupAlreadyEnabled(t *testing.T) {
	t.Parallel()
	repo := newFakeRepository()
	svc := newTestService(t, repo)
	ctx := context.Background()

	registerMFAUser(t, svc, repo, "alice@example.com", "strongpassword")

	userID := repo.users["alice@example.com"].ID
	_, err := svc.BeginMFASetup(ctx, userID, "strongpassword")
	if !errors.Is(err, ErrMFAAlreadyEnabled) {
		t.Errorf("BeginMFASetup() error = %v, want ErrMFAAlreadyEnabled", err)
	}
}

func TestServiceConfirmMFASetupSuccess(t *testing.T) {
	t.Parallel()
	repo := newFakeRepository()
	svc := newTestService(t, repo)
	ctx := context.Background()

	_, err := svc.Register(ctx, RegisterRequest{
		Email:    "alice@example.com",
		Username: "alice",
		Password: "strongpassword",
	})
	if err != nil {
		t.Fatalf("Register() error = %v", err)
	}

	userID := repo.users["alice@example.com"].ID
	setup, err := svc.BeginMFASetup(ctx, userID, "strongpassword")
	if err != nil {
		t.Fatalf("BeginMFASetup() error = %v", err)
	}

	code, err := totp.GenerateCode(setup.Secret, time.Now())
	if err != nil {
		t.Fatalf("totp.GenerateCode() error = %v", err)
	}

	codes, err := svc.ConfirmMFASetup(ctx, userID, code)
	if err != nil {
		t.Fatalf("ConfirmMFASetup() error = %v", err)
	}
	if len(codes) != recoveryCodeCount {
		t.Errorf("ConfirmMFASetup() returned %d codes, want %d", len(codes), recoveryCodeCount)
	}
	if !repo.users["alice@example.com"].MFAEnabled {
		t.Error("ConfirmMFASetup() did not enable MFA on user")
	}
}

func TestServiceConfirmMFASetupInvalidCode(t *testing.T) {
	t.Parallel()
	repo := newFakeRepository()
	svc := newTestService(t, repo)
	ctx := context.Background()

	_, err := svc.Register(ctx, RegisterRequest{
		Email:    "alice@example.com",
		Username: "alice",
		Password: "strongpassword",
	})
	if err != nil {
		t.Fatalf("Register() error = %v", err)
	}

	userID := repo.users["alice@example.com"].ID
	_, err = svc.BeginMFASetup(ctx, userID, "strongpassword")
	if err != nil {
		t.Fatalf("BeginMFASetup() error = %v", err)
	}

	_, err = svc.ConfirmMFASetup(ctx, userID, "000000")
	if !errors.Is(err, ErrInvalidMFACode) {
		t.Errorf("ConfirmMFASetup() error = %v, want ErrInvalidMFACode", err)
	}
}

func TestServiceVerifyMFAWithTOTP(t *testing.T) {
	t.Parallel()
	repo := newFakeRepository()
	svc := newTestService(t, repo)
	ctx := context.Background()

	secret := registerMFAUser(t, svc, repo, "alice@example.com", "strongpassword")

	result, err := svc.Login(ctx, LoginRequest{
		Email:    "alice@example.com",
		Password: "strongpassword",
		IP:       "127.0.0.1",
	})
	if err != nil {
		t.Fatalf("Login() error = %v", err)
	}
	if !result.MFARequired {
		t.Fatal("Login() MFARequired = false, want true")
	}

	code, err := totp.GenerateCode(secret, time.Now())
	if err != nil {
		t.Fatalf("totp.GenerateCode() error = %v", err)
	}

	authResult, err := svc.VerifyMFA(ctx, result.Ticket, code)
	if err != nil {
		t.Fatalf("VerifyMFA() error = %v", err)
	}
	if authResult.AccessToken == "" {
		t.Error("VerifyMFA() returned empty access token")
	}
	if !authResult.User.MFAEnabled {
		t.Error("VerifyMFA() user MFAEnabled = false, want true")
	}
}

func TestServiceVerifyMFAWithRecoveryCode(t *testing.T) {
	t.Parallel()
	repo := newFakeRepository()
	svc := newTestService(t, repo)
	ctx := context.Background()

	_ = registerMFAUser(t, svc, repo, "alice@example.com", "strongpassword")

	// Get a recovery code from the repo
	userID := repo.users["alice@example.com"].ID
	codes := repo.recoveryCodes[userID]
	if len(codes) == 0 {
		t.Fatal("no recovery codes found")
	}

	// We need the plaintext code, but the repo only has hashes. Instead, generate new codes through the service and
	// use one of those.
	plainCodes, err := svc.RegenerateRecoveryCodes(ctx, userID, "strongpassword")
	if err != nil {
		t.Fatalf("RegenerateRecoveryCodes() error = %v", err)
	}

	result, err := svc.Login(ctx, LoginRequest{
		Email:    "alice@example.com",
		Password: "strongpassword",
		IP:       "127.0.0.1",
	})
	if err != nil {
		t.Fatalf("Login() error = %v", err)
	}

	authResult, err := svc.VerifyMFA(ctx, result.Ticket, plainCodes[0])
	if err != nil {
		t.Fatalf("VerifyMFA() with recovery code error = %v", err)
	}
	if authResult.AccessToken == "" {
		t.Error("VerifyMFA() returned empty access token")
	}
}

func TestServiceVerifyMFAInvalidTicket(t *testing.T) {
	t.Parallel()
	svc := newTestService(t, newFakeRepository())
	ctx := context.Background()

	_, err := svc.VerifyMFA(ctx, "nonexistent-ticket", "123456")
	if !errors.Is(err, ErrInvalidToken) {
		t.Errorf("VerifyMFA() error = %v, want ErrInvalidToken", err)
	}
}

func TestServiceVerifyMFAInvalidCode(t *testing.T) {
	t.Parallel()
	repo := newFakeRepository()
	svc := newTestService(t, repo)
	ctx := context.Background()

	_ = registerMFAUser(t, svc, repo, "alice@example.com", "strongpassword")

	result, err := svc.Login(ctx, LoginRequest{
		Email:    "alice@example.com",
		Password: "strongpassword",
		IP:       "127.0.0.1",
	})
	if err != nil {
		t.Fatalf("Login() error = %v", err)
	}

	_, err = svc.VerifyMFA(ctx, result.Ticket, "000000")
	if !errors.Is(err, ErrInvalidMFACode) {
		t.Errorf("VerifyMFA() error = %v, want ErrInvalidMFACode", err)
	}
}

func TestServiceDisableMFASuccess(t *testing.T) {
	t.Parallel()
	repo := newFakeRepository()
	svc := newTestService(t, repo)
	ctx := context.Background()

	secret := registerMFAUser(t, svc, repo, "alice@example.com", "strongpassword")
	userID := repo.users["alice@example.com"].ID

	code, err := totp.GenerateCode(secret, time.Now())
	if err != nil {
		t.Fatalf("totp.GenerateCode() error = %v", err)
	}

	err = svc.DisableMFA(ctx, userID, "strongpassword", code)
	if err != nil {
		t.Fatalf("DisableMFA() error = %v", err)
	}
	if repo.users["alice@example.com"].MFAEnabled {
		t.Error("DisableMFA() did not clear MFAEnabled")
	}
}

func TestServiceDisableMFAWithRecoveryCode(t *testing.T) {
	t.Parallel()
	repo := newFakeRepository()
	svc := newTestService(t, repo)
	ctx := context.Background()

	_ = registerMFAUser(t, svc, repo, "alice@example.com", "strongpassword")
	userID := repo.users["alice@example.com"].ID

	plainCodes, err := svc.RegenerateRecoveryCodes(ctx, userID, "strongpassword")
	if err != nil {
		t.Fatalf("RegenerateRecoveryCodes() error = %v", err)
	}

	err = svc.DisableMFA(ctx, userID, "strongpassword", plainCodes[0])
	if err != nil {
		t.Fatalf("DisableMFA() with recovery code error = %v", err)
	}
	if repo.users["alice@example.com"].MFAEnabled {
		t.Error("DisableMFA() did not clear MFAEnabled")
	}
}

func TestServiceDisableMFAWrongPassword(t *testing.T) {
	t.Parallel()
	repo := newFakeRepository()
	svc := newTestService(t, repo)
	ctx := context.Background()

	secret := registerMFAUser(t, svc, repo, "alice@example.com", "strongpassword")
	userID := repo.users["alice@example.com"].ID

	code, err := totp.GenerateCode(secret, time.Now())
	if err != nil {
		t.Fatalf("totp.GenerateCode() error = %v", err)
	}

	err = svc.DisableMFA(ctx, userID, "wrongpassword", code)
	if !errors.Is(err, ErrInvalidCredentials) {
		t.Errorf("DisableMFA() error = %v, want ErrInvalidCredentials", err)
	}
}

func TestServiceDisableMFAWrongCode(t *testing.T) {
	t.Parallel()
	repo := newFakeRepository()
	svc := newTestService(t, repo)
	ctx := context.Background()

	_ = registerMFAUser(t, svc, repo, "alice@example.com", "strongpassword")
	userID := repo.users["alice@example.com"].ID

	err := svc.DisableMFA(ctx, userID, "strongpassword", "000000")
	if !errors.Is(err, ErrInvalidMFACode) {
		t.Errorf("DisableMFA() error = %v, want ErrInvalidMFACode", err)
	}
}

func TestServiceDisableMFANotEnabled(t *testing.T) {
	t.Parallel()
	repo := newFakeRepository()
	svc := newTestService(t, repo)
	ctx := context.Background()

	_, err := svc.Register(ctx, RegisterRequest{
		Email:    "alice@example.com",
		Username: "alice",
		Password: "strongpassword",
	})
	if err != nil {
		t.Fatalf("Register() error = %v", err)
	}

	userID := repo.users["alice@example.com"].ID
	err = svc.DisableMFA(ctx, userID, "strongpassword", "123456")
	if !errors.Is(err, ErrMFANotEnabled) {
		t.Errorf("DisableMFA() error = %v, want ErrMFANotEnabled", err)
	}
}

func TestServiceRegenerateRecoveryCodesSuccess(t *testing.T) {
	t.Parallel()
	repo := newFakeRepository()
	svc := newTestService(t, repo)
	ctx := context.Background()

	_ = registerMFAUser(t, svc, repo, "alice@example.com", "strongpassword")
	userID := repo.users["alice@example.com"].ID

	codes, err := svc.RegenerateRecoveryCodes(ctx, userID, "strongpassword")
	if err != nil {
		t.Fatalf("RegenerateRecoveryCodes() error = %v", err)
	}
	if len(codes) != recoveryCodeCount {
		t.Errorf("RegenerateRecoveryCodes() returned %d codes, want %d", len(codes), recoveryCodeCount)
	}
}

func TestServiceRegenerateRecoveryCodesNotEnabled(t *testing.T) {
	t.Parallel()
	repo := newFakeRepository()
	svc := newTestService(t, repo)
	ctx := context.Background()

	_, err := svc.Register(ctx, RegisterRequest{
		Email:    "alice@example.com",
		Username: "alice",
		Password: "strongpassword",
	})
	if err != nil {
		t.Fatalf("Register() error = %v", err)
	}

	userID := repo.users["alice@example.com"].ID
	_, err = svc.RegenerateRecoveryCodes(ctx, userID, "strongpassword")
	if !errors.Is(err, ErrMFANotEnabled) {
		t.Errorf("RegenerateRecoveryCodes() error = %v, want ErrMFANotEnabled", err)
	}
}

func TestServiceBeginMFASetupNotConfigured(t *testing.T) {
	t.Parallel()
	repo := newFakeRepository()
	_, rdb := setupMiniredis(t)
	bl := disposable.NewBlocklist("", false, 10*time.Second, zerolog.Nop())
	cfg := testConfig()
	cfg.MFAEncryptionKey = "" // simulate unconfigured MFA
	serverRepo := &fakeServerRepo{ownerID: uuid.New()}
	permPub := permission.NewPublisher(rdb)
	svc, err := NewService(repo, rdb, cfg, bl, nil, serverRepo, permPub, zerolog.Nop())
	if err != nil {
		t.Fatalf("NewService() error = %v", err)
	}

	_, err = svc.Register(context.Background(), RegisterRequest{
		Email:    "alice@example.com",
		Username: "alice",
		Password: "strongpassword",
	})
	if err != nil {
		t.Fatalf("Register() error = %v", err)
	}

	userID := repo.users["alice@example.com"].ID
	_, err = svc.BeginMFASetup(context.Background(), userID, "strongpassword")
	if !errors.Is(err, ErrMFANotConfigured) {
		t.Errorf("BeginMFASetup() error = %v, want ErrMFANotConfigured", err)
	}
}

func TestServiceVerifyMFANotConfigured(t *testing.T) {
	t.Parallel()
	repo := newFakeRepository()
	svc := newTestService(t, repo)
	ctx := context.Background()

	_ = registerMFAUser(t, svc, repo, "alice@example.com", "strongpassword")

	result, err := svc.Login(ctx, LoginRequest{
		Email:    "alice@example.com",
		Password: "strongpassword",
		IP:       "127.0.0.1",
	})
	if err != nil {
		t.Fatalf("Login() error = %v", err)
	}

	// Simulate removing the encryption key after users have enrolled.
	svc.config.MFAEncryptionKey = ""

	_, err = svc.VerifyMFA(ctx, result.Ticket, "123456")
	if !errors.Is(err, ErrMFANotConfigured) {
		t.Errorf("VerifyMFA() error = %v, want ErrMFANotConfigured", err)
	}
}

func TestServiceConfirmMFASetupNotConfigured(t *testing.T) {
	t.Parallel()
	repo := newFakeRepository()
	svc := newTestService(t, repo)
	ctx := context.Background()

	_, err := svc.Register(ctx, RegisterRequest{
		Email:    "alice@example.com",
		Username: "alice",
		Password: "strongpassword",
	})
	if err != nil {
		t.Fatalf("Register() error = %v", err)
	}

	userID := repo.users["alice@example.com"].ID
	_, err = svc.BeginMFASetup(ctx, userID, "strongpassword")
	if err != nil {
		t.Fatalf("BeginMFASetup() error = %v", err)
	}

	// Simulate removing the encryption key after the setup flow has begun.
	svc.config.MFAEncryptionKey = ""

	_, err = svc.ConfirmMFASetup(ctx, userID, "123456")
	if !errors.Is(err, ErrMFANotConfigured) {
		t.Errorf("ConfirmMFASetup() error = %v, want ErrMFANotConfigured", err)
	}
}

func TestServiceDisableMFANotConfigured(t *testing.T) {
	t.Parallel()
	repo := newFakeRepository()
	svc := newTestService(t, repo)
	ctx := context.Background()

	_ = registerMFAUser(t, svc, repo, "alice@example.com", "strongpassword")
	userID := repo.users["alice@example.com"].ID

	// Simulate removing the encryption key after users have enrolled.
	svc.config.MFAEncryptionKey = ""

	err := svc.DisableMFA(ctx, userID, "strongpassword", "123456")
	if !errors.Is(err, ErrMFANotConfigured) {
		t.Errorf("DisableMFA() error = %v, want ErrMFANotConfigured", err)
	}
}

// --- Account deletion tests ---

// newTestServiceWithServerRepo creates a test service with a specific fakeServerRepo so tests can control the owner ID.
func newTestServiceWithServerRepo(t *testing.T, repo *fakeRepository, srvRepo *fakeServerRepo) *Service {
	t.Helper()
	_, rdb := setupMiniredis(t)
	bl := disposable.NewBlocklist("", false, 10*time.Second, zerolog.Nop())
	permPub := permission.NewPublisher(rdb)
	svc, err := NewService(repo, rdb, testConfig(), bl, nil, srvRepo, permPub, zerolog.Nop())
	if err != nil {
		t.Fatalf("NewService() error = %v", err)
	}
	return svc
}

func TestDeleteAccountSuccess(t *testing.T) {
	t.Parallel()
	repo := newFakeRepository()
	srvRepo := &fakeServerRepo{ownerID: uuid.New()}
	svc := newTestServiceWithServerRepo(t, repo, srvRepo)
	ctx := context.Background()

	_, err := svc.Register(ctx, RegisterRequest{
		Email:    "alice@example.com",
		Username: "alice",
		Password: "strongpassword",
	})
	if err != nil {
		t.Fatalf("Register() error = %v", err)
	}

	userID := repo.users["alice@example.com"].ID

	if err := svc.DeleteAccount(ctx, userID, "strongpassword"); err != nil {
		t.Fatalf("DeleteAccount() error = %v", err)
	}

	// User should be deleted.
	if _, exists := repo.users["alice@example.com"]; exists {
		t.Error("DeleteAccount() did not remove user from repository")
	}

	// Tombstones should exist for email and username.
	if len(repo.tombstones) != 2 {
		t.Errorf("DeleteAccount() created %d tombstones, want 2", len(repo.tombstones))
	}
}

func TestDeleteAccountWrongPassword(t *testing.T) {
	t.Parallel()
	repo := newFakeRepository()
	srvRepo := &fakeServerRepo{ownerID: uuid.New()}
	svc := newTestServiceWithServerRepo(t, repo, srvRepo)
	ctx := context.Background()

	_, err := svc.Register(ctx, RegisterRequest{
		Email:    "alice@example.com",
		Username: "alice",
		Password: "strongpassword",
	})
	if err != nil {
		t.Fatalf("Register() error = %v", err)
	}

	userID := repo.users["alice@example.com"].ID

	err = svc.DeleteAccount(ctx, userID, "wrongpassword")
	if !errors.Is(err, ErrInvalidCredentials) {
		t.Errorf("DeleteAccount() error = %v, want ErrInvalidCredentials", err)
	}
}

func TestDeleteAccountServerOwner(t *testing.T) {
	t.Parallel()
	repo := newFakeRepository()
	srvRepo := &fakeServerRepo{ownerID: uuid.New()} // will be overwritten below
	svc := newTestServiceWithServerRepo(t, repo, srvRepo)
	ctx := context.Background()

	_, err := svc.Register(ctx, RegisterRequest{
		Email:    "owner@example.com",
		Username: "owner",
		Password: "strongpassword",
	})
	if err != nil {
		t.Fatalf("Register() error = %v", err)
	}

	userID := repo.users["owner@example.com"].ID
	srvRepo.ownerID = userID // make this user the server owner

	err = svc.DeleteAccount(ctx, userID, "strongpassword")
	if !errors.Is(err, ErrServerOwner) {
		t.Errorf("DeleteAccount() error = %v, want ErrServerOwner", err)
	}
}

func TestDeleteAccountUserNotFound(t *testing.T) {
	t.Parallel()
	repo := newFakeRepository()
	srvRepo := &fakeServerRepo{ownerID: uuid.New()}
	svc := newTestServiceWithServerRepo(t, repo, srvRepo)

	err := svc.DeleteAccount(context.Background(), uuid.New(), "strongpassword")
	if err == nil {
		t.Fatal("DeleteAccount() should fail for nonexistent user")
	}
}

func TestRegisterBlockedByEmailTombstone(t *testing.T) {
	t.Parallel()
	repo := newFakeRepository()
	srvRepo := &fakeServerRepo{ownerID: uuid.New()}
	svc := newTestServiceWithServerRepo(t, repo, srvRepo)
	ctx := context.Background()

	_, err := svc.Register(ctx, RegisterRequest{
		Email:    "alice@example.com",
		Username: "alice",
		Password: "strongpassword",
	})
	if err != nil {
		t.Fatalf("Register() error = %v", err)
	}

	userID := repo.users["alice@example.com"].ID
	if err := svc.DeleteAccount(ctx, userID, "strongpassword"); err != nil {
		t.Fatalf("DeleteAccount() error = %v", err)
	}

	// Re-register with same email should be blocked.
	_, err = svc.Register(ctx, RegisterRequest{
		Email:    "alice@example.com",
		Username: "alice2",
		Password: "strongpassword",
	})
	if !errors.Is(err, ErrAccountTombstoned) {
		t.Errorf("Register() error = %v, want ErrAccountTombstoned", err)
	}
}

func TestRegisterBlockedByUsernameTombstone(t *testing.T) {
	t.Parallel()
	repo := newFakeRepository()
	srvRepo := &fakeServerRepo{ownerID: uuid.New()}
	svc := newTestServiceWithServerRepo(t, repo, srvRepo)
	ctx := context.Background()

	_, err := svc.Register(ctx, RegisterRequest{
		Email:    "alice@example.com",
		Username: "Alice",
		Password: "strongpassword",
	})
	if err != nil {
		t.Fatalf("Register() error = %v", err)
	}

	userID := repo.users["alice@example.com"].ID
	if err := svc.DeleteAccount(ctx, userID, "strongpassword"); err != nil {
		t.Fatalf("DeleteAccount() error = %v", err)
	}

	// Re-register with same username (different case) should be blocked.
	_, err = svc.Register(ctx, RegisterRequest{
		Email:    "bob@example.com",
		Username: "alice",
		Password: "strongpassword",
	})
	if !errors.Is(err, ErrAccountTombstoned) {
		t.Errorf("Register() error = %v, want ErrAccountTombstoned", err)
	}
}

func TestRegisterUsernameTombstoneDisabled(t *testing.T) {
	t.Parallel()
	repo := newFakeRepository()
	_, rdb := setupMiniredis(t)
	bl := disposable.NewBlocklist("", false, 10*time.Second, zerolog.Nop())
	srvRepo := &fakeServerRepo{ownerID: uuid.New()}
	permPub := permission.NewPublisher(rdb)
	cfg := testConfig()
	cfg.DeletionTombstoneUsernames = false
	svc, err := NewService(repo, rdb, cfg, bl, nil, srvRepo, permPub, zerolog.Nop())
	if err != nil {
		t.Fatalf("NewService() error = %v", err)
	}
	ctx := context.Background()

	_, err = svc.Register(ctx, RegisterRequest{
		Email:    "alice@example.com",
		Username: "alice",
		Password: "strongpassword",
	})
	if err != nil {
		t.Fatalf("Register() error = %v", err)
	}

	userID := repo.users["alice@example.com"].ID
	if err := svc.DeleteAccount(ctx, userID, "strongpassword"); err != nil {
		t.Fatalf("DeleteAccount() error = %v", err)
	}

	// Only email tombstone should exist (no username tombstone).
	if len(repo.tombstones) != 1 {
		t.Errorf("DeleteAccount() created %d tombstones, want 1 (email only)", len(repo.tombstones))
	}

	// Re-register with same username should succeed (tombstone not created).
	_, err = svc.Register(ctx, RegisterRequest{
		Email:    "bob@example.com",
		Username: "alice",
		Password: "strongpassword",
	})
	if err != nil {
		t.Errorf("Register() with same username should succeed when tombstone disabled, got error = %v", err)
	}
}

func TestRegisterUsernameTombstoneRetroactive(t *testing.T) {
	t.Parallel()
	repo := newFakeRepository()
	_, rdb := setupMiniredis(t)
	bl := disposable.NewBlocklist("", false, 10*time.Second, zerolog.Nop())
	srvRepo := &fakeServerRepo{ownerID: uuid.New()}
	permPub := permission.NewPublisher(rdb)

	// Create service with tombstone usernames enabled.
	cfg := testConfig()
	svc, err := NewService(repo, rdb, cfg, bl, nil, srvRepo, permPub, zerolog.Nop())
	if err != nil {
		t.Fatalf("NewService() error = %v", err)
	}
	ctx := context.Background()

	_, err = svc.Register(ctx, RegisterRequest{
		Email:    "alice@example.com",
		Username: "alice",
		Password: "strongpassword",
	})
	if err != nil {
		t.Fatalf("Register() error = %v", err)
	}

	userID := repo.users["alice@example.com"].ID
	if err := svc.DeleteAccount(ctx, userID, "strongpassword"); err != nil {
		t.Fatalf("DeleteAccount() error = %v", err)
	}

	// Now disable tombstone usernames in config. Existing tombstones should still block.
	cfg.DeletionTombstoneUsernames = false
	svc2, err := NewService(repo, rdb, cfg, bl, nil, srvRepo, permPub, zerolog.Nop())
	if err != nil {
		t.Fatalf("NewService() error = %v", err)
	}

	_, err = svc2.Register(ctx, RegisterRequest{
		Email:    "bob@example.com",
		Username: "alice",
		Password: "strongpassword",
	})
	if !errors.Is(err, ErrAccountTombstoned) {
		t.Errorf("Register() error = %v, want ErrAccountTombstoned (retroactive enforcement)", err)
	}
}
