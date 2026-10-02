package selfupdate

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"runtime"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// makeArchive returns a gzipped tarball with the given entries.
func makeArchive(t *testing.T, entries map[string][]byte) (archive []byte) {
	t.Helper()

	buf := &bytes.Buffer{}
	gz := gzip.NewWriter(buf)
	tw := tar.NewWriter(gz)

	for name, content := range entries {
		hdr := &tar.Header{
			Name:     name,
			Mode:     0o755,
			Size:     int64(len(content)),
			Typeflag: tar.TypeReg,
		}

		require.NoError(t, tw.WriteHeader(hdr))
		_, err := tw.Write(content)
		require.NoError(t, err)
	}

	require.NoError(t, tw.Close())
	require.NoError(t, gz.Close())

	return buf.Bytes()
}

// sha256Hex returns the hex-encoded SHA-256 checksum of the data.
func sha256Hex(data []byte) (sum string) {
	h := sha256.Sum256(data)

	return hex.EncodeToString(h[:])
}

// newTestUpdater returns an updater that talks to the given test server.
func newTestUpdater(srv *httptest.Server) (u *Updater) {
	return &Updater{
		logger: testLogger,
		client: srv.Client(),
		status: &Status{Progress: -1},
	}
}

// newReleaseServer serves a release archive and its checksums.  When badSum is
// true, the checksums file contains a wrong hash.
func newReleaseServer(t *testing.T, archive []byte, name string, badSum bool) (srv *httptest.Server) {
	t.Helper()

	sum := sha256Hex(archive)
	if badSum {
		sum = sha256Hex([]byte("wrong"))
	}

	mux := http.NewServeMux()
	mux.HandleFunc("/asset", func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write(archive)
	})
	mux.HandleFunc("/checksums.txt", func(w http.ResponseWriter, _ *http.Request) {
		_, _ = fmt.Fprintf(w, "%s  %s\n", sum, name)
	})

	return httptest.NewServer(mux)
}

func TestDownloadVerifyExtract(t *testing.T) {
	dir := t.TempDir()

	binContent := []byte("#!/bin/sh\necho aghub\n")
	archive := makeArchive(t, map[string][]byte{"aghub": binContent})

	name := assetName("v1.0.0")
	srv := newReleaseServer(t, archive, name, false)
	defer srv.Close()

	u := newTestUpdater(srv)

	asset := &Asset{Name: name, URL: srv.URL + "/asset", Size: int64(len(archive))}
	path := filepath.Join(dir, name)

	require.NoError(t, u.download(t.Context(), asset, path))

	rel := &Release{
		Assets: []Asset{
			*asset,
			{Name: "checksums.txt", URL: srv.URL + "/checksums.txt"},
		},
	}

	require.NoError(t, u.verifyChecksums(t.Context(), rel, name, path))

	bin, err := extractBinary(path, dir, "aghub")
	require.NoError(t, err)

	got, err := os.ReadFile(bin)
	require.NoError(t, err)
	assert.Equal(t, binContent, got)

	// The download progress must have been reported.
	assert.Equal(t, float64(1), u.Status().Progress)
}

func TestVerifyChecksumsMismatch(t *testing.T) {
	dir := t.TempDir()

	archive := makeArchive(t, map[string][]byte{"aghub": []byte("x")})
	name := assetName("v1.0.0")

	srv := newReleaseServer(t, archive, name, true)
	defer srv.Close()

	u := newTestUpdater(srv)

	asset := &Asset{Name: name, URL: srv.URL + "/asset", Size: int64(len(archive))}
	path := filepath.Join(dir, name)

	require.NoError(t, u.download(t.Context(), asset, path))

	rel := &Release{
		Assets: []Asset{
			*asset,
			{Name: "checksums.txt", URL: srv.URL + "/checksums.txt"},
		},
	}

	err := u.verifyChecksums(t.Context(), rel, name, path)
	assert.ErrorContains(t, err, "checksum mismatch")
}

func TestVerifyChecksumsMissingFile(t *testing.T) {
	dir := t.TempDir()

	archive := makeArchive(t, map[string][]byte{"aghub": []byte("x")})
	name := assetName("v1.0.0")

	srv := newReleaseServer(t, archive, name, false)
	defer srv.Close()

	u := newTestUpdater(srv)

	asset := &Asset{Name: name, URL: srv.URL + "/asset", Size: int64(len(archive))}
	path := filepath.Join(dir, name)

	require.NoError(t, u.download(t.Context(), asset, path))

	// A release without a checksums.txt asset is allowed, but the update is
	// then unverified.
	rel := &Release{Assets: []Asset{*asset}}

	assert.NoError(t, u.verifyChecksums(t.Context(), rel, name, path))
}

func TestVerifyChecksumsNoEntryForAsset(t *testing.T) {
	dir := t.TempDir()

	archive := makeArchive(t, map[string][]byte{"aghub": []byte("x")})
	name := assetName("v1.0.0")

	srv := newReleaseServer(t, archive, "other-file.tar.gz", false)
	defer srv.Close()

	u := newTestUpdater(srv)

	asset := &Asset{Name: name, URL: srv.URL + "/asset", Size: int64(len(archive))}
	path := filepath.Join(dir, name)

	require.NoError(t, u.download(t.Context(), asset, path))

	rel := &Release{
		Assets: []Asset{
			*asset,
			{Name: "checksums.txt", URL: srv.URL + "/checksums.txt"},
		},
	}

	err := u.verifyChecksums(t.Context(), rel, name, path)
	assert.ErrorContains(t, err, "no checksum")
}

func TestExtractBinarySkipsPathTraversal(t *testing.T) {
	dir := t.TempDir()

	archive := makeArchive(t, map[string][]byte{
		"../evil": []byte("evil"),
		"aghub":   []byte("good"),
	})

	path := filepath.Join(dir, "a.tar.gz")
	require.NoError(t, os.WriteFile(path, archive, 0o600))

	bin, err := extractBinary(path, dir, "aghub")
	require.NoError(t, err)

	got, err := os.ReadFile(bin)
	require.NoError(t, err)
	assert.Equal(t, []byte("good"), got)

	// The malicious entry must not have escaped the directory.
	assert.NoFileExists(t, filepath.Join(filepath.Dir(dir), "evil"))
}

func TestExtractBinaryFallback(t *testing.T) {
	dir := t.TempDir()

	archive := makeArchive(t, map[string][]byte{"renamed-binary": []byte("bin")})

	path := filepath.Join(dir, "a.tar.gz")
	require.NoError(t, os.WriteFile(path, archive, 0o600))

	// The requested name is not present, so the first executable is used.
	bin, err := extractBinary(path, dir, "aghub")
	require.NoError(t, err)

	got, err := os.ReadFile(bin)
	require.NoError(t, err)
	assert.Equal(t, []byte("bin"), got)
}

func TestReplaceExecutable(t *testing.T) {
	dir := t.TempDir()

	dst := filepath.Join(dir, "aghub")
	require.NoError(t, os.WriteFile(dst, []byte("old"), 0o755))

	src := filepath.Join(dir, "new-binary")
	require.NoError(t, os.WriteFile(src, []byte("new"), 0o755))

	require.NoError(t, replaceExecutable(src, dst))

	got, err := os.ReadFile(dst)
	require.NoError(t, err)
	assert.Equal(t, []byte("new"), got)

	info, err := os.Stat(dst)
	require.NoError(t, err)
	assert.Equal(t, os.FileMode(0o755), info.Mode().Perm())

	// The previous binary must be kept as a backup.
	backup, err := os.ReadFile(dst + ".bak")
	require.NoError(t, err)
	assert.Equal(t, []byte("old"), backup)

	// No temporary files must be left behind.
	entries, err := os.ReadDir(dir)
	require.NoError(t, err)

	for _, e := range entries {
		assert.NotContains(t, e.Name(), ".aghub-new-")
	}
}

func TestAssetNameMatchesReleaseScript(t *testing.T) {
	// This is the contract between scripts/aghub-release.sh and the updater.
	name := assetName("v1.2.3")

	assert.Equal(
		t,
		fmt.Sprintf("aghub_1.2.3_%s_%s.tar.gz", runtime.GOOS, runtime.GOARCH),
		name,
	)
}

// newFakeAPI serves a GitHub-like API with a single release.
func newFakeAPI(
	t *testing.T,
	tag string,
	archive []byte,
	assetFile string,
	badSum bool,
) (srv *httptest.Server) {
	t.Helper()

	sum := sha256Hex(archive)
	if badSum {
		sum = sha256Hex([]byte("wrong"))
	}

	mux := http.NewServeMux()

	var base string

	mux.HandleFunc("/repos/owner/aghub/releases/latest", func(w http.ResponseWriter, _ *http.Request) {
		rel := map[string]any{
			"tag_name":     tag,
			"name":         tag,
			"body":         "release notes",
			"html_url":     base + "/release",
			"published_at": time.Now().UTC(),
			"assets": []map[string]any{
				{
					"name":                 assetFile,
					"browser_download_url": base + "/asset",
					"size":                 len(archive),
				},
				{
					"name":                 "checksums.txt",
					"browser_download_url": base + "/checksums.txt",
					"size":                 0,
				},
			},
		}

		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(rel)
	})

	mux.HandleFunc("/asset", func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write(archive)
	})

	mux.HandleFunc("/checksums.txt", func(w http.ResponseWriter, _ *http.Request) {
		_, _ = fmt.Fprintf(w, "%s  %s\n", sum, assetFile)
	})

	srv = httptest.NewServer(mux)
	base = srv.URL

	return srv
}

func TestApplyEndToEnd(t *testing.T) {
	dir := t.TempDir()

	// The installation that is about to be replaced.
	execPath := filepath.Join(dir, "aghub")
	require.NoError(t, os.WriteFile(execPath, []byte("old-binary"), 0o755))

	newBin := []byte("new-binary")
	archive := makeArchive(t, map[string][]byte{"aghub": newBin})

	name := assetName("v9.9.9")
	srv := newFakeAPI(t, "v9.9.9", archive, name, false)
	defer srv.Close()

	u := &Updater{
		logger:   testLogger,
		client:   srv.Client(),
		repo:     "owner/aghub",
		api:      srv.URL,
		execPath: execPath,
		status:   &Status{Progress: -1},
	}

	// Version is "dev" during tests, so any real version is newer.
	res := u.Check(t.Context())
	require.Empty(t, res.Error)
	assert.True(t, res.HasUpdate)
	assert.True(t, res.CanUpdate)
	assert.Equal(t, "9.9.9", res.Latest.Version)
	assert.Equal(t, "v9.9.9", res.Latest.TagName)
	assert.Len(t, res.Latest.Assets, 2)

	require.NoError(t, u.Apply(t.Context()))

	got, err := os.ReadFile(execPath)
	require.NoError(t, err)
	assert.Equal(t, newBin, got)

	backup, err := os.ReadFile(execPath + ".bak")
	require.NoError(t, err)
	assert.Equal(t, []byte("old-binary"), backup)

	info, err := os.Stat(execPath)
	require.NoError(t, err)
	assert.Equal(t, os.FileMode(0o755), info.Mode().Perm())

	st := u.Status()
	assert.False(t, st.Running)
	assert.True(t, st.Done)
	assert.Empty(t, st.Error)
	assert.Equal(t, "9.9.9", st.Version)
	assert.Equal(t, float64(1), st.Progress)
}

func TestApplyRejectsBadChecksum(t *testing.T) {
	dir := t.TempDir()

	execPath := filepath.Join(dir, "aghub")
	require.NoError(t, os.WriteFile(execPath, []byte("old-binary"), 0o755))

	archive := makeArchive(t, map[string][]byte{"aghub": []byte("new-binary")})
	name := assetName("v9.9.9")

	srv := newFakeAPI(t, "v9.9.9", archive, name, true)
	defer srv.Close()

	u := &Updater{
		logger:   testLogger,
		client:   srv.Client(),
		repo:     "owner/aghub",
		api:      srv.URL,
		execPath: execPath,
		status:   &Status{Progress: -1},
	}

	err := u.Apply(t.Context())
	assert.ErrorContains(t, err, "checksum mismatch")

	// The installation must be left untouched.
	got, rerr := os.ReadFile(execPath)
	require.NoError(t, rerr)
	assert.Equal(t, []byte("old-binary"), got)

	assert.Contains(t, u.Status().Error, "checksum mismatch")
	assert.False(t, u.Status().Done)
}

func TestApplyRejectsOlderRelease(t *testing.T) {
	dir := t.TempDir()

	execPath := filepath.Join(dir, "aghub")
	require.NoError(t, os.WriteFile(execPath, []byte("old-binary"), 0o755))

	archive := makeArchive(t, map[string][]byte{"aghub": []byte("new-binary")})
	name := assetName("v0.0.0")

	// 0.0.0 is not newer than the "dev" test version.
	srv := newFakeAPI(t, "v0.0.0", archive, name, false)
	defer srv.Close()

	u := &Updater{
		logger:   testLogger,
		client:   srv.Client(),
		repo:     "owner/aghub",
		api:      srv.URL,
		execPath: execPath,
		status:   &Status{Progress: -1},
	}

	err := u.Apply(t.Context())
	assert.ErrorContains(t, err, "no newer version")

	got, rerr := os.ReadFile(execPath)
	require.NoError(t, rerr)
	assert.Equal(t, []byte("old-binary"), got)
}

func TestCheckReportsMissingAsset(t *testing.T) {
	dir := t.TempDir()

	execPath := filepath.Join(dir, "aghub")
	require.NoError(t, os.WriteFile(execPath, []byte("old-binary"), 0o755))

	archive := makeArchive(t, map[string][]byte{"aghub": []byte("new-binary")})

	// The release has an asset, but not the one for this platform.
	srv := newFakeAPI(t, "v9.9.9", archive, "aghub_9.9.9_plan9_mips.tar.gz", false)
	defer srv.Close()

	u := &Updater{
		logger:   testLogger,
		client:   srv.Client(),
		repo:     "owner/aghub",
		api:      srv.URL,
		execPath: execPath,
		status:   &Status{Progress: -1},
	}

	res := u.Check(t.Context())
	require.Empty(t, res.Error)
	assert.True(t, res.HasUpdate)

	// An update exists, but it cannot be installed on this platform.
	assert.False(t, res.CanUpdate)
}
