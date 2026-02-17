package httputil

import (
	"encoding/json"
	"io"
	"mime"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/gofiber/fiber/v3"

	apierrors "github.com/uncord-chat/uncord-protocol/errors"
)

func TestSuccess(t *testing.T) {
	t.Parallel()

	type payload struct {
		Name string `json:"name"`
	}

	app := fiber.New()
	app.Get("/ok", func(c fiber.Ctx) error {
		return Success(c, payload{Name: "alice"})
	})

	resp := doRequest(t, app, "/ok")
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status = %d, want %d", resp.StatusCode, http.StatusOK)
	}

	var env struct {
		Data payload `json:"data"`
	}
	decodeBody(t, resp, &env)

	if env.Data.Name != "alice" {
		t.Errorf("data.name = %q, want %q", env.Data.Name, "alice")
	}
}

func TestSuccess_nilData(t *testing.T) {
	t.Parallel()

	app := fiber.New()
	app.Get("/ok", func(c fiber.Ctx) error {
		return Success(c, nil)
	})

	resp := doRequest(t, app, "/ok")
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status = %d, want %d", resp.StatusCode, http.StatusOK)
	}

	var env struct {
		Data any `json:"data"`
	}
	decodeBody(t, resp, &env)

	if env.Data != nil {
		t.Errorf("data = %v, want nil", env.Data)
	}
}

func TestSuccessStatus(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name   string
		status int
		data   any
	}{
		{name: "201 with string data", status: http.StatusCreated, data: "created"},
		{name: "202 with int data", status: http.StatusAccepted, data: float64(42)},
		{name: "204 equivalent with nil", status: http.StatusOK, data: nil},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			app := fiber.New()
			app.Get("/s", func(c fiber.Ctx) error {
				return SuccessStatus(c, tt.status, tt.data)
			})

			resp := doRequest(t, app, "/s")
			defer func() { _ = resp.Body.Close() }()

			if resp.StatusCode != tt.status {
				t.Fatalf("status = %d, want %d", resp.StatusCode, tt.status)
			}

			var env struct {
				Data any `json:"data"`
			}
			decodeBody(t, resp, &env)

			if env.Data != tt.data {
				t.Errorf("data = %v, want %v", env.Data, tt.data)
			}
		})
	}
}

func TestFail(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		status  int
		code    apierrors.Code
		message string
	}{
		{
			name:    "400 validation error",
			status:  http.StatusBadRequest,
			code:    apierrors.ValidationError,
			message: "invalid input",
		},
		{
			name:    "401 unauthorised",
			status:  http.StatusUnauthorized,
			code:    apierrors.Unauthorised,
			message: "authentication required",
		},
		{
			name:    "404 not found",
			status:  http.StatusNotFound,
			code:    apierrors.NotFound,
			message: "resource not found",
		},
		{
			name:    "500 internal error",
			status:  http.StatusInternalServerError,
			code:    apierrors.InternalError,
			message: "something went wrong",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			app := fiber.New()
			app.Get("/err", func(c fiber.Ctx) error {
				return Fail(c, tt.status, tt.code, tt.message)
			})

			resp := doRequest(t, app, "/err")
			defer func() { _ = resp.Body.Close() }()

			if resp.StatusCode != tt.status {
				t.Fatalf("status = %d, want %d", resp.StatusCode, tt.status)
			}

			var env struct {
				Error struct {
					Code    string `json:"code"`
					Message string `json:"message"`
				} `json:"error"`
			}
			decodeBody(t, resp, &env)

			if env.Error.Code != string(tt.code) {
				t.Errorf("error.code = %q, want %q", env.Error.Code, tt.code)
			}
			if env.Error.Message != tt.message {
				t.Errorf("error.message = %q, want %q", env.Error.Message, tt.message)
			}
		})
	}
}

func TestResponseContentType(t *testing.T) {
	t.Parallel()

	app := fiber.New()
	app.Get("/success", func(c fiber.Ctx) error {
		return Success(c, "ok")
	})
	app.Get("/fail", func(c fiber.Ctx) error {
		return Fail(c, http.StatusBadRequest, apierrors.InvalidBody, "bad")
	})

	for _, path := range []string{"/success", "/fail"} {
		t.Run(path, func(t *testing.T) {
			t.Parallel()

			resp := doRequest(t, app, path)
			defer func() { _ = resp.Body.Close() }()

			mediaType, _, err := mime.ParseMediaType(resp.Header.Get("Content-Type"))
			if err != nil {
				t.Fatalf("parsing Content-Type: %v", err)
			}
			if mediaType != "application/json" {
				t.Errorf("media type = %q, want %q", mediaType, "application/json")
			}
		})
	}
}

// doRequest sends a request to the Fiber test server and returns the response.
func doRequest(t *testing.T, app *fiber.App, path string) *http.Response {
	t.Helper()

	req := httptest.NewRequest(http.MethodGet, path, nil)
	resp, err := app.Test(req)
	if err != nil {
		t.Fatalf("app.Test() error: %v", err)
	}
	return resp
}

// decodeBody reads the response body and JSON-decodes it into dst.
func decodeBody(t *testing.T, resp *http.Response, dst any) {
	t.Helper()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		t.Fatalf("reading body: %v", err)
	}
	if err := json.Unmarshal(body, dst); err != nil {
		t.Fatalf("decoding JSON: %v\nraw: %s", err, body)
	}
}
