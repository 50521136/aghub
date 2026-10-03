package users

import (
	"bufio"
	"encoding/base64"
	"fmt"
	"net"
	"strings"
	"sync"
	"testing"
	"time"
)

// fakeSMTP is a minimal SMTP server.  It exists to prove that [SendMail] speaks
// the protocol in the right order and writes a well-formed message, which a
// unit test of the message builder alone cannot show.
type fakeSMTP struct {
	ln net.Listener

	mu sync.Mutex

	// msgs holds the bodies of the accepted messages.
	msgs []string

	// from holds the envelope senders.
	from []string

	// rcpt holds the envelope recipients.
	rcpt []string

	// authSeen is true when a client authenticated.
	authSeen bool

	// tlsSeen is true when a client started TLS.
	tlsSeen bool

	// wantUser is the user name accepted by AUTH, when set.
	wantUser string

	// wantPass is the password accepted by AUTH, when set.
	wantPass string
}

// startFakeSMTP starts a server on a loopback port.
func startFakeSMTP(tb testing.TB) (f *fakeSMTP) {
	tb.Helper()

	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		tb.Fatalf("listening: %v", err)
	}

	f = &fakeSMTP{ln: ln}

	go f.serve()

	tb.Cleanup(func() {
		_ = ln.Close()
	})

	return f
}

// host and port return the address of the server.
func (f *fakeSMTP) host() (host string) {
	host, _, _ = net.SplitHostPort(f.ln.Addr().String())

	return host
}

func (f *fakeSMTP) port() (port int) {
	_, portStr, _ := net.SplitHostPort(f.ln.Addr().String())
	_, _ = fmt.Sscanf(portStr, "%d", &port)

	return port
}

// messages returns the accepted message bodies.
func (f *fakeSMTP) messages() (msgs []string) {
	f.mu.Lock()
	defer f.mu.Unlock()

	return append([]string(nil), f.msgs...)
}

// lastEnvelope returns the sender and recipients of the last message.
func (f *fakeSMTP) lastEnvelope() (from string, rcpt []string) {
	f.mu.Lock()
	defer f.mu.Unlock()

	if len(f.from) > 0 {
		from = f.from[len(f.from)-1]
	}
	if len(f.rcpt) > 0 {
		rcpt = []string{f.rcpt[len(f.rcpt)-1]}
	}

	return from, rcpt
}

func (f *fakeSMTP) serve() {
	for {
		conn, err := f.ln.Accept()
		if err != nil {
			return
		}

		go f.handle(conn)
	}
}

// handle runs one session.  It is deliberately forgiving about the exact
// wording and strict about the order of the commands.
func (f *fakeSMTP) handle(conn net.Conn) {
	defer func() {
		_ = conn.Close()
	}()

	_ = conn.SetDeadline(time.Now().Add(10 * time.Second))

	r := bufio.NewReader(conn)
	w := bufio.NewWriter(conn)

	say := func(format string, args ...any) {
		_, _ = fmt.Fprintf(w, format+"\r\n", args...)
		_ = w.Flush()
	}

	say("220 fake ESMTP ready")

	var (
		from string
		rcpt string
	)

	for {
		line, err := r.ReadString('\n')
		if err != nil {
			return
		}

		line = strings.TrimRight(line, "\r\n")
		upper := strings.ToUpper(line)

		switch {
		case strings.HasPrefix(upper, "EHLO"), strings.HasPrefix(upper, "HELO"):
			say("250-fake")
			say("250 AUTH PLAIN LOGIN")
		case strings.HasPrefix(upper, "AUTH PLAIN"):
			// The client may send the credentials on the same line or expect
			// the 334 challenge first.
			payload := strings.TrimSpace(line[len("AUTH PLAIN"):])
			if payload == "" {
				say("334 ")
				payload, err = r.ReadString('\n')
				if err != nil {
					return
				}

				payload = strings.TrimSpace(payload)
			}

			if !f.checkAuth(payload) {
				say("535 authentication failed")

				continue
			}

			f.mu.Lock()
			f.authSeen = true
			f.mu.Unlock()

			say("235 authentication successful")
		case strings.HasPrefix(upper, "MAIL FROM:"):
			from = extractAddress(line)

			f.mu.Lock()
			f.from = append(f.from, from)
			f.mu.Unlock()

			say("250 sender ok")
		case strings.HasPrefix(upper, "RCPT TO:"):
			rcpt = extractAddress(line)

			f.mu.Lock()
			f.rcpt = append(f.rcpt, rcpt)
			f.mu.Unlock()

			say("250 recipient ok")
		case upper == "DATA":
			say("354 end with a line containing only a period")

			var body strings.Builder

			for {
				dl, derr := r.ReadString('\n')
				if derr != nil {
					return
				}

				if strings.TrimRight(dl, "\r\n") == "." {
					break
				}

				body.WriteString(dl)
			}

			f.mu.Lock()
			f.msgs = append(f.msgs, body.String())
			f.mu.Unlock()

			say("250 message accepted")
		case upper == "QUIT":
			say("221 bye")

			return
		case upper == "RSET":
			say("250 ok")
		case strings.HasPrefix(upper, "STARTTLS"):
			say("454 TLS not available in the test server")
		default:
			say("502 command not implemented")
		}
	}
}

// checkAuth validates a base64 PLAIN payload of the form \0user\0pass.
func (f *fakeSMTP) checkAuth(payload string) (ok bool) {
	raw, err := base64.StdEncoding.DecodeString(payload)
	if err != nil {
		return false
	}

	parts := strings.Split(string(raw), "\x00")
	if len(parts) != 3 {
		return false
	}

	if f.wantUser == "" {
		return true
	}

	return parts[1] == f.wantUser && parts[2] == f.wantPass
}

// extractAddress returns what is inside the angle brackets.
func extractAddress(line string) (addr string) {
	start := strings.Index(line, "<")
	end := strings.LastIndex(line, ">")
	if start < 0 || end <= start {
		return strings.TrimSpace(line[strings.Index(line, ":")+1:])
	}

	return line[start+1 : end]
}

func TestSendMail(t *testing.T) {
	f := startFakeSMTP(t)

	conf := &SMTPConfig{
		Host: f.host(),
		Port: f.port(),
		User: "mailer",
		// The test server refuses STARTTLS, and the point here is the message
		// and the command order, not the transport.
		Plain: true,
		From:  "noreply@example.com",
	}

	f.wantUser = "mailer"
	f.wantPass = ""

	err := SendMail(conf, "alice@example.com", "验证码", "你的验证码是：123456")
	if err != nil {
		t.Fatalf("sending: %v", err)
	}

	msgs := f.messages()
	if len(msgs) != 1 {
		t.Fatalf("expected one message, got %d", len(msgs))
	}

	msg := msgs[0]

	if !strings.Contains(msg, "To: alice@example.com") {
		t.Errorf("expected the recipient header, got:\n%s", msg)
	}
	if !strings.Contains(msg, "From: noreply@example.com") {
		t.Errorf("expected the sender header, got:\n%s", msg)
	}
	if !strings.Contains(msg, "Content-Type: text/plain; charset=UTF-8") {
		t.Errorf("expected the plain-text content type, got:\n%s", msg)
	}
	if !strings.Contains(msg, "123456") {
		t.Errorf("expected the body, got:\n%s", msg)
	}

	// A non-ASCII subject has to be encoded, or it arrives as mojibake.
	if strings.Contains(msg, "Subject: 验证码") {
		t.Errorf("expected the subject to be MIME-encoded, got:\n%s", msg)
	}
	if !strings.Contains(msg, "Subject: =?utf-8?") {
		t.Errorf("expected a MIME-encoded subject, got:\n%s", msg)
	}

	from, rcpt := f.lastEnvelope()
	if from != "noreply@example.com" {
		t.Errorf("expected the envelope sender, got %q", from)
	}
	if len(rcpt) != 1 || rcpt[0] != "alice@example.com" {
		t.Errorf("expected the envelope recipient, got %q", rcpt)
	}

	// The body must use CRLF, or a strict server rejects the message.
	if strings.Contains(strings.ReplaceAll(msg, "\r\n", ""), "\n") {
		t.Error("expected CRLF line endings")
	}
}

func TestSendMailWithoutConfig(t *testing.T) {
	err := SendMail(nil, "alice@example.com", "s", "b")
	if err == nil {
		t.Error("expected an error without a configuration")
	}

	err = SendMail(&SMTPConfig{}, "alice@example.com", "s", "b")
	if err == nil {
		t.Error("expected an error without a host")
	}
}

func TestSendMailRefusesHeaderInjection(t *testing.T) {
	f := startFakeSMTP(t)

	conf := &SMTPConfig{
		Host:  f.host(),
		Port:  f.port(),
		From:  "noreply@example.com",
		Plain: true,
	}

	// A line break in the subject would let the caller add headers, for
	// example a Bcc that turns the portal into a spam relay.
	err := SendMail(conf, "alice@example.com", "hi\r\nBcc: victim@example.com", "body")
	if err == nil {
		t.Error("expected a subject with a line break to be refused")
	}

	err = SendMail(conf, "alice@example.com\r\nBcc: victim@example.com", "hi", "body")
	if err == nil {
		t.Error("expected a recipient with a line break to be refused")
	}

	if len(f.messages()) != 0 {
		t.Error("expected nothing to be sent")
	}
}

func TestMailConfig(t *testing.T) {
	// No host means no mail.
	if c := (&Settings{}).MailConfig(); c != nil {
		t.Errorf("expected no configuration, got %+v", c)
	}

	if c := (*Settings)(nil).MailConfig(); c != nil {
		t.Errorf("expected no configuration, got %+v", c)
	}

	c := (&Settings{SMTPHost: "smtp.example.com"}).MailConfig()
	if c == nil {
		t.Fatal("expected a configuration")
	}
	if c.Port != defaultSMTPPort {
		t.Errorf("expected the default port %d, got %d", defaultSMTPPort, c.Port)
	}

	// Without a sender of its own the user name stands in for one, which is
	// what most providers require.
	c = (&Settings{
		SMTPHost: "smtp.example.com",
		SMTPPort: 465,
		SMTPUser: "mailer@example.com",
	}).MailConfig()
	if c.From != "mailer@example.com" {
		t.Errorf("expected the user name as the sender, got %q", c.From)
	}
	if c.Port != 465 {
		t.Errorf("expected the configured port, got %d", c.Port)
	}

	// An impossible port falls back rather than making every send fail.
	c = (&Settings{SMTPHost: "smtp.example.com", SMTPPort: 99999}).MailConfig()
	if c.Port != defaultSMTPPort {
		t.Errorf("expected the default port, got %d", c.Port)
	}

	// A host without a sender cannot send anything, so it does not count as
	// configured and the portal refuses to hand out codes.
	if c.IsConfigured() {
		t.Error("expected a host without a sender not to be configured")
	}

	c = (&Settings{SMTPHost: "smtp.example.com", SMTPUser: "mailer"}).MailConfig()
	if !c.IsConfigured() {
		t.Error("expected a host and a user name to be configured")
	}
}

func TestSMTPConfigIsConfigured(t *testing.T) {
	if (*SMTPConfig)(nil).IsConfigured() {
		t.Error("expected nil not to be configured")
	}
	if (&SMTPConfig{Host: "smtp.example.com"}).IsConfigured() {
		t.Error("expected a host without a sender not to be configured")
	}
	if !(&SMTPConfig{Host: "smtp.example.com", From: "a@b.c"}).IsConfigured() {
		t.Error("expected a host and a sender to be configured")
	}
}

func TestStartTLSIsUsedByDefault(t *testing.T) {
	// The fake server answers STARTTLS with 454, so a default configuration
	// must fail rather than silently send in the clear.
	f := startFakeSMTP(t)

	conf := &SMTPConfig{
		Host: f.host(),
		Port: f.port(),
		From: "noreply@example.com",
	}

	err := SendMail(conf, "alice@example.com", "hi", "body")
	if err == nil {
		t.Fatal("expected the send to fail when STARTTLS is refused")
	}
	if !strings.Contains(err.Error(), "TLS") {
		t.Errorf("expected a TLS error, got %v", err)
	}
	if len(f.messages()) != 0 {
		t.Error("expected nothing to be sent without TLS")
	}
}
