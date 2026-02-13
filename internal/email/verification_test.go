package email

import (
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
	c := NewClient(host, port, "", "", "noreply@example.com")

	if err := c.SendVerification("alice@example.com", "abc123", "https://chat.example.com", "Test Server"); err != nil {
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

func TestVerificationBody(t *testing.T) {
	t.Parallel()

	body := verificationBody("My Server", "https://example.com", "tok123")

	if !strings.Contains(body, "Welcome to My Server") {
		t.Error("verificationBody missing welcome text")
	}
	if !strings.Contains(body, "https://example.com/verify-email?token=tok123") {
		t.Error("verificationBody missing verification link")
	}
	if !strings.Contains(body, "24 hours") {
		t.Error("verificationBody missing expiry note")
	}
}
