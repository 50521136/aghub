package querylog

import (
	"context"
	"fmt"
	"net/netip"
	"net/url"
	"time"
)

// SearchRequest is a search over the query log that is not tied to the query
// parameters of the administrator HTTP API.
type SearchRequest struct {
	// ClientIDs restricts the search to the entries whose client identifier is
	// one of these.  The values are combined with OR, unlike the search
	// criteria, which are combined with AND.
	ClientIDs []string

	// ClientNets restricts the search to the entries whose source address
	// belongs to one of these networks.  It is combined with ClientIDs using
	// OR, so that a user identified by an address is covered as well.
	ClientNets []netip.Prefix

	// Term, if not empty, is matched against the host name, the client and
	// the address the same way the administrator API does it.
	Term string

	// OlderThan, if not zero, returns only the entries older than it.
	OlderThan time.Time

	// Limit is the maximum number of entries to return.  It must be positive.
	Limit int
}

// SearchResponse is the result of a [SearchRequest].
type SearchResponse struct {
	// Entries is the API representation of the matching entries, newest
	// first, in the same shape as the administrator query log API.  It has
	// the "data" and "oldest" properties.
	Entries map[string]any
}

// Search returns the log entries matching req.  It is meant for callers that
// are not the administrator API, such as the user portal.
func (l *queryLog) Search(
	ctx context.Context,
	req *SearchRequest,
) (resp *SearchResponse, err error) {
	if req == nil {
		return nil, fmt.Errorf("querylog: no search request")
	}

	if req.Limit <= 0 {
		return nil, fmt.Errorf("querylog: limit must be positive")
	}

	if len(req.ClientIDs) == 0 && len(req.ClientNets) == 0 {
		// Without a client filter the search would return the entries of
		// every user, which is never what a caller wants.
		return nil, fmt.Errorf("querylog: no client filter")
	}

	p := newSearchParams()
	p.limit = req.Limit
	p.olderThan = req.OlderThan
	p.clientIDs = req.ClientIDs
	p.clientNets = req.ClientNets

	if req.Term != "" {
		q := url.Values{"search": []string{req.Term}}

		err = l.parseSearchCriterions(ctx, q, p)
		if err != nil {
			return nil, fmt.Errorf("querylog: parsing the search term: %w", err)
		}
	}

	var entries []*logEntry
	var oldest time.Time
	func() {
		l.confMu.RLock()
		defer l.confMu.RUnlock()

		entries, oldest = l.search(ctx, p)
	}()

	return &SearchResponse{
		Entries: l.entriesToJSON(ctx, entries, oldest, l.anonymizer.Load()),
	}, nil
}
