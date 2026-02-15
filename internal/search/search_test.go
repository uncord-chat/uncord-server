package search

import (
	"context"
	"errors"
	"testing"

	"github.com/google/uuid"
	"github.com/rs/zerolog"
	"github.com/uncord-chat/uncord-protocol/permissions"

	"github.com/uncord-chat/uncord-server/internal/channel"
)

type fakeChannelLister struct {
	channels []channel.Channel
	err      error
}

func (f *fakeChannelLister) List(_ context.Context) ([]channel.Channel, error) {
	return f.channels, f.err
}

type fakePermissionFilter struct {
	results []bool
	err     error
}

func (f *fakePermissionFilter) FilterPermitted(_ context.Context, _ uuid.UUID, channelIDs []uuid.UUID,
	_ permissions.Permission) ([]bool, error) {
	if f.err != nil {
		return nil, f.err
	}
	if f.results != nil {
		return f.results, nil
	}
	out := make([]bool, len(channelIDs))
	for i := range out {
		out[i] = true
	}
	return out, nil
}

type fakeSearcher struct {
	result *SearchResult
	err    error
	params SearchParams
}

func (f *fakeSearcher) Search(_ context.Context, params SearchParams) (*SearchResult, error) {
	f.params = params
	if f.err != nil {
		return nil, f.err
	}
	if f.result != nil {
		return f.result, nil
	}
	return &SearchResult{Found: 0, Hits: nil}, nil
}

func TestService_SearchEmptyQuery(t *testing.T) {
	t.Parallel()
	svc := NewService(&fakeChannelLister{}, &fakePermissionFilter{}, &fakeSearcher{}, zerolog.Nop())

	_, err := svc.Search(context.Background(), uuid.New(), "   ", Options{})
	if !errors.Is(err, ErrEmptyQuery) {
		t.Errorf("Search() error = %v, want ErrEmptyQuery", err)
	}
}

func TestService_SearchInvalidAuthorID(t *testing.T) {
	t.Parallel()
	svc := NewService(&fakeChannelLister{}, &fakePermissionFilter{}, &fakeSearcher{}, zerolog.Nop())

	_, err := svc.Search(context.Background(), uuid.New(), "hello", Options{
		AuthorID: "not-a-uuid",
	})
	if !errors.Is(err, ErrInvalidFilter) {
		t.Errorf("Search() error = %v, want ErrInvalidFilter", err)
	}
}

func TestService_SearchInvalidChannelID(t *testing.T) {
	t.Parallel()
	svc := NewService(&fakeChannelLister{}, &fakePermissionFilter{}, &fakeSearcher{}, zerolog.Nop())

	_, err := svc.Search(context.Background(), uuid.New(), "hello", Options{
		ChannelID: "not-a-uuid",
	})
	if !errors.Is(err, ErrInvalidFilter) {
		t.Errorf("Search() error = %v, want ErrInvalidFilter", err)
	}
}

func TestService_SearchValidFiltersPassThrough(t *testing.T) {
	t.Parallel()

	chID := uuid.New()
	authorID := uuid.New()
	searcher := &fakeSearcher{}
	svc := NewService(
		&fakeChannelLister{channels: []channel.Channel{{ID: chID}}},
		&fakePermissionFilter{},
		searcher,
		zerolog.Nop(),
	)

	_, err := svc.Search(context.Background(), uuid.New(), "hello", Options{
		ChannelID: chID.String(),
		AuthorID:  authorID.String(),
		Page:      1,
		PerPage:   10,
	})
	if err != nil {
		t.Fatalf("Search() unexpected error: %v", err)
	}

	if searcher.params.AuthorID != authorID.String() {
		t.Errorf("searcher received AuthorID = %q, want %q", searcher.params.AuthorID, authorID.String())
	}
	if len(searcher.params.ChannelIDs) != 1 || searcher.params.ChannelIDs[0] != chID.String() {
		t.Errorf("searcher received ChannelIDs = %v, want [%s]", searcher.params.ChannelIDs, chID.String())
	}
}

func TestService_SearchNoPermittedChannels(t *testing.T) {
	t.Parallel()

	svc := NewService(
		&fakeChannelLister{channels: []channel.Channel{{ID: uuid.New()}}},
		&fakePermissionFilter{results: []bool{false}},
		&fakeSearcher{},
		zerolog.Nop(),
	)

	result, err := svc.Search(context.Background(), uuid.New(), "hello", Options{Page: 1, PerPage: 10})
	if err != nil {
		t.Fatalf("Search() unexpected error: %v", err)
	}
	if result.TotalCount != 0 {
		t.Errorf("TotalCount = %d, want 0 for no permitted channels", result.TotalCount)
	}
}

func TestService_SearchInjectionBlocked(t *testing.T) {
	t.Parallel()

	injectionValues := []string{
		"abc && created_at:<0",
		"abc] || channel_id:[*",
		"'; DROP TABLE messages; --",
	}

	svc := NewService(&fakeChannelLister{}, &fakePermissionFilter{}, &fakeSearcher{}, zerolog.Nop())

	for _, val := range injectionValues {
		_, err := svc.Search(context.Background(), uuid.New(), "hello", Options{
			AuthorID: val,
		})
		if !errors.Is(err, ErrInvalidFilter) {
			t.Errorf("Search(AuthorID=%q) error = %v, want ErrInvalidFilter", val, err)
		}
	}
}
