package querylog

import (
	"context"
	"log/slog"
	"net"
	"net/netip"
	"strings"
	"time"
)

// searchParams represent the search query sent by the client.
type searchParams struct {
	// olderThen represents a parameter for entries that are older than this
	// parameter value.  If not set, disregard it and return any value.
	olderThan time.Time

	// searchCriteria is a list of search criteria that we use to get filter
	// results.
	searchCriteria []searchCriterion

	// clientIDs, when not nil, restricts the search to the entries belonging
	// to a client whose identifier is one of these.  Unlike
	// [searchParams.searchCriteria], which is combined with AND, the values
	// here are combined with OR.
	//
	// It is used by the user portal, which searches on behalf of a user that
	// may have several identifiers.
	clientIDs []string

	// clientNets, when not nil, restricts the search to the entries whose
	// source address belongs to one of these networks.  It complements
	// [searchParams.clientIDs] for the users identified by an address.
	clientNets []netip.Prefix

	// offset for the search.
	offset int

	// limit the number of records returned.
	limit int

	// maxFileScanEntries is a maximum of log entries to scan in query log
	// files.  If not set, then no limit.
	maxFileScanEntries int
}

// matchesClient returns true if an entry with the given client ID and source
// address belongs to the client the search is restricted to.  It returns true
// when the search is not restricted.
func (s *searchParams) matchesClient(clientID string, ip net.IP) (ok bool) {
	if s.clientIDs == nil && s.clientNets == nil {
		return true
	}

	for _, id := range s.clientIDs {
		if id != "" && strings.EqualFold(id, clientID) {
			return true
		}
	}

	if addr, aok := netip.AddrFromSlice(ip); aok {
		addr = addr.Unmap()
		for _, n := range s.clientNets {
			if n.Contains(addr) {
				return true
			}
		}
	}

	return false
}

// newSearchParams - creates an empty instance of searchParams
func newSearchParams() *searchParams {
	return &searchParams{
		// default max log entries to return
		limit: 500,

		// by default, we scan up to 50k entries at once
		maxFileScanEntries: 50000,
	}
}

// quickMatchClientFunc is a simplified client finder for quick matches.
type quickMatchClientFunc = func(
	ctx context.Context,
	logger *slog.Logger,
	clientID, ip string,
) (c *Client)

// quickMatch quickly checks if the line matches the given search parameters.
// It returns false if the line doesn't match.  This method is only here for
// optimization purposes.
func (s *searchParams) quickMatch(
	ctx context.Context,
	logger *slog.Logger,
	line string,
	findClient quickMatchClientFunc,
) (ok bool) {
	if s.clientIDs != nil || s.clientNets != nil {
		clientID := readJSONValue(line, `"CID":"`)

		var ip net.IP
		if raw := readJSONValue(line, `"IP":"`); raw != "" {
			ip = net.ParseIP(raw)
		}

		if !s.matchesClient(clientID, ip) {
			return false
		}
	}

	for _, c := range s.searchCriteria {
		if !c.quickMatch(ctx, logger, line, findClient) {
			return false
		}
	}

	return true
}

// match - checks if the logEntry matches the searchParams
func (s *searchParams) match(entry *logEntry) bool {
	if !s.olderThan.IsZero() && !entry.Time.Before(s.olderThan) {
		// Ignore entries newer than what was requested
		return false
	}

	if !s.matchesClient(entry.ClientID, entry.IP) {
		return false
	}

	for _, c := range s.searchCriteria {
		if !c.match(entry) {
			return false
		}
	}

	return true
}
