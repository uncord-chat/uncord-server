package page

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/alicebob/miniredis/v2"
	"github.com/gofiber/fiber/v3"
	"github.com/google/uuid"
	"github.com/redis/go-redis/v9"
	"github.com/rs/zerolog"

	"github.com/uncord-chat/uncord-server/internal/auth"
	"github.com/uncord-chat/uncord-server/internal/config"
	"github.com/uncord-chat/uncord-server/internal/disposable"
	"github.com/uncord-chat/uncord-server/internal/permission"
	"github.com/uncord-chat/uncord-server/internal/server"
	"github.com/uncord-chat/uncord-server/internal/user"
)

// testTimeout extends the default app.Test() deadline so that argon2 hashing under the race detector does not trigger
// a spurious i/o timeout.
var testTimeout = fiber.TestConfig{Timeout: 5 * time.Second}

// fakeRepo implements user.Repository for handler tests.
type fakeRepo struct {
	users map[string]*user.Credentials
}

func newFakeRepo() *fakeRepo {
	return &fakeRepo{users: make(map[string]*user.Credentials)}
}

func (r *fakeRepo) Create(_ context.Context, params user.CreateParams) (uuid.UUID, error) {
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

func (r *fakeRepo) GetByEmail(_ context.Context, email string) (*user.Credentials, error) {
	c, ok := r.users[email]
	if !ok {
		return nil, user.ErrNotFound
	}
	return c, nil
}

func (r *fakeRepo) VerifyEmail(_ context.Context, token string) (uuid.UUID, error) {
	if token == "valid-token" {
		return uuid.New(), nil
	}
	return uuid.Nil, user.ErrInvalidToken
}

func (r *fakeRepo) ReplaceVerificationToken(context.Context, uuid.UUID, string, time.Time, time.Duration) error {
	return nil
}

func (r *fakeRepo) GetByID(_ context.Context, id uuid.UUID) (*user.User, error) {
	for _, c := range r.users {
		if c.ID == id {
			cpy := c.User
			return &cpy, nil
		}
	}
	return nil, user.ErrNotFound
}

func (r *fakeRepo) Update(_ context.Context, id uuid.UUID, params user.UpdateParams) (*user.User, error) {
	for _, c := range r.users {
		if c.ID == id {
			if params.DisplayName != nil {
				trimmed := strings.TrimSpace(*params.DisplayName)
				c.DisplayName = &trimmed
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

func (r *fakeRepo) RecordLoginAttempt(context.Context, string, string, bool) error { return nil }
func (r *fakeRepo) UpdatePasswordHash(context.Context, uuid.UUID, string) error    { return nil }

func (r *fakeRepo) GetCredentialsByID(_ context.Context, id uuid.UUID) (*user.Credentials, error) {
	for _, c := range r.users {
		if c.ID == id {
			return c, nil
		}
	}
	return nil, user.ErrNotFound
}

func (r *fakeRepo) EnableMFA(context.Context, uuid.UUID, string, []string) error { return nil }
func (r *fakeRepo) DisableMFA(context.Context, uuid.UUID) error                  { return nil }
func (r *fakeRepo) GetUnusedRecoveryCodes(context.Context, uuid.UUID) ([]user.MFARecoveryCode, error) {
	return nil, nil
}
func (r *fakeRepo) UseRecoveryCode(context.Context, uuid.UUID) error                { return nil }
func (r *fakeRepo) ReplaceRecoveryCodes(context.Context, uuid.UUID, []string) error { return nil }
func (r *fakeRepo) DeleteWithTombstones(context.Context, uuid.UUID, []user.Tombstone) error {
	return nil
}
func (r *fakeRepo) CheckTombstone(context.Context, user.TombstoneType, string) (bool, error) {
	return false, nil
}

type fakeServerRepo struct{}

func (r *fakeServerRepo) Get(context.Context) (*server.Config, error) {
	return &server.Config{OwnerID: uuid.New()}, nil
}

func (r *fakeServerRepo) Update(context.Context, server.UpdateParams) (*server.Config, error) {
	return nil, nil
}

func testVerifyHandler(t *testing.T) *fiber.App {
	t.Helper()
	mr := miniredis.RunT(t)
	rdb := redis.NewClient(&redis.Options{Addr: mr.Addr()})

	cfg := &config.Config{
		ServerName:                 "Test Server",
		ServerURL:                  "https://test.example.com",
		ServerEnv:                  "production",
		JWTSecret:                  "test-secret-at-least-32-chars-long!!",
		JWTAccessTTL:               15 * time.Minute,
		JWTRefreshTTL:              7 * 24 * time.Hour,
		Argon2Memory:               64 * 1024,
		Argon2Iterations:           1,
		Argon2Parallelism:          1,
		Argon2SaltLength:           16,
		Argon2KeyLength:            32,
		MFATicketTTL:               5 * time.Minute,
		ServerSecret:               "0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef",
		DeletionTombstoneUsernames: true,
	}

	bl := disposable.NewBlocklist("", false, 10*time.Second, zerolog.Nop())
	permPub := permission.NewPublisher(rdb)
	svc, err := auth.NewService(newFakeRepo(), rdb, cfg, bl, nil, &fakeServerRepo{}, permPub, zerolog.Nop())
	if err != nil {
		t.Fatalf("NewService() error = %v", err)
	}

	handler := NewVerifyHandler(svc, cfg.ServerName, nil, zerolog.Nop())
	app := fiber.New()
	app.Get("/verify-email", handler.VerifyEmail)
	return app
}

func readBody(t *testing.T, resp *http.Response) string {
	t.Helper()
	defer func() { _ = resp.Body.Close() }()
	b, err := io.ReadAll(resp.Body)
	if err != nil {
		t.Fatalf("read response body: %v", err)
	}
	return string(b)
}

func doReq(t *testing.T, app *fiber.App, req *http.Request) *http.Response {
	t.Helper()
	resp, err := app.Test(req, testTimeout)
	if err != nil {
		t.Fatalf("app.Test() error = %v", err)
	}
	return resp
}

func TestVerifyEmail_MissingToken(t *testing.T) {
	t.Parallel()
	app := testVerifyHandler(t)

	resp := doReq(t, app, httptest.NewRequest(http.MethodGet, "/verify-email", nil))
	body := readBody(t, resp)

	if resp.StatusCode != fiber.StatusBadRequest {
		t.Errorf("status = %d, want %d", resp.StatusCode, fiber.StatusBadRequest)
	}
	ct := resp.Header.Get("Content-Type")
	if !strings.HasPrefix(ct, "text/html") {
		t.Errorf("Content-Type = %q, want text/html", ct)
	}
	if !strings.Contains(body, "Missing Token") {
		t.Errorf("body does not contain expected heading, got: %s", body)
	}
}

func TestVerifyEmail_InvalidToken(t *testing.T) {
	t.Parallel()
	app := testVerifyHandler(t)

	resp := doReq(t, app, httptest.NewRequest(http.MethodGet, "/verify-email?token=bad-token", nil))
	body := readBody(t, resp)

	if resp.StatusCode != fiber.StatusBadRequest {
		t.Errorf("status = %d, want %d", resp.StatusCode, fiber.StatusBadRequest)
	}
	ct := resp.Header.Get("Content-Type")
	if !strings.HasPrefix(ct, "text/html") {
		t.Errorf("Content-Type = %q, want text/html", ct)
	}
	if !strings.Contains(body, "Verification Failed") {
		t.Errorf("body does not contain expected heading, got: %s", body)
	}
}

func TestVerifyEmail_Success(t *testing.T) {
	t.Parallel()
	app := testVerifyHandler(t)

	resp := doReq(t, app, httptest.NewRequest(http.MethodGet, "/verify-email?token=valid-token", nil))
	body := readBody(t, resp)

	if resp.StatusCode != fiber.StatusOK {
		t.Errorf("status = %d, want %d", resp.StatusCode, fiber.StatusOK)
	}
	ct := resp.Header.Get("Content-Type")
	if !strings.HasPrefix(ct, "text/html") {
		t.Errorf("Content-Type = %q, want text/html", ct)
	}
	if !strings.Contains(body, "Email Verified") {
		t.Errorf("body does not contain expected heading, got: %s", body)
	}
}
