// Package selfupdate implements the online update of the application from
// GitHub releases.
package selfupdate

import (
	"archive/tar"
	"compress/gzip"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"sync/atomic"
	"time"
)

// These variables are set by the linker during the release build.
var (
	// Version is the version of the application, for example "v1.0.0".
	Version = "dev"

	// Repo is the GitHub repository in the "owner/name" format.
	Repo = ""

	// ProxyURL is an optional URL prefix used to reach GitHub, for example
	// "https://gh-proxy.com/".  It is used for the download URLs only.
	ProxyURL = ""
)

// Environment variables that override the build-time values.
const (
	// EnvRepo overrides the GitHub repository.
	EnvRepo = "AGHUB_UPDATE_REPO"

	// EnvProxy overrides the download proxy prefix.
	EnvProxy = "AGHUB_UPDATE_PROXY"

	// EnvToken is a GitHub API token used for private repositories.
	EnvToken = "AGHUB_UPDATE_TOKEN"

	// EnvAPI overrides the GitHub API base URL.  It is useful for GitHub
	// Enterprise installations and for mirrors.
	EnvAPI = "AGHUB_UPDATE_API"
)

// githubAPI is the default base URL of the GitHub API.
const githubAPI = "https://api.github.com"

// assetPrefix is the prefix of the release assets.
const assetPrefix = "aghub"

// checkTimeout is the timeout of a version check request.
const checkTimeout = 30 * time.Second

// downloadTimeout is the timeout of an asset download.
const downloadTimeout = 15 * time.Minute

// Asset is a release asset.
type Asset struct {
	// Name is the file name of the asset.
	Name string `json:"name"`

	// URL is the download URL of the asset.
	URL string `json:"url"`

	// Size is the size of the asset in bytes.
	Size int64 `json:"size"`
}

// Release is the information about a GitHub release.
type Release struct {
	// PublishedAt is the time of the publication of the release.
	PublishedAt time.Time `json:"published_at"`

	// Version is the version of the release without the leading "v".
	Version string `json:"version"`

	// TagName is the tag of the release.
	TagName string `json:"tag_name"`

	// Name is the name of the release.
	Name string `json:"name"`

	// Notes is the body of the release.
	Notes string `json:"notes"`

	// URL is the HTML URL of the release.
	URL string `json:"url"`

	// Assets are the assets of the release.
	Assets []Asset `json:"assets"`
}

// CheckResult is the result of a version check.
type CheckResult struct {
	// CheckedAt is the time of the check.
	CheckedAt time.Time `json:"checked_at"`

	// CurrentVersion is the version of the running application.
	CurrentVersion string `json:"current_version"`

	// Latest is the latest available release, if any.
	Latest *Release `json:"latest,omitempty"`

	// Repo is the GitHub repository used for the check.
	Repo string `json:"repo"`

	// Error is the error message of a failed check, if any.
	Error string `json:"error,omitempty"`

	// HasUpdate is true if a newer version is available.
	HasUpdate bool `json:"has_update"`

	// UpdateAvailable is true if the check succeeded and an update can be
	// installed.
	CanUpdate bool `json:"can_update"`
}

// Status is the state of an update in progress.
type Status struct {
	// StartedAt is the time the update started.
	StartedAt time.Time `json:"started_at"`

	// Version is the version being installed.
	Version string `json:"version"`

	// Message is the current step of the update.
	Message string `json:"message"`

	// Error is the error message of a failed update, if any.
	Error string `json:"error,omitempty"`

	// Progress is the download progress in the range 0..1, or -1 if unknown.
	Progress float64 `json:"progress"`

	// Running is true while the update is in progress.
	Running bool `json:"running"`

	// Done is true when the update has finished successfully.
	Done bool `json:"done"`
}

// Updater checks for and installs updates from GitHub releases.  It is safe
// for concurrent use.
type Updater struct {
	logger *slog.Logger
	client *http.Client

	repo  string
	token string
	api   string

	// proxy is the acceleration prefix.  It may be changed at runtime from
	// the web UI, so it is read through an atomic pointer.  An empty string
	// means a direct connection.
	proxy atomic.Pointer[string]

	// execPath is the path of the running executable.
	execPath string

	mu     sync.Mutex
	status *Status
}

// Config is the configuration of an Updater.
type Config struct {
	// Logger is used for logging.  It must not be nil.
	Logger *slog.Logger

	// ExecPath is the path of the running executable.  If empty, it is
	// detected from [os.Executable].
	ExecPath string
}

// New creates a new updater.  It does not perform any network requests.
func New(cfg *Config) (u *Updater, err error) {
	if cfg == nil || cfg.Logger == nil {
		return nil, fmt.Errorf("selfupdate: logger is nil")
	}

	execPath := cfg.ExecPath
	if execPath == "" {
		execPath, err = os.Executable()
		if err != nil {
			return nil, fmt.Errorf("selfupdate: getting executable path: %w", err)
		}
	}

	repo := os.Getenv(EnvRepo)
	if repo == "" {
		repo = Repo
	}

	proxy := os.Getenv(EnvProxy)
	if proxy == "" {
		proxy = ProxyURL
	}

	proxy = NormalizeProxy(proxy)

	api := os.Getenv(EnvAPI)
	if api == "" {
		api = githubAPI
	}

	u = &Updater{
		logger:   cfg.Logger.With("prefix", "selfupdate"),
		client:   &http.Client{Timeout: downloadTimeout},
		repo:     repo,
		token:    os.Getenv(EnvToken),
		api:      strings.TrimRight(api, "/"),
		execPath: execPath,
		status:   &Status{Progress: -1},
	}

	u.proxy.Store(&proxy)

	return u, nil
}

// Proxy returns the acceleration prefix in use, or an empty string when
// requests go directly to GitHub.
func (u *Updater) Proxy() (prefix string) {
	p := u.proxy.Load()
	if p == nil {
		return ""
	}

	return *p
}

// SetProxy replaces the acceleration prefix.  An empty prefix disables the
// acceleration.
func (u *Updater) SetProxy(prefix string) {
	prefix = NormalizeProxy(prefix)
	u.proxy.Store(&prefix)
}

// Repo returns the GitHub repository used for updates.
func (u *Updater) Repo() (repo string) {
	return u.repo
}

// ExecPath returns the path of the running executable.
func (u *Updater) ExecPath() (path string) {
	return u.execPath
}

// apiBase returns the base URL of the GitHub API.
func (u *Updater) apiBase() (base string) {
	if u.api == "" {
		return githubAPI
	}

	return u.api
}

// CurrentVersion returns the version of the running application.
func (u *Updater) CurrentVersion() (v string) {
	return Version
}

// Enabled returns true if the updater is configured with a repository.
func (u *Updater) Enabled() (ok bool) {
	return u.repo != ""
}

// Status returns the current state of the update.
func (u *Updater) Status() (s Status) {
	u.mu.Lock()
	defer u.mu.Unlock()

	return *u.status
}

// setStatus updates the state of the update.
func (u *Updater) setStatus(f func(s *Status)) {
	u.mu.Lock()
	defer u.mu.Unlock()

	f(u.status)
}

// assetName returns the name of the release asset for the current platform.
func assetName(version string) (name string) {
	v := strings.TrimPrefix(version, "v")

	return fmt.Sprintf("%s_%s_%s_%s.tar.gz", assetPrefix, v, runtime.GOOS, runtime.GOARCH)
}

// Check queries GitHub for the latest release and compares it with the running
// version.
func (u *Updater) Check(ctx context.Context) (res *CheckResult) {
	res = &CheckResult{
		CheckedAt:      time.Now(),
		CurrentVersion: Version,
		Repo:           u.repo,
	}

	if !u.Enabled() {
		res.Error = "update repository is not configured"

		return res
	}

	reqCtx, cancel := context.WithTimeout(ctx, checkTimeout)
	defer cancel()

	rel, err := u.latestRelease(reqCtx)
	if err != nil {
		res.Error = err.Error()

		return res
	}

	res.Latest = rel
	res.HasUpdate = compareVersions(rel.Version, Version) > 0
	res.CanUpdate = res.HasUpdate && u.findAsset(rel, assetName(rel.Version)) != nil

	return res
}

// latestRelease returns the latest release of the repository.
func (u *Updater) latestRelease(ctx context.Context) (rel *Release, err error) {
	url := fmt.Sprintf("%s/repos/%s/releases/latest", u.apiBase(), u.repo)

	body, err := u.get(ctx, url)
	if err != nil {
		return nil, err
	}
	defer func() { _ = body.Close() }()

	raw := &struct {
		PublishedAt time.Time `json:"published_at"`
		TagName     string    `json:"tag_name"`
		Name        string    `json:"name"`
		Body        string    `json:"body"`
		HTMLURL     string    `json:"html_url"`
		Assets      []struct {
			Name               string `json:"name"`
			BrowserDownloadURL string `json:"browser_download_url"`
			Size               int64  `json:"size"`
		} `json:"assets"`
	}{}

	err = json.NewDecoder(body).Decode(raw)
	if err != nil {
		return nil, fmt.Errorf("decoding release: %w", err)
	}

	rel = &Release{
		TagName:     raw.TagName,
		Name:        raw.Name,
		Version:     strings.TrimPrefix(raw.TagName, "v"),
		Notes:       raw.Body,
		URL:         raw.HTMLURL,
		PublishedAt: raw.PublishedAt,
	}

	for _, a := range raw.Assets {
		rel.Assets = append(rel.Assets, Asset{
			Name: a.Name,
			URL:  a.BrowserDownloadURL,
			Size: a.Size,
		})
	}

	return rel, nil
}

// findAsset returns the asset with the given name, if any.
func (u *Updater) findAsset(rel *Release, name string) (asset *Asset) {
	for i := range rel.Assets {
		if rel.Assets[i].Name == name {
			return &rel.Assets[i]
		}
	}

	return nil
}

// findChecksums returns the checksums asset, if any.
func (u *Updater) findChecksums(rel *Release) (asset *Asset) {
	for i := range rel.Assets {
		if rel.Assets[i].Name == "checksums.txt" {
			return &rel.Assets[i]
		}
	}

	return nil
}

// get performs a GET request to the GitHub API.  A configured acceleration
// proxy is tried first, because api.github.com is unreachable from some
// networks; most proxies serve release downloads only, though, so a direct
// request is used as a fallback.
func (u *Updater) get(ctx context.Context, url string) (body io.ReadCloser, err error) {
	proxy := u.Proxy()
	if proxy == "" {
		return u.getDirect(ctx, url)
	}

	body, perr := u.getDirect(ctx, proxy+"/"+url)
	if perr == nil {
		return body, nil
	}

	body, derr := u.getDirect(ctx, url)
	if derr != nil {
		return nil, fmt.Errorf("via proxy: %w; directly: %w", perr, derr)
	}

	return body, nil
}

// getDirect performs a GET request to url without acceleration, adding the
// authorization header when a token is configured.
func (u *Updater) getDirect(
	ctx context.Context,
	url string,
) (body io.ReadCloser, err error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, fmt.Errorf("creating request: %w", err)
	}

	req.Header.Set("Accept", "application/vnd.github+json")
	req.Header.Set("User-Agent", "aghub-selfupdate/"+Version)

	if u.token != "" {
		req.Header.Set("Authorization", "Bearer "+u.token)
	}

	resp, err := u.client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("requesting %s: %w", url, err)
	}

	if resp.StatusCode != http.StatusOK {
		_ = resp.Body.Close()

		return nil, fmt.Errorf("requesting %s: unexpected status %s", url, resp.Status)
	}

	return resp.Body, nil
}

// downloadURL returns the URL to use for downloading an asset, applying the
// configured proxy prefix.
func (u *Updater) downloadURL(url string) (out string) {
	proxy := u.Proxy()
	if proxy == "" {
		return url
	}

	return proxy + "/" + url
}

// Apply downloads the latest release, verifies it, replaces the running
// executable and restarts the application.  It returns once the download and
// the replacement are done; the restart terminates the process.
//
// The caller is expected to have checked that an update is available.
func (u *Updater) Apply(ctx context.Context) (err error) {
	if !u.Enabled() {
		return fmt.Errorf("selfupdate: update repository is not configured")
	}

	u.setStatus(func(s *Status) {
		*s = Status{StartedAt: time.Now(), Running: true, Progress: -1}
	})

	defer func() {
		u.setStatus(func(s *Status) {
			s.Running = false
			s.Error = ""
			if err != nil {
				s.Error = err.Error()
			}
		})
	}()

	u.setStatus(func(s *Status) { s.Message = "checking for updates" })

	rel, err := u.latestRelease(ctx)
	if err != nil {
		return err
	}

	if compareVersions(rel.Version, Version) <= 0 {
		return fmt.Errorf("selfupdate: no newer version than %s", Version)
	}

	u.setStatus(func(s *Status) {
		s.Version = rel.Version
		s.Message = "downloading " + rel.Version
	})

	name := assetName(rel.Version)
	asset := u.findAsset(rel, name)
	if asset == nil {
		return fmt.Errorf("selfupdate: release %s has no asset %q", rel.Version, name)
	}

	dir, err := os.MkdirTemp("", "aghub-update-*")
	if err != nil {
		return fmt.Errorf("selfupdate: creating temporary directory: %w", err)
	}

	defer func() { _ = os.RemoveAll(dir) }()

	archivePath := filepath.Join(dir, name)

	err = u.download(ctx, asset, archivePath)
	if err != nil {
		return err
	}

	err = u.verifyChecksums(ctx, rel, name, archivePath)
	if err != nil {
		return err
	}

	u.setStatus(func(s *Status) { s.Message = "installing " + rel.Version })

	binPath, err := extractBinary(archivePath, dir, filepath.Base(u.execPath))
	if err != nil {
		return err
	}

	err = replaceExecutable(binPath, u.execPath)
	if err != nil {
		return err
	}

	u.logger.InfoContext(ctx, "update installed", "version", rel.Version, "path", u.execPath)

	u.setStatus(func(s *Status) {
		s.Message = "restarting"
		s.Done = true
		s.Progress = 1
	})

	return nil
}

// download downloads the asset into path.
func (u *Updater) download(ctx context.Context, asset *Asset, path string) (err error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, u.downloadURL(asset.URL), nil)
	if err != nil {
		return fmt.Errorf("selfupdate: creating download request: %w", err)
	}

	req.Header.Set("User-Agent", "aghub-selfupdate/"+Version)
	if u.token != "" {
		req.Header.Set("Authorization", "Bearer "+u.token)
	}

	resp, err := u.client.Do(req)
	if err != nil {
		return fmt.Errorf("selfupdate: downloading: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("selfupdate: downloading: unexpected status %s", resp.Status)
	}

	f, err := os.Create(path)
	if err != nil {
		return fmt.Errorf("selfupdate: creating file: %w", err)
	}

	defer func() {
		cerr := f.Close()
		if err == nil {
			err = cerr
		}
	}()

	total := asset.Size
	if total <= 0 {
		total = resp.ContentLength
	}

	written := int64(0)

	buf := make([]byte, 64*1024)
	for {
		var n int
		n, err = resp.Body.Read(buf)
		if n > 0 {
			_, werr := f.Write(buf[:n])
			if werr != nil {
				return fmt.Errorf("selfupdate: writing file: %w", werr)
			}

			written += int64(n)

			if total > 0 {
				u.setStatus(func(s *Status) {
					s.Progress = float64(written) / float64(total)
				})
			}
		}

		if err == io.EOF {
			err = nil

			break
		}

		if err != nil {
			return fmt.Errorf("selfupdate: reading response: %w", err)
		}
	}

	return nil
}

// verifyChecksums verifies the SHA-256 checksum of the downloaded archive
// against the checksums.txt asset of the release.  A missing checksums.txt is
// not an error, but it is logged.
func (u *Updater) verifyChecksums(
	ctx context.Context,
	rel *Release,
	name string,
	path string,
) (err error) {
	asset := u.findChecksums(rel)
	if asset == nil {
		u.logger.WarnContext(ctx, "release has no checksums.txt, skipping verification")

		return nil
	}

	body, err := u.get(ctx, asset.URL)
	if err != nil {
		return fmt.Errorf("selfupdate: fetching checksums: %w", err)
	}
	defer func() { _ = body.Close() }()

	data, err := io.ReadAll(io.LimitReader(body, 1<<20))
	if err != nil {
		return fmt.Errorf("selfupdate: reading checksums: %w", err)
	}

	var want string

	for _, line := range strings.Split(string(data), "\n") {
		fields := strings.Fields(line)
		if len(fields) != 2 {
			continue
		}

		if strings.TrimPrefix(fields[1], "*") == name {
			want = strings.ToLower(fields[0])

			break
		}
	}

	if want == "" {
		return fmt.Errorf("selfupdate: no checksum for %q", name)
	}

	got, err := fileSHA256(path)
	if err != nil {
		return err
	}

	if got != want {
		return fmt.Errorf("selfupdate: checksum mismatch for %q: got %s, want %s", name, got, want)
	}

	u.logger.InfoContext(ctx, "checksum verified", "asset", name)

	return nil
}

// fileSHA256 returns the hex-encoded SHA-256 checksum of a file.
func fileSHA256(path string) (sum string, err error) {
	f, err := os.Open(path)
	if err != nil {
		return "", fmt.Errorf("selfupdate: opening file: %w", err)
	}
	defer func() { _ = f.Close() }()

	h := sha256.New()

	_, err = io.Copy(h, f)
	if err != nil {
		return "", fmt.Errorf("selfupdate: hashing file: %w", err)
	}

	return hex.EncodeToString(h.Sum(nil)), nil
}

// extractBinary extracts the executable named binName from the tar.gz archive
// into dir and returns its path.  When binName is not present, the first
// regular executable file is used.
func extractBinary(archivePath, dir, binName string) (path string, err error) {
	f, err := os.Open(archivePath)
	if err != nil {
		return "", fmt.Errorf("selfupdate: opening archive: %w", err)
	}
	defer func() { _ = f.Close() }()

	gz, err := gzip.NewReader(f)
	if err != nil {
		return "", fmt.Errorf("selfupdate: reading gzip: %w", err)
	}
	defer func() { _ = gz.Close() }()

	tr := tar.NewReader(gz)

	var fallback string

	for {
		hdr, herr := tr.Next()
		if herr == io.EOF {
			break
		}

		if herr != nil {
			return "", fmt.Errorf("selfupdate: reading archive: %w", herr)
		}

		if hdr.Typeflag != tar.TypeReg {
			continue
		}

		name := filepath.Base(hdr.Name)
		if name == "" || name == "." {
			continue
		}

		// Guard against path traversal.
		if strings.Contains(hdr.Name, "..") {
			continue
		}

		out, werr := writeEntry(tr, dir, name, hdr.FileInfo().Mode())
		if werr != nil {
			return "", werr
		}

		if name == binName {
			return out, nil
		}

		if fallback == "" && hdr.FileInfo().Mode().Perm()&0o111 != 0 {
			fallback = out
		}
	}

	if fallback == "" {
		return "", fmt.Errorf("selfupdate: no executable found in %q", filepath.Base(archivePath))
	}

	return fallback, nil
}

// writeEntry writes the current archive entry into dir and returns its path.
func writeEntry(r io.Reader, dir, name string, mode os.FileMode) (path string, err error) {
	out := filepath.Join(dir, "new-"+name)

	f, err := os.OpenFile(out, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, mode.Perm()|0o600)
	if err != nil {
		return "", fmt.Errorf("selfupdate: creating file: %w", err)
	}

	defer func() {
		cerr := f.Close()
		if err == nil {
			err = cerr
		}
	}()

	_, err = io.Copy(f, io.LimitReader(r, 512<<20))
	if err != nil {
		return "", fmt.Errorf("selfupdate: extracting file: %w", err)
	}

	return out, nil
}

// replaceExecutable replaces the executable at dst with the file at src,
// keeping a backup of the previous binary next to it.
func replaceExecutable(src, dst string) (err error) {
	dir := filepath.Dir(dst)

	tmp, err := os.CreateTemp(dir, ".aghub-new-*")
	if err != nil {
		return fmt.Errorf("selfupdate: creating temporary file: %w", err)
	}

	tmpName := tmp.Name()

	defer func() {
		_ = os.Remove(tmpName)
	}()

	srcFile, err := os.Open(src)
	if err != nil {
		_ = tmp.Close()

		return fmt.Errorf("selfupdate: opening new binary: %w", err)
	}
	defer func() { _ = srcFile.Close() }()

	_, err = io.Copy(tmp, srcFile)
	if err != nil {
		_ = tmp.Close()

		return fmt.Errorf("selfupdate: copying new binary: %w", err)
	}

	err = tmp.Sync()
	if err != nil {
		_ = tmp.Close()

		return fmt.Errorf("selfupdate: syncing new binary: %w", err)
	}

	err = tmp.Close()
	if err != nil {
		return fmt.Errorf("selfupdate: closing new binary: %w", err)
	}

	err = os.Chmod(tmpName, 0o755)
	if err != nil {
		return fmt.Errorf("selfupdate: chmod new binary: %w", err)
	}

	// Keep a backup so that a manual rollback stays possible.
	backup := dst + ".bak"

	if _, serr := os.Stat(dst); serr == nil {
		_ = os.Remove(backup)

		if cerr := copyFile(dst, backup, 0o755); cerr != nil {
			return fmt.Errorf("selfupdate: backing up the current binary: %w", cerr)
		}
	}

	err = os.Rename(tmpName, dst)
	if err != nil {
		return fmt.Errorf("selfupdate: replacing the binary: %w", err)
	}

	return nil
}

// copyFile copies src to dst with the given permissions.
func copyFile(src, dst string, perm os.FileMode) (err error) {
	in, err := os.Open(src)
	if err != nil {
		return err
	}
	defer func() { _ = in.Close() }()

	out, err := os.OpenFile(dst, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, perm)
	if err != nil {
		return err
	}

	defer func() {
		cerr := out.Close()
		if err == nil {
			err = cerr
		}
	}()

	_, err = io.Copy(out, in)

	return err
}
