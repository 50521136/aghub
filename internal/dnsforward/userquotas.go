package dnsforward

import (
	"net/netip"
)

// UserQuotas checks the per-user query quotas.  It is implemented by the
// user management module.
type UserQuotas interface {
	// AllowQuery returns true if the query from the client with the given
	// ClientID and IP address is allowed.  Allowed queries are counted by the
	// implementation.  reason is the reason for the refusal, and it is empty
	// when the query is allowed.
	AllowQuery(clientID string, ip netip.Addr) (ok bool, reason string)
}

// EmptyUserQuotas is a [UserQuotas] implementation that allows every query.
type EmptyUserQuotas struct{}

// type check
var _ UserQuotas = EmptyUserQuotas{}

// AllowQuery implements the [UserQuotas] interface for EmptyUserQuotas.
func (EmptyUserQuotas) AllowQuery(_ string, _ netip.Addr) (ok bool, _ string) {
	return true, ""
}
