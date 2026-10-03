package users

import (
	"crypto/tls"
	"fmt"
	"mime"
	"net"
	"net/smtp"
	"strconv"
	"strings"
	"time"
)

// defaultSMTPPort is the port used when the settings do not name one.
const defaultSMTPPort = 587

// mailTimeout bounds every step of a delivery.
const mailTimeout = 20 * time.Second

// SMTPConfig is the mail configuration resolved from the settings.
type SMTPConfig struct {
	// Host is the host name of the mail server.
	Host string

	// Port is the port of the mail server.
	Port int

	// User is the user name for the mail server.
	User string

	// Password is the password for the mail server.
	Password string

	// From is the sender address.
	From string

	// Plain is true when the connection must be made without STARTTLS.
	Plain bool
}

// MailConfig returns the mail configuration held in the settings.  It returns
// nil when no mail server is configured.
func (s *Settings) MailConfig() (c *SMTPConfig) {
	if s == nil || s.SMTPHost == "" {
		return nil
	}

	port := s.SMTPPort
	if port <= 0 || port > 65535 {
		port = defaultSMTPPort
	}

	from := s.SMTPFrom
	if from == "" {
		from = s.SMTPUser
	}

	return &SMTPConfig{
		Host:     s.SMTPHost,
		Port:     port,
		User:     s.SMTPUser,
		Password: s.SMTPPassword,
		From:     from,
		Plain:    s.SMTPPlain,
	}
}

// IsConfigured reports whether the configuration can send mail at all.
func (c *SMTPConfig) IsConfigured() (ok bool) {
	return c != nil && c.Host != "" && c.From != ""
}

// headerSafe reports whether the value may be placed in a message header.  A
// value containing a line break would let the caller inject headers, so it is
// refused rather than escaped.
func headerSafe(v string) (ok bool) {
	return !strings.ContainsAny(v, "\r\n")
}

// SendMail delivers a plain-text message through the configured mail server.
//
// It is deliberately a single blocking call with a deadline on the connection:
// the caller is an HTTP handler that must not hang, and there is no queue to
// write to.
func SendMail(c *SMTPConfig, to, subject, body string) (err error) {
	if c == nil || c.Host == "" {
		return fmt.Errorf("the mail server is not configured")
	}

	if !headerSafe(to) || !headerSafe(subject) || !headerSafe(c.From) {
		return fmt.Errorf("the address or subject contains a line break")
	}

	if c.From == "" {
		return fmt.Errorf("the sender address is not configured")
	}

	msg := buildMessage(c.From, to, subject, body)
	addr := net.JoinHostPort(c.Host, strconv.Itoa(c.Port))

	conn, err := net.DialTimeout("tcp", addr, mailTimeout)
	if err != nil {
		return fmt.Errorf("connecting to the mail server: %w", err)
	}

	defer func() {
		_ = conn.Close()
	}()

	_ = conn.SetDeadline(time.Now().Add(mailTimeout))

	client, err := smtp.NewClient(conn, c.Host)
	if err != nil {
		return fmt.Errorf("starting the mail session: %w", err)
	}

	defer func() {
		_ = client.Close()
	}()

	if !c.Plain {
		tlsConf := &tls.Config{
			ServerName: c.Host,
			MinVersion: tls.VersionTLS12,
		}

		err = client.StartTLS(tlsConf)
		if err != nil {
			return fmt.Errorf("starting TLS: %w", err)
		}
	}

	if c.User != "" {
		auth := smtp.PlainAuth("", c.User, c.Password, c.Host)

		err = client.Auth(auth)
		if err != nil {
			return fmt.Errorf("authenticating: %w", err)
		}
	}

	err = client.Mail(c.From)
	if err != nil {
		return fmt.Errorf("setting the sender: %w", err)
	}

	err = client.Rcpt(to)
	if err != nil {
		return fmt.Errorf("setting the recipient: %w", err)
	}

	w, err := client.Data()
	if err != nil {
		return fmt.Errorf("starting the message: %w", err)
	}

	_, err = w.Write(msg)
	if err != nil {
		return fmt.Errorf("writing the message: %w", err)
	}

	err = w.Close()
	if err != nil {
		return fmt.Errorf("finishing the message: %w", err)
	}

	err = client.Quit()
	if err != nil {
		return fmt.Errorf("closing the mail session: %w", err)
	}

	return nil
}

// buildMessage renders a plain-text message.  The subject is MIME-encoded so
// that a non-ASCII one survives.
func buildMessage(from, to, subject, body string) (msg []byte) {
	var b strings.Builder

	b.WriteString("From: ")
	b.WriteString(from)
	b.WriteString("\r\n")

	b.WriteString("To: ")
	b.WriteString(to)
	b.WriteString("\r\n")

	b.WriteString("Subject: ")
	b.WriteString(mime.QEncoding.Encode("utf-8", subject))
	b.WriteString("\r\n")

	b.WriteString("MIME-Version: 1.0\r\n")
	b.WriteString("Content-Type: text/plain; charset=UTF-8\r\n")
	b.WriteString("Content-Transfer-Encoding: 8bit\r\n")
	b.WriteString("\r\n")

	// A bare LF is not a line ending in SMTP, so normalise before sending.
	b.WriteString(strings.ReplaceAll(strings.ReplaceAll(body, "\r\n", "\n"), "\n", "\r\n"))

	return []byte(b.String())
}
