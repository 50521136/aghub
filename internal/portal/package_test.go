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

	b, err := Package(apiBase, []string{"https://portal.example.com"})
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
	assert.Contains(t, files[readmeFileName], "https://portal.example.com")
}

func TestPackageWithoutAnAddress(t *testing.T) {
	b, err := Package("", nil)
	require.NoError(t, err)

	files := filesOf(t, b)

	// An empty address means the front-end talks to its own origin, which the
	// page treats as the default.
	assert.Contains(t, files[configFileName], `apiBase: ""`)

	// Without origins the note has to say that the list is still empty,
	// otherwise the administrator never learns why the browser refuses the
	// requests.
	assert.Contains(t, files[readmeFileName], "门户来源")
	assert.Contains(t, files[readmeFileName], "没有配置")
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
	b, err := Package("", nil)
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

	b, err := Package(apiBase, nil)
	require.NoError(t, err)

	config := filesOf(t, b)[configFileName]

	// The quote must be backslash-escaped, so the literal ends where the
	// generator says it does and not where the address says it does.
	assert.Contains(t, config, `apiBase: "https://x.example/\";alert(1);//",`)
}
