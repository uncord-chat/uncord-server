package email

import (
	"context"
	"crypto/tls"
	"fmt"
	"net"
	"net/mail"
	"net/smtp"
	"strings"
	"time"
)

// Client sends emails over SMTP. Each call to Send or Ping creates and closes its own connection, so the Client is safe
// for concurrent use without additional locking.
type Client struct {
	host     string
	port     int
	username string
	password string
	from     mail.Address
}

// NewClient creates a new SMTP client. The from address is parsed as an RFC 5322 address; callers should validate it
// before calling NewClient (config validation handles this at startup).
func NewClient(host string, port int, username, password, from string) *Client {
	addr, _ := mail.ParseAddress(from)
	if addr == nil {
		addr = &mail.Address{Address: from}
	}
	return &Client{
		host:     host,
		port:     port,
		username: username,
		password: password,
		from:     *addr,
	}
}

// Ping verifies that the SMTP server is reachable and accepts authentication (if credentials are configured). It is
// intended for startup health checks and logs a warning on failure rather than preventing startup.
func (c *Client) Ping() error {
	client, err := c.dial()
	if err != nil {
		return err
	}
	defer func() { _ = client.Quit() }()

	if c.username != "" {
		auth := smtp.PlainAuth("", c.username, c.password, c.host)
		if err := client.Auth(auth); err != nil {
			return fmt.Errorf("AUTH: %w", err)
		}
	}

	return nil
}

// Send delivers an email with the given subject and plain text body to the specified recipient.
func (c *Client) Send(to, subject, body string) error {
	client, err := c.dial()
	if err != nil {
		return err
	}
	defer func() { _ = client.Quit() }()

	if c.username != "" {
		auth := smtp.PlainAuth("", c.username, c.password, c.host)
		if err := client.Auth(auth); err != nil {
			return fmt.Errorf("AUTH: %w", err)
		}
	}

	if err := client.Mail(c.from.Address); err != nil {
		return fmt.Errorf("MAIL FROM: %w", err)
	}
	if err := client.Rcpt(to); err != nil {
		return fmt.Errorf("RCPT TO: %w", err)
	}

	w, err := client.Data()
	if err != nil {
		return fmt.Errorf("DATA: %w", err)
	}

	msg := buildMessage(c.from.String(), to, subject, body)
	if _, err := w.Write([]byte(msg)); err != nil {
		return fmt.Errorf("write message: %w", err)
	}
	if err := w.Close(); err != nil {
		return fmt.Errorf("close data: %w", err)
	}

	return nil
}

// SendVerification composes and sends an email verification message containing a link the recipient must visit to
// confirm their address.
func (c *Client) SendVerification(to, token, serverURL, serverName string) error {
	subject := fmt.Sprintf("Verify your email for %s", serverName)
	body := verificationBody(serverName, serverURL, token)
	return c.Send(to, subject, body)
}

// dial opens a TCP connection to the SMTP server, performs the EHLO handshake, and upgrades to TLS if the server
// advertises STARTTLS support.
func (c *Client) dial() (*smtp.Client, error) {
	dialer := net.Dialer{Timeout: 10 * time.Second}
	conn, err := dialer.DialContext(context.Background(), "tcp", c.addr())
	if err != nil {
		return nil, fmt.Errorf("dial %s: %w", c.addr(), err)
	}

	client, err := smtp.NewClient(conn, c.host)
	if err != nil {
		_ = conn.Close()
		return nil, fmt.Errorf("smtp handshake: %w", err)
	}

	if err := client.Hello("localhost"); err != nil {
		_ = client.Close()
		return nil, fmt.Errorf("EHLO: %w", err)
	}

	if ok, _ := client.Extension("STARTTLS"); ok {
		if err := client.StartTLS(&tls.Config{ServerName: c.host}); err != nil {
			_ = client.Close()
			return nil, fmt.Errorf("STARTTLS: %w", err)
		}
	}

	return client, nil
}

func (c *Client) addr() string {
	return fmt.Sprintf("%s:%d", c.host, c.port)
}

func buildMessage(from, to, subject, body string) string {
	var b strings.Builder
	fmt.Fprintf(&b, "From: %s\r\n", from)
	fmt.Fprintf(&b, "To: %s\r\n", to)
	fmt.Fprintf(&b, "Subject: %s\r\n", subject)
	b.WriteString("MIME-Version: 1.0\r\n")
	b.WriteString("Content-Type: text/plain; charset=UTF-8\r\n")
	b.WriteString("\r\n")
	b.WriteString(body)
	return b.String()
}
