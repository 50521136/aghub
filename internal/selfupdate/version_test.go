package selfupdate

import (
	"testing"

	"github.com/AdguardTeam/golibs/logutil/slogutil"
	"github.com/stretchr/testify/assert"
)

// testLogger is a logger that discards all messages.
var testLogger = slogutil.NewDiscardLogger()

func TestCompareVersions(t *testing.T) {
	testCases := []struct {
		name string
		a    string
		b    string
		want int
	}{{
		name: "equal",
		a:    "v1.0.0",
		b:    "v1.0.0",
		want: 0,
	}, {
		name: "equal_without_prefix",
		a:    "1.2.3",
		b:    "v1.2.3",
		want: 0,
	}, {
		name: "newer_major",
		a:    "v2.0.0",
		b:    "v1.9.9",
		want: 1,
	}, {
		name: "newer_minor",
		a:    "v1.10.0",
		b:    "v1.9.0",
		want: 1,
	}, {
		name: "newer_patch",
		a:    "v1.0.1",
		b:    "v1.0.0",
		want: 1,
	}, {
		name: "older",
		a:    "v0.107.0",
		b:    "v1.0.0",
		want: -1,
	}, {
		name: "prerelease_is_ignored",
		a:    "v1.0.0-rc.1",
		b:    "v1.0.0",
		want: 0,
	}, {
		name: "build_metadata_is_ignored",
		a:    "v1.0.0+meta",
		b:    "v1.0.0",
		want: 0,
	}, {
		name: "dev_is_zero",
		a:    "dev",
		b:    "v1.0.0",
		want: -1,
	}, {
		name: "empty_is_zero",
		a:    "",
		b:    "v0.0.1",
		want: -1,
	}, {
		name: "short_version",
		a:    "v2",
		b:    "v1.5.0",
		want: 1,
	}, {
		name: "whitespace",
		a:    " v1.2.3 ",
		b:    "v1.2.3",
		want: 0,
	}}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			assert.Equal(t, tc.want, compareVersions(tc.a, tc.b))
		})
	}
}

func TestParseVersion(t *testing.T) {
	assert.Equal(t, [3]int{1, 2, 3}, parseVersion("v1.2.3"))
	assert.Equal(t, [3]int{1, 2, 0}, parseVersion("1.2"))
	assert.Equal(t, [3]int{5, 0, 0}, parseVersion("5"))
	assert.Equal(t, [3]int{0, 0, 0}, parseVersion("dev"))
	assert.Equal(t, [3]int{1, 2, 3}, parseVersion("v1.2.3-beta.1+build.7"))
}

func TestAssetName(t *testing.T) {
	// The asset name is platform-dependent, but the prefix and the suffix are
	// not.
	name := assetName("v1.0.0")

	assert.NotContains(t, name, "v1.0.0")
	assert.Contains(t, name, "aghub_1.0.0_")
	assert.Contains(t, name, ".tar.gz")
}

func TestFormatVersion(t *testing.T) {
	assert.Equal(t, "v1.0.0", formatVersion("1.0.0"))
	assert.Equal(t, "v1.0.0", formatVersion("v1.0.0"))
	assert.Equal(t, "unknown", formatVersion(""))
}

func TestDescribeVersion(t *testing.T) {
	assert.Equal(t, "development build", describeVersion("dev"))
	assert.Equal(t, "development build", describeVersion(""))
	assert.Equal(t, "v1.0.0", describeVersion("1.0.0"))
}

func TestUpdaterDisabledWithoutRepo(t *testing.T) {
	t.Setenv(EnvRepo, "")

	u, err := New(&Config{Logger: testLogger})
	assert.NoError(t, err)
	assert.False(t, u.Enabled())
	assert.Empty(t, u.Repo())
	assert.Equal(t, Version, u.CurrentVersion())
	assert.NotEmpty(t, u.ExecPath())
}

func TestUpdaterRepoFromEnv(t *testing.T) {
	t.Setenv(EnvRepo, "owner/name")

	u, err := New(&Config{Logger: testLogger})
	assert.NoError(t, err)
	assert.True(t, u.Enabled())
	assert.Equal(t, "owner/name", u.Repo())
}

func TestUpdaterCheckWithoutRepo(t *testing.T) {
	t.Setenv(EnvRepo, "")

	u, err := New(&Config{Logger: testLogger})
	assert.NoError(t, err)

	res := u.Check(t.Context())
	assert.False(t, res.HasUpdate)
	assert.False(t, res.CanUpdate)
	assert.NotEmpty(t, res.Error)
	assert.Equal(t, Version, res.CurrentVersion)
}

func TestDownloadURL(t *testing.T) {
	t.Setenv(EnvRepo, "owner/name")

	u, err := New(&Config{Logger: testLogger})
	assert.NoError(t, err)
	assert.Equal(t, "https://example.com/a", u.downloadURL("https://example.com/a"))

	t.Setenv(EnvProxy, "https://gh-proxy.com/")

	u, err = New(&Config{Logger: testLogger})
	assert.NoError(t, err)
	assert.Equal(
		t,
		"https://gh-proxy.com/https://example.com/a",
		u.downloadURL("https://example.com/a"),
	)
}

func TestFindAsset(t *testing.T) {
	u := &Updater{}

	rel := &Release{
		Assets: []Asset{
			{Name: "checksums.txt"},
			{Name: "aghub_1.0.0_linux_amd64.tar.gz"},
		},
	}

	assert.NotNil(t, u.findAsset(rel, "checksums.txt"))
	assert.NotNil(t, u.findAsset(rel, "aghub_1.0.0_linux_amd64.tar.gz"))
	assert.Nil(t, u.findAsset(rel, "missing.tar.gz"))
	assert.NotNil(t, u.findChecksums(rel))
}
