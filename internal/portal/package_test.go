package portal

import (
	"archive/zip"
	"bytes"
	"io"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// filesOf unpacks the package into a name-to-content map.
func filesOf(tb testing.TB, b []byte) (files map[string]string) {
	tb.Helper()

	zr, err := zip.NewReader(bytes.NewReader(b), int64(len(b)))
	require.NoError(tb, err)

	files = make(map[string]string, len(zr.File))

	for _, f := range zr.File {
		rc, oerr := f.Open()
		require.NoError(tb, oerr)

		data, rerr := io.ReadAll(rc)
		require.NoError(tb, rerr)
		require.NoError(tb, rc.Close())

		files[f.Name] = string(data)
	}

	return files
}

func TestPackage(t *testing.T) {
	const apiBase = "https://dns.example.com:3004"

	b, err := Package(apiBase, "tok123", []string{"https://portal.example.com"})
	require.NoError(t, err)
	require.NotEmpty(t, b)

	files := filesOf(t, b)

	// The front-end itself must be there, or the package is not deployable.
	require.Contains(t, files, "index.html")
	require.Contains(t, files, configFileName)
	require.Contains(t, files, readmeFileName)

	// The address has to reach the page, and the page has to look for it.
	assert.Contains(t, files[configFileName], apiBase)
	assert.Contains(t, files["index.html"], "AGHUB_PORTAL_CONFIG")

	// The note names the origins the administrator has to match.
	// The token is what replaces the origin allow-list, so the note has to
	// carry it rather than a list of origins.
	assert.Contains(t, files[readmeFileName], "X-Portal-Token")
}

func TestPackageWithoutAnAddress(t *testing.T) {
	b, err := Package("", "tok123", nil)
	require.NoError(t, err)

	files := filesOf(t, b)

	// An empty address means the front-end talks to its own origin, which the
	// page treats as the default.
	assert.Contains(t, files[configFileName], `api: ""`)

	// With no API address the note has to say the front-end will call its own
	// origin, and that the pairing token is what lets it reach AGHub from
	// there -- otherwise the administrator has no idea why it works.
	assert.Contains(t, files[readmeFileName], "与页面同源")
	assert.Contains(t, files[readmeFileName], "X-Portal-Token")
}

// TestPackageShipsThePHPBackEnd checks that the PHP back-end travels with the
// front-end.
//
// A site that has PHP but no reverse proxy can then serve the portal without
// touching the AGHub host: the browser talks to PHP, and the PHP-to-AGHub hop
// is server side, so a plain HTTP AGHub keeps working.  Shipping it in the
// package is the difference between "read the docs and go find a script" and
// "unzip and it works".
func TestPackageShipsThePHPBackEnd(t *testing.T) {
	b, err := Package("https://api.example.com", "tok123", nil)
	require.NoError(t, err)

	files := filesOf(t, b)

	require.Contains(t, files, "php/index.php")
	require.Contains(t, files, "php/config.sample.php")
	require.Contains(t, files, "php/README.md")

	// The proxy has to be a real proxy: forward the scheme, and drop Origin so
	// that AGHub does not treat the call as cross-origin and hand out a cookie
	// the browser will refuse.
	php := files["php/index.php"]
	assert.Contains(t, php, "X-Forwarded-Proto")
	assert.Contains(t, php, "SameSite=None")
	// PHP 7.4 has no str_starts_with; the note inside the file mentions it, so
	// look for a call rather than the bare name.
	assert.NotContains(t, php, "str_starts_with(", "PHP 7.4 has no str_starts_with")
}

// TestReadmeLeadsWithTheProxy checks that the note offers the same-origin
// reverse proxy before it offers TLS.
//
// A server-side hop is not subject to the browser rules that break a
// cross-origin portal, so the deployment that already has a back-end in front
// of its pages does not have to put a certificate on AGHub at all.  The note
// used to describe only the TLS route, which sent those deployments down a
// path they did not need.
func TestReadmeLeadsWithTheProxy(t *testing.T) {
	b, err := Package("", "tok123", nil)
	require.NoError(t, err)

	note := filesOf(t, b)[readmeFileName]

	proxyAt := strings.Index(note, "同源反代")
	tlsAt := strings.Index(note, "备选")
	require.NotEqual(t, -1, proxyAt, "the note must describe the reverse proxy")
	require.NotEqual(t, -1, tlsAt, "the note must describe the TLS route too")
	assert.Less(t, proxyAt, tlsAt, "the proxy has to come first")

	// Without this header AGHub thinks the browser side is plain HTTP and
	// hands out a cookie without Secure.
	assert.Contains(t, note, "X-Forwarded-Proto")
}

func TestPackageEscapesTheAddress(t *testing.T) {
	// An address is administrator input, and it ends up inside a JavaScript
	// string literal.  A quote must not be able to end it early.
	const apiBase = `https://x.example/";alert(1);//`

	b, err := Package(apiBase, "tok123", nil)
	require.NoError(t, err)

	config := filesOf(t, b)[configFileName]

	// The quote must be backslash-escaped, so the literal ends where the
	// generator says it does and not where the address says it does.
	assert.Contains(t, config, `api: "https://x.example/\";alert(1);//",`)
}
