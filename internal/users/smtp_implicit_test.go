package users

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

// TestUsesImplicitTLS checks the port classification that decides whether the
// TLS handshake comes before the SMTP greeting.
//
// Getting this wrong is not a subtle failure: on 465 the server waits for the
// handshake and the client waits for a greeting, and the only symptom is a
// timeout with nothing in it that names the cause.
func TestUsesImplicitTLS(t *testing.T) {
	for port, want := range map[int]bool{
		465: true,
		994: true,
		587: false,
		25:  false,
		0:   false,
	} {
		assert.Equal(t, want, usesImplicitTLS(port), "port %d", port)
	}
}

// TestMailConfigKeepsPort checks that the configured port survives, since the
// implicit-TLS decision is made from it.
func TestMailConfigKeepsPort(t *testing.T) {
	s := &Settings{
		SMTPHost: "smtp.example.com",
		SMTPPort: 465,
		SMTPUser: "u@example.com",
		SMTPFrom: "u@example.com",
	}

	c := s.MailConfig()
	assert.Equal(t, 465, c.Port)
	assert.True(t, usesImplicitTLS(c.Port))
	assert.True(t, c.IsConfigured())
}
