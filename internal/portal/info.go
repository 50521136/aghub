package portal

import (
	"context"
	"net/http"
	"net/netip"
	"reflect"
	"strings"
	"time"

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

	// Client is what the panel shows about the connection of the visitor.
	Client *ClientInfo `json:"client,omitempty"`
}

// ClientInfo is what the panel shows about the connection the visitor is using
// right now, as opposed to the account as a whole.
type ClientInfo struct {
	// IP is the address the request came from.
	IP string `json:"ip"`

	// Device is the kind of device guessed from the user agent.  It is empty
	// when the user agent says nothing useful.
	Device string `json:"device"`

	// Connected is true when the account has been used recently enough that a
	// device is probably still pointed at us.
	Connected bool `json:"connected"`

	// Allowed and Blocked count the entries of this account in the retained
	// query log, and Scanned is how many entries they were counted over.  The
	// log is a ring buffer, so these describe a window of recent entries and
	// not a period; the panel labels them as such.
	Allowed int64 `json:"allowed"`
	Blocked int64 `json:"blocked"`
	Scanned int64 `json:"scanned"`
}

// statsLogLimit is the number of log entries the blocked and allowed counts are
// taken over.
const statsLogLimit = 500

// connectedWindow is how long after the last query an account is still shown as
// connected.  It is generous, because a phone that is idle for a few minutes
// has not stopped using the resolver.
const connectedWindow = 15 * time.Minute

// The portal front-end is a web site that calls AGHub from its own host, so the
// address of the request belongs to the web server, not to the visitor, and the
// connection card would describe the wrong machine.  A portal forwards what it
// saw instead, in these headers.
//
// The forwarded values only ever describe the visitor in that card, and they are
// honoured on a token-authenticated request alone.  A wrong or forged header can
// therefore mislabel a card and nothing else: it cannot change what AGHub
// allows, counts, or reports about the account.
const (
	portalClientIPHeader = "X-Portal-Client-Ip"
	portalClientUAHeader = "X-Portal-Client-Ua"
)

// InfoForRequest is [Manager.Info] with the parts that only make sense for a
// signed-in visitor: where the request came from and how their traffic looks.
//
// Every response that hands a user back to the panel must go through this one
// rather than through [Manager.Info].  The panel renders the signed-in user
// from the sign-in and sign-up responses, so building those with Info alone
// left the connection card and the counters empty until the visitor happened
// to reload the page.
//
// The connection card describes the visitor, so a portal deployment that calls
// AGHub from its own host has to forward the visitor's address and user agent.

// forwardedClientIP returns the visitor address a portal deployment forwarded,
// if it forwarded a usable one.
func forwardedClientIP(r *http.Request) (ip netip.Addr, ok bool) {
	v := strings.TrimSpace(r.Header.Get(portalClientIPHeader))
	if v == "" {
		return netip.Addr{}, false
	}

	// The portal may itself sit behind a reverse proxy and pass a list on.
	if i := strings.IndexByte(v, ','); i >= 0 {
		v = strings.TrimSpace(v[:i])
	}

	addr, err := netip.ParseAddr(v)
	if err != nil {
		return netip.Addr{}, false
	}

	return addr.Unmap(), true
}

// visitorOf returns the address and the user agent to describe in the
// connection card: the forwarded ones when a portal sent them, the ones of the
// request itself otherwise.
func visitorOf(r *http.Request) (ip netip.Addr, ua string) {
	ip, ua = clientIP(r), r.UserAgent()

	if fwd, ok := forwardedClientIP(r); ok {
		ip = fwd
	}

	if fwd := strings.TrimSpace(r.Header.Get(portalClientUAHeader)); fwd != "" {
		ua = fwd
	}

	return ip, ua
}

// requestVisitor returns the address and the user agent to describe for this
// request: the ones a portal forwarded when the request proves it is the
// portal, the ones of the request itself otherwise.
func (m *Manager) requestVisitor(r *http.Request) (ip netip.Addr, ua string) {
	if m.users.CheckPortalToken(portalTokenOf(r)) {
		return visitorOf(r)
	}

	return clientIP(r), r.UserAgent()
}

// InfoForRequest returns the portal information for the user the request is
// authenticated as, with the connection card filled in for whoever made the
// request.
func (m *Manager) InfoForRequest(
	ctx context.Context,
	r *http.Request,
	u *users.User,
) (info *Info) {
	info = m.Info(u)
	if info == nil {
		return nil
	}

	ip, ua := m.requestVisitor(r)

	info.Client = m.clientInfoOf(ctx, u, info.Info, ip, ua)

	return info
}

// clientInfoOf fills in the connection part of the panel state.
func (m *Manager) clientInfoOf(
	ctx context.Context,
	u *users.User,
	info *users.Info,
	ip netip.Addr,
	userAgent string,
) (c *ClientInfo) {
	c = &ClientInfo{
		IP:     ip.String(),
		Device: DeviceFromUserAgent(userAgent),
	}

	if info != nil && info.LastSeen > 0 {
		seen := time.Unix(info.LastSeen, 0)
		c.Connected = time.Since(seen) < connectedWindow
	}

	resp, err := m.SearchLog(ctx, u, &LogRequest{Limit: statsLogLimit})
	if err != nil {
		// The counts are decoration; the panel must still render without them.
		m.logger.WarnContext(ctx, "counting the log for the panel", "err", err)

		return c
	}

	for _, raw := range entriesOf(resp) {
		entry, ok := raw.(map[string]any)
		if !ok {
			continue
		}

		c.Scanned++

		if reason, _ := entry["reason"].(string); isBlockedReason(reason) {
			c.Blocked++
		} else {
			c.Allowed++
		}
	}

	return c
}

// entriesOf pulls the entry list out of the search response, which is the raw
// JSON shape of the log API: an object with a "data" array.
//
// The array is walked with reflection on purpose.  The field is an
// [any] because it is passed through to the browser verbatim, and the concrete
// type depends on how the log source built it: a slice of maps, a slice of
// structs, or a slice of [any] all decode to valid JSON and all reach the
// panel.  Asserting on []any silently counts nothing for the real log source,
// so the type is never assumed here.
func entriesOf(resp *LogResponse) (entries []any) {
	if resp == nil || resp.Entries == nil {
		return nil
	}

	v := reflect.ValueOf(resp.Entries["data"])
	if v.Kind() != reflect.Slice {
		return nil
	}

	entries = make([]any, 0, v.Len())
	for i := range v.Len() {
		entries = append(entries, v.Index(i).Interface())
	}

	return entries
}

// isBlockedReason reports whether a filtering reason means the query was
// blocked rather than answered.  The reasons come from the filtering package
// as strings; anything that is not a filter rejection was answered.
func isBlockedReason(reason string) (blocked bool) {
	return strings.HasPrefix(reason, "Filtered")
}

// DeviceFromUserAgent guesses the kind of device from a user agent.  It returns
// an empty string when there is nothing to go on, and the panel then says so
// instead of guessing.
func DeviceFromUserAgent(ua string) (device string) {
	ua = strings.ToLower(ua)
	if ua == "" {
		return ""
	}

	switch {
	case strings.Contains(ua, "iphone"):
		return "iPhone"
	case strings.Contains(ua, "ipad"):
		return "iPad"
	case strings.Contains(ua, "android"):
		return "Android 设备"
	case strings.Contains(ua, "windows"):
		return "Windows 设备"
	case strings.Contains(ua, "macintosh"), strings.Contains(ua, "mac os"):
		return "Mac"
	case strings.Contains(ua, "linux"):
		return "Linux 设备"
	default:
		return ""
	}
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

// PublicStats is the anonymous summary of the service that the portal shows to
// visitors who have not signed in.
//
// This is the only portal response anybody on the internet can read, so it must
// never carry anything that belongs to an individual account: the totals are
// sums over all of them.
type PublicStats struct {
	// Queries is the number of queries the service has answered for all
	// accounts, counted since each account was created.
	Queries int64 `json:"queries"`

	// Queries24h, Blocked24h and Passed24h are the counters of the last 24
	// hours as the statistics module reports them: the queries answered,
	// the queries rejected by filtering, and the queries that matched a
	// rule but were allowed by the allow-list.  They are not the cumulative
	// per-account figures above, and they come from an optional module, so
	// they are zero when it is not available.
	Queries24h int64 `json:"queries_24h"`
	Blocked24h int64 `json:"blocked_24h"`
	Passed24h  int64 `json:"passed_24h"`

	// Blocked is the cumulative number of queries that filtering rejected
	// over all accounts.  The public page falls back to it when the 24-hour
	// figures are not available, so that the fallback shows a real number
	// instead of zero.
	Blocked int64 `json:"blocked"`

	// Passed is the cumulative number of queries that matched a filtering
	// rule but were allowed by the allow-list over all accounts.  It is the
	// fallback of [PublicStats.Passed24h], for the same reason as
	// [PublicStats.Blocked].
	Passed int64 `json:"passed"`

	// Rules is the number of filtering rules in force.  Lists and
	// CustomRules say where they come from.
	Rules       uint64 `json:"rules"`
	Lists       int    `json:"lists"`
	CustomRules int    `json:"custom_rules"`

	// Accounts is the number of accounts, and Active how many of them can be
	// used right now.
	Accounts int `json:"accounts"`
	Active   int `json:"active"`

	// Protected is whether the service is filtering anything at all.
	Protected bool `json:"protected"`

	// Domain is the domain of the DoT and DoH endpoints, if one is set.
	Domain string `json:"domain"`
}

// PublicStats returns the anonymous summary of the service.
func (m *Manager) PublicStats() (s *PublicStats) {
	s = &PublicStats{Domain: m.users.Domain()}

	if sum := m.users.Summary(); sum != nil {
		s.Queries = sum.TotalRequests
		s.Accounts = sum.Total
		s.Blocked = sum.Blocked
		s.Passed = sum.Passed

		// A disabled, expired or over-quota account cannot be used, so it is
		// not active.  One that is merely about to expire still works.
		s.Active = sum.Total - sum.Disabled - sum.Expired - sum.OverQuota
	}

	// Statistics are optional, so the page reports zeroes instead of a
	// number nobody measured when the module is not there.
	if m.stats != nil {
		st := m.stats.Stats24h()
		s.Queries24h = st.Queries
		s.Blocked24h = st.Blocked
		s.Passed24h = st.Passed
	}

	// Filtering is optional, and a page that claims zero rules would read as
	// "nothing is blocked" rather than "not known here".
	if m.filtering != nil {
		fs := m.filtering.FilteringStatus()
		s.Rules, s.Lists, s.CustomRules = fs.Rules, fs.Lists, fs.CustomRules
		s.Protected = fs.Enabled
	}

	return s
}
