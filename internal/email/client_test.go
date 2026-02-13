package email

import (
	"bufio"
	"context"
	"fmt"
	"net"
	"strconv"
	"strings"
	"testing"
)

func TestBuildMessage(t *testing.T) {
	t.Parallel()

	msg := buildMessage("sender@example.com", "recipient@example.com", "Test Subject", "Hello, world!")

	checks := []struct {
		label string
		want  string
	}{
		{"From header", "From: sender@example.com\r\n"},
		{"To header", "To: recipient@example.com\r\n"},
		{"Subject header", "Subject: Test Subject\r\n"},
		{"MIME version", "MIME-Version: 1.0\r\n"},
		{"Content-Type", "Content-Type: text/plain; charset=UTF-8\r\n"},
		{"body", "Hello, world!"},
	}
	for _, c := range checks {
		if !strings.Contains(msg, c.want) {
			t.Errorf("buildMessage missing %s: want substring %q in %q", c.label, c.want, msg)
		}
	}
}

func TestPingSuccess(t *testing.T) {
	t.Parallel()

	ln := listenTCP(t)
	defer func() { _ = ln.Close() }()

	done := make(chan struct{})
	go func() {
		defer close(done)
		serveSMTP(t, ln, nil)
	}()

	host, port := splitHostPort(t, ln.Addr().String())
	c := NewClient(host, port, "", "", "test@example.com")

	if err := c.Ping(); err != nil {
		t.Fatalf("Ping() error = %v", err)
	}

	_ = ln.Close()
	<-done
}

func TestPingConnectionRefused(t *testing.T) {
	t.Parallel()

	// Listen and immediately close to get an unused port.
	ln := listenTCP(t)
	_, port := splitHostPort(t, ln.Addr().String())
	_ = ln.Close()

	c := NewClient("127.0.0.1", port, "", "", "test@example.com")
	if err := c.Ping(); err == nil {
		t.Fatal("Ping() on closed port should return error")
	}
}

func TestSendSuccess(t *testing.T) {
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
	c := NewClient(host, port, "", "", "sender@example.com")

	if err := c.Send("recipient@example.com", "Hello", "Test body"); err != nil {
		t.Fatalf("Send() error = %v", err)
	}

	_ = ln.Close()
	<-done

	data := <-captured
	if !strings.Contains(data, "Subject: Hello") {
		t.Errorf("captured data missing subject: %q", data)
	}
	if !strings.Contains(data, "Test body") {
		t.Errorf("captured data missing body: %q", data)
	}
}

// serveSMTP accepts a single connection on ln and speaks a minimal SMTP protocol. If captured is non-nil, the DATA
// payload is sent to the channel.
func serveSMTP(t *testing.T, ln net.Listener, captured chan<- string) {
	t.Helper()

	conn, err := ln.Accept()
	if err != nil {
		return
	}
	defer func() { _ = conn.Close() }()

	scanner := bufio.NewScanner(conn)
	write := func(s string) { _, _ = fmt.Fprintf(conn, "%s\r\n", s) }

	write("220 localhost ESMTP test")

	for scanner.Scan() {
		line := scanner.Text()
		cmd := strings.ToUpper(line)

		switch {
		case strings.HasPrefix(cmd, "EHLO"), strings.HasPrefix(cmd, "HELO"):
			write("250-localhost")
			write("250 OK")
		case strings.HasPrefix(cmd, "MAIL FROM:"):
			write("250 OK")
		case strings.HasPrefix(cmd, "RCPT TO:"):
			write("250 OK")
		case cmd == "DATA":
			write("354 Start mail input")
			var data strings.Builder
			for scanner.Scan() {
				dl := scanner.Text()
				if dl == "." {
					break
				}
				data.WriteString(dl)
				data.WriteString("\n")
			}
			if captured != nil {
				captured <- data.String()
			}
			write("250 OK")
		case cmd == "QUIT":
			write("221 Bye")
			return
		default:
			write("250 OK")
		}
	}
}

// listenTCP opens a TCP listener on a random port on the loopback interface.
func listenTCP(t *testing.T) net.Listener {
	t.Helper()
	var lc net.ListenConfig
	ln, err := lc.Listen(context.Background(), "tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen: %v", err)
	}
	return ln
}

func splitHostPort(t *testing.T, addr string) (string, int) {
	t.Helper()
	host, portStr, err := net.SplitHostPort(addr)
	if err != nil {
		t.Fatalf("SplitHostPort(%q): %v", addr, err)
	}
	port, err := strconv.Atoi(portStr)
	if err != nil {
		t.Fatalf("invalid port %q: %v", portStr, err)
	}
	return host, port
}
