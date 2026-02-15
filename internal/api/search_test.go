package api

import (
	"context"
	"encoding/json"
	"net/http"
	"testing"
	"time"

	"github.com/gofiber/fiber/v3"
	"github.com/google/uuid"
	"github.com/rs/zerolog"
	apierrors "github.com/uncord-chat/uncord-protocol/errors"
	"github.com/uncord-chat/uncord-protocol/models"

	"github.com/uncord-chat/uncord-server/internal/search"
)

// fakeSearcher implements search.Searcher for handler tests.
type fakeSearcher struct {
	result *search.SearchResult
	err    error
}

func (f *fakeSearcher) Search(_ context.Context, _ search.SearchParams) (*search.SearchResult, error) {
	if f.err != nil {
		return nil, f.err
	}
	return f.result, nil
}

func testSearchApp(t *testing.T, channelRepo *fakeChannelRepo, searcher search.Searcher, resolver search.PermissionFilter, userID uuid.UUID) *fiber.App {
	t.Helper()
	svc := search.NewService(channelRepo, resolver, searcher, zerolog.Nop())
	handler := NewSearchHandler(svc, zerolog.Nop())

	app := fiber.New()
	app.Use(fakeAuth(userID))
	app.Get("/search/messages", handler.SearchMessages)
	return app
}

func TestSearchMessages_Success(t *testing.T) {
	t.Parallel()
	repo := newFakeChannelRepo()
	ch := seedChannel(repo)

	msgID := uuid.New().String()
	authorID := uuid.New().String()
	now := time.Now().Unix()

	searcher := &fakeSearcher{
		result: &search.SearchResult{
			Found: 1,
			Hits: []search.SearchHit{
				{
					Document: search.SearchDocument{
						ID:        msgID,
						ChannelID: ch.ID.String(),
						AuthorID:  authorID,
						Content:   "hello world",
						CreatedAt: now,
					},
					Highlights: []search.SearchHighlight{
						{Field: "content", Snippets: []string{"<mark>hello</mark> world"}},
					},
				},
			},
		},
	}
	app := testSearchApp(t, repo, searcher, allowAllResolver(), uuid.New())

	resp := doReq(t, app, jsonReq(http.MethodGet, "/search/messages?q=hello", ""))
	body := readBody(t, resp)

	if resp.StatusCode != fiber.StatusOK {
		t.Errorf("status = %d, want %d", resp.StatusCode, fiber.StatusOK)
	}

	env := parseSuccess(t, body)
	var result models.SearchResponse
	if err := json.Unmarshal(env.Data, &result); err != nil {
		t.Fatalf("unmarshal search response: %v", err)
	}
	if result.TotalCount != 1 {
		t.Errorf("total_count = %d, want 1", result.TotalCount)
	}
	if len(result.Hits) != 1 {
		t.Fatalf("hits = %d, want 1", len(result.Hits))
	}
	if result.Hits[0].Content != "hello world" {
		t.Errorf("content = %q, want %q", result.Hits[0].Content, "hello world")
	}
	if len(result.Hits[0].Highlights) != 1 {
		t.Fatalf("highlights = %d, want 1", len(result.Hits[0].Highlights))
	}
	if result.Hits[0].Highlights[0] != "<mark>hello</mark> world" {
		t.Errorf("highlight = %q, want %q", result.Hits[0].Highlights[0], "<mark>hello</mark> world")
	}
}

func TestSearchMessages_EmptyQuery(t *testing.T) {
	t.Parallel()
	repo := newFakeChannelRepo()
	searcher := &fakeSearcher{}
	app := testSearchApp(t, repo, searcher, allowAllResolver(), uuid.New())

	resp := doReq(t, app, jsonReq(http.MethodGet, "/search/messages?q=", ""))
	body := readBody(t, resp)

	if resp.StatusCode != fiber.StatusBadRequest {
		t.Errorf("status = %d, want %d", resp.StatusCode, fiber.StatusBadRequest)
	}
	env := parseError(t, body)
	if env.Error.Code != string(apierrors.ValidationError) {
		t.Errorf("error code = %q, want %q", env.Error.Code, apierrors.ValidationError)
	}
}

func TestSearchMessages_MissingQuery(t *testing.T) {
	t.Parallel()
	repo := newFakeChannelRepo()
	searcher := &fakeSearcher{}
	app := testSearchApp(t, repo, searcher, allowAllResolver(), uuid.New())

	resp := doReq(t, app, jsonReq(http.MethodGet, "/search/messages", ""))
	body := readBody(t, resp)

	if resp.StatusCode != fiber.StatusBadRequest {
		t.Errorf("status = %d, want %d", resp.StatusCode, fiber.StatusBadRequest)
	}
	env := parseError(t, body)
	if env.Error.Code != string(apierrors.ValidationError) {
		t.Errorf("error code = %q, want %q", env.Error.Code, apierrors.ValidationError)
	}
}

func TestSearchMessages_InvalidChannelID(t *testing.T) {
	t.Parallel()
	repo := newFakeChannelRepo()
	searcher := &fakeSearcher{}
	app := testSearchApp(t, repo, searcher, allowAllResolver(), uuid.New())

	resp := doReq(t, app, jsonReq(http.MethodGet, "/search/messages?q=test&channel_id=not-a-uuid", ""))
	body := readBody(t, resp)

	if resp.StatusCode != fiber.StatusBadRequest {
		t.Errorf("status = %d, want %d", resp.StatusCode, fiber.StatusBadRequest)
	}
	env := parseError(t, body)
	if env.Error.Code != string(apierrors.ValidationError) {
		t.Errorf("error code = %q, want %q", env.Error.Code, apierrors.ValidationError)
	}
}

func TestSearchMessages_InvalidAuthorID(t *testing.T) {
	t.Parallel()
	repo := newFakeChannelRepo()
	searcher := &fakeSearcher{}
	app := testSearchApp(t, repo, searcher, allowAllResolver(), uuid.New())

	resp := doReq(t, app, jsonReq(http.MethodGet, "/search/messages?q=test&author_id=not-a-uuid", ""))
	body := readBody(t, resp)

	if resp.StatusCode != fiber.StatusBadRequest {
		t.Errorf("status = %d, want %d", resp.StatusCode, fiber.StatusBadRequest)
	}
	env := parseError(t, body)
	if env.Error.Code != string(apierrors.ValidationError) {
		t.Errorf("error code = %q, want %q", env.Error.Code, apierrors.ValidationError)
	}
}

func TestSearchMessages_SearchUnavailable(t *testing.T) {
	t.Parallel()
	repo := newFakeChannelRepo()
	seedChannel(repo)
	searcher := &fakeSearcher{err: search.ErrSearchUnavailable}
	app := testSearchApp(t, repo, searcher, allowAllResolver(), uuid.New())

	resp := doReq(t, app, jsonReq(http.MethodGet, "/search/messages?q=test", ""))
	body := readBody(t, resp)

	if resp.StatusCode != fiber.StatusServiceUnavailable {
		t.Errorf("status = %d, want %d", resp.StatusCode, fiber.StatusServiceUnavailable)
	}
	env := parseError(t, body)
	if env.Error.Code != string(apierrors.SearchUnavailable) {
		t.Errorf("error code = %q, want %q", env.Error.Code, apierrors.SearchUnavailable)
	}
}

func TestSearchMessages_NoPermittedChannels(t *testing.T) {
	t.Parallel()
	repo := newFakeChannelRepo()
	seedChannel(repo)
	searcher := &fakeSearcher{}
	app := testSearchApp(t, repo, searcher, denyAllResolver(), uuid.New())

	resp := doReq(t, app, jsonReq(http.MethodGet, "/search/messages?q=test", ""))
	body := readBody(t, resp)

	if resp.StatusCode != fiber.StatusOK {
		t.Errorf("status = %d, want %d", resp.StatusCode, fiber.StatusOK)
	}

	env := parseSuccess(t, body)
	var result models.SearchResponse
	if err := json.Unmarshal(env.Data, &result); err != nil {
		t.Fatalf("unmarshal search response: %v", err)
	}
	if result.TotalCount != 0 {
		t.Errorf("total_count = %d, want 0", result.TotalCount)
	}
	if len(result.Hits) != 0 {
		t.Errorf("hits = %d, want 0", len(result.Hits))
	}
}

func TestSearchMessages_ChannelFilterNotPermitted(t *testing.T) {
	t.Parallel()
	repo := newFakeChannelRepo()
	seedChannel(repo)
	searcher := &fakeSearcher{}
	restrictedChannel := uuid.New().String()
	app := testSearchApp(t, repo, searcher, denyAllResolver(), uuid.New())

	resp := doReq(t, app, jsonReq(http.MethodGet, "/search/messages?q=test&channel_id="+restrictedChannel, ""))
	body := readBody(t, resp)

	if resp.StatusCode != fiber.StatusOK {
		t.Errorf("status = %d, want %d", resp.StatusCode, fiber.StatusOK)
	}

	env := parseSuccess(t, body)
	var result models.SearchResponse
	if err := json.Unmarshal(env.Data, &result); err != nil {
		t.Fatalf("unmarshal search response: %v", err)
	}
	if len(result.Hits) != 0 {
		t.Errorf("hits = %d, want 0", len(result.Hits))
	}
}

func TestSearchMessages_PaginationDefaults(t *testing.T) {
	t.Parallel()
	repo := newFakeChannelRepo()
	seedChannel(repo)
	searcher := &fakeSearcher{
		result: &search.SearchResult{Found: 0},
	}
	app := testSearchApp(t, repo, searcher, allowAllResolver(), uuid.New())

	resp := doReq(t, app, jsonReq(http.MethodGet, "/search/messages?q=test", ""))
	body := readBody(t, resp)

	if resp.StatusCode != fiber.StatusOK {
		t.Errorf("status = %d, want %d", resp.StatusCode, fiber.StatusOK)
	}

	env := parseSuccess(t, body)
	var result models.SearchResponse
	if err := json.Unmarshal(env.Data, &result); err != nil {
		t.Fatalf("unmarshal search response: %v", err)
	}
	if result.Page != 1 {
		t.Errorf("page = %d, want 1", result.Page)
	}
	if result.PerPage != 25 {
		t.Errorf("per_page = %d, want 25", result.PerPage)
	}
}

func TestSearchMessages_PaginationClamp(t *testing.T) {
	t.Parallel()
	repo := newFakeChannelRepo()
	seedChannel(repo)
	searcher := &fakeSearcher{
		result: &search.SearchResult{Found: 0},
	}
	app := testSearchApp(t, repo, searcher, allowAllResolver(), uuid.New())

	resp := doReq(t, app, jsonReq(http.MethodGet, "/search/messages?q=test&limit=200", ""))
	body := readBody(t, resp)

	if resp.StatusCode != fiber.StatusOK {
		t.Errorf("status = %d, want %d", resp.StatusCode, fiber.StatusOK)
	}

	env := parseSuccess(t, body)
	var result models.SearchResponse
	if err := json.Unmarshal(env.Data, &result); err != nil {
		t.Fatalf("unmarshal search response: %v", err)
	}
	if result.PerPage != 100 {
		t.Errorf("per_page = %d, want 100", result.PerPage)
	}
}

func TestSearchMessages_Unauthenticated(t *testing.T) {
	t.Parallel()
	repo := newFakeChannelRepo()
	searcher := &fakeSearcher{}
	app := testSearchApp(t, repo, searcher, allowAllResolver(), uuid.Nil)

	resp := doReq(t, app, jsonReq(http.MethodGet, "/search/messages?q=test", ""))
	body := readBody(t, resp)

	if resp.StatusCode != fiber.StatusUnauthorized {
		t.Errorf("status = %d, want %d", resp.StatusCode, fiber.StatusUnauthorized)
	}
	env := parseError(t, body)
	if env.Error.Code != string(apierrors.Unauthorised) {
		t.Errorf("error code = %q, want %q", env.Error.Code, apierrors.Unauthorised)
	}
}

func TestSearchMessages_EmptyResults(t *testing.T) {
	t.Parallel()
	repo := newFakeChannelRepo()
	seedChannel(repo)
	searcher := &fakeSearcher{
		result: &search.SearchResult{Found: 0},
	}
	app := testSearchApp(t, repo, searcher, allowAllResolver(), uuid.New())

	resp := doReq(t, app, jsonReq(http.MethodGet, "/search/messages?q=nonexistent", ""))
	body := readBody(t, resp)

	if resp.StatusCode != fiber.StatusOK {
		t.Errorf("status = %d, want %d", resp.StatusCode, fiber.StatusOK)
	}

	env := parseSuccess(t, body)
	var result models.SearchResponse
	if err := json.Unmarshal(env.Data, &result); err != nil {
		t.Fatalf("unmarshal search response: %v", err)
	}
	if result.TotalCount != 0 {
		t.Errorf("total_count = %d, want 0", result.TotalCount)
	}
	if len(result.Hits) != 0 {
		t.Errorf("hits = %d, want 0", len(result.Hits))
	}
}
