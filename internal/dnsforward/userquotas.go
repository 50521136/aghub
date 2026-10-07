package dnsforward

import (
	"net/netip"
	"time"
)

// UserQuotas checks the per-user query quotas.  It is implemented by the
// user management module.
type UserQuotas interface {
	// AllowQuery returns true if the query from the client with the given
	// ClientID and IP address is allowed.  Allowed queries are counted by the
	// implementation.  reason is the reason for the refusal, and it is empty
	// when the query is allowed.
	AllowQuery(clientID string, ip netip.Addr, qname string) (ok bool, reason string)

	// RecordResult records the filtering outcome of a query for the user
	// that owns clientID.  blocked is true when a filtering rule rejected
	// the query, and passed is true when a rule matched but the query was
	// allowed by the allow-list.  A client that belongs to no user is
	// ignored by the implementation.
	RecordResult(clientID string, blocked, passed bool)

	// ObserveLatency records how long one query took for the user that owns
	// the client.  It is called after the response is known, so that the
	// portal can show the average resolution time of an account next to its
	// request counts.  A client that belongs to no user is ignored by the
	// implementation.
	ObserveLatency(clientID string, ip netip.Addr, d time.Duration)
}

// EmptyUserQuotas is a [UserQuotas] implementation that allows every query.
type EmptyUserQuotas struct{}

// type check
var _ UserQuotas = EmptyUserQuotas{}

// AllowQuery implements the [UserQuotas] interface for EmptyUserQuotas.
func (EmptyUserQuotas) AllowQuery(_ string, _ netip.Addr, _ string) (ok bool, _ string) {
	return true, ""
}

// RecordResult implements the [UserQuotas] interface for EmptyUserQuotas.
func (EmptyUserQuotas) RecordResult(_ string, _, _ bool) {}

// ObserveLatency implements the [UserQuotas] interface for EmptyUserQuotas.
func (EmptyUserQuotas) ObserveLatency(_ string, _ netip.Addr, _ time.Duration) {}
