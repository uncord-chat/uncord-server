package api

import (
	"bytes"
	"context"
	"encoding/json"
	"image"
	"image/color" //nolint:misspell // stdlib package name
	"image/png"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/gofiber/fiber/v3"
	"github.com/google/uuid"
	"github.com/rs/zerolog"
	apierrors "github.com/uncord-chat/uncord-protocol/errors"

	"github.com/uncord-chat/uncord-server/internal/server"
	"github.com/uncord-chat/uncord-server/internal/user"
)

// testImagePNG creates a minimal valid PNG image with the given dimensions.
func testImagePNG(t *testing.T, width, height int) []byte {
	t.Helper()
	img := image.NewRGBA(image.Rect(0, 0, width, height))
	for y := range height {
		for x := range width {
			img.Set(x, y, color.RGBA{R: 255, A: 255}) //nolint:misspell // stdlib type
		}
	}
	var buf bytes.Buffer
	if err := png.Encode(&buf, img); err != nil {
		t.Fatalf("encode test PNG: %v", err)
	}
	return buf.Bytes()
}

// multipartPutReq builds a multipart/form-data PUT request with a file field.
func multipartPutReq(t *testing.T, url, filename string, content []byte) *http.Request {
	t.Helper()
	var buf bytes.Buffer
	writer := multipart.NewWriter(&buf)
	part, err := writer.CreateFormFile("file", filename)
	if err != nil {
		t.Fatalf("create form file: %v", err)
	}
	if _, err := part.Write(content); err != nil {
		t.Fatalf("write file content: %v", err)
	}
	if err := writer.Close(); err != nil {
		t.Fatalf("close multipart writer: %v", err)
	}

	req := httptest.NewRequestWithContext(context.Background(), http.MethodPut, url, &buf)
	req.Header.Set("Content-Type", writer.FormDataContentType())
	return req
}

func seedImageUser(repo *fakeRepo) *user.User {
	id := uuid.New()
	c := &user.Credentials{
		User: user.User{
			ID:            id,
			Email:         "img@example.com",
			Username:      "imguser",
			EmailVerified: true,
		},
	}
	repo.users[c.Email] = c
	return &c.User
}

func testUserAvatarApp(t *testing.T, repo user.Repository, storage *fakeStorageForUpload, userID uuid.UUID) *fiber.App {
	t.Helper()
	srvRepo := &fakeServerRepo{cfg: seedServerConfig().cfg}
	handler := NewImageUploadHandler(repo, srvRepo, storage, nil, 10*1024*1024, 1080, 1920, 480, zerolog.Nop())
	app := fiber.New(fiber.Config{BodyLimit: 11 * 1024 * 1024})
	app.Use(fakeAuth(userID))
	app.Put("/avatar", handler.UploadUserAvatar)
	app.Delete("/avatar", handler.DeleteUserAvatar)
	app.Put("/banner", handler.UploadUserBanner)
	app.Delete("/banner", handler.DeleteUserBanner)
	return app
}

func testServerImageApp(t *testing.T, srvRepo server.Repository, storage *fakeStorageForUpload, userID uuid.UUID) *fiber.App {
	t.Helper()
	userRepo := newFakeRepo()
	handler := NewImageUploadHandler(userRepo, srvRepo, storage, nil, 10*1024*1024, 1080, 1920, 480, zerolog.Nop())
	app := fiber.New(fiber.Config{BodyLimit: 11 * 1024 * 1024})
	app.Use(fakeAuth(userID))
	app.Put("/icon", handler.UploadServerIcon)
	app.Delete("/icon", handler.DeleteServerIcon)
	app.Put("/banner", handler.UploadServerBanner)
	app.Delete("/banner", handler.DeleteServerBanner)
	return app
}

// --- User Avatar Upload ---

func TestUploadUserAvatar_SuccessJPEG(t *testing.T) {
	t.Parallel()
	repo := newFakeRepo()
	u := seedImageUser(repo)
	storage := newFakeStorageForUpload()
	app := testUserAvatarApp(t, repo, storage, u.ID)

	content := testImagePNG(t, 200, 200)
	req := multipartPutReq(t, "/avatar", "photo.jpg", content)

	resp := doReq(t, app, req)
	body := readBody(t, resp)

	if resp.StatusCode != fiber.StatusOK {
		t.Errorf("status = %d, want %d; body: %s", resp.StatusCode, fiber.StatusOK, body)
	}

	env := parseSuccess(t, body)
	var userResp struct {
		AvatarKey *string `json:"avatar_key"`
	}
	if err := json.Unmarshal(env.Data, &userResp); err != nil {
		t.Fatalf("unmarshal response: %v", err)
	}
	if userResp.AvatarKey == nil {
		t.Fatal("avatar_key is nil after upload")
	}
	if len(storage.files) != 1 {
		t.Errorf("storage file count = %d, want 1", len(storage.files))
	}
}

func TestUploadUserAvatar_SuccessPNG(t *testing.T) {
	t.Parallel()
	repo := newFakeRepo()
	u := seedImageUser(repo)
	storage := newFakeStorageForUpload()
	app := testUserAvatarApp(t, repo, storage, u.ID)

	content := testImagePNG(t, 100, 100)
	req := multipartPutReq(t, "/avatar", "photo.png", content)

	resp := doReq(t, app, req)
	body := readBody(t, resp)

	if resp.StatusCode != fiber.StatusOK {
		t.Errorf("status = %d, want %d; body: %s", resp.StatusCode, fiber.StatusOK, body)
	}
}

func TestUploadUserAvatar_UnsupportedContentType(t *testing.T) {
	t.Parallel()
	repo := newFakeRepo()
	u := seedImageUser(repo)
	storage := newFakeStorageForUpload()
	app := testUserAvatarApp(t, repo, storage, u.ID)

	req := multipartPutReq(t, "/avatar", "vector.svg", []byte("<svg></svg>"))

	resp := doReq(t, app, req)
	body := readBody(t, resp)

	if resp.StatusCode != fiber.StatusBadRequest {
		t.Errorf("status = %d, want %d", resp.StatusCode, fiber.StatusBadRequest)
	}
	env := parseError(t, body)
	if env.Error.Code != string(apierrors.UnsupportedContentType) {
		t.Errorf("error code = %q, want %q", env.Error.Code, apierrors.UnsupportedContentType)
	}
}

func TestUploadUserAvatar_FileTooLarge(t *testing.T) {
	t.Parallel()
	repo := newFakeRepo()
	u := seedImageUser(repo)
	storage := newFakeStorageForUpload()

	srvRepo := &fakeServerRepo{cfg: seedServerConfig().cfg}
	handler := NewImageUploadHandler(repo, srvRepo, storage, nil, 100, 1080, 1920, 480, zerolog.Nop())
	app := fiber.New(fiber.Config{BodyLimit: 1024 * 1024})
	app.Use(fakeAuth(u.ID))
	app.Put("/avatar", handler.UploadUserAvatar)

	content := testImagePNG(t, 50, 50)
	req := multipartPutReq(t, "/avatar", "photo.png", content)

	resp := doReq(t, app, req)
	body := readBody(t, resp)

	if resp.StatusCode != fiber.StatusBadRequest {
		t.Errorf("status = %d, want %d; body: %s", resp.StatusCode, fiber.StatusBadRequest, body)
	}
	env := parseError(t, body)
	if env.Error.Code != string(apierrors.PayloadTooLarge) {
		t.Errorf("error code = %q, want %q", env.Error.Code, apierrors.PayloadTooLarge)
	}
}

func TestUploadUserAvatar_MissingFile(t *testing.T) {
	t.Parallel()
	repo := newFakeRepo()
	u := seedImageUser(repo)
	storage := newFakeStorageForUpload()
	app := testUserAvatarApp(t, repo, storage, u.ID)

	req := httptest.NewRequestWithContext(context.Background(), http.MethodPut, "/avatar", nil)
	req.Header.Set("Content-Type", "multipart/form-data")

	resp := doReq(t, app, req)
	body := readBody(t, resp)

	if resp.StatusCode != fiber.StatusBadRequest {
		t.Errorf("status = %d, want %d", resp.StatusCode, fiber.StatusBadRequest)
	}
	env := parseError(t, body)
	if env.Error.Code != string(apierrors.InvalidBody) {
		t.Errorf("error code = %q, want %q", env.Error.Code, apierrors.InvalidBody)
	}
}

func TestUploadUserAvatar_ReplacesOldFile(t *testing.T) {
	t.Parallel()
	repo := newFakeRepo()
	u := seedImageUser(repo)
	storage := newFakeStorageForUpload()
	app := testUserAvatarApp(t, repo, storage, u.ID)

	// First upload
	content := testImagePNG(t, 200, 200)
	req := multipartPutReq(t, "/avatar", "photo1.png", content)
	resp := doReq(t, app, req)
	readBody(t, resp)
	if resp.StatusCode != fiber.StatusOK {
		t.Fatalf("first upload status = %d, want %d", resp.StatusCode, fiber.StatusOK)
	}
	if len(storage.files) != 1 {
		t.Fatalf("storage files after first upload = %d, want 1", len(storage.files))
	}

	// Second upload should replace the old file
	req2 := multipartPutReq(t, "/avatar", "photo2.png", content)
	resp2 := doReq(t, app, req2)
	readBody(t, resp2)
	if resp2.StatusCode != fiber.StatusOK {
		t.Fatalf("second upload status = %d, want %d", resp2.StatusCode, fiber.StatusOK)
	}
	if len(storage.files) != 1 {
		t.Errorf("storage files after replacement = %d, want 1 (old file should be deleted)", len(storage.files))
	}
}

// --- User Avatar Delete ---

func TestDeleteUserAvatar_Success(t *testing.T) {
	t.Parallel()
	repo := newFakeRepo()
	u := seedImageUser(repo)
	storage := newFakeStorageForUpload()
	app := testUserAvatarApp(t, repo, storage, u.ID)

	// Upload first
	content := testImagePNG(t, 200, 200)
	req := multipartPutReq(t, "/avatar", "photo.png", content)
	resp := doReq(t, app, req)
	readBody(t, resp)

	// Delete
	delReq := httptest.NewRequest(http.MethodDelete, "/avatar", nil)
	delResp := doReq(t, app, delReq)
	body := readBody(t, delResp)

	if delResp.StatusCode != fiber.StatusOK {
		t.Errorf("status = %d, want %d; body: %s", delResp.StatusCode, fiber.StatusOK, body)
	}

	env := parseSuccess(t, body)
	var userResp struct {
		AvatarKey *string `json:"avatar_key"`
	}
	if err := json.Unmarshal(env.Data, &userResp); err != nil {
		t.Fatalf("unmarshal response: %v", err)
	}
	if userResp.AvatarKey != nil {
		t.Errorf("avatar_key = %v, want nil", userResp.AvatarKey)
	}
	if len(storage.files) != 0 {
		t.Errorf("storage file count = %d, want 0", len(storage.files))
	}
}

func TestDeleteUserAvatar_Idempotent(t *testing.T) {
	t.Parallel()
	repo := newFakeRepo()
	u := seedImageUser(repo)
	storage := newFakeStorageForUpload()
	app := testUserAvatarApp(t, repo, storage, u.ID)

	// Delete without uploading first (idempotent)
	req := httptest.NewRequest(http.MethodDelete, "/avatar", nil)
	resp := doReq(t, app, req)
	body := readBody(t, resp)

	if resp.StatusCode != fiber.StatusOK {
		t.Errorf("status = %d, want %d; body: %s", resp.StatusCode, fiber.StatusOK, body)
	}
}

// --- User Banner Upload ---

func TestUploadUserBanner_Success(t *testing.T) {
	t.Parallel()
	repo := newFakeRepo()
	u := seedImageUser(repo)
	storage := newFakeStorageForUpload()
	app := testUserAvatarApp(t, repo, storage, u.ID)

	content := testImagePNG(t, 800, 200)
	req := multipartPutReq(t, "/banner", "banner.png", content)

	resp := doReq(t, app, req)
	body := readBody(t, resp)

	if resp.StatusCode != fiber.StatusOK {
		t.Errorf("status = %d, want %d; body: %s", resp.StatusCode, fiber.StatusOK, body)
	}

	env := parseSuccess(t, body)
	var userResp struct {
		BannerKey *string `json:"banner_key"`
	}
	if err := json.Unmarshal(env.Data, &userResp); err != nil {
		t.Fatalf("unmarshal response: %v", err)
	}
	if userResp.BannerKey == nil {
		t.Fatal("banner_key is nil after upload")
	}
}

// --- User Banner Delete ---

func TestDeleteUserBanner_Idempotent(t *testing.T) {
	t.Parallel()
	repo := newFakeRepo()
	u := seedImageUser(repo)
	storage := newFakeStorageForUpload()
	app := testUserAvatarApp(t, repo, storage, u.ID)

	req := httptest.NewRequest(http.MethodDelete, "/banner", nil)
	resp := doReq(t, app, req)
	body := readBody(t, resp)

	if resp.StatusCode != fiber.StatusOK {
		t.Errorf("status = %d, want %d; body: %s", resp.StatusCode, fiber.StatusOK, body)
	}
}

// --- Server Icon Upload ---

func TestUploadServerIcon_Success(t *testing.T) {
	t.Parallel()
	srvRepo := seedServerConfig()
	storage := newFakeStorageForUpload()
	app := testServerImageApp(t, srvRepo, storage, uuid.New())

	content := testImagePNG(t, 256, 256)
	req := multipartPutReq(t, "/icon", "icon.png", content)

	resp := doReq(t, app, req)
	body := readBody(t, resp)

	if resp.StatusCode != fiber.StatusOK {
		t.Errorf("status = %d, want %d; body: %s", resp.StatusCode, fiber.StatusOK, body)
	}

	env := parseSuccess(t, body)
	var sc struct {
		IconKey *string `json:"icon_key"`
	}
	if err := json.Unmarshal(env.Data, &sc); err != nil {
		t.Fatalf("unmarshal response: %v", err)
	}
	if sc.IconKey == nil {
		t.Fatal("icon_key is nil after upload")
	}
}

func TestUploadServerIcon_UnsupportedContentType(t *testing.T) {
	t.Parallel()
	srvRepo := seedServerConfig()
	storage := newFakeStorageForUpload()
	app := testServerImageApp(t, srvRepo, storage, uuid.New())

	req := multipartPutReq(t, "/icon", "icon.bmp", []byte("BM"))

	resp := doReq(t, app, req)
	body := readBody(t, resp)

	if resp.StatusCode != fiber.StatusBadRequest {
		t.Errorf("status = %d, want %d", resp.StatusCode, fiber.StatusBadRequest)
	}
	env := parseError(t, body)
	if env.Error.Code != string(apierrors.UnsupportedContentType) {
		t.Errorf("error code = %q, want %q", env.Error.Code, apierrors.UnsupportedContentType)
	}
}

// --- Server Icon Delete ---

func TestDeleteServerIcon_Success(t *testing.T) {
	t.Parallel()
	srvRepo := seedServerConfig()
	storage := newFakeStorageForUpload()
	app := testServerImageApp(t, srvRepo, storage, uuid.New())

	// Upload first
	content := testImagePNG(t, 256, 256)
	req := multipartPutReq(t, "/icon", "icon.png", content)
	resp := doReq(t, app, req)
	readBody(t, resp)

	// Delete
	delReq := httptest.NewRequest(http.MethodDelete, "/icon", nil)
	delResp := doReq(t, app, delReq)
	body := readBody(t, delResp)

	if delResp.StatusCode != fiber.StatusOK {
		t.Errorf("status = %d, want %d; body: %s", delResp.StatusCode, fiber.StatusOK, body)
	}

	env := parseSuccess(t, body)
	var sc struct {
		IconKey *string `json:"icon_key"`
	}
	if err := json.Unmarshal(env.Data, &sc); err != nil {
		t.Fatalf("unmarshal response: %v", err)
	}
	if sc.IconKey != nil {
		t.Errorf("icon_key = %v, want nil", sc.IconKey)
	}
}

func TestDeleteServerIcon_Idempotent(t *testing.T) {
	t.Parallel()
	now := time.Date(2025, 1, 1, 0, 0, 0, 0, time.UTC)
	srvRepo := &fakeServerRepo{
		cfg: &server.Config{
			ID:        uuid.New(),
			Name:      "Test",
			OwnerID:   uuid.New(),
			CreatedAt: now,
			UpdatedAt: now,
		},
	}
	storage := newFakeStorageForUpload()
	app := testServerImageApp(t, srvRepo, storage, uuid.New())

	req := httptest.NewRequest(http.MethodDelete, "/icon", nil)
	resp := doReq(t, app, req)
	body := readBody(t, resp)

	if resp.StatusCode != fiber.StatusOK {
		t.Errorf("status = %d, want %d; body: %s", resp.StatusCode, fiber.StatusOK, body)
	}
}

// --- Server Banner Upload ---

func TestUploadServerBanner_Success(t *testing.T) {
	t.Parallel()
	srvRepo := seedServerConfig()
	storage := newFakeStorageForUpload()
	app := testServerImageApp(t, srvRepo, storage, uuid.New())

	content := testImagePNG(t, 800, 200)
	req := multipartPutReq(t, "/banner", "banner.png", content)

	resp := doReq(t, app, req)
	body := readBody(t, resp)

	if resp.StatusCode != fiber.StatusOK {
		t.Errorf("status = %d, want %d; body: %s", resp.StatusCode, fiber.StatusOK, body)
	}

	env := parseSuccess(t, body)
	var sc struct {
		BannerKey *string `json:"banner_key"`
	}
	if err := json.Unmarshal(env.Data, &sc); err != nil {
		t.Fatalf("unmarshal response: %v", err)
	}
	if sc.BannerKey == nil {
		t.Fatal("banner_key is nil after upload")
	}
}

// --- Server Banner Delete ---

func TestDeleteServerBanner_Idempotent(t *testing.T) {
	t.Parallel()
	now := time.Date(2025, 1, 1, 0, 0, 0, 0, time.UTC)
	srvRepo := &fakeServerRepo{
		cfg: &server.Config{
			ID:        uuid.New(),
			Name:      "Test",
			OwnerID:   uuid.New(),
			CreatedAt: now,
			UpdatedAt: now,
		},
	}
	storage := newFakeStorageForUpload()
	app := testServerImageApp(t, srvRepo, storage, uuid.New())

	req := httptest.NewRequest(http.MethodDelete, "/banner", nil)
	resp := doReq(t, app, req)
	body := readBody(t, resp)

	if resp.StatusCode != fiber.StatusOK {
		t.Errorf("status = %d, want %d; body: %s", resp.StatusCode, fiber.StatusOK, body)
	}
}

// fakeImageUserRepo is not needed separately because fakeRepo in auth_test.go already implements all
// user.Repository methods including the new Set/Clear image key methods.
// fakeServerRepo in server_test.go already implements all server.Repository methods.
// fakeStorageForUpload in attachment_test.go already implements media.StorageProvider.
// These are all in the same api package and are reused directly.
