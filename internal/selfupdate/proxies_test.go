package selfupdate

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestValidateProxy(t *testing.T) {
	testCases := []struct {
		name    string
		prefix  string
		wantErr bool
	}{{
		name:   "empty",
		prefix: "",
	}, {
		name:   "https",
		prefix: "https://gh-proxy.com",
	}, {
		name:   "http",
		prefix: "http://127.0.0.1:8080",
	}, {
		name:    "no scheme",
		prefix:  "gh-proxy.com",
		wantErr: true,
	}, {
		name:    "wrong scheme",
		prefix:  "ftp://gh-proxy.com",
		wantErr: true,
	}, {
		name:    "no host",
		prefix:  "https://",
		wantErr: true,
	}}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			err := ValidateProxy(tc.prefix)
			if tc.wantErr {
				assert.Error(t, err)
			} else {
				assert.NoError(t, err)
			}
		})
	}
}

func TestNormalizeProxy(t *testing.T) {
	assert.Equal(t, "", NormalizeProxy(""))
	assert.Equal(t, "", NormalizeProxy("   "))
	assert.Equal(t, "https://gh-proxy.com", NormalizeProxy("https://gh-proxy.com/"))
	assert.Equal(t, "https://gh-proxy.com", NormalizeProxy("  https://gh-proxy.com//  "))
}

func TestProxyHost(t *testing.T) {
	assert.Equal(t, "gh-proxy.com", ProxyHost("https://gh-proxy.com"))
	assert.Equal(t, "gh-proxy.com", ProxyHost("https://gh-proxy.com/path"))
	assert.Equal(t, "127.0.0.1:8080", ProxyHost("http://127.0.0.1:8080"))

	// A prefix that does not parse is returned as is, so that the caller
	// still has something to show.
	assert.Equal(t, "not a url", ProxyHost("not a url"))
}

func TestDefaultProxies(t *testing.T) {
	nodes := DefaultProxies()
	require.NotEmpty(t, nodes)

	seen := make(map[string]struct{}, len(nodes))

	for _, n := range nodes {
		assert.NoError(t, ValidateProxy(n.URL), "node %q", n.Host)
		assert.Equal(t, n.Host, ProxyHost(n.URL), "node %q", n.Host)

		_, ok := seen[n.Host]
		assert.False(t, ok, "duplicate host %q", n.Host)

		seen[n.Host] = struct{}{}
	}

	// The caller must be able to modify the result without affecting the
	// package state.
	nodes[0].Host = "changed"
	assert.NotEqual(t, "changed", DefaultProxies()[0].Host)
}

func TestSetProxy(t *testing.T) {
	u, err := New(&Config{Logger: testLogger})
	require.NoError(t, err)

	assert.Equal(t, "", u.Proxy())

	u.SetProxy("https://gh-proxy.com/")
	assert.Equal(t, "https://gh-proxy.com", u.Proxy())
	assert.Equal(t, "https://gh-proxy.com/https://example.com/a", u.downloadURL("https://example.com/a"))

	u.SetProxy("")
	assert.Equal(t, "", u.Proxy())
	assert.Equal(t, "https://example.com/a", u.downloadURL("https://example.com/a"))
}

// TestGetFallsBackToDirect checks that a proxy which does not serve the GitHub
// API does not break the version check: most public accelerators only serve
// release downloads.
func TestGetFallsBackToDirect(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = io.WriteString(w, `{"tag_name":"v9.9.9"}`)
	}))
	t.Cleanup(srv.Close)

	u, err := New(&Config{Logger: testLogger})
	require.NoError(t, err)

	u.api = srv.URL

	// A port that nothing listens on, so that the proxied request fails
	// immediately instead of waiting for a timeout.
	u.SetProxy("http://127.0.0.1:1")

	body, err := u.get(context.Background(), srv.URL+"/repos/owner/name/releases/latest")
	require.NoError(t, err)

	t.Cleanup(func() { _ = body.Close() })

	data, err := io.ReadAll(body)
	require.NoError(t, err)

	assert.JSONEq(t, `{"tag_name":"v9.9.9"}`, string(data))
}

// TestGetProxyError checks that the failure of both routes is reported with
// both reasons.
func TestGetProxyError(t *testing.T) {
	u, err := New(&Config{Logger: testLogger})
	require.NoError(t, err)

	u.api = "http://127.0.0.1:1"
	u.SetProxy("http://127.0.0.1:2")

	_, err = u.get(context.Background(), u.api+"/repos/owner/name/releases/latest")
	require.Error(t, err)

	assert.Contains(t, err.Error(), "via proxy")
	assert.Contains(t, err.Error(), "directly")
}

// TestProbeRejectsNonOK checks that a proxy which answers with an error status
// is not considered usable.
func TestProbeRejectsNonOK(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "nope", http.StatusForbidden)
	}))
	t.Cleanup(srv.Close)

	u, err := New(&Config{Logger: testLogger})
	require.NoError(t, err)

	_, err = u.probe(context.Background(), srv.URL, checkAssetBody)
	assert.ErrorContains(t, err, "403")
}

// TestProbeOK checks that a proxy which answers with the expected document is
// considered usable and reports a latency.
func TestProbeOK(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = io.WriteString(w, `{"tag_name":"v9.9.9"}`)
	}))
	t.Cleanup(srv.Close)

	u, err := New(&Config{Logger: testLogger})
	require.NoError(t, err)

	ms, err := u.probe(context.Background(), srv.URL, checkAPIRelease)
	require.NoError(t, err)

	assert.GreaterOrEqual(t, ms, int64(0))
}

func TestCheckAPIRelease(t *testing.T) {
	testCases := []struct {
		name    string
		body    string
		wantErr bool
	}{{
		name: "release",
		body: `{"tag_name":"v1.0.1","assets":[]}`,
	}, {
		name:    "html error page",
		body:    "<!DOCTYPE html><html><body>Not Found</body></html>",
		wantErr: true,
	}, {
		name:    "other json",
		body:    `{"message":"Not Found"}`,
		wantErr: true,
	}, {
		name:    "empty",
		body:    "",
		wantErr: true,
	}}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			err := checkAPIRelease([]byte(tc.body))
			if tc.wantErr {
				assert.Error(t, err)
			} else {
				assert.NoError(t, err)
			}
		})
	}
}

func TestCheckAssetBody(t *testing.T) {
	assert.NoError(t, checkAssetBody([]byte("abc123  aghub_1.0.1_linux_amd64.tar.gz\n")))

	// An HTML landing page with a success status must not pass for an asset.
	assert.ErrorContains(
		t,
		checkAssetBody([]byte("\n  <!DOCTYPE html>\n<html></html>")),
		"HTML",
	)
	assert.ErrorContains(t, checkAssetBody(nil), "empty")
}

func TestTestAssetURL(t *testing.T) {
	u, err := New(&Config{Logger: testLogger})
	require.NoError(t, err)

	u.repo = "owner/name"

	// The "latest" alias keeps the download test independent of the API,
	// which the proxy in use may not be able to serve.
	assert.Equal(
		t,
		"https://github.com/owner/name/releases/latest/download/checksums.txt",
		u.testAssetURL(),
	)

	u.repo = ""
	assert.Equal(t, "", u.testAssetURL())
}

func TestPreviewBody(t *testing.T) {
	assert.Equal(t, "(empty)", previewBody(nil))
	assert.Equal(t, "a b c", previewBody([]byte("  a\n b	c  ")))

	long := strings.Repeat("x", proxyTestPreviewLen*2)
	assert.Len(t, previewBody([]byte(long)), proxyTestPreviewLen)
}
