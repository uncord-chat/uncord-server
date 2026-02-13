package disposable

import (
	"context"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
	"time"

	"github.com/rs/zerolog"
)

func TestBlocklistDisabled(t *testing.T) {
	t.Parallel()
	bl := NewBlocklist("http://unused", false, zerolog.Nop())

	blocked, err := bl.IsBlocked(context.Background(), "mailinator.com")
	if err != nil {
		t.Fatalf("IsBlocked() error = %v", err)
	}
	if blocked {
		t.Error("IsBlocked() = true, want false when disabled")
	}
}

func TestBlocklistBlockedDomain(t *testing.T) {
	t.Parallel()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte("mailinator.com\nguerrillamail.com\n"))
	}))
	defer srv.Close()

	bl := NewBlocklist(srv.URL, true, zerolog.Nop())

	blocked, err := bl.IsBlocked(context.Background(), "mailinator.com")
	if err != nil {
		t.Fatalf("IsBlocked() error = %v", err)
	}
	if !blocked {
		t.Error("IsBlocked(mailinator.com) = false, want true")
	}
}

func TestBlocklistAllowedDomain(t *testing.T) {
	t.Parallel()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte("mailinator.com\nguerrillamail.com\n"))
	}))
	defer srv.Close()

	bl := NewBlocklist(srv.URL, true, zerolog.Nop())

	blocked, err := bl.IsBlocked(context.Background(), "gmail.com")
	if err != nil {
		t.Fatalf("IsBlocked() error = %v", err)
	}
	if blocked {
		t.Error("IsBlocked(gmail.com) = true, want false")
	}
}

func TestBlocklistFetchError(t *testing.T) {
	t.Parallel()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
	}))
	defer srv.Close()

	bl := NewBlocklist(srv.URL, true, zerolog.Nop())

	_, err := bl.IsBlocked(context.Background(), "test.com")
	if err == nil {
		t.Fatal("IsBlocked() should return error on fetch failure")
	}
}

func TestBlocklistLazyCaching(t *testing.T) {
	t.Parallel()
	var fetchCount atomic.Int32

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		fetchCount.Add(1)
		_, _ = w.Write([]byte("mailinator.com\n"))
	}))
	defer srv.Close()

	bl := NewBlocklist(srv.URL, true, zerolog.Nop())

	// Call multiple times; should only fetch once
	for i := 0; i < 5; i++ {
		_, err := bl.IsBlocked(context.Background(), "mailinator.com")
		if err != nil {
			t.Fatalf("IsBlocked() call %d error = %v", i, err)
		}
	}

	if fetchCount.Load() != 1 {
		t.Errorf("fetch count = %d, want 1 (lazy caching)", fetchCount.Load())
	}
}

func TestBlocklistCaseInsensitive(t *testing.T) {
	t.Parallel()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte("Mailinator.COM\n"))
	}))
	defer srv.Close()

	bl := NewBlocklist(srv.URL, true, zerolog.Nop())

	blocked, err := bl.IsBlocked(context.Background(), "mailinator.com")
	if err != nil {
		t.Fatalf("IsBlocked() error = %v", err)
	}
	if !blocked {
		t.Error("IsBlocked() = false, want true (case insensitive)")
	}
}

func TestBlocklistCommentsAndBlanks(t *testing.T) {
	t.Parallel()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte("# comment\n\nmailinator.com\n\n# another comment\n"))
	}))
	defer srv.Close()

	bl := NewBlocklist(srv.URL, true, zerolog.Nop())

	blocked, err := bl.IsBlocked(context.Background(), "mailinator.com")
	if err != nil {
		t.Fatalf("IsBlocked() error = %v", err)
	}
	if !blocked {
		t.Error("IsBlocked() = false, want true")
	}
}

func TestPrefetchLoadsBlocklist(t *testing.T) {
	t.Parallel()
	var fetchCount atomic.Int32

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		fetchCount.Add(1)
		_, _ = w.Write([]byte("mailinator.com\n"))
	}))
	defer srv.Close()

	bl := NewBlocklist(srv.URL, true, zerolog.Nop())
	bl.Prefetch(context.Background())

	// After prefetch, IsBlocked should not trigger another fetch
	blocked, err := bl.IsBlocked(context.Background(), "mailinator.com")
	if err != nil {
		t.Fatalf("IsBlocked() error = %v", err)
	}
	if !blocked {
		t.Error("IsBlocked(mailinator.com) = false after Prefetch, want true")
	}

	if fetchCount.Load() != 1 {
		t.Errorf("fetch count = %d, want 1 (prefetch only)", fetchCount.Load())
	}
}

func TestPrefetchDisabledNoop(t *testing.T) {
	t.Parallel()
	bl := NewBlocklist("http://unused", false, zerolog.Nop())
	bl.Prefetch(context.Background()) // should not panic or fetch
}

func TestRunPeriodicRefresh(t *testing.T) {
	t.Parallel()
	var fetchCount atomic.Int32

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		fetchCount.Add(1)
		_, _ = w.Write([]byte("mailinator.com\n"))
	}))
	defer srv.Close()

	bl := NewBlocklist(srv.URL, true, zerolog.Nop())

	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() {
		bl.Run(ctx, 50*time.Millisecond)
		close(done)
	}()

	// Wait long enough for at least one refresh tick beyond the initial prefetch.
	time.Sleep(200 * time.Millisecond)

	cancel()
	<-done

	count := fetchCount.Load()
	if count < 2 {
		t.Errorf("fetch count = %d, want >= 2 (initial + at least one refresh)", count)
	}

	blocked, err := bl.IsBlocked(context.Background(), "mailinator.com")
	if err != nil {
		t.Fatalf("IsBlocked() error = %v", err)
	}
	if !blocked {
		t.Error("IsBlocked(mailinator.com) = false after Run, want true")
	}
}

func TestRunContextCancellation(t *testing.T) {
	t.Parallel()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte("mailinator.com\n"))
	}))
	defer srv.Close()

	bl := NewBlocklist(srv.URL, true, zerolog.Nop())

	ctx, cancel := context.WithCancel(context.Background())
	cancel() // cancel immediately

	done := make(chan struct{})
	go func() {
		bl.Run(ctx, 24*time.Hour)
		close(done)
	}()

	select {
	case <-done:
		// Run exited promptly as expected.
	case <-time.After(2 * time.Second):
		t.Fatal("Run did not exit promptly after context cancellation")
	}
}

func TestRunRefreshFailureContinues(t *testing.T) {
	t.Parallel()
	var fetchCount atomic.Int32

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		n := fetchCount.Add(1)
		if n > 1 {
			// Fail all refreshes after the initial prefetch.
			w.WriteHeader(http.StatusInternalServerError)
			return
		}
		_, _ = w.Write([]byte("mailinator.com\n"))
	}))
	defer srv.Close()

	bl := NewBlocklist(srv.URL, true, zerolog.Nop())

	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() {
		bl.Run(ctx, 50*time.Millisecond)
		close(done)
	}()

	// Wait for at least one failed refresh attempt.
	time.Sleep(200 * time.Millisecond)

	cancel()
	<-done

	// The cached domains from the initial fetch should still be valid.
	blocked, err := bl.IsBlocked(context.Background(), "mailinator.com")
	if err != nil {
		t.Fatalf("IsBlocked() error = %v", err)
	}
	if !blocked {
		t.Error("IsBlocked(mailinator.com) = false after failed refresh, want true (cached list should persist)")
	}
}
