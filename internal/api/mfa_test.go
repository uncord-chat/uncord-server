package api

import (
	"encoding/json"
	"net/http"
	"testing"
	"time"

	"github.com/alicebob/miniredis/v2"
	"github.com/gofiber/fiber/v3"
	"github.com/google/uuid"
	"github.com/pquerna/otp/totp"
	"github.com/redis/go-redis/v9"
	"github.com/rs/zerolog"
	apierrors "github.com/uncord-chat/uncord-protocol/errors"

	"github.com/uncord-chat/uncord-server/internal/auth"
	"github.com/uncord-chat/uncord-server/internal/disposable"
	"github.com/uncord-chat/uncord-server/internal/permission"
)

// testMFAApp creates a Fiber app with simulated auth middleware and MFA routes.
func testMFAApp(t *testing.T) (*fiber.App, *auth.Service) {
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

	// Register a test user.
	result, err := svc.Register(t.Context(), auth.RegisterRequest{
		Email:    "mfa@example.com",
		Username: "mfauser",
		Password: "strongpassword",
	})
	if err != nil {
		t.Fatalf("Register() error = %v", err)
	}

	userID, err := uuid.Parse(result.User.ID)
	if err != nil {
		t.Fatalf("Parse user ID: %v", err)
	}

	handler := NewMFAHandler(svc, zerolog.Nop())

	app := fiber.New()
	// Simulate auth middleware by injecting userID into Locals.
	app.Use(func(c fiber.Ctx) error {
		c.Locals("userID", userID)
		return c.Next()
	})
	app.Post("/enable", handler.Enable)
	app.Post("/confirm", handler.Confirm)
	app.Post("/disable", handler.Disable)
	app.Post("/recovery-codes", handler.RegenerateCodes)

	return app, svc
}

// --- Enable handler tests ---

func TestMFAEnableHandler_InvalidJSON(t *testing.T) {
	t.Parallel()
	app, _ := testMFAApp(t)

	resp := doReq(t, app, jsonReq(http.MethodPost, "/enable", "not json"))
	body := readBody(t, resp)

	if resp.StatusCode != fiber.StatusBadRequest {
		t.Errorf("status = %d, want %d", resp.StatusCode, fiber.StatusBadRequest)
	}
	env := parseError(t, body)
	if env.Error.Code != string(apierrors.InvalidBody) {
		t.Errorf("error code = %q, want %q", env.Error.Code, apierrors.InvalidBody)
	}
}

func TestMFAEnableHandler_MissingPassword(t *testing.T) {
	t.Parallel()
	app, _ := testMFAApp(t)

	resp := doReq(t, app, jsonReq(http.MethodPost, "/enable", `{}`))
	body := readBody(t, resp)

	if resp.StatusCode != fiber.StatusBadRequest {
		t.Errorf("status = %d, want %d", resp.StatusCode, fiber.StatusBadRequest)
	}
	env := parseError(t, body)
	if env.Error.Code != string(apierrors.InvalidBody) {
		t.Errorf("error code = %q, want %q", env.Error.Code, apierrors.InvalidBody)
	}
}

func TestMFAEnableHandler_WrongPassword(t *testing.T) {
	t.Parallel()
	app, _ := testMFAApp(t)

	resp := doReq(t, app, jsonReq(http.MethodPost, "/enable", `{"password":"wrongpassword"}`))
	body := readBody(t, resp)

	if resp.StatusCode != fiber.StatusUnauthorized {
		t.Errorf("status = %d, want %d", resp.StatusCode, fiber.StatusUnauthorized)
	}
	env := parseError(t, body)
	if env.Error.Code != string(apierrors.InvalidCredentials) {
		t.Errorf("error code = %q, want %q", env.Error.Code, apierrors.InvalidCredentials)
	}
}

func TestMFAEnableHandler_Success(t *testing.T) {
	t.Parallel()
	app, _ := testMFAApp(t)

	resp := doReq(t, app, jsonReq(http.MethodPost, "/enable", `{"password":"strongpassword"}`))
	body := readBody(t, resp)

	if resp.StatusCode != fiber.StatusOK {
		t.Errorf("status = %d, want %d; body = %s", resp.StatusCode, fiber.StatusOK, string(body))
	}

	env := parseSuccess(t, body)
	var setupResp struct {
		Secret string `json:"secret"`
		URI    string `json:"uri"`
	}
	if err := json.Unmarshal(env.Data, &setupResp); err != nil {
		t.Fatalf("unmarshal setup response: %v", err)
	}
	if setupResp.Secret == "" {
		t.Error("secret is empty")
	}
	if setupResp.URI == "" {
		t.Error("uri is empty")
	}
}

// --- Confirm handler tests ---

func TestMFAConfirmHandler_InvalidJSON(t *testing.T) {
	t.Parallel()
	app, _ := testMFAApp(t)

	resp := doReq(t, app, jsonReq(http.MethodPost, "/confirm", "not json"))
	body := readBody(t, resp)

	if resp.StatusCode != fiber.StatusBadRequest {
		t.Errorf("status = %d, want %d", resp.StatusCode, fiber.StatusBadRequest)
	}
	env := parseError(t, body)
	if env.Error.Code != string(apierrors.InvalidBody) {
		t.Errorf("error code = %q, want %q", env.Error.Code, apierrors.InvalidBody)
	}
}

func TestMFAConfirmHandler_MissingCode(t *testing.T) {
	t.Parallel()
	app, _ := testMFAApp(t)

	resp := doReq(t, app, jsonReq(http.MethodPost, "/confirm", `{}`))
	body := readBody(t, resp)

	if resp.StatusCode != fiber.StatusBadRequest {
		t.Errorf("status = %d, want %d", resp.StatusCode, fiber.StatusBadRequest)
	}
	env := parseError(t, body)
	if env.Error.Code != string(apierrors.InvalidBody) {
		t.Errorf("error code = %q, want %q", env.Error.Code, apierrors.InvalidBody)
	}
}

func TestMFAConfirmHandler_NoPendingSetup(t *testing.T) {
	t.Parallel()
	app, _ := testMFAApp(t)

	resp := doReq(t, app, jsonReq(http.MethodPost, "/confirm", `{"code":"123456"}`))
	body := readBody(t, resp)

	if resp.StatusCode != fiber.StatusBadRequest {
		t.Errorf("status = %d, want %d", resp.StatusCode, fiber.StatusBadRequest)
	}
	env := parseError(t, body)
	if env.Error.Code != string(apierrors.InvalidToken) {
		t.Errorf("error code = %q, want %q", env.Error.Code, apierrors.InvalidToken)
	}
}

func TestMFAConfirmHandler_Success(t *testing.T) {
	t.Parallel()
	app, _ := testMFAApp(t)

	// Begin MFA setup to get a pending secret.
	enableResp := doReq(t, app, jsonReq(http.MethodPost, "/enable", `{"password":"strongpassword"}`))
	enableBody := readBody(t, enableResp)
	if enableResp.StatusCode != fiber.StatusOK {
		t.Fatalf("enable status = %d, body = %s", enableResp.StatusCode, string(enableBody))
	}
	enableEnv := parseSuccess(t, enableBody)
	var setupData struct {
		Secret string `json:"secret"`
	}
	if err := json.Unmarshal(enableEnv.Data, &setupData); err != nil {
		t.Fatalf("unmarshal setup: %v", err)
	}

	code, err := totp.GenerateCode(setupData.Secret, time.Now())
	if err != nil {
		t.Fatalf("totp.GenerateCode() error = %v", err)
	}

	resp := doReq(t, app, jsonReq(http.MethodPost, "/confirm", `{"code":"`+code+`"}`))
	body := readBody(t, resp)

	if resp.StatusCode != fiber.StatusOK {
		t.Errorf("status = %d, want %d; body = %s", resp.StatusCode, fiber.StatusOK, string(body))
	}

	env := parseSuccess(t, body)
	var confirmResp struct {
		RecoveryCodes []string `json:"recovery_codes"`
	}
	if err := json.Unmarshal(env.Data, &confirmResp); err != nil {
		t.Fatalf("unmarshal confirm response: %v", err)
	}
	if len(confirmResp.RecoveryCodes) == 0 {
		t.Error("recovery_codes is empty")
	}
}

// --- Disable handler tests ---

func TestMFADisableHandler_InvalidJSON(t *testing.T) {
	t.Parallel()
	app, _ := testMFAApp(t)

	resp := doReq(t, app, jsonReq(http.MethodPost, "/disable", "not json"))
	body := readBody(t, resp)

	if resp.StatusCode != fiber.StatusBadRequest {
		t.Errorf("status = %d, want %d", resp.StatusCode, fiber.StatusBadRequest)
	}
	env := parseError(t, body)
	if env.Error.Code != string(apierrors.InvalidBody) {
		t.Errorf("error code = %q, want %q", env.Error.Code, apierrors.InvalidBody)
	}
}

func TestMFADisableHandler_MissingFields(t *testing.T) {
	t.Parallel()
	app, _ := testMFAApp(t)

	resp := doReq(t, app, jsonReq(http.MethodPost, "/disable", `{"password":"test"}`))
	body := readBody(t, resp)

	if resp.StatusCode != fiber.StatusBadRequest {
		t.Errorf("status = %d, want %d", resp.StatusCode, fiber.StatusBadRequest)
	}
	env := parseError(t, body)
	if env.Error.Code != string(apierrors.InvalidBody) {
		t.Errorf("error code = %q, want %q", env.Error.Code, apierrors.InvalidBody)
	}
}

func TestMFADisableHandler_NotEnabled(t *testing.T) {
	t.Parallel()
	app, _ := testMFAApp(t)

	resp := doReq(t, app, jsonReq(http.MethodPost, "/disable",
		`{"password":"strongpassword","code":"123456"}`))
	body := readBody(t, resp)

	if resp.StatusCode != fiber.StatusBadRequest {
		t.Errorf("status = %d, want %d", resp.StatusCode, fiber.StatusBadRequest)
	}
	env := parseError(t, body)
	if env.Error.Code != string(apierrors.MFANotEnabled) {
		t.Errorf("error code = %q, want %q", env.Error.Code, apierrors.MFANotEnabled)
	}
}

// --- RegenerateCodes handler tests ---

func TestMFARegenerateCodesHandler_InvalidJSON(t *testing.T) {
	t.Parallel()
	app, _ := testMFAApp(t)

	resp := doReq(t, app, jsonReq(http.MethodPost, "/recovery-codes", "not json"))
	body := readBody(t, resp)

	if resp.StatusCode != fiber.StatusBadRequest {
		t.Errorf("status = %d, want %d", resp.StatusCode, fiber.StatusBadRequest)
	}
	env := parseError(t, body)
	if env.Error.Code != string(apierrors.InvalidBody) {
		t.Errorf("error code = %q, want %q", env.Error.Code, apierrors.InvalidBody)
	}
}

func TestMFARegenerateCodesHandler_MissingPassword(t *testing.T) {
	t.Parallel()
	app, _ := testMFAApp(t)

	resp := doReq(t, app, jsonReq(http.MethodPost, "/recovery-codes", `{}`))
	body := readBody(t, resp)

	if resp.StatusCode != fiber.StatusBadRequest {
		t.Errorf("status = %d, want %d", resp.StatusCode, fiber.StatusBadRequest)
	}
	env := parseError(t, body)
	if env.Error.Code != string(apierrors.InvalidBody) {
		t.Errorf("error code = %q, want %q", env.Error.Code, apierrors.InvalidBody)
	}
}

func TestMFARegenerateCodesHandler_NotEnabled(t *testing.T) {
	t.Parallel()
	app, _ := testMFAApp(t)

	resp := doReq(t, app, jsonReq(http.MethodPost, "/recovery-codes",
		`{"password":"strongpassword"}`))
	body := readBody(t, resp)

	if resp.StatusCode != fiber.StatusBadRequest {
		t.Errorf("status = %d, want %d", resp.StatusCode, fiber.StatusBadRequest)
	}
	env := parseError(t, body)
	if env.Error.Code != string(apierrors.MFANotEnabled) {
		t.Errorf("error code = %q, want %q", env.Error.Code, apierrors.MFANotEnabled)
	}
}
