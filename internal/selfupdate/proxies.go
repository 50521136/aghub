package selfupdate

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"time"
)

// ProxyNode is one GitHub acceleration proxy from the built-in list.  The list
// mirrors the community-maintained one published on github.akams.cn, which
// aggregates nodes contributed by their owners and found by scanning.
type ProxyNode struct {
	// Host is the host name of the proxy.
	Host string `json:"host"`

	// URL is the prefix to prepend to a GitHub URL.
	URL string `json:"url"`

	// Contributed is true for nodes submitted by their owners, as opposed to
	// ones found by scanning.
	Contributed bool `json:"contributed"`
}

// defaultProxies is the built-in list of GitHub acceleration proxies.  Most of
// them serve release downloads only; [Updater.TestProxies] tells which ones
// also serve the API, which the version check needs.
var defaultProxies = []ProxyNode{
	{Host: "gh.dpik.top", URL: "https://gh.dpik.top", Contributed: true},
	{Host: "github.tbap.top", URL: "https://github.tbap.top", Contributed: true},
	{Host: "ghfile.geekertao.top", URL: "https://ghfile.geekertao.top", Contributed: true},
	{Host: "ghproxy.net", URL: "https://ghproxy.net", Contributed: true},
	{Host: "gh-proxy.com", URL: "https://gh-proxy.com", Contributed: true},
	{Host: "cdn.gh-proxy.com", URL: "https://cdn.gh-proxy.com", Contributed: true},
	{Host: "github.dpik.top", URL: "https://github.dpik.top", Contributed: true},
	{Host: "j.1lin.dpdns.org", URL: "https://j.1lin.dpdns.org", Contributed: true},
	{Host: "github.starrlzy.cn", URL: "https://github.starrlzy.cn", Contributed: true},
	{Host: "github-proxy.memory-echoes.cn", URL: "https://github-proxy.memory-echoes.cn", Contributed: true},
	{Host: "git.yylx.win", URL: "https://git.yylx.win", Contributed: true},
	{Host: "ghm.078465.xyz", URL: "https://ghm.078465.xyz", Contributed: true},
	{Host: "gh.927223.xyz", URL: "https://gh.927223.xyz", Contributed: true},
	{Host: "ghf.xn--eqrr82bzpe.top", URL: "https://ghf.xn--eqrr82bzpe.top", Contributed: true},
	{Host: "gh.felicity.ac.cn", URL: "https://gh.felicity.ac.cn", Contributed: true},
	{Host: "gh.bugdey.us.kg", URL: "https://gh.bugdey.us.kg", Contributed: true},
	{Host: "cdn.akaere.online", URL: "https://cdn.akaere.online", Contributed: true},
	{Host: "jiashu.1win.eu.org", URL: "https://jiashu.1win.eu.org", Contributed: true},
	{Host: "tvv.tw", URL: "https://tvv.tw", Contributed: true},
	{Host: "j.1win.ggff.net", URL: "https://j.1win.ggff.net", Contributed: true},
	{Host: "gitproxy.127731.xyz", URL: "https://gitproxy.127731.xyz", Contributed: true},
	{Host: "gh.inkchills.cn", URL: "https://gh.inkchills.cn", Contributed: true},
	{Host: "gh.catmak.name", URL: "https://gh.catmak.name", Contributed: true},
	{Host: "gh.b52m.cn", URL: "https://gh.b52m.cn", Contributed: true},
	{Host: "down.mxw.xx.kg", URL: "https://down.mxw.xx.kg", Contributed: true},
	{Host: "down.mxw.qzz.io", URL: "https://down.mxw.qzz.io", Contributed: true},
	{Host: "github.mxw.qzz.io", URL: "https://github.mxw.qzz.io", Contributed: true},
	{Host: "gh.acmsz.top", URL: "https://gh.acmsz.top", Contributed: true},
	{Host: "gh.jjj.gv.uy", URL: "https://gh.jjj.gv.uy", Contributed: true},
	{Host: "slink.ltd", URL: "https://slink.ltd", Contributed: false},
	{Host: "github.tmby.shop", URL: "https://github.tmby.shop", Contributed: false},
	{Host: "ghpr.cc", URL: "https://ghpr.cc", Contributed: false},
	{Host: "gh.tryxd.cn", URL: "https://gh.tryxd.cn", Contributed: false},
	{Host: "gitproxy.click", URL: "https://gitproxy.click", Contributed: false},
	{Host: "github.chenc.dev", URL: "https://github.chenc.dev", Contributed: false},
	{Host: "gh.ddlc.top", URL: "https://gh.ddlc.top", Contributed: false},
	{Host: "gitproxy.mrhjx.cn", URL: "https://gitproxy.mrhjx.cn", Contributed: false},
	{Host: "gh.sixyin.com", URL: "https://gh.sixyin.com", Contributed: false},
	{Host: "gh.monlor.com", URL: "https://gh.monlor.com", Contributed: false},
	{Host: "ghpxy.hwinzniej.top", URL: "https://ghpxy.hwinzniej.top", Contributed: false},
	{Host: "git.669966.xyz", URL: "https://git.669966.xyz", Contributed: false},
	{Host: "ghfast.top", URL: "https://ghfast.top", Contributed: false},
	{Host: "gh.jasonzeng.dev", URL: "https://gh.jasonzeng.dev", Contributed: false},
	{Host: "github.geekery.cn", URL: "https://github.geekery.cn", Contributed: false},
	{Host: "gp.zkitefly.eu.org", URL: "https://gp.zkitefly.eu.org", Contributed: false},
	{Host: "fastgit.cc", URL: "https://fastgit.cc", Contributed: false},
	{Host: "ghproxy.1888866.xyz", URL: "https://ghproxy.1888866.xyz", Contributed: false},
	{Host: "ghp.arslantu.xyz", URL: "https://ghp.arslantu.xyz", Contributed: false},
	{Host: "github.ednovas.xyz", URL: "https://github.ednovas.xyz", Contributed: false},
	{Host: "ghproxy.imciel.com", URL: "https://ghproxy.imciel.com", Contributed: false},
	{Host: "ghproxy.cxkpro.top", URL: "https://ghproxy.cxkpro.top", Contributed: false},
	{Host: "github.xxlab.tech", URL: "https://github.xxlab.tech", Contributed: false},
	{Host: "gh.idayer.com", URL: "https://gh.idayer.com", Contributed: false},
	{Host: "free.cn.eu.org", URL: "https://free.cn.eu.org", Contributed: false},
	{Host: "gh.chjina.com", URL: "https://gh.chjina.com", Contributed: false},
	{Host: "ghp.keleyaa.com", URL: "https://ghp.keleyaa.com", Contributed: false},
	{Host: "proxy.yaoyaoling.net", URL: "https://proxy.yaoyaoling.net", Contributed: false},
	{Host: "ghproxy.monkeyray.net", URL: "https://ghproxy.monkeyray.net", Contributed: false},
	{Host: "gh.noki.icu", URL: "https://gh.noki.icu", Contributed: false},
	{Host: "g.blfrp.cn", URL: "https://g.blfrp.cn", Contributed: false},
	{Host: "githubdog.com", URL: "https://githubdog.com", Contributed: true},
	{Host: "gh.meali.top", URL: "https://gh.meali.top", Contributed: true},
	{Host: "777.z321.cc.cd", URL: "https://777.z321.cc.cd", Contributed: true},
	{Host: "gg.z321.cc.cd", URL: "https://gg.z321.cc.cd", Contributed: true},
	{Host: "g.z321.cc.cd", URL: "https://g.z321.cc.cd", Contributed: true},
	{Host: "js.jiangss.shop", URL: "https://js.jiangss.shop", Contributed: true},
	{Host: "gap.andyjin.website", URL: "https://gap.andyjin.website", Contributed: true},
	{Host: "gh.my-website.ccwu.cc", URL: "https://gh.my-website.ccwu.cc", Contributed: true},
	{Host: "github.ikgy.top", URL: "https://github.ikgy.top", Contributed: true},
	{Host: "gh.07150721.xyz", URL: "https://gh.07150721.xyz", Contributed: true},
	{Host: "cfgh.ikgy.top", URL: "https://cfgh.ikgy.top", Contributed: true},
	{Host: "xsadwsd.kdns.fr", URL: "https://xsadwsd.kdns.fr", Contributed: true},
	{Host: "gh.ruan.dpdns.org", URL: "https://gh.ruan.dpdns.org", Contributed: true},
	{Host: "ghproxy.felicity.land", URL: "https://ghproxy.felicity.land", Contributed: true},
	{Host: "github.nswrz.cn", URL: "https://github.nswrz.cn", Contributed: true},
	{Host: "gh.zhai.edu.pl", URL: "https://gh.zhai.edu.pl", Contributed: true},
	{Host: "gh.qfmc0721.cc.cd", URL: "https://gh.qfmc0721.cc.cd", Contributed: true},
	{Host: "github-cf.947563.xyz", URL: "https://github-cf.947563.xyz", Contributed: true},
	{Host: "github.gohj99.site", URL: "https://github.gohj99.site", Contributed: true},
	{Host: "githubproxy.gohj99.site", URL: "https://githubproxy.gohj99.site", Contributed: true},
	{Host: "ghproxy.icu", URL: "https://ghproxy.icu", Contributed: true},
}

// DefaultProxies returns the built-in list of GitHub acceleration proxies.
// The result is a copy and may be modified by the caller.
func DefaultProxies() (nodes []ProxyNode) {
	nodes = make([]ProxyNode, len(defaultProxies))
	copy(nodes, defaultProxies)

	return nodes
}

// ProxyTest is the result of testing one acceleration proxy.
type ProxyTest struct {
	// URL is the proxy prefix that was tested.
	URL string `json:"url"`

	// Host is the host name of the proxy.
	Host string `json:"host"`

	// APIOK is true if the proxy served the GitHub API.
	APIOK bool `json:"api_ok"`

	// DownloadOK is true if the proxy served a release asset.
	DownloadOK bool `json:"download_ok"`

	// LatencyMS is the round-trip time of the successful request, in
	// milliseconds.
	LatencyMS int64 `json:"latency_ms"`

	// Error is the failure reason, if any.
	Error string `json:"error,omitempty"`
}

const (
	// proxyTestConcurrency is the number of proxies tested at once.  The
	// test is dominated by the proxies that never answer, so the run takes
	// roughly (dead proxies / concurrency) * proxyTestTimeout.
	proxyTestConcurrency = 32

	// proxyTestTimeout is the timeout of a single test request.  The run is
	// dominated by the proxies that no longer resolve, each of which costs
	// the whole timeout, so raising this makes the test much slower.
	proxyTestTimeout = 5 * time.Second

	// proxyTestMaxBytes caps how much of a response is read while testing.
	proxyTestMaxBytes = 1 << 20

	// proxyTestPreviewLen is how many bytes of an unexpected response are
	// quoted in the error.
	proxyTestPreviewLen = 120
)

// TestProxies tests acceleration proxies against the GitHub API and a release
// asset, so that the caller can pick one that works from this network.  An
// empty list means the built-in list.  The order of the result matches the
// order of the input.
func (u *Updater) TestProxies(
	ctx context.Context,
	prefixes []string,
) (res []ProxyTest) {
	if len(prefixes) == 0 {
		prefixes = make([]string, 0, len(defaultProxies))
		for _, n := range defaultProxies {
			prefixes = append(prefixes, n.URL)
		}
	}

	res = make([]ProxyTest, len(prefixes))

	// assetURL is empty when the repository is not configured, in which case
	// only the API is tested.
	assetURL := u.testAssetURL()

	sem := make(chan struct{}, proxyTestConcurrency)

	var wg sync.WaitGroup

	for i, prefix := range prefixes {
		wg.Add(1)

		go func(i int, prefix string) {
			defer wg.Done()

			sem <- struct{}{}
			defer func() { <-sem }()

			res[i] = u.testProxy(ctx, prefix, assetURL)
		}(i, prefix)
	}

	wg.Wait()

	return res
}

// testAssetURL returns the download URL of the checksums asset of the latest
// release.  It uses the "latest" alias rather than asking the API, so that the
// download can be tested even when the proxy in use cannot serve the API.
func (u *Updater) testAssetURL() (url string) {
	if u.repo == "" {
		return ""
	}

	return fmt.Sprintf(
		"https://github.com/%s/releases/latest/download/checksums.txt",
		u.repo,
	)
}

// testProxy tests a single acceleration proxy.
func (u *Updater) testProxy(
	ctx context.Context,
	prefix string,
	assetURL string,
) (t ProxyTest) {
	t.URL = prefix
	t.Host = ProxyHost(prefix)

	apiURL := fmt.Sprintf("%s/repos/%s/releases/latest", u.apiBase(), u.repo)

	var errs []string

	ms, err := u.probe(ctx, prefix+"/"+apiURL, checkAPIRelease)
	if err == nil {
		t.APIOK = true
		t.LatencyMS = ms
	} else {
		errs = append(errs, "api: "+err.Error())
	}

	if assetURL != "" {
		ms, err = u.probe(ctx, prefix+"/"+assetURL, checkAssetBody)
		if err == nil {
			t.DownloadOK = true

			if !t.APIOK || ms < t.LatencyMS {
				t.LatencyMS = ms
			}
		} else {
			errs = append(errs, "download: "+err.Error())
		}
	}

	if !t.APIOK && !t.DownloadOK {
		t.Error = strings.Join(errs, "; ")
	}

	return t
}

// probe requests url and returns how long it took, in milliseconds.  check
// receives the body and returns an error when the response is not the expected
// document; a proxy that answers with a success status and an HTML error page
// would otherwise look usable.
func (u *Updater) probe(
	ctx context.Context,
	url string,
	check func(body []byte) (err error),
) (ms int64, err error) {
	pctx, cancel := context.WithTimeout(ctx, proxyTestTimeout)
	defer cancel()

	req, err := http.NewRequestWithContext(pctx, http.MethodGet, url, nil)
	if err != nil {
		return 0, err
	}

	req.Header.Set("Accept", "application/vnd.github+json")
	req.Header.Set("User-Agent", "aghub-selfupdate/"+Version)

	start := time.Now()

	resp, err := u.client.Do(req)
	if err != nil {
		return 0, err
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode != http.StatusOK {
		return 0, fmt.Errorf("status %s", resp.Status)
	}

	body, err := io.ReadAll(io.LimitReader(resp.Body, proxyTestMaxBytes))
	if err != nil {
		return 0, fmt.Errorf("reading body: %w", err)
	}

	err = check(body)
	if err != nil {
		return 0, err
	}

	return time.Since(start).Milliseconds(), nil
}

// checkAPIRelease checks that body is the JSON document of a GitHub release.
func checkAPIRelease(body []byte) (err error) {
	if !json.Valid(body) {
		return fmt.Errorf("response is not JSON: %s", previewBody(body))
	}

	if !bytes.Contains(body, []byte(`"tag_name"`)) {
		return fmt.Errorf("response is not a release: %s", previewBody(body))
	}

	return nil
}

// checkAssetBody checks that body looks like the content of a release asset
// rather than an error page.
func checkAssetBody(body []byte) (err error) {
	if len(body) == 0 {
		return fmt.Errorf("empty response")
	}

	if isHTML(body) {
		return fmt.Errorf("response is an HTML page: %s", previewBody(body))
	}

	return nil
}

// isHTML reports whether body starts with an HTML document.
func isHTML(body []byte) (ok bool) {
	head := bytes.TrimLeft(body, " 	\r\n")
	lower := bytes.ToLower(head)

	return bytes.HasPrefix(lower, []byte("<!doctype")) ||
		bytes.HasPrefix(lower, []byte("<html"))
}

// previewBody returns a short, single-line excerpt of body for an error
// message.
func previewBody(body []byte) (s string) {
	if len(body) > proxyTestPreviewLen {
		body = body[:proxyTestPreviewLen]
	}

	s = strings.Join(strings.Fields(string(body)), " ")
	if s == "" {
		return "(empty)"
	}

	return s
}

// ProxyHost returns the host name of a proxy prefix.
func ProxyHost(prefix string) (host string) {
	u, err := url.Parse(prefix)
	if err != nil || u.Host == "" {
		return prefix
	}

	return u.Host
}

// ValidateProxy returns an error if prefix is not usable as an acceleration
// proxy prefix.  An empty prefix is valid and means a direct connection.
func ValidateProxy(prefix string) (err error) {
	if prefix == "" {
		return nil
	}

	u, err := url.Parse(prefix)
	if err != nil {
		return fmt.Errorf("invalid proxy URL: %w", err)
	}

	if u.Scheme != "http" && u.Scheme != "https" {
		return fmt.Errorf("proxy URL must use http or https, got %q", u.Scheme)
	}

	if u.Host == "" {
		return fmt.Errorf("proxy URL has no host")
	}

	return nil
}

// NormalizeProxy trims the trailing slashes of a proxy prefix, so that it can
// be joined with a GitHub URL using a single slash.
func NormalizeProxy(prefix string) (out string) {
	return strings.TrimRight(strings.TrimSpace(prefix), "/")
}
