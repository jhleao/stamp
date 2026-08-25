package updater

import (
	"archive/tar"
	"archive/zip"
	"bytes"
	"compress/gzip"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"time"
)

const (
	repository       = "jhleao/stamp"
	latestReleaseURL = "https://api.github.com/repos/" + repository + "/releases/latest"
	maxArchiveSize   = 128 << 20
	maxChecksumSize  = 1 << 20
)

type Release struct {
	Version   string
	PageURL   string
	Notes     string
	AssetName string
	AssetURL  string
	Checksums string
}

type Result struct {
	Current   string
	Latest    string
	Available bool
	PageURL   string
}

type githubRelease struct {
	TagName string `json:"tag_name"`
	HTMLURL string `json:"html_url"`
	Body    string `json:"body"`
	Assets  []struct {
		Name string `json:"name"`
		URL  string `json:"browser_download_url"`
	} `json:"assets"`
}

type cache struct {
	CheckedAt time.Time `json:"checkedAt"`
	Latest    string    `json:"latest"`
	PageURL   string    `json:"pageUrl"`
}

func Check(ctx context.Context, current string) (Result, error) {
	if !isReleaseVersion(current) {
		return Result{}, fmt.Errorf("%q is a development build; update it from its source checkout", current)
	}
	release, err := latest(ctx, releaseClient())
	if err != nil {
		return Result{}, err
	}
	return result(current, release.Version, release.PageURL), nil
}

// StartupCheck returns a cached result when possible and limits a stale network
// refresh so update discovery never becomes a meaningful startup dependency.
func StartupCheck(current string) (Result, error) {
	if !isReleaseVersion(current) || os.Getenv("STAMP_NO_UPDATE_CHECK") != "" {
		return Result{}, nil
	}
	path, err := cachePath()
	if err != nil {
		return Result{}, err
	}
	if saved, err := readCache(path); err == nil && time.Since(saved.CheckedAt) < 24*time.Hour {
		return result(current, saved.Latest, saved.PageURL), nil
	}
	ctx, cancel := context.WithTimeout(context.Background(), 1200*time.Millisecond)
	defer cancel()
	checked, err := Check(ctx, current)
	if err != nil {
		_ = writeCache(path, cache{CheckedAt: time.Now()})
		return Result{}, err
	}
	_ = writeCache(path, cache{CheckedAt: time.Now(), Latest: checked.Latest, PageURL: checked.PageURL})
	return checked, nil
}

func Install(ctx context.Context, current, expected string) (Release, bool, error) {
	if !isReleaseVersion(current) {
		return Release{}, false, fmt.Errorf("%q is a development build; update it from its source checkout", current)
	}
	if runtime.GOOS != "darwin" && runtime.GOOS != "linux" {
		return Release{}, false, fmt.Errorf("self-update is not supported on %s yet", runtime.GOOS)
	}
	client := releaseClient()
	release, err := latest(ctx, client)
	if err != nil {
		return Release{}, false, err
	}
	if compareVersions(release.Version, current) <= 0 {
		return release, false, nil
	}
	if release.Version != expected {
		return Release{}, false, fmt.Errorf("latest release changed from %s to %s; review the update again", expected, release.Version)
	}
	executable, err := os.Executable()
	if err != nil {
		return Release{}, false, fmt.Errorf("locate Stamp executable: %w", err)
	}
	executable, err = filepath.EvalSymlinks(executable)
	if err != nil {
		return Release{}, false, fmt.Errorf("resolve Stamp executable: %w", err)
	}
	if err := install(ctx, client, release, executable); err != nil {
		return Release{}, false, err
	}
	return release, true, nil
}

func latest(ctx context.Context, client *http.Client) (Release, error) {
	var payload githubRelease
	if err := getJSON(ctx, client, latestReleaseURL, &payload); err != nil {
		return Release{}, fmt.Errorf("check GitHub releases: %w", err)
	}
	version := strings.TrimPrefix(payload.TagName, "v")
	if !isReleaseVersion(version) {
		return Release{}, fmt.Errorf("GitHub returned invalid release version %q", payload.TagName)
	}
	asset := "stamp_" + version + "_" + runtime.GOOS + "_" + runtime.GOARCH
	if runtime.GOOS == "windows" {
		asset += ".zip"
	} else {
		asset += ".tar.gz"
	}
	release := Release{Version: version, PageURL: payload.HTMLURL, Notes: payload.Body, AssetName: asset}
	for _, candidate := range payload.Assets {
		switch candidate.Name {
		case asset:
			release.AssetURL = candidate.URL
		case "checksums.txt":
			release.Checksums = candidate.URL
		}
	}
	if release.AssetURL == "" || release.Checksums == "" {
		return Release{}, fmt.Errorf("release %s does not contain %s and checksums.txt", version, asset)
	}
	if !trustedReleaseURL(release.AssetURL) || !trustedReleaseURL(release.Checksums) {
		return Release{}, errors.New("GitHub returned an untrusted release download URL")
	}
	return release, nil
}

func install(ctx context.Context, client *http.Client, release Release, executable string) error {
	temporary, err := os.MkdirTemp(filepath.Dir(executable), ".stamp-update-")
	if err != nil {
		if os.IsPermission(err) {
			return fmt.Errorf("%s is not user-writable; install Stamp in $HOME/.local/bin or update it with the tool that installed it", executable)
		}
		return fmt.Errorf("prepare update beside %s: %w", executable, err)
	}
	defer os.RemoveAll(temporary)

	checksums, err := download(ctx, client, release.Checksums, maxChecksumSize)
	if err != nil {
		return fmt.Errorf("download checksums: %w", err)
	}
	want, err := checksumFor(checksums, release.AssetName)
	if err != nil {
		return err
	}
	archive, err := download(ctx, client, release.AssetURL, maxArchiveSize)
	if err != nil {
		return fmt.Errorf("download %s: %w", release.AssetName, err)
	}
	got := sha256.Sum256(archive)
	if hex.EncodeToString(got[:]) != want {
		return errors.New("downloaded archive failed SHA-256 verification")
	}

	candidate := filepath.Join(temporary, "stamp.new")
	if err := extractBinary(archive, release.AssetName, candidate); err != nil {
		return err
	}
	if err := os.Chmod(candidate, 0o755); err != nil {
		return fmt.Errorf("make update executable: %w", err)
	}
	if output, err := exec.Command(candidate, "version").CombinedOutput(); err != nil || strings.TrimSpace(string(output)) != release.Version {
		return fmt.Errorf("downloaded binary did not report version %s", release.Version)
	}
	return replace(executable, candidate)
}

func replace(executable, candidate string) error {
	backup := filepath.Join(filepath.Dir(candidate), "stamp.previous")
	if err := os.Rename(executable, backup); err != nil {
		return fmt.Errorf("replace %s: %w", executable, err)
	}
	if err := os.Rename(candidate, executable); err != nil {
		_ = os.Rename(backup, executable)
		return fmt.Errorf("install update: %w", err)
	}
	_ = os.Remove(backup)
	return nil
}

func extractBinary(archive []byte, name, destination string) error {
	if strings.HasSuffix(name, ".tar.gz") {
		compressed, err := gzip.NewReader(bytes.NewReader(archive))
		if err != nil {
			return fmt.Errorf("open update archive: %w", err)
		}
		defer compressed.Close()
		reader := tar.NewReader(compressed)
		for {
			header, err := reader.Next()
			if errors.Is(err, io.EOF) {
				break
			}
			if err != nil {
				return fmt.Errorf("read update archive: %w", err)
			}
			if header.Typeflag == tar.TypeReg && filepath.Base(header.Name) == "stamp" {
				return copyLimited(destination, reader, header.Size)
			}
		}
	} else if strings.HasSuffix(name, ".zip") {
		reader, err := zip.NewReader(bytes.NewReader(archive), int64(len(archive)))
		if err != nil {
			return fmt.Errorf("open update archive: %w", err)
		}
		for _, file := range reader.File {
			if filepath.Base(file.Name) != "stamp.exe" {
				continue
			}
			input, err := file.Open()
			if err != nil {
				return err
			}
			err = copyLimited(destination, input, int64(file.UncompressedSize64))
			input.Close()
			return err
		}
	}
	return errors.New("update archive does not contain the Stamp binary")
}

func copyLimited(destination string, source io.Reader, size int64) error {
	if size <= 0 || size > maxArchiveSize {
		return errors.New("binary in update archive has an invalid size")
	}
	output, err := os.OpenFile(destination, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0o755)
	if err != nil {
		return err
	}
	written, copyErr := io.CopyN(output, source, size)
	closeErr := output.Close()
	if copyErr != nil || written != size {
		return errors.New("update archive contains a truncated binary")
	}
	return closeErr
}

func checksumFor(data []byte, name string) (string, error) {
	for _, line := range strings.Split(string(data), "\n") {
		fields := strings.Fields(line)
		if len(fields) != 2 {
			continue
		}
		listedName := strings.TrimPrefix(strings.TrimPrefix(fields[1], "*"), "./")
		if listedName == name {
			if len(fields[0]) != sha256.Size*2 {
				break
			}
			if _, err := hex.DecodeString(fields[0]); err == nil {
				return strings.ToLower(fields[0]), nil
			}
		}
	}
	return "", fmt.Errorf("checksums.txt does not contain a valid checksum for %s", name)
}

func trustedReleaseURL(value string) bool {
	parsed, err := url.Parse(value)
	return err == nil && parsed.Scheme == "https" && parsed.Hostname() == "github.com"
}

func releaseClient() *http.Client {
	return &http.Client{
		Timeout: 2 * time.Minute,
		CheckRedirect: func(request *http.Request, _ []*http.Request) error {
			host := request.URL.Hostname()
			if request.URL.Scheme != "https" || (host != "github.com" && !strings.HasSuffix(host, ".githubusercontent.com")) {
				return errors.New("release download redirected outside GitHub")
			}
			return nil
		},
	}
}

func download(ctx context.Context, client *http.Client, url string, limit int64) ([]byte, error) {
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, err
	}
	request.Header.Set("User-Agent", "stamp-updater")
	response, err := client.Do(request)
	if err != nil {
		return nil, err
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("%s returned %s", url, response.Status)
	}
	data, err := io.ReadAll(io.LimitReader(response.Body, limit+1))
	if err != nil {
		return nil, err
	}
	if int64(len(data)) > limit {
		return nil, errors.New("download exceeded size limit")
	}
	return data, nil
}

func getJSON(ctx context.Context, client *http.Client, url string, destination any) error {
	data, err := download(ctx, client, url, maxChecksumSize)
	if err != nil {
		return err
	}
	return json.Unmarshal(data, destination)
}

func result(current, latest, pageURL string) Result {
	return Result{Current: current, Latest: latest, Available: compareVersions(latest, current) > 0, PageURL: pageURL}
}

func isReleaseVersion(value string) bool {
	_, ok := parseVersion(value)
	return ok && !strings.Contains(value, "-") && !strings.Contains(value, "+")
}

func compareVersions(left, right string) int {
	l, lok := parseVersion(left)
	r, rok := parseVersion(right)
	if !lok || !rok {
		return 0
	}
	for i := range l {
		if l[i] < r[i] {
			return -1
		}
		if l[i] > r[i] {
			return 1
		}
	}
	return 0
}

func parseVersion(value string) ([3]int, bool) {
	var parsed [3]int
	parts := strings.Split(strings.TrimPrefix(value, "v"), ".")
	if len(parts) != 3 {
		return parsed, false
	}
	for index, part := range parts {
		number, err := strconv.Atoi(part)
		if err != nil || number < 0 {
			return parsed, false
		}
		parsed[index] = number
	}
	return parsed, true
}

func cachePath() (string, error) {
	directory, err := os.UserConfigDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(directory, "Stamp", "update.json"), nil
}

func readCache(path string) (cache, error) {
	var saved cache
	data, err := os.ReadFile(path)
	if err != nil {
		return saved, err
	}
	err = json.Unmarshal(data, &saved)
	return saved, err
}

func writeCache(path string, saved cache) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return err
	}
	data, err := json.Marshal(saved)
	if err != nil {
		return err
	}
	return os.WriteFile(path, data, 0o600)
}
