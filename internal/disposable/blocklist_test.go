package disposable

import (
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
)

func TestBlocklistDisabled(t *testing.T) {
	bl := NewBlocklist("http://unused", false)

	blocked, err := bl.IsBlocked("mailinator.com")
	if err != nil {
		t.Fatalf("IsBlocked() error = %v", err)
	}
	if blocked {
		t.Error("IsBlocked() = true, want false when disabled")
	}
}

func TestBlocklistBlockedDomain(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte("mailinator.com\nguerrillamail.com\n"))
	}))
	defer srv.Close()

	bl := NewBlocklist(srv.URL, true)

	blocked, err := bl.IsBlocked("mailinator.com")
	if err != nil {
		t.Fatalf("IsBlocked() error = %v", err)
	}
	if !blocked {
		t.Error("IsBlocked(mailinator.com) = false, want true")
	}
}

func TestBlocklistAllowedDomain(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte("mailinator.com\nguerrillamail.com\n"))
	}))
	defer srv.Close()

	bl := NewBlocklist(srv.URL, true)

	blocked, err := bl.IsBlocked("gmail.com")
	if err != nil {
		t.Fatalf("IsBlocked() error = %v", err)
	}
	if blocked {
		t.Error("IsBlocked(gmail.com) = true, want false")
	}
}

func TestBlocklistFetchError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
	}))
	defer srv.Close()

	bl := NewBlocklist(srv.URL, true)

	_, err := bl.IsBlocked("test.com")
	if err == nil {
		t.Fatal("IsBlocked() should return error on fetch failure")
	}
}

func TestBlocklistLazyCaching(t *testing.T) {
	var fetchCount atomic.Int32

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		fetchCount.Add(1)
		w.Write([]byte("mailinator.com\n"))
	}))
	defer srv.Close()

	bl := NewBlocklist(srv.URL, true)

	// Call multiple times — should only fetch once
	for i := 0; i < 5; i++ {
		_, err := bl.IsBlocked("mailinator.com")
		if err != nil {
			t.Fatalf("IsBlocked() call %d error = %v", i, err)
		}
	}

	if fetchCount.Load() != 1 {
		t.Errorf("fetch count = %d, want 1 (lazy caching)", fetchCount.Load())
	}
}

func TestBlocklistCaseInsensitive(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte("Mailinator.COM\n"))
	}))
	defer srv.Close()

	bl := NewBlocklist(srv.URL, true)

	blocked, err := bl.IsBlocked("mailinator.com")
	if err != nil {
		t.Fatalf("IsBlocked() error = %v", err)
	}
	if !blocked {
		t.Error("IsBlocked() = false, want true (case insensitive)")
	}
}

func TestBlocklistCommentsAndBlanks(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte("# comment\n\nmailinator.com\n\n# another comment\n"))
	}))
	defer srv.Close()

	bl := NewBlocklist(srv.URL, true)

	blocked, err := bl.IsBlocked("mailinator.com")
	if err != nil {
		t.Fatalf("IsBlocked() error = %v", err)
	}
	if !blocked {
		t.Error("IsBlocked() = false, want true")
	}
}
