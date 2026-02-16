package email

import (
	"context"
	"strings"
	"testing"
)

func TestSendVerificationComposition(t *testing.T) {
	t.Parallel()

	ln := listenTCP(t)
	defer func() { _ = ln.Close() }()

	captured := make(chan string, 1)
	done := make(chan struct{})
	go func() {
		defer close(done)
		serveSMTP(t, ln, captured)
	}()

	host, port := splitHostPort(t, ln.Addr().String())
	c := NewClient(host, port, "", "", "noreply@example.com", nil)

	if err := c.SendVerification(context.Background(), "alice@example.com", "abc123", "https://chat.example.com", "Test Server"); err != nil {
		t.Fatalf("SendVerification() error = %v", err)
	}

	_ = ln.Close()
	<-done

	data := <-captured

	checks := []struct {
		label string
		want  string
	}{
		{"subject", "Verify your email for Test Server"},
		{"html content type", "Content-Type: text/html; charset=UTF-8"},
		{"verification link", "https://chat.example.com/verify-email?token=abc123"},
		{"welcome text", "Welcome to Test Server"},
		{"expiry note", "24 hours"},
	}
	for _, c := range checks {
		if !strings.Contains(data, c.want) {
			t.Errorf("verification email missing %s: want substring %q in %q", c.label, c.want, data)
		}
	}
}

func TestRenderVerification(t *testing.T) {
	t.Parallel()

	body, err := renderVerification(nil, "My Server", "https://example.com", "tok123")
	if err != nil {
		t.Fatalf("renderVerification() error = %v", err)
	}

	checks := []struct {
		label string
		want  string
	}{
		{"welcome text", "Welcome to My Server"},
		{"verification link", "https://example.com/verify-email?token=tok123"},
		{"expiry note", "24 hours"},
		{"html doctype", "<!DOCTYPE html>"},
	}
	for _, c := range checks {
		if !strings.Contains(body, c.want) {
			t.Errorf("renderVerification missing %s: want substring %q", c.label, c.want)
		}
	}
}
