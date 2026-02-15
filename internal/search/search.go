package search

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/rs/zerolog"
	"github.com/uncord-chat/uncord-protocol/models"
	"github.com/uncord-chat/uncord-protocol/permissions"

	"github.com/uncord-chat/uncord-server/internal/channel"
)

// Sentinel errors for the search package.
var (
	ErrSearchUnavailable = errors.New("search service is unavailable")
	ErrEmptyQuery        = errors.New("search query must not be empty")
)

// Pagination defaults and limits.
const (
	DefaultPerPage = 25
	MaxPerPage     = 100
	DefaultPage    = 1
)

// ChannelLister retrieves all channels. Satisfied by channel.Repository.
type ChannelLister interface {
	List(ctx context.Context) ([]channel.Channel, error)
}

// PermissionFilter checks channel access for a user. Satisfied by *permission.Resolver.
type PermissionFilter interface {
	FilterPermitted(ctx context.Context, userID uuid.UUID, channelIDs []uuid.UUID,
		perm permissions.Permission) ([]bool, error)
}

// Searcher performs raw search queries against a search backend.
type Searcher interface {
	Search(ctx context.Context, params SearchParams) (*SearchResult, error)
}

// Options groups optional query parameters from the handler.
type Options struct {
	ChannelID string
	AuthorID  string
	Before    int64
	After     int64
	Page      int
	PerPage   int
}

// ClampPagination normalises page and per-page values to valid ranges.
func ClampPagination(page, perPage int) (int, int) {
	if page < DefaultPage {
		page = DefaultPage
	}
	if perPage < 1 {
		perPage = DefaultPerPage
	}
	if perPage > MaxPerPage {
		perPage = MaxPerPage
	}
	return page, perPage
}

// SearchParams groups the parameters sent to the search backend.
type SearchParams struct {
	Query      string
	ChannelIDs []string
	AuthorID   string
	Before     int64
	After      int64
	Page       int
	PerPage    int
}

// SearchResult holds the raw search backend response.
type SearchResult struct {
	Found int         `json:"found"`
	Hits  []SearchHit `json:"hits"`
}

// SearchHit represents a single search hit from the backend.
type SearchHit struct {
	Document   SearchDocument    `json:"document"`
	Highlights []SearchHighlight `json:"highlights"`
}

// SearchDocument holds the indexed message fields.
type SearchDocument struct {
	ID        string `json:"id"`
	Content   string `json:"content"`
	AuthorID  string `json:"author_id"`
	ChannelID string `json:"channel_id"`
	CreatedAt int64  `json:"created_at"`
}

// SearchHighlight holds highlight information for a single field.
type SearchHighlight struct {
	Field    string   `json:"field"`
	Snippets []string `json:"snippets"`
}

// TypesenseSearcher performs search requests against the Typesense HTTP API.
type TypesenseSearcher struct {
	baseURL string
	apiKey  string
	client  *http.Client
}

// NewTypesenseSearcher creates a new Typesense search client.
func NewTypesenseSearcher(baseURL, apiKey string, timeout time.Duration) *TypesenseSearcher {
	return &TypesenseSearcher{
		baseURL: baseURL,
		apiKey:  apiKey,
		client:  &http.Client{Timeout: timeout},
	}
}

// Search executes a search query against the Typesense messages collection.
func (ts *TypesenseSearcher) Search(ctx context.Context, params SearchParams) (*SearchResult, error) {
	filterParts := []string{
		"channel_id:[" + strings.Join(params.ChannelIDs, ",") + "]",
	}
	if params.AuthorID != "" {
		filterParts = append(filterParts, "author_id:="+params.AuthorID)
	}
	if params.Before > 0 {
		filterParts = append(filterParts, "created_at:<"+strconv.FormatInt(params.Before, 10))
	}
	if params.After > 0 {
		filterParts = append(filterParts, "created_at:>"+strconv.FormatInt(params.After, 10))
	}

	qv := url.Values{}
	qv.Set("q", params.Query)
	qv.Set("query_by", "content")
	qv.Set("filter_by", strings.Join(filterParts, " && "))
	qv.Set("sort_by", "created_at:desc")
	qv.Set("page", strconv.Itoa(params.Page))
	qv.Set("per_page", strconv.Itoa(params.PerPage))
	qv.Set("highlight_fields", "content")
	qv.Set("highlight_start_tag", "<mark>")
	qv.Set("highlight_end_tag", "</mark>")

	searchURL := ts.baseURL + "/collections/messages/documents/search?" + qv.Encode()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, searchURL, nil)
	if err != nil {
		return nil, fmt.Errorf("build search request: %w", err)
	}
	req.Header.Set("X-TYPESENSE-API-KEY", ts.apiKey)

	resp, err := ts.client.Do(req)
	if err != nil {
		return nil, ErrSearchUnavailable
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode >= 500 {
		return nil, ErrSearchUnavailable
	}
	if resp.StatusCode >= 400 {
		detail, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("typesense returned status %d on search: %s", resp.StatusCode, detail)
	}

	var result SearchResult
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return nil, fmt.Errorf("decode search response: %w", err)
	}
	return &result, nil
}

// Service orchestrates permission-scoped message search.
type Service struct {
	channels ChannelLister
	perms    PermissionFilter
	searcher Searcher
	log      zerolog.Logger
}

// NewService creates a new search service.
func NewService(channels ChannelLister, perms PermissionFilter, searcher Searcher, logger zerolog.Logger) *Service {
	return &Service{channels: channels, perms: perms, searcher: searcher, log: logger}
}

// Search executes a permission-scoped message search. Only messages from channels the user has ViewChannels access to
// are returned.
func (s *Service) Search(ctx context.Context, userID uuid.UUID, query string, opts Options) (*models.SearchResponse, error) {
	query = strings.TrimSpace(query)
	if query == "" {
		return nil, ErrEmptyQuery
	}

	all, err := s.channels.List(ctx)
	if err != nil {
		return nil, fmt.Errorf("list channels: %w", err)
	}

	channelIDs := make([]uuid.UUID, len(all))
	for i := range all {
		channelIDs[i] = all[i].ID
	}

	permitted, err := s.perms.FilterPermitted(ctx, userID, channelIDs, permissions.ViewChannels)
	if err != nil {
		return nil, fmt.Errorf("filter permitted channels: %w", err)
	}

	var allowedIDs []string
	for i, ok := range permitted {
		if ok {
			allowedIDs = append(allowedIDs, channelIDs[i].String())
		}
	}

	// If the caller specified a channel filter, intersect with the permitted set.
	if opts.ChannelID != "" {
		found := false
		for _, id := range allowedIDs {
			if id == opts.ChannelID {
				found = true
				break
			}
		}
		if !found {
			return emptyResponse(opts.Page, opts.PerPage), nil
		}
		allowedIDs = []string{opts.ChannelID}
	}

	if len(allowedIDs) == 0 {
		return emptyResponse(opts.Page, opts.PerPage), nil
	}

	result, err := s.searcher.Search(ctx, SearchParams{
		Query:      query,
		ChannelIDs: allowedIDs,
		AuthorID:   opts.AuthorID,
		Before:     opts.Before,
		After:      opts.After,
		Page:       opts.Page,
		PerPage:    opts.PerPage,
	})
	if err != nil {
		return nil, err
	}

	hits := make([]models.SearchMessageHit, 0, len(result.Hits))
	for _, h := range result.Hits {
		hit := models.SearchMessageHit{
			ID:        h.Document.ID,
			ChannelID: h.Document.ChannelID,
			AuthorID:  h.Document.AuthorID,
			Content:   h.Document.Content,
			CreatedAt: h.Document.CreatedAt,
		}
		for _, hl := range h.Highlights {
			if hl.Field == "content" {
				hit.Highlights = hl.Snippets
				break
			}
		}
		hits = append(hits, hit)
	}

	return &models.SearchResponse{
		TotalCount: result.Found,
		Page:       opts.Page,
		PerPage:    opts.PerPage,
		Hits:       hits,
	}, nil
}

func emptyResponse(page, perPage int) *models.SearchResponse {
	return &models.SearchResponse{
		TotalCount: 0,
		Page:       page,
		PerPage:    perPage,
		Hits:       []models.SearchMessageHit{},
	}
}
