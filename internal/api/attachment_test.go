package api

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/gofiber/fiber/v3"
	"github.com/google/uuid"
	"github.com/rs/zerolog"
	apierrors "github.com/uncord-chat/uncord-protocol/errors"

	"github.com/uncord-chat/uncord-server/internal/attachment"
	"github.com/uncord-chat/uncord-server/internal/media"
)

// fakeStorageForUpload implements media.StorageProvider for attachment upload tests.
type fakeStorageForUpload struct {
	files map[string][]byte
}

func newFakeStorageForUpload() *fakeStorageForUpload {
	return &fakeStorageForUpload{files: make(map[string][]byte)}
}

func (s *fakeStorageForUpload) Put(_ context.Context, key string, r io.Reader) error {
	data, err := io.ReadAll(r)
	if err != nil {
		return err
	}
	s.files[key] = data
	return nil
}

func (s *fakeStorageForUpload) Get(_ context.Context, key string) (io.ReadCloser, error) {
	data, ok := s.files[key]
	if !ok {
		return nil, media.ErrStorageKeyNotFound
	}
	return io.NopCloser(bytes.NewReader(data)), nil
}

func (s *fakeStorageForUpload) Delete(_ context.Context, key string) error {
	delete(s.files, key)
	return nil
}

func (s *fakeStorageForUpload) URL(key string) string {
	return "http://localhost:8080/media/" + key
}

func testUploadApp(t *testing.T, repo attachment.Repository, storage media.StorageProvider, maxSize int64, userID uuid.UUID) *fiber.App {
	t.Helper()
	handler := NewAttachmentHandler(repo, storage, nil, maxSize, zerolog.Nop())
	app := fiber.New(fiber.Config{BodyLimit: int(maxSize) + 1024*1024})
	app.Use(fakeAuth(userID))
	app.Post("/channels/:channelID/attachments", handler.Upload)
	return app
}

func multipartFileReq(t *testing.T, url, filename string, content []byte) *http.Request {
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

	req := httptest.NewRequest(http.MethodPost, url, &buf)
	req.Header.Set("Content-Type", writer.FormDataContentType())
	return req
}

func TestUpload_Success(t *testing.T) {
	t.Parallel()
	repo := newFakeAttachmentRepo()
	storage := newFakeStorageForUpload()
	channelID := uuid.New()
	userID := uuid.New()
	app := testUploadApp(t, repo, storage, 10*1024*1024, userID)

	content := []byte("fake jpeg data")
	req := multipartFileReq(t, "/channels/"+channelID.String()+"/attachments", "photo.jpg", content)

	resp, err := app.Test(req, testTimeout)
	if err != nil {
		t.Fatalf("app.Test() error: %v", err)
	}
	body := readBody(t, resp)

	if resp.StatusCode != fiber.StatusCreated {
		t.Errorf("status = %d, want %d; body: %s", resp.StatusCode, fiber.StatusCreated, body)
	}

	env := parseSuccess(t, body)
	var att struct {
		ID          string `json:"id"`
		Filename    string `json:"filename"`
		URL         string `json:"url"`
		Size        int64  `json:"size"`
		ContentType string `json:"content_type"`
	}
	if err := json.Unmarshal(env.Data, &att); err != nil {
		t.Fatalf("unmarshal attachment: %v", err)
	}
	if att.Filename != "photo.jpg" {
		t.Errorf("filename = %q, want %q", att.Filename, "photo.jpg")
	}
	if att.Size != int64(len(content)) {
		t.Errorf("size = %d, want %d", att.Size, len(content))
	}
	if att.ID == "" {
		t.Error("id is empty")
	}
	if att.URL == "" {
		t.Error("url is empty")
	}
}

func TestUpload_UnsupportedContentType(t *testing.T) {
	t.Parallel()
	repo := newFakeAttachmentRepo()
	storage := newFakeStorageForUpload()
	channelID := uuid.New()
	userID := uuid.New()
	app := testUploadApp(t, repo, storage, 10*1024*1024, userID)

	req := multipartFileReq(t, "/channels/"+channelID.String()+"/attachments", "malware.exe", []byte("evil"))

	resp, err := app.Test(req, testTimeout)
	if err != nil {
		t.Fatalf("app.Test() error: %v", err)
	}
	body := readBody(t, resp)

	if resp.StatusCode != fiber.StatusBadRequest {
		t.Errorf("status = %d, want %d", resp.StatusCode, fiber.StatusBadRequest)
	}
	env := parseError(t, body)
	if env.Error.Code != string(apierrors.UnsupportedContentType) {
		t.Errorf("error code = %q, want %q", env.Error.Code, apierrors.UnsupportedContentType)
	}
}

func TestUpload_FileTooLarge(t *testing.T) {
	t.Parallel()
	repo := newFakeAttachmentRepo()
	storage := newFakeStorageForUpload()
	channelID := uuid.New()
	userID := uuid.New()
	maxSize := int64(100) // 100 bytes
	app := testUploadApp(t, repo, storage, maxSize, userID)

	content := make([]byte, 200) // Exceeds 100 byte limit
	req := multipartFileReq(t, "/channels/"+channelID.String()+"/attachments", "big.txt", content)

	resp, err := app.Test(req, testTimeout)
	if err != nil {
		t.Fatalf("app.Test() error: %v", err)
	}
	body := readBody(t, resp)

	if resp.StatusCode != fiber.StatusBadRequest {
		t.Errorf("status = %d, want %d; body: %s", resp.StatusCode, fiber.StatusBadRequest, body)
	}
	env := parseError(t, body)
	if env.Error.Code != string(apierrors.PayloadTooLarge) {
		t.Errorf("error code = %q, want %q", env.Error.Code, apierrors.PayloadTooLarge)
	}
}

func TestUpload_InvalidChannelID(t *testing.T) {
	t.Parallel()
	repo := newFakeAttachmentRepo()
	storage := newFakeStorageForUpload()
	userID := uuid.New()
	app := testUploadApp(t, repo, storage, 10*1024*1024, userID)

	req := multipartFileReq(t, "/channels/not-a-uuid/attachments", "file.txt", []byte("data"))

	resp, err := app.Test(req, testTimeout)
	if err != nil {
		t.Fatalf("app.Test() error: %v", err)
	}
	body := readBody(t, resp)

	if resp.StatusCode != fiber.StatusBadRequest {
		t.Errorf("status = %d, want %d", resp.StatusCode, fiber.StatusBadRequest)
	}
	env := parseError(t, body)
	if env.Error.Code != string(apierrors.InvalidChannelID) {
		t.Errorf("error code = %q, want %q", env.Error.Code, apierrors.InvalidChannelID)
	}
}

func TestUpload_MissingFile(t *testing.T) {
	t.Parallel()
	repo := newFakeAttachmentRepo()
	storage := newFakeStorageForUpload()
	channelID := uuid.New()
	userID := uuid.New()
	app := testUploadApp(t, repo, storage, 10*1024*1024, userID)

	req := httptest.NewRequest(http.MethodPost, "/channels/"+channelID.String()+"/attachments", nil)
	req.Header.Set("Content-Type", "multipart/form-data")

	resp, err := app.Test(req, testTimeout)
	if err != nil {
		t.Fatalf("app.Test() error: %v", err)
	}
	body := readBody(t, resp)

	if resp.StatusCode != fiber.StatusBadRequest {
		t.Errorf("status = %d, want %d", resp.StatusCode, fiber.StatusBadRequest)
	}
	env := parseError(t, body)
	if env.Error.Code != string(apierrors.InvalidBody) {
		t.Errorf("error code = %q, want %q", env.Error.Code, apierrors.InvalidBody)
	}
}

func TestSanitiseFilename(t *testing.T) {
	t.Parallel()

	tests := []struct {
		input string
		want  string
	}{
		{"photo.jpg", "photo.jpg"},
		{"/path/to/photo.jpg", "photo.jpg"},
		{"../../etc/passwd", "passwd"},
		{"a" + string(make([]rune, 300)), "a" + string(make([]rune, 254))},
	}
	for _, tt := range tests {
		if got := sanitiseFilename(tt.input); got != tt.want {
			t.Errorf("sanitiseFilename(%q) = %q, want %q", tt.input, got, tt.want)
		}
	}
}

func TestDetectContentType(t *testing.T) {
	t.Parallel()

	tests := []struct {
		header   string
		filename string
		want     string
	}{
		{"image/jpeg", "photo.jpg", "image/jpeg"},
		{"", "photo.jpg", "image/jpeg"},
		{"application/octet-stream", "document.pdf", "application/pdf"},
		{"text/plain", "file.txt", "text/plain"},
		{"", "unknown", ""},
	}
	for _, tt := range tests {
		if got := detectContentType(tt.header, tt.filename); got != tt.want {
			t.Errorf("detectContentType(%q, %q) = %q, want %q", tt.header, tt.filename, got, tt.want)
		}
	}
}
