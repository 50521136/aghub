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

// readPackage unzips b and returns the files by name.
func readPackage(tb testing.TB, b []byte) (files map[string]string) {
	tb.Helper()

	zr, err := zip.NewReader(bytes.NewReader(b), int64(len(b)))
	require.NoError(tb, err)

	files = map[string]string{}
	for _, f := range zr.File {
		rc, rerr := f.Open()
		require.NoError(tb, rerr)

		data, rerr := io.ReadAll(rc)
		require.NoError(tb, rerr)
		require.NoError(tb, rc.Close())

		files[f.Name] = string(data)
	}

	return files
}

func TestPackage(t *testing.T) {
	const apiBase = "https://api.example.com"

	b, err := Package(apiBase, "tok123")
	require.NoError(t, err)

	files := readPackage(t, b)

	// The portal is a set of PHP files, not a static front-end: the browser
	// must only ever talk to the site that serves them.
	for _, name := range []string{
		"index.php",
		"lib.php",
		"page_report.php",
		"page_ranking.php",
		"page_feedback.php",
		"page_me.php",
		"page_login.php",
		"page_log.php",
		"page_register.php",
		"style.css",
		"config.php",
	} {
		assert.Contains(t, files, name)
	}

	// The sample config is replaced by the generated one.
	assert.NotContains(t, files, sampleFileName)

	// And nothing of the old static front-end survives, or the administrator
	// would be back to configuring origins and certificates.
	assert.NotContains(t, files, "index.html")
	assert.NotContains(t, files, "config.js")

	cfg := files[configFileName]
	assert.Contains(t, cfg, "'aghub_url' => '"+apiBase+"'")
	assert.Contains(t, cfg, "'token' => 'tok123'")

	// The token is what replaces the origin allow-list, so the note has to
	// explain it rather than a list of origins.
	note := files[readmeFileName]
	assert.Contains(t, note, "X-Portal-Token")
	assert.Contains(t, note, "没有跨域")
	assert.Contains(t, note, "没有混合内容")
	assert.Contains(t, note, apiBase)
}

// TestPackageIsReproducible makes sure two builds of the same package are
// byte-identical, so that a checksum is meaningful.
func TestPackageIsReproducible(t *testing.T) {
	first, err := Package("https://api.example.com", "tok123")
	require.NoError(t, err)

	second, err := Package("https://api.example.com", "tok123")
	require.NoError(t, err)

	assert.Equal(t, first, second)
}

// TestPackageWithoutAnAddress checks the fallback when no API address is
// configured.
func TestPackageWithoutAnAddress(t *testing.T) {
	b, err := Package("", "tok123")
	require.NoError(t, err)

	files := readPackage(t, b)

	// The PHP hop is server-to-server, so there is no same-origin case to
	// fall back to; the local address is the only sensible default.
	assert.Contains(t, files[configFileName], "http://127.0.0.1:3000")
	assert.Contains(t, files[readmeFileName], "未填写")
}

// TestPackageQuotesTheValues checks that a token or address containing a quote
// cannot break out of the generated PHP string.
func TestPackageQuotesTheValues(t *testing.T) {
	b, err := Package("https://api.example.com", `tok'123\`)
	require.NoError(t, err)

	files := readPackage(t, b)

	assert.Contains(t, files[configFileName], `'token' => 'tok\'123\\'`)
}

// TestPackageShipsNoBrowserSideCall makes sure the portal never asks the
// browser to reach AGHub itself.
//
// That is the whole point of the PHP portal: a browser-side call is what
// brings cross-origin and mixed-content rules into play.
func TestPackageShipsNoBrowserSideCall(t *testing.T) {
	b, err := Package("https://api.example.com", "tok123")
	require.NoError(t, err)

	files := readPackage(t, b)

	for name, body := range files {
		if !strings.HasSuffix(name, ".php") {
			continue
		}

		// The API address and the token belong in config.php only.
		if name == configFileName {
			continue
		}

		assert.NotContains(t, body, "api.example.com", "in %s", name)
		assert.NotContains(t, body, "tok123", "in %s", name)
	}
}
