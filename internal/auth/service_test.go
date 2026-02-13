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
	"github.com/rs/zerolog"

	"github.com/uncord-chat/uncord-server/internal/config"
	"github.com/uncord-chat/uncord-server/internal/disposable"
	"github.com/uncord-chat/uncord-server/internal/user"
)

// fakeRepository implements user.Repository for unit tests.
type fakeRepository struct {
	users         map[string]*user.Credentials // keyed by email
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
	return &fakeRepository{users: make(map[string]*user.Credentials)}
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

func testConfig() *config.Config {
	return &config.Config{
		ServerURL:         "https://test.example.com",
		ServerEnv:         "production",
		JWTSecret:         "test-secret-at-least-32-chars-long!!",
		JWTAccessTTL:      15 * time.Minute,
		JWTRefreshTTL:     7 * 24 * time.Hour,
		Argon2Memory:      64 * 1024,
		Argon2Iterations:  1, // fast for tests
		Argon2Parallelism: 1,
		Argon2SaltLength:  16,
		Argon2KeyLength:   32,
	}
}

func newTestService(t *testing.T, repo *fakeRepository) *Service {
	t.Helper()
	_, rdb := setupMiniredis(t)
	bl := disposable.NewBlocklist("", false, zerolog.Nop())
	svc, err := NewService(repo, rdb, testConfig(), bl, nil, zerolog.Nop())
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

	bl := disposable.NewBlocklist(srv.URL, true, zerolog.Nop())
	svc, err := NewService(repo, rdb, testConfig(), bl, nil, zerolog.Nop())
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
	if result.User.Email != "alice@example.com" {
		t.Errorf("Login() email = %q, want %q", result.User.Email, "alice@example.com")
	}
	if result.AccessToken == "" {
		t.Error("Login() returned empty access token")
	}
	if result.RefreshToken == "" {
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

	_, err = svc.Login(ctx, LoginRequest{
		Email:    "alice@example.com",
		Password: "strongpassword",
		IP:       "127.0.0.1",
	})
	if !errors.Is(err, ErrMFARequired) {
		t.Errorf("Login() error = %v, want ErrMFARequired", err)
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

func (f *fakeSender) SendVerification(to, token, serverURL, serverName string) error {
	f.calls = append(f.calls, fakeSendCall{To: to, Token: token, ServerURL: serverURL, ServerName: serverName})
	return f.err
}

func newTestServiceWithSender(t *testing.T, repo *fakeRepository, sender Sender) *Service {
	t.Helper()
	_, rdb := setupMiniredis(t)
	bl := disposable.NewBlocklist("", false, zerolog.Nop())
	svc, err := NewService(repo, rdb, testConfig(), bl, sender, zerolog.Nop())
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
