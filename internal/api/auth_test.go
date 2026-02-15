package api

import (
	"context"
	"encoding/json"
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
	apierrors "github.com/uncord-chat/uncord-protocol/errors"

	"github.com/uncord-chat/uncord-server/internal/auth"
	"github.com/uncord-chat/uncord-server/internal/config"
	"github.com/uncord-chat/uncord-server/internal/disposable"
	"github.com/uncord-chat/uncord-server/internal/permission"
	"github.com/uncord-chat/uncord-server/internal/server"
	"github.com/uncord-chat/uncord-server/internal/user"
)

// testTimeout extends the default app.Test() deadline so that argon2 hashing under the race detector does not trigger
// a spurious i/o timeout. MFA operations hash multiple recovery codes, requiring a wider margin.
var testTimeout = fiber.TestConfig{Timeout: 30 * time.Second}

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

func (r *fakeRepo) EnableMFA(_ context.Context, userID uuid.UUID, encryptedSecret string, _ []string) error {
	for _, c := range r.users {
		if c.ID == userID {
			c.MFAEnabled = true
			c.MFASecret = &encryptedSecret
			return nil
		}
	}
	return user.ErrNotFound
}

func (r *fakeRepo) DisableMFA(_ context.Context, userID uuid.UUID) error {
	for _, c := range r.users {
		if c.ID == userID {
			c.MFAEnabled = false
			c.MFASecret = nil
			return nil
		}
	}
	return user.ErrNotFound
}

func (r *fakeRepo) GetUnusedRecoveryCodes(context.Context, uuid.UUID) ([]user.MFARecoveryCode, error) {
	return nil, nil
}

func (r *fakeRepo) UseRecoveryCode(context.Context, uuid.UUID) error { return nil }

func (r *fakeRepo) ReplaceRecoveryCodes(context.Context, uuid.UUID, []string) error { return nil }
func (r *fakeRepo) DeleteWithTombstones(context.Context, uuid.UUID, []user.Tombstone) error {
	return nil
}
func (r *fakeRepo) CheckTombstone(context.Context, user.TombstoneType, string) (bool, error) {
	return false, nil
}

func testAuthConfig() *config.Config {
	return &config.Config{
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
		MFAEncryptionKey:           "0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef",
		MFATicketTTL:               5 * time.Minute,
		ServerSecret:               "0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef",
		DeletionTombstoneUsernames: true,
	}
}

func testAuthHandler(t *testing.T) (*AuthHandler, *fiber.App) {
	t.Helper()
	mr := miniredis.RunT(t)
	rdb := redis.NewClient(&redis.Options{Addr: mr.Addr()})

	_ = mr
	bl := disposable.NewBlocklist("", false, 10*time.Second, zerolog.Nop())
	permPub := permission.NewPublisher(rdb)
	srvRepo := &fakeServerRepo{cfg: &server.Config{OwnerID: uuid.New()}}
	svc, err := auth.NewService(newFakeRepo(), rdb, testAuthConfig(), bl, nil, srvRepo, permPub, zerolog.Nop())
	if err != nil {
		t.Fatalf("NewService() error = %v", err)
	}
	handler := NewAuthHandler(svc, zerolog.Nop())

	app := fiber.New()
	app.Post("/register", handler.Register)
	app.Post("/login", handler.Login)
	app.Post("/refresh", handler.Refresh)
	app.Post("/verify-email", handler.VerifyEmail)
	app.Post("/mfa/verify", handler.MFAVerify)

	return handler, app
}

// --- response parsing helpers ---

type successEnvelope struct {
	Data json.RawMessage `json:"data"`
}

type errorEnvelope struct {
	Error struct {
		Code    string `json:"code"`
		Message string `json:"message"`
	} `json:"error"`
}

func readBody(t *testing.T, resp *http.Response) []byte {
	t.Helper()
	defer func() { _ = resp.Body.Close() }()
	b, err := io.ReadAll(resp.Body)
	if err != nil {
		t.Fatalf("read response body: %v", err)
	}
	return b
}

func parseError(t *testing.T, body []byte) errorEnvelope {
	t.Helper()
	var env errorEnvelope
	if err := json.Unmarshal(body, &env); err != nil {
		t.Fatalf("unmarshal error response %q: %v", string(body), err)
	}
	return env
}

func parseSuccess(t *testing.T, body []byte) successEnvelope {
	t.Helper()
	var env successEnvelope
	if err := json.Unmarshal(body, &env); err != nil {
		t.Fatalf("unmarshal success response %q: %v", string(body), err)
	}
	return env
}

func jsonReq(method, url, body string) *http.Request {
	req := httptest.NewRequest(method, url, strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	return req
}

// doReq sends a request through app.Test with the extended test timeout.
func doReq(t *testing.T, app *fiber.App, req *http.Request) *http.Response {
	t.Helper()
	resp, err := app.Test(req, testTimeout)
	if err != nil {
		t.Fatalf("app.Test() error = %v", err)
	}
	return resp
}

// --- Register handler tests ---

func TestRegisterHandler_InvalidJSON(t *testing.T) {
	t.Parallel()
	_, app := testAuthHandler(t)

	resp := doReq(t, app, jsonReq(http.MethodPost, "/register", "not json"))
	body := readBody(t, resp)

	if resp.StatusCode != fiber.StatusBadRequest {
		t.Errorf("status = %d, want %d", resp.StatusCode, fiber.StatusBadRequest)
	}
	env := parseError(t, body)
	if env.Error.Code != string(apierrors.InvalidBody) {
		t.Errorf("error code = %q, want %q", env.Error.Code, apierrors.InvalidBody)
	}
}

func TestRegisterHandler_ValidationErrors(t *testing.T) {
	t.Parallel()
	_, app := testAuthHandler(t)

	tests := []struct {
		name     string
		body     string
		wantCode apierrors.Code
	}{
		{
			"invalid email",
			`{"email":"bad","username":"alice","password":"strongpassword"}`,
			apierrors.InvalidEmail,
		},
		{
			"username too short",
			`{"email":"alice@example.com","username":"a","password":"strongpassword"}`,
			apierrors.InvalidUsername,
		},
		{
			"password too short",
			`{"email":"alice@example.com","username":"alice","password":"short"}`,
			apierrors.InvalidPassword,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			resp := doReq(t, app, jsonReq(http.MethodPost, "/register", tt.body))
			body := readBody(t, resp)

			if resp.StatusCode != fiber.StatusBadRequest {
				t.Errorf("status = %d, want %d", resp.StatusCode, fiber.StatusBadRequest)
			}
			env := parseError(t, body)
			if env.Error.Code != string(tt.wantCode) {
				t.Errorf("error code = %q, want %q", env.Error.Code, tt.wantCode)
			}
		})
	}
}

func TestRegisterHandler_Success(t *testing.T) {
	t.Parallel()
	_, app := testAuthHandler(t)

	resp := doReq(t, app, jsonReq(http.MethodPost, "/register",
		`{"email":"alice@example.com","username":"alice","password":"strongpassword"}`))
	body := readBody(t, resp)

	if resp.StatusCode != fiber.StatusCreated {
		t.Errorf("status = %d, want %d", resp.StatusCode, fiber.StatusCreated)
	}

	env := parseSuccess(t, body)
	var authResp struct {
		User struct {
			Email    string `json:"email"`
			Username string `json:"username"`
		} `json:"user"`
		AccessToken  string `json:"access_token"`
		RefreshToken string `json:"refresh_token"`
	}
	if err := json.Unmarshal(env.Data, &authResp); err != nil {
		t.Fatalf("unmarshal auth response: %v", err)
	}
	if authResp.User.Email != "alice@example.com" {
		t.Errorf("email = %q, want %q", authResp.User.Email, "alice@example.com")
	}
	if authResp.AccessToken == "" {
		t.Error("access_token is empty")
	}
	if authResp.RefreshToken == "" {
		t.Error("refresh_token is empty")
	}
}

// --- Login handler tests ---

func TestLoginHandler_InvalidJSON(t *testing.T) {
	t.Parallel()
	_, app := testAuthHandler(t)

	resp := doReq(t, app, jsonReq(http.MethodPost, "/login", "{bad"))
	body := readBody(t, resp)

	if resp.StatusCode != fiber.StatusBadRequest {
		t.Errorf("status = %d, want %d", resp.StatusCode, fiber.StatusBadRequest)
	}
	env := parseError(t, body)
	if env.Error.Code != string(apierrors.InvalidBody) {
		t.Errorf("error code = %q, want %q", env.Error.Code, apierrors.InvalidBody)
	}
}

func TestLoginHandler_InvalidCredentials(t *testing.T) {
	t.Parallel()
	_, app := testAuthHandler(t)

	resp := doReq(t, app, jsonReq(http.MethodPost, "/login",
		`{"email":"nobody@example.com","password":"strongpassword"}`))
	body := readBody(t, resp)

	if resp.StatusCode != fiber.StatusUnauthorized {
		t.Errorf("status = %d, want %d", resp.StatusCode, fiber.StatusUnauthorized)
	}
	env := parseError(t, body)
	if env.Error.Code != string(apierrors.InvalidCredentials) {
		t.Errorf("error code = %q, want %q", env.Error.Code, apierrors.InvalidCredentials)
	}
}

func TestLoginHandler_Success(t *testing.T) {
	t.Parallel()
	_, app := testAuthHandler(t)

	// Register first.
	resp := doReq(t, app, jsonReq(http.MethodPost, "/register",
		`{"email":"bob@example.com","username":"bob","password":"strongpassword"}`))
	readBody(t, resp)

	// Login.
	resp = doReq(t, app, jsonReq(http.MethodPost, "/login",
		`{"email":"bob@example.com","password":"strongpassword"}`))
	body := readBody(t, resp)

	if resp.StatusCode != fiber.StatusOK {
		t.Errorf("status = %d, want %d", resp.StatusCode, fiber.StatusOK)
	}

	env := parseSuccess(t, body)
	var authResp struct {
		AccessToken  string `json:"access_token"`
		RefreshToken string `json:"refresh_token"`
	}
	if err := json.Unmarshal(env.Data, &authResp); err != nil {
		t.Fatalf("unmarshal auth response: %v", err)
	}
	if authResp.AccessToken == "" {
		t.Error("access_token is empty")
	}
	if authResp.RefreshToken == "" {
		t.Error("refresh_token is empty")
	}
}

// --- Refresh handler tests ---

func TestRefreshHandler_InvalidJSON(t *testing.T) {
	t.Parallel()
	_, app := testAuthHandler(t)

	resp := doReq(t, app, jsonReq(http.MethodPost, "/refresh", "%%%"))
	body := readBody(t, resp)

	if resp.StatusCode != fiber.StatusBadRequest {
		t.Errorf("status = %d, want %d", resp.StatusCode, fiber.StatusBadRequest)
	}
	env := parseError(t, body)
	if env.Error.Code != string(apierrors.InvalidBody) {
		t.Errorf("error code = %q, want %q", env.Error.Code, apierrors.InvalidBody)
	}
}

func TestRefreshHandler_MissingToken(t *testing.T) {
	t.Parallel()
	_, app := testAuthHandler(t)

	resp := doReq(t, app, jsonReq(http.MethodPost, "/refresh", `{}`))
	body := readBody(t, resp)

	if resp.StatusCode != fiber.StatusBadRequest {
		t.Errorf("status = %d, want %d", resp.StatusCode, fiber.StatusBadRequest)
	}
	env := parseError(t, body)
	if env.Error.Code != string(apierrors.InvalidBody) {
		t.Errorf("error code = %q, want %q", env.Error.Code, apierrors.InvalidBody)
	}
}

func TestRefreshHandler_Success(t *testing.T) {
	t.Parallel()
	_, app := testAuthHandler(t)

	// Register to obtain tokens.
	resp := doReq(t, app, jsonReq(http.MethodPost, "/register",
		`{"email":"carol@example.com","username":"carol","password":"strongpassword"}`))
	regBody := readBody(t, resp)
	regEnv := parseSuccess(t, regBody)

	var regData struct {
		RefreshToken string `json:"refresh_token"`
	}
	if err := json.Unmarshal(regEnv.Data, &regData); err != nil {
		t.Fatalf("unmarshal register response: %v", err)
	}

	// Refresh.
	resp = doReq(t, app, jsonReq(http.MethodPost, "/refresh",
		`{"refresh_token":"`+regData.RefreshToken+`"}`))
	body := readBody(t, resp)

	if resp.StatusCode != fiber.StatusOK {
		t.Errorf("status = %d, want %d", resp.StatusCode, fiber.StatusOK)
	}

	env := parseSuccess(t, body)
	var tokenResp struct {
		AccessToken  string `json:"access_token"`
		RefreshToken string `json:"refresh_token"`
	}
	if err := json.Unmarshal(env.Data, &tokenResp); err != nil {
		t.Fatalf("unmarshal token response: %v", err)
	}
	if tokenResp.AccessToken == "" {
		t.Error("access_token is empty")
	}
	if tokenResp.RefreshToken == "" {
		t.Error("refresh_token is empty")
	}
	if tokenResp.RefreshToken == regData.RefreshToken {
		t.Error("refresh_token was not rotated")
	}
}

// --- VerifyEmail handler tests ---

func TestVerifyEmailHandler_InvalidJSON(t *testing.T) {
	t.Parallel()
	_, app := testAuthHandler(t)

	resp := doReq(t, app, jsonReq(http.MethodPost, "/verify-email", "{{"))
	body := readBody(t, resp)

	if resp.StatusCode != fiber.StatusBadRequest {
		t.Errorf("status = %d, want %d", resp.StatusCode, fiber.StatusBadRequest)
	}
	env := parseError(t, body)
	if env.Error.Code != string(apierrors.InvalidBody) {
		t.Errorf("error code = %q, want %q", env.Error.Code, apierrors.InvalidBody)
	}
}

func TestVerifyEmailHandler_MissingToken(t *testing.T) {
	t.Parallel()
	_, app := testAuthHandler(t)

	resp := doReq(t, app, jsonReq(http.MethodPost, "/verify-email", `{}`))
	body := readBody(t, resp)

	if resp.StatusCode != fiber.StatusBadRequest {
		t.Errorf("status = %d, want %d", resp.StatusCode, fiber.StatusBadRequest)
	}
	env := parseError(t, body)
	if env.Error.Code != string(apierrors.InvalidBody) {
		t.Errorf("error code = %q, want %q", env.Error.Code, apierrors.InvalidBody)
	}
}

func TestVerifyEmailHandler_InvalidToken(t *testing.T) {
	t.Parallel()
	_, app := testAuthHandler(t)

	resp := doReq(t, app, jsonReq(http.MethodPost, "/verify-email", `{"token":"bad-token"}`))
	body := readBody(t, resp)

	if resp.StatusCode != fiber.StatusBadRequest {
		t.Errorf("status = %d, want %d", resp.StatusCode, fiber.StatusBadRequest)
	}
	env := parseError(t, body)
	if env.Error.Code != string(apierrors.InvalidToken) {
		t.Errorf("error code = %q, want %q", env.Error.Code, apierrors.InvalidToken)
	}
}

func TestVerifyEmailHandler_Success(t *testing.T) {
	t.Parallel()
	_, app := testAuthHandler(t)

	resp := doReq(t, app, jsonReq(http.MethodPost, "/verify-email", `{"token":"valid-token"}`))
	body := readBody(t, resp)

	if resp.StatusCode != fiber.StatusOK {
		t.Errorf("status = %d, want %d", resp.StatusCode, fiber.StatusOK)
	}

	env := parseSuccess(t, body)
	var msgResp struct {
		Message string `json:"message"`
	}
	if err := json.Unmarshal(env.Data, &msgResp); err != nil {
		t.Fatalf("unmarshal message response: %v", err)
	}
	if msgResp.Message == "" {
		t.Error("message is empty")
	}
}

// --- VerifyPassword handler tests ---

// testVerifyPasswordApp creates a Fiber app with simulated auth middleware and the verify-password route.
func testVerifyPasswordApp(t *testing.T) *fiber.App {
	t.Helper()
	mr := miniredis.RunT(t)
	rdb := redis.NewClient(&redis.Options{Addr: mr.Addr()})

	bl := disposable.NewBlocklist("", false, 10*time.Second, zerolog.Nop())
	repo := newFakeRepo()
	permPub := permission.NewPublisher(rdb)
	svc, err := auth.NewService(repo, rdb, testAuthConfig(), bl, nil, &fakeServerRepo{}, permPub, zerolog.Nop())
	if err != nil {
		t.Fatalf("NewService() error = %v", err)
	}

	result, err := svc.Register(t.Context(), auth.RegisterRequest{
		Email:    "verify@example.com",
		Username: "verifyuser",
		Password: "strongpassword",
	})
	if err != nil {
		t.Fatalf("Register() error = %v", err)
	}

	userID, err := uuid.Parse(result.User.ID)
	if err != nil {
		t.Fatalf("Parse user ID: %v", err)
	}

	handler := NewAuthHandler(svc, zerolog.Nop())

	app := fiber.New()
	app.Use(func(c fiber.Ctx) error {
		c.Locals("userID", userID)
		return c.Next()
	})
	app.Post("/verify-password", handler.VerifyPassword)

	return app
}

func TestVerifyPasswordHandler_InvalidJSON(t *testing.T) {
	t.Parallel()
	app := testVerifyPasswordApp(t)

	resp := doReq(t, app, jsonReq(http.MethodPost, "/verify-password", "not json"))
	body := readBody(t, resp)

	if resp.StatusCode != fiber.StatusBadRequest {
		t.Errorf("status = %d, want %d", resp.StatusCode, fiber.StatusBadRequest)
	}
	env := parseError(t, body)
	if env.Error.Code != string(apierrors.InvalidBody) {
		t.Errorf("error code = %q, want %q", env.Error.Code, apierrors.InvalidBody)
	}
}

func TestVerifyPasswordHandler_MissingPassword(t *testing.T) {
	t.Parallel()
	app := testVerifyPasswordApp(t)

	resp := doReq(t, app, jsonReq(http.MethodPost, "/verify-password", `{}`))
	body := readBody(t, resp)

	if resp.StatusCode != fiber.StatusBadRequest {
		t.Errorf("status = %d, want %d", resp.StatusCode, fiber.StatusBadRequest)
	}
	env := parseError(t, body)
	if env.Error.Code != string(apierrors.InvalidBody) {
		t.Errorf("error code = %q, want %q", env.Error.Code, apierrors.InvalidBody)
	}
}

func TestVerifyPasswordHandler_WrongPassword(t *testing.T) {
	t.Parallel()
	app := testVerifyPasswordApp(t)

	resp := doReq(t, app, jsonReq(http.MethodPost, "/verify-password", `{"password":"wrongpassword"}`))
	body := readBody(t, resp)

	if resp.StatusCode != fiber.StatusUnauthorized {
		t.Errorf("status = %d, want %d", resp.StatusCode, fiber.StatusUnauthorized)
	}
	env := parseError(t, body)
	if env.Error.Code != string(apierrors.InvalidCredentials) {
		t.Errorf("error code = %q, want %q", env.Error.Code, apierrors.InvalidCredentials)
	}
}

func TestVerifyPasswordHandler_Success(t *testing.T) {
	t.Parallel()
	app := testVerifyPasswordApp(t)

	resp := doReq(t, app, jsonReq(http.MethodPost, "/verify-password", `{"password":"strongpassword"}`))
	_ = readBody(t, resp)

	if resp.StatusCode != fiber.StatusNoContent {
		t.Errorf("status = %d, want %d", resp.StatusCode, fiber.StatusNoContent)
	}
}

// --- MFA verify handler tests ---

func TestMFAVerifyHandler_InvalidJSON(t *testing.T) {
	t.Parallel()
	_, app := testAuthHandler(t)

	resp := doReq(t, app, jsonReq(http.MethodPost, "/mfa/verify", "not json"))
	body := readBody(t, resp)

	if resp.StatusCode != fiber.StatusBadRequest {
		t.Errorf("status = %d, want %d", resp.StatusCode, fiber.StatusBadRequest)
	}
	env := parseError(t, body)
	if env.Error.Code != string(apierrors.InvalidBody) {
		t.Errorf("error code = %q, want %q", env.Error.Code, apierrors.InvalidBody)
	}
}

func TestMFAVerifyHandler_MissingFields(t *testing.T) {
	t.Parallel()
	_, app := testAuthHandler(t)

	resp := doReq(t, app, jsonReq(http.MethodPost, "/mfa/verify", `{"ticket":"abc"}`))
	body := readBody(t, resp)

	if resp.StatusCode != fiber.StatusBadRequest {
		t.Errorf("status = %d, want %d", resp.StatusCode, fiber.StatusBadRequest)
	}
	env := parseError(t, body)
	if env.Error.Code != string(apierrors.InvalidBody) {
		t.Errorf("error code = %q, want %q", env.Error.Code, apierrors.InvalidBody)
	}
}

func TestMFAVerifyHandler_InvalidTicket(t *testing.T) {
	t.Parallel()
	_, app := testAuthHandler(t)

	resp := doReq(t, app, jsonReq(http.MethodPost, "/mfa/verify",
		`{"ticket":"nonexistent","code":"123456"}`))
	body := readBody(t, resp)

	if resp.StatusCode != fiber.StatusBadRequest {
		t.Errorf("status = %d, want %d", resp.StatusCode, fiber.StatusBadRequest)
	}
	env := parseError(t, body)
	if env.Error.Code != string(apierrors.InvalidToken) {
		t.Errorf("error code = %q, want %q", env.Error.Code, apierrors.InvalidToken)
	}
}
