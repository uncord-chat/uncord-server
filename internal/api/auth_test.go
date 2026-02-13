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
	"github.com/uncord-chat/uncord-server/internal/user"
)

// testTimeout extends the default app.Test() deadline so that argon2 hashing under the race detector does not trigger
// a spurious i/o timeout.
var testTimeout = fiber.TestConfig{Timeout: 5 * time.Second}

// fakeRepo implements user.Repository for handler tests.
type fakeRepo struct {
	users map[string]*user.User
}

func newFakeRepo() *fakeRepo {
	return &fakeRepo{users: make(map[string]*user.User)}
}

func (r *fakeRepo) Create(_ context.Context, params user.CreateParams) (uuid.UUID, error) {
	if _, exists := r.users[params.Email]; exists {
		return uuid.Nil, user.ErrAlreadyExists
	}
	id := uuid.New()
	r.users[params.Email] = &user.User{
		ID:           id,
		Email:        params.Email,
		Username:     params.Username,
		PasswordHash: params.PasswordHash,
	}
	return id, nil
}

func (r *fakeRepo) GetByEmail(_ context.Context, email string) (*user.User, error) {
	u, ok := r.users[email]
	if !ok {
		return nil, user.ErrNotFound
	}
	return u, nil
}

func (r *fakeRepo) VerifyEmail(_ context.Context, token string) (uuid.UUID, error) {
	if token == "valid-token" {
		return uuid.New(), nil
	}
	return uuid.Nil, user.ErrInvalidToken
}

func (r *fakeRepo) GetByID(_ context.Context, id uuid.UUID) (*user.User, error) {
	for _, u := range r.users {
		if u.ID == id {
			return u, nil
		}
	}
	return nil, user.ErrNotFound
}

func (r *fakeRepo) Update(_ context.Context, id uuid.UUID, params user.UpdateParams) (*user.User, error) {
	for _, u := range r.users {
		if u.ID == id {
			if params.DisplayName != nil {
				u.DisplayName = params.DisplayName
			}
			if params.AvatarKey != nil {
				u.AvatarKey = params.AvatarKey
			}
			return u, nil
		}
	}
	return nil, user.ErrNotFound
}

func (r *fakeRepo) RecordLoginAttempt(context.Context, string, string, bool) error { return nil }
func (r *fakeRepo) UpdatePasswordHash(context.Context, uuid.UUID, string) error    { return nil }

func testAuthHandler(t *testing.T) (*AuthHandler, *fiber.App) {
	t.Helper()
	mr := miniredis.RunT(t)
	rdb := redis.NewClient(&redis.Options{Addr: mr.Addr()})

	cfg := &config.Config{
		ServerURL:         "https://test.example.com",
		ServerEnv:         "production",
		JWTSecret:         "test-secret-at-least-32-chars-long!!",
		JWTAccessTTL:      15 * time.Minute,
		JWTRefreshTTL:     7 * 24 * time.Hour,
		Argon2Memory:      64 * 1024,
		Argon2Iterations:  1,
		Argon2Parallelism: 1,
		Argon2SaltLength:  16,
		Argon2KeyLength:   32,
	}

	bl := disposable.NewBlocklist("", false, zerolog.Nop())
	svc := auth.NewService(newFakeRepo(), rdb, cfg, bl, zerolog.Nop())
	handler := NewAuthHandler(svc)

	app := fiber.New()
	app.Post("/register", handler.Register)
	app.Post("/login", handler.Login)
	app.Post("/refresh", handler.Refresh)
	app.Post("/verify-email", handler.VerifyEmail)

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
