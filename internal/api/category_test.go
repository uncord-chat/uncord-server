package api

import (
	"context"
	"encoding/json"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/gofiber/fiber/v3"
	"github.com/google/uuid"
	"github.com/rs/zerolog"
	apierrors "github.com/uncord-chat/uncord-protocol/errors"

	"github.com/uncord-chat/uncord-server/internal/category"
)

// fakeCategoryRepo implements category.Repository for handler tests.
type fakeCategoryRepo struct {
	categories []category.Category
	maxReached bool
}

func newFakeCategoryRepo() *fakeCategoryRepo {
	return &fakeCategoryRepo{}
}

func (r *fakeCategoryRepo) List(_ context.Context) ([]category.Category, error) {
	return r.categories, nil
}

func (r *fakeCategoryRepo) GetByID(_ context.Context, id uuid.UUID) (*category.Category, error) {
	for i := range r.categories {
		if r.categories[i].ID == id {
			return &r.categories[i], nil
		}
	}
	return nil, category.ErrNotFound
}

func (r *fakeCategoryRepo) Create(_ context.Context, params category.CreateParams, _ int) (*category.Category, error) {
	if r.maxReached {
		return nil, category.ErrMaxCategoriesReached
	}
	now := time.Now()
	cat := category.Category{
		ID:        uuid.New(),
		Name:      params.Name,
		Position:  len(r.categories),
		CreatedAt: now,
		UpdatedAt: now,
	}
	r.categories = append(r.categories, cat)
	return &cat, nil
}

func (r *fakeCategoryRepo) Update(_ context.Context, id uuid.UUID, params category.UpdateParams) (*category.Category, error) {
	for i := range r.categories {
		if r.categories[i].ID == id {
			if params.Name != nil {
				r.categories[i].Name = *params.Name
			}
			if params.Position != nil {
				r.categories[i].Position = *params.Position
			}
			return &r.categories[i], nil
		}
	}
	return nil, category.ErrNotFound
}

func (r *fakeCategoryRepo) Delete(_ context.Context, id uuid.UUID) error {
	for i := range r.categories {
		if r.categories[i].ID == id {
			r.categories = append(r.categories[:i], r.categories[i+1:]...)
			return nil
		}
	}
	return category.ErrNotFound
}

func seedCategory(repo *fakeCategoryRepo) *category.Category {
	now := time.Date(2025, 1, 1, 0, 0, 0, 0, time.UTC)
	cat := category.Category{
		ID:        uuid.New(),
		Name:      "General",
		Position:  0,
		CreatedAt: now,
		UpdatedAt: now,
	}
	repo.categories = append(repo.categories, cat)
	return &cat
}

func testCategoryApp(t *testing.T, repo category.Repository, userID uuid.UUID) *fiber.App {
	t.Helper()
	handler := NewCategoryHandler(repo, 50, zerolog.Nop())
	app := fiber.New()

	app.Use(fakeAuth(userID))

	app.Get("/categories", handler.ListCategories)
	app.Post("/categories", handler.CreateCategory)
	app.Patch("/categories/:categoryID", handler.UpdateCategory)
	app.Delete("/categories/:categoryID", handler.DeleteCategory)
	return app
}

// --- List tests ---

func TestListCategories_Unauthenticated(t *testing.T) {
	t.Parallel()
	repo := newFakeCategoryRepo()
	app := testCategoryApp(t, repo, uuid.Nil)

	resp := doReq(t, app, jsonReq(http.MethodGet, "/categories", ""))
	body := readBody(t, resp)

	if resp.StatusCode != fiber.StatusUnauthorized {
		t.Errorf("status = %d, want %d", resp.StatusCode, fiber.StatusUnauthorized)
	}
	env := parseError(t, body)
	if env.Error.Code != string(apierrors.Unauthorised) {
		t.Errorf("error code = %q, want %q", env.Error.Code, apierrors.Unauthorised)
	}
}

func TestListCategories_Empty(t *testing.T) {
	t.Parallel()
	repo := newFakeCategoryRepo()
	app := testCategoryApp(t, repo, uuid.New())

	resp := doReq(t, app, jsonReq(http.MethodGet, "/categories", ""))
	body := readBody(t, resp)

	if resp.StatusCode != fiber.StatusOK {
		t.Errorf("status = %d, want %d", resp.StatusCode, fiber.StatusOK)
	}

	env := parseSuccess(t, body)
	var cats []json.RawMessage
	if err := json.Unmarshal(env.Data, &cats); err != nil {
		t.Fatalf("unmarshal categories: %v", err)
	}
	if len(cats) != 0 {
		t.Errorf("got %d categories, want 0", len(cats))
	}
}

func TestListCategories_Success(t *testing.T) {
	t.Parallel()
	repo := newFakeCategoryRepo()
	seedCategory(repo)
	app := testCategoryApp(t, repo, uuid.New())

	resp := doReq(t, app, jsonReq(http.MethodGet, "/categories", ""))
	body := readBody(t, resp)

	if resp.StatusCode != fiber.StatusOK {
		t.Errorf("status = %d, want %d", resp.StatusCode, fiber.StatusOK)
	}

	env := parseSuccess(t, body)
	var cats []struct {
		ID   string `json:"id"`
		Name string `json:"name"`
	}
	if err := json.Unmarshal(env.Data, &cats); err != nil {
		t.Fatalf("unmarshal categories: %v", err)
	}
	if len(cats) != 1 {
		t.Fatalf("got %d categories, want 1", len(cats))
	}
	if cats[0].Name != "General" {
		t.Errorf("name = %q, want %q", cats[0].Name, "General")
	}
}

// --- Create tests ---

func TestCreateCategory_InvalidJSON(t *testing.T) {
	t.Parallel()
	repo := newFakeCategoryRepo()
	app := testCategoryApp(t, repo, uuid.New())

	resp := doReq(t, app, jsonReq(http.MethodPost, "/categories", "not json"))
	body := readBody(t, resp)

	if resp.StatusCode != fiber.StatusBadRequest {
		t.Errorf("status = %d, want %d", resp.StatusCode, fiber.StatusBadRequest)
	}
	env := parseError(t, body)
	if env.Error.Code != string(apierrors.InvalidBody) {
		t.Errorf("error code = %q, want %q", env.Error.Code, apierrors.InvalidBody)
	}
}

func TestCreateCategory_EmptyName(t *testing.T) {
	t.Parallel()
	repo := newFakeCategoryRepo()
	app := testCategoryApp(t, repo, uuid.New())

	resp := doReq(t, app, jsonReq(http.MethodPost, "/categories", `{"name":""}`))
	body := readBody(t, resp)

	if resp.StatusCode != fiber.StatusBadRequest {
		t.Errorf("status = %d, want %d", resp.StatusCode, fiber.StatusBadRequest)
	}
	env := parseError(t, body)
	if env.Error.Code != string(apierrors.ValidationError) {
		t.Errorf("error code = %q, want %q", env.Error.Code, apierrors.ValidationError)
	}
}

func TestCreateCategory_NameTooLong(t *testing.T) {
	t.Parallel()
	repo := newFakeCategoryRepo()
	app := testCategoryApp(t, repo, uuid.New())

	longName := strings.Repeat("a", 101)
	resp := doReq(t, app, jsonReq(http.MethodPost, "/categories", `{"name":"`+longName+`"}`))
	body := readBody(t, resp)

	if resp.StatusCode != fiber.StatusBadRequest {
		t.Errorf("status = %d, want %d", resp.StatusCode, fiber.StatusBadRequest)
	}
	env := parseError(t, body)
	if env.Error.Code != string(apierrors.ValidationError) {
		t.Errorf("error code = %q, want %q", env.Error.Code, apierrors.ValidationError)
	}
}

func TestCreateCategory_MaxReached(t *testing.T) {
	t.Parallel()
	repo := newFakeCategoryRepo()
	repo.maxReached = true
	app := testCategoryApp(t, repo, uuid.New())

	resp := doReq(t, app, jsonReq(http.MethodPost, "/categories", `{"name":"New Category"}`))
	body := readBody(t, resp)

	if resp.StatusCode != fiber.StatusBadRequest {
		t.Errorf("status = %d, want %d", resp.StatusCode, fiber.StatusBadRequest)
	}
	env := parseError(t, body)
	if env.Error.Code != string(apierrors.MaxCategoriesReached) {
		t.Errorf("error code = %q, want %q", env.Error.Code, apierrors.MaxCategoriesReached)
	}
}

func TestCreateCategory_Success(t *testing.T) {
	t.Parallel()
	repo := newFakeCategoryRepo()
	app := testCategoryApp(t, repo, uuid.New())

	resp := doReq(t, app, jsonReq(http.MethodPost, "/categories", `{"name":"Voice Channels"}`))
	body := readBody(t, resp)

	if resp.StatusCode != fiber.StatusCreated {
		t.Errorf("status = %d, want %d", resp.StatusCode, fiber.StatusCreated)
	}

	env := parseSuccess(t, body)
	var cat struct {
		ID   string `json:"id"`
		Name string `json:"name"`
	}
	if err := json.Unmarshal(env.Data, &cat); err != nil {
		t.Fatalf("unmarshal category: %v", err)
	}
	if cat.Name != "Voice Channels" {
		t.Errorf("name = %q, want %q", cat.Name, "Voice Channels")
	}
	if cat.ID == "" {
		t.Error("id is empty")
	}
}

// --- Update tests ---

func TestUpdateCategory_InvalidID(t *testing.T) {
	t.Parallel()
	repo := newFakeCategoryRepo()
	app := testCategoryApp(t, repo, uuid.New())

	resp := doReq(t, app, jsonReq(http.MethodPatch, "/categories/not-a-uuid", `{"name":"Updated"}`))
	body := readBody(t, resp)

	if resp.StatusCode != fiber.StatusBadRequest {
		t.Errorf("status = %d, want %d", resp.StatusCode, fiber.StatusBadRequest)
	}
	env := parseError(t, body)
	if env.Error.Code != string(apierrors.ValidationError) {
		t.Errorf("error code = %q, want %q", env.Error.Code, apierrors.ValidationError)
	}
}

func TestUpdateCategory_NotFound(t *testing.T) {
	t.Parallel()
	repo := newFakeCategoryRepo()
	app := testCategoryApp(t, repo, uuid.New())

	resp := doReq(t, app, jsonReq(http.MethodPatch, "/categories/"+uuid.New().String(), `{"name":"Updated"}`))
	body := readBody(t, resp)

	if resp.StatusCode != fiber.StatusNotFound {
		t.Errorf("status = %d, want %d", resp.StatusCode, fiber.StatusNotFound)
	}
	env := parseError(t, body)
	if env.Error.Code != string(apierrors.UnknownCategory) {
		t.Errorf("error code = %q, want %q", env.Error.Code, apierrors.UnknownCategory)
	}
}

func TestUpdateCategory_NameValidation(t *testing.T) {
	t.Parallel()
	repo := newFakeCategoryRepo()
	cat := seedCategory(repo)
	app := testCategoryApp(t, repo, uuid.New())

	resp := doReq(t, app, jsonReq(http.MethodPatch, "/categories/"+cat.ID.String(), `{"name":"   "}`))
	body := readBody(t, resp)

	if resp.StatusCode != fiber.StatusBadRequest {
		t.Errorf("status = %d, want %d", resp.StatusCode, fiber.StatusBadRequest)
	}
	env := parseError(t, body)
	if env.Error.Code != string(apierrors.ValidationError) {
		t.Errorf("error code = %q, want %q", env.Error.Code, apierrors.ValidationError)
	}
}

func TestUpdateCategory_NegativePosition(t *testing.T) {
	t.Parallel()
	repo := newFakeCategoryRepo()
	cat := seedCategory(repo)
	app := testCategoryApp(t, repo, uuid.New())

	resp := doReq(t, app, jsonReq(http.MethodPatch, "/categories/"+cat.ID.String(), `{"position":-1}`))
	body := readBody(t, resp)

	if resp.StatusCode != fiber.StatusBadRequest {
		t.Errorf("status = %d, want %d", resp.StatusCode, fiber.StatusBadRequest)
	}
	env := parseError(t, body)
	if env.Error.Code != string(apierrors.ValidationError) {
		t.Errorf("error code = %q, want %q", env.Error.Code, apierrors.ValidationError)
	}
}

func TestUpdateCategory_Success(t *testing.T) {
	t.Parallel()
	repo := newFakeCategoryRepo()
	cat := seedCategory(repo)
	app := testCategoryApp(t, repo, uuid.New())

	resp := doReq(t, app, jsonReq(http.MethodPatch, "/categories/"+cat.ID.String(), `{"name":"Updated"}`))
	body := readBody(t, resp)

	if resp.StatusCode != fiber.StatusOK {
		t.Errorf("status = %d, want %d", resp.StatusCode, fiber.StatusOK)
	}

	env := parseSuccess(t, body)
	var result struct {
		Name string `json:"name"`
	}
	if err := json.Unmarshal(env.Data, &result); err != nil {
		t.Fatalf("unmarshal category: %v", err)
	}
	if result.Name != "Updated" {
		t.Errorf("name = %q, want %q", result.Name, "Updated")
	}
}

func TestUpdateCategory_EmptyBody(t *testing.T) {
	t.Parallel()
	repo := newFakeCategoryRepo()
	cat := seedCategory(repo)
	app := testCategoryApp(t, repo, uuid.New())

	resp := doReq(t, app, jsonReq(http.MethodPatch, "/categories/"+cat.ID.String(), `{}`))
	body := readBody(t, resp)

	if resp.StatusCode != fiber.StatusOK {
		t.Errorf("status = %d, want %d", resp.StatusCode, fiber.StatusOK)
	}

	env := parseSuccess(t, body)
	var result struct {
		Name string `json:"name"`
	}
	if err := json.Unmarshal(env.Data, &result); err != nil {
		t.Fatalf("unmarshal category: %v", err)
	}
	if result.Name != "General" {
		t.Errorf("name = %q, want %q (should be unchanged)", result.Name, "General")
	}
}

// --- Delete tests ---

func TestDeleteCategory_NotFound(t *testing.T) {
	t.Parallel()
	repo := newFakeCategoryRepo()
	app := testCategoryApp(t, repo, uuid.New())

	resp := doReq(t, app, jsonReq(http.MethodDelete, "/categories/"+uuid.New().String(), ""))
	body := readBody(t, resp)

	if resp.StatusCode != fiber.StatusNotFound {
		t.Errorf("status = %d, want %d", resp.StatusCode, fiber.StatusNotFound)
	}
	env := parseError(t, body)
	if env.Error.Code != string(apierrors.UnknownCategory) {
		t.Errorf("error code = %q, want %q", env.Error.Code, apierrors.UnknownCategory)
	}
}

func TestDeleteCategory_Success(t *testing.T) {
	t.Parallel()
	repo := newFakeCategoryRepo()
	cat := seedCategory(repo)
	app := testCategoryApp(t, repo, uuid.New())

	resp := doReq(t, app, jsonReq(http.MethodDelete, "/categories/"+cat.ID.String(), ""))
	_ = readBody(t, resp)

	if resp.StatusCode != fiber.StatusNoContent {
		t.Errorf("status = %d, want %d", resp.StatusCode, fiber.StatusNoContent)
	}
	if len(repo.categories) != 0 {
		t.Errorf("categories remaining = %d, want 0", len(repo.categories))
	}
}
