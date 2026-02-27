package api

import (
	"encoding/json"
	"net/http"
	"strings"
	"testing"

	"github.com/gofiber/fiber/v3"
	"github.com/google/uuid"
	"github.com/rs/zerolog"
	apierrors "github.com/uncord-chat/uncord-protocol/errors"

	"github.com/uncord-chat/uncord-server/internal/user"
)

func seedUser(repo *fakeRepo) *user.User {
	id := uuid.New()
	displayName := "Alice"
	pronouns := "she/her"
	about := "Hello there"
	primary := 16711680
	c := &user.Credentials{
		User: user.User{
			ID:                 id,
			Email:              "alice@example.com",
			Username:           "alice",
			DisplayName:        &displayName,
			Pronouns:           &pronouns,
			About:              &about,
			MFAEnabled:         true,
			EmailVerified:      true,
			ThemeColourPrimary: &primary,
		},
	}
	repo.users[c.Email] = c
	return &c.User
}

func testUserApp(t *testing.T, repo *fakeRepo, userID uuid.UUID) *fiber.App {
	t.Helper()
	handler := NewUserHandler(repo, nil, zerolog.Nop())
	app := fiber.New()

	app.Use(fakeAuth(userID))

	app.Get("/@me", handler.GetMe)
	app.Patch("/@me", handler.UpdateMe)
	app.Get("/:userID", handler.GetProfile)
	return app
}

// --- GetMe tests ---

func TestGetMe_Unauthenticated(t *testing.T) {
	t.Parallel()
	repo := newFakeRepo()
	app := testUserApp(t, repo, uuid.Nil)

	resp := doReq(t, app, jsonReq(http.MethodGet, "/@me", ""))
	body := readBody(t, resp)

	if resp.StatusCode != fiber.StatusUnauthorized {
		t.Errorf("status = %d, want %d", resp.StatusCode, fiber.StatusUnauthorized)
	}
	env := parseError(t, body)
	if env.Error.Code != string(apierrors.Unauthorised) {
		t.Errorf("error code = %q, want %q", env.Error.Code, apierrors.Unauthorised)
	}
}

func TestGetMe_UserNotFound(t *testing.T) {
	t.Parallel()
	repo := newFakeRepo()
	app := testUserApp(t, repo, uuid.New())

	resp := doReq(t, app, jsonReq(http.MethodGet, "/@me", ""))
	body := readBody(t, resp)

	if resp.StatusCode != fiber.StatusNotFound {
		t.Errorf("status = %d, want %d", resp.StatusCode, fiber.StatusNotFound)
	}
	env := parseError(t, body)
	if env.Error.Code != string(apierrors.UnknownUser) {
		t.Errorf("error code = %q, want %q", env.Error.Code, apierrors.UnknownUser)
	}
}

func TestGetMe_Success(t *testing.T) {
	t.Parallel()
	repo := newFakeRepo()
	u := seedUser(repo)
	app := testUserApp(t, repo, u.ID)

	resp := doReq(t, app, jsonReq(http.MethodGet, "/@me", ""))
	body := readBody(t, resp)

	if resp.StatusCode != fiber.StatusOK {
		t.Errorf("status = %d, want %d", resp.StatusCode, fiber.StatusOK)
	}

	env := parseSuccess(t, body)
	var userResp struct {
		ID          string  `json:"id"`
		Email       string  `json:"email"`
		Username    string  `json:"username"`
		DisplayName *string `json:"display_name"`
		AvatarKey   *string `json:"avatar_key"`
	}
	if err := json.Unmarshal(env.Data, &userResp); err != nil {
		t.Fatalf("unmarshal user response: %v", err)
	}
	if userResp.ID != u.ID.String() {
		t.Errorf("id = %q, want %q", userResp.ID, u.ID.String())
	}
	if userResp.Email != "alice@example.com" {
		t.Errorf("email = %q, want %q", userResp.Email, "alice@example.com")
	}
	if userResp.Username != "alice" {
		t.Errorf("username = %q, want %q", userResp.Username, "alice")
	}
	if userResp.DisplayName == nil || *userResp.DisplayName != "Alice" {
		t.Errorf("display_name = %v, want %q", userResp.DisplayName, "Alice")
	}
	if userResp.AvatarKey != nil {
		t.Errorf("avatar_key = %v, want nil", userResp.AvatarKey)
	}
}

// --- UpdateMe tests ---

func TestUpdateMe_InvalidJSON(t *testing.T) {
	t.Parallel()
	repo := newFakeRepo()
	u := seedUser(repo)
	app := testUserApp(t, repo, u.ID)

	resp := doReq(t, app, jsonReq(http.MethodPatch, "/@me", "not json"))
	body := readBody(t, resp)

	if resp.StatusCode != fiber.StatusBadRequest {
		t.Errorf("status = %d, want %d", resp.StatusCode, fiber.StatusBadRequest)
	}
	env := parseError(t, body)
	if env.Error.Code != string(apierrors.InvalidBody) {
		t.Errorf("error code = %q, want %q", env.Error.Code, apierrors.InvalidBody)
	}
}

func TestUpdateMe_DisplayNameTooLong(t *testing.T) {
	t.Parallel()
	repo := newFakeRepo()
	u := seedUser(repo)
	app := testUserApp(t, repo, u.ID)

	longName := strings.Repeat("a", 33)
	resp := doReq(t, app, jsonReq(http.MethodPatch, "/@me", `{"display_name":"`+longName+`"}`))
	body := readBody(t, resp)

	if resp.StatusCode != fiber.StatusBadRequest {
		t.Errorf("status = %d, want %d", resp.StatusCode, fiber.StatusBadRequest)
	}
	env := parseError(t, body)
	if env.Error.Code != string(apierrors.ValidationError) {
		t.Errorf("error code = %q, want %q", env.Error.Code, apierrors.ValidationError)
	}
}

func TestUpdateMe_DisplayNameEmpty(t *testing.T) {
	t.Parallel()
	repo := newFakeRepo()
	u := seedUser(repo)
	app := testUserApp(t, repo, u.ID)

	resp := doReq(t, app, jsonReq(http.MethodPatch, "/@me", `{"display_name":"   "}`))
	body := readBody(t, resp)

	if resp.StatusCode != fiber.StatusBadRequest {
		t.Errorf("status = %d, want %d", resp.StatusCode, fiber.StatusBadRequest)
	}
	env := parseError(t, body)
	if env.Error.Code != string(apierrors.ValidationError) {
		t.Errorf("error code = %q, want %q", env.Error.Code, apierrors.ValidationError)
	}
}

func TestUpdateMe_Success(t *testing.T) {
	t.Parallel()
	repo := newFakeRepo()
	u := seedUser(repo)
	app := testUserApp(t, repo, u.ID)

	resp := doReq(t, app, jsonReq(http.MethodPatch, "/@me", `{"display_name":"Bob"}`))
	body := readBody(t, resp)

	if resp.StatusCode != fiber.StatusOK {
		t.Errorf("status = %d, want %d", resp.StatusCode, fiber.StatusOK)
	}

	env := parseSuccess(t, body)
	var userResp struct {
		DisplayName *string `json:"display_name"`
	}
	if err := json.Unmarshal(env.Data, &userResp); err != nil {
		t.Fatalf("unmarshal user response: %v", err)
	}
	if userResp.DisplayName == nil || *userResp.DisplayName != "Bob" {
		t.Errorf("display_name = %v, want %q", userResp.DisplayName, "Bob")
	}
}

func TestUpdateMe_EmptyBody(t *testing.T) {
	t.Parallel()
	repo := newFakeRepo()
	u := seedUser(repo)
	app := testUserApp(t, repo, u.ID)

	resp := doReq(t, app, jsonReq(http.MethodPatch, "/@me", `{}`))
	body := readBody(t, resp)

	if resp.StatusCode != fiber.StatusOK {
		t.Errorf("status = %d, want %d", resp.StatusCode, fiber.StatusOK)
	}

	env := parseSuccess(t, body)
	var userResp struct {
		DisplayName *string `json:"display_name"`
	}
	if err := json.Unmarshal(env.Data, &userResp); err != nil {
		t.Fatalf("unmarshal user response: %v", err)
	}
	// Display name should remain unchanged.
	if userResp.DisplayName == nil || *userResp.DisplayName != "Alice" {
		t.Errorf("display_name = %v, want %q", userResp.DisplayName, "Alice")
	}
}

func TestUpdateMe_AvatarKey(t *testing.T) {
	t.Parallel()
	repo := newFakeRepo()
	u := seedUser(repo)
	app := testUserApp(t, repo, u.ID)

	resp := doReq(t, app, jsonReq(http.MethodPatch, "/@me", `{"avatar_key":"abc123"}`))
	body := readBody(t, resp)

	if resp.StatusCode != fiber.StatusOK {
		t.Errorf("status = %d, want %d", resp.StatusCode, fiber.StatusOK)
	}

	env := parseSuccess(t, body)
	var userResp struct {
		AvatarKey *string `json:"avatar_key"`
	}
	if err := json.Unmarshal(env.Data, &userResp); err != nil {
		t.Fatalf("unmarshal user response: %v", err)
	}
	if userResp.AvatarKey == nil || *userResp.AvatarKey != "abc123" {
		t.Errorf("avatar_key = %v, want %q", userResp.AvatarKey, "abc123")
	}
}

// --- Pronouns tests ---

func TestUpdateMe_PronounsTooLong(t *testing.T) {
	t.Parallel()
	repo := newFakeRepo()
	u := seedUser(repo)
	app := testUserApp(t, repo, u.ID)

	longPronouns := strings.Repeat("a", 41)
	resp := doReq(t, app, jsonReq(http.MethodPatch, "/@me", `{"pronouns":"`+longPronouns+`"}`))
	body := readBody(t, resp)

	if resp.StatusCode != fiber.StatusBadRequest {
		t.Errorf("status = %d, want %d", resp.StatusCode, fiber.StatusBadRequest)
	}
	env := parseError(t, body)
	if env.Error.Code != string(apierrors.ValidationError) {
		t.Errorf("error code = %q, want %q", env.Error.Code, apierrors.ValidationError)
	}
}

func TestUpdateMe_PronounsEmpty(t *testing.T) {
	t.Parallel()
	repo := newFakeRepo()
	u := seedUser(repo)
	app := testUserApp(t, repo, u.ID)

	resp := doReq(t, app, jsonReq(http.MethodPatch, "/@me", `{"pronouns":"   "}`))
	body := readBody(t, resp)

	if resp.StatusCode != fiber.StatusBadRequest {
		t.Errorf("status = %d, want %d", resp.StatusCode, fiber.StatusBadRequest)
	}
	env := parseError(t, body)
	if env.Error.Code != string(apierrors.ValidationError) {
		t.Errorf("error code = %q, want %q", env.Error.Code, apierrors.ValidationError)
	}
}

func TestUpdateMe_PronounsSuccess(t *testing.T) {
	t.Parallel()
	repo := newFakeRepo()
	u := seedUser(repo)
	app := testUserApp(t, repo, u.ID)

	resp := doReq(t, app, jsonReq(http.MethodPatch, "/@me", `{"pronouns":"they/them"}`))
	body := readBody(t, resp)

	if resp.StatusCode != fiber.StatusOK {
		t.Errorf("status = %d, want %d", resp.StatusCode, fiber.StatusOK)
	}

	env := parseSuccess(t, body)
	var userResp struct {
		Pronouns *string `json:"pronouns"`
	}
	if err := json.Unmarshal(env.Data, &userResp); err != nil {
		t.Fatalf("unmarshal user response: %v", err)
	}
	if userResp.Pronouns == nil || *userResp.Pronouns != "they/them" {
		t.Errorf("pronouns = %v, want %q", userResp.Pronouns, "they/them")
	}
}

// --- About tests ---

func TestUpdateMe_AboutTooLong(t *testing.T) {
	t.Parallel()
	repo := newFakeRepo()
	u := seedUser(repo)
	app := testUserApp(t, repo, u.ID)

	longAbout := strings.Repeat("a", 191)
	resp := doReq(t, app, jsonReq(http.MethodPatch, "/@me", `{"about":"`+longAbout+`"}`))
	body := readBody(t, resp)

	if resp.StatusCode != fiber.StatusBadRequest {
		t.Errorf("status = %d, want %d", resp.StatusCode, fiber.StatusBadRequest)
	}
	env := parseError(t, body)
	if env.Error.Code != string(apierrors.ValidationError) {
		t.Errorf("error code = %q, want %q", env.Error.Code, apierrors.ValidationError)
	}
}

func TestUpdateMe_AboutSuccess(t *testing.T) {
	t.Parallel()
	repo := newFakeRepo()
	u := seedUser(repo)
	app := testUserApp(t, repo, u.ID)

	resp := doReq(t, app, jsonReq(http.MethodPatch, "/@me", `{"about":"Hello, world!"}`))
	body := readBody(t, resp)

	if resp.StatusCode != fiber.StatusOK {
		t.Errorf("status = %d, want %d", resp.StatusCode, fiber.StatusOK)
	}

	env := parseSuccess(t, body)
	var userResp struct {
		About *string `json:"about"`
	}
	if err := json.Unmarshal(env.Data, &userResp); err != nil {
		t.Fatalf("unmarshal user response: %v", err)
	}
	if userResp.About == nil || *userResp.About != "Hello, world!" {
		t.Errorf("about = %v, want %q", userResp.About, "Hello, world!")
	}
}

// --- Theme colour tests ---

func TestUpdateMe_ThemeColourNegative(t *testing.T) {
	t.Parallel()
	repo := newFakeRepo()
	u := seedUser(repo)
	app := testUserApp(t, repo, u.ID)

	resp := doReq(t, app, jsonReq(http.MethodPatch, "/@me", `{"theme_colour_primary":-1}`))
	body := readBody(t, resp)

	if resp.StatusCode != fiber.StatusBadRequest {
		t.Errorf("status = %d, want %d", resp.StatusCode, fiber.StatusBadRequest)
	}
	env := parseError(t, body)
	if env.Error.Code != string(apierrors.ValidationError) {
		t.Errorf("error code = %q, want %q", env.Error.Code, apierrors.ValidationError)
	}
}

func TestUpdateMe_ThemeColourTooLarge(t *testing.T) {
	t.Parallel()
	repo := newFakeRepo()
	u := seedUser(repo)
	app := testUserApp(t, repo, u.ID)

	resp := doReq(t, app, jsonReq(http.MethodPatch, "/@me", `{"theme_colour_secondary":16777216}`))
	body := readBody(t, resp)

	if resp.StatusCode != fiber.StatusBadRequest {
		t.Errorf("status = %d, want %d", resp.StatusCode, fiber.StatusBadRequest)
	}
	env := parseError(t, body)
	if env.Error.Code != string(apierrors.ValidationError) {
		t.Errorf("error code = %q, want %q", env.Error.Code, apierrors.ValidationError)
	}
}

func TestUpdateMe_ThemeColourSuccess(t *testing.T) {
	t.Parallel()
	repo := newFakeRepo()
	u := seedUser(repo)
	app := testUserApp(t, repo, u.ID)

	resp := doReq(t, app, jsonReq(http.MethodPatch, "/@me", `{"theme_colour_primary":16711680,"theme_colour_secondary":255}`))
	body := readBody(t, resp)

	if resp.StatusCode != fiber.StatusOK {
		t.Errorf("status = %d, want %d", resp.StatusCode, fiber.StatusOK)
	}

	env := parseSuccess(t, body)
	var userResp struct {
		ThemeColourPrimary   *int `json:"theme_colour_primary"`
		ThemeColourSecondary *int `json:"theme_colour_secondary"`
	}
	if err := json.Unmarshal(env.Data, &userResp); err != nil {
		t.Fatalf("unmarshal user response: %v", err)
	}
	if userResp.ThemeColourPrimary == nil || *userResp.ThemeColourPrimary != 16711680 {
		t.Errorf("theme_colour_primary = %v, want 16711680", userResp.ThemeColourPrimary)
	}
	if userResp.ThemeColourSecondary == nil || *userResp.ThemeColourSecondary != 255 {
		t.Errorf("theme_colour_secondary = %v, want 255", userResp.ThemeColourSecondary)
	}
}

// --- Banner key tests ---

func TestUpdateMe_BannerKey(t *testing.T) {
	t.Parallel()
	repo := newFakeRepo()
	u := seedUser(repo)
	app := testUserApp(t, repo, u.ID)

	resp := doReq(t, app, jsonReq(http.MethodPatch, "/@me", `{"banner_key":"banner_abc123"}`))
	body := readBody(t, resp)

	if resp.StatusCode != fiber.StatusOK {
		t.Errorf("status = %d, want %d", resp.StatusCode, fiber.StatusOK)
	}

	env := parseSuccess(t, body)
	var userResp struct {
		BannerKey *string `json:"banner_key"`
	}
	if err := json.Unmarshal(env.Data, &userResp); err != nil {
		t.Fatalf("unmarshal user response: %v", err)
	}
	if userResp.BannerKey == nil || *userResp.BannerKey != "banner_abc123" {
		t.Errorf("banner_key = %v, want %q", userResp.BannerKey, "banner_abc123")
	}
}

// --- GetProfile tests ---

func TestGetProfile_InvalidID(t *testing.T) {
	t.Parallel()
	repo := newFakeRepo()
	u := seedUser(repo)
	app := testUserApp(t, repo, u.ID)

	resp := doReq(t, app, jsonReq(http.MethodGet, "/not-a-uuid", ""))
	body := readBody(t, resp)

	if resp.StatusCode != fiber.StatusBadRequest {
		t.Errorf("status = %d, want %d", resp.StatusCode, fiber.StatusBadRequest)
	}
	env := parseError(t, body)
	if env.Error.Code != string(apierrors.ValidationError) {
		t.Errorf("error code = %q, want %q", env.Error.Code, apierrors.ValidationError)
	}
}

func TestGetProfile_NotFound(t *testing.T) {
	t.Parallel()
	repo := newFakeRepo()
	u := seedUser(repo)
	app := testUserApp(t, repo, u.ID)

	resp := doReq(t, app, jsonReq(http.MethodGet, "/"+uuid.New().String(), ""))
	body := readBody(t, resp)

	if resp.StatusCode != fiber.StatusNotFound {
		t.Errorf("status = %d, want %d", resp.StatusCode, fiber.StatusNotFound)
	}
	env := parseError(t, body)
	if env.Error.Code != string(apierrors.UnknownUser) {
		t.Errorf("error code = %q, want %q", env.Error.Code, apierrors.UnknownUser)
	}
}

func TestGetProfile_Success(t *testing.T) {
	t.Parallel()
	repo := newFakeRepo()
	u := seedUser(repo)
	app := testUserApp(t, repo, u.ID)

	resp := doReq(t, app, jsonReq(http.MethodGet, "/"+u.ID.String(), ""))
	body := readBody(t, resp)

	if resp.StatusCode != fiber.StatusOK {
		t.Errorf("status = %d, want %d", resp.StatusCode, fiber.StatusOK)
	}

	// Verify the raw JSON contains public fields and excludes private ones.
	env := parseSuccess(t, body)
	raw := make(map[string]json.RawMessage)
	if err := json.Unmarshal(env.Data, &raw); err != nil {
		t.Fatalf("unmarshal profile response: %v", err)
	}

	for _, field := range []string{"id", "username", "display_name", "pronouns", "about", "theme_colour_primary"} {
		if _, ok := raw[field]; !ok {
			t.Errorf("expected field %q in response", field)
		}
	}
	for _, field := range []string{"email", "mfa_enabled", "email_verified"} {
		if _, ok := raw[field]; ok {
			t.Errorf("private field %q must not appear in profile response", field)
		}
	}

	var profile struct {
		ID          string  `json:"id"`
		Username    string  `json:"username"`
		DisplayName *string `json:"display_name"`
	}
	if err := json.Unmarshal(env.Data, &profile); err != nil {
		t.Fatalf("unmarshal profile fields: %v", err)
	}
	if profile.ID != u.ID.String() {
		t.Errorf("id = %q, want %q", profile.ID, u.ID.String())
	}
	if profile.Username != "alice" {
		t.Errorf("username = %q, want %q", profile.Username, "alice")
	}
	if profile.DisplayName == nil || *profile.DisplayName != "Alice" {
		t.Errorf("display_name = %v, want %q", profile.DisplayName, "Alice")
	}
}
