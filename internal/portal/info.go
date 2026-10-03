package portal

import (
	"net/netip"
	"strings"

	"github.com/AdguardTeam/AdGuardHome/internal/users"
)

// Info is what the portal tells a user about their own account.  It is the
// user's own [users.Info] plus the pieces that only make sense in the portal,
// and it never carries anything about other users.
type Info struct {
	// Info is the underlying user information: the quota, the usage and the
	// per-day history.
	*users.Info

	// Domain is the domain of the DoT and DoH endpoints.  The UI appends it
	// to an identifier to build the host name the client has to use.
	Domain string `json:"domain"`

	// Hosts are the host names the user connects to, one per identifier that
	// can be used as a host name label.  The other identifiers are addresses
	// and have no host name.
	Hosts []string `json:"hosts"`
}

// splitIDs separates the identifiers of a user into the client identifiers and
// the networks they are identified by.
//
// The client identifiers are matched against the client ID of a log entry and
// the networks against its source address, so a user with both kinds gets the
// entries of either.
func splitIDs(ids []string) (clientIDs []string, nets []netip.Prefix) {
	for _, id := range ids {
		id = strings.TrimSpace(id)
		if id == "" {
			continue
		}

		if p, err := netip.ParsePrefix(id); err == nil {
			nets = append(nets, p.Masked())

			continue
		}

		if a, err := netip.ParseAddr(id); err == nil {
			nets = append(nets, netip.PrefixFrom(a, a.BitLen()))

			continue
		}

		clientIDs = append(clientIDs, id)
	}

	return clientIDs, nets
}

// hostsOf returns the host names a user connects to for the given domain.
func hostsOf(ids []string, domain string) (hosts []string) {
	if domain == "" {
		return nil
	}

	clientIDs, _ := splitIDs(ids)
	for _, id := range clientIDs {
		hosts = append(hosts, id+"."+domain)
	}

	return hosts
}
