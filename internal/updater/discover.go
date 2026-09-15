package updater

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"runtime"
	"strings"
	"time"
)

const (
	DefaultGitHubRepo     = "Nerdy-Technician/NerdyAgent"
	githubReleasesAPITmpl = "https://api.github.com/repos/%s/releases/latest"
	githubDownloadBase    = "https://github.com/%s/releases/download"
)

// githubLatestURL can be swapped in tests.
var githubLatestURL = func(repo string) string {
	return fmt.Sprintf(githubReleasesAPITmpl, repo)
}

// Release is a candidate agent build discovered from the server or GitHub.
type Release struct {
	Version     string
	BinaryURL   string
	SHA256      string
	TrayURL     string
	TraySHA256  string
	ServiceName string
	Source      string
}

// DiscoverConfig controls where the updater looks for a newer build.
type DiscoverConfig struct {
	ServerURL  string
	Token      string
	GitHubRepo string
	HTTPClient *http.Client
}

// Discover polls the NerdyRMM server first, then GitHub Releases, and returns
// the newest candidate. It never returns a release older than currentVersion
// when currentVersion is non-empty; callers still compare before applying.
func Discover(ctx context.Context, currentVersion string, cfg DiscoverConfig) (Release, error) {
	client := cfg.HTTPClient
	if client == nil {
		client = &http.Client{Timeout: 20 * time.Second}
	}
	repo := strings.TrimSpace(cfg.GitHubRepo)
	if repo == "" {
		repo = DefaultGitHubRepo
	}

	var candidates []Release
	if r, err := fetchServerRelease(ctx, client, cfg.ServerURL, cfg.Token); err == nil && r.Version != "" {
		candidates = append(candidates, r)
	}
	if r, err := fetchGitHubRelease(ctx, client, repo); err == nil && r.Version != "" {
		candidates = append(candidates, r)
	}

	best, ok := pickNewest(candidates)
	if !ok {
		return Release{}, fmt.Errorf("no update source returned a version")
	}
	if strings.TrimSpace(currentVersion) != "" && CompareVersions(currentVersion, best.Version) > 0 {
		return Release{}, fmt.Errorf("discovered %s (%s) is older than current %s", best.Version, best.Source, currentVersion)
	}
	return best, nil
}

func pickNewest(releases []Release) (Release, bool) {
	var best Release
	found := false
	for _, r := range releases {
		r.Version = normalizeVersion(r.Version)
		if r.Version == "" {
			continue
		}
		if !found || CompareVersions(best.Version, r.Version) < 0 {
			best = r
			found = true
		}
	}
	return best, found
}

func fetchServerRelease(ctx context.Context, client *http.Client, serverURL, token string) (Release, error) {
	serverURL = strings.TrimRight(strings.TrimSpace(serverURL), "/")
	if serverURL == "" {
		return Release{}, fmt.Errorf("empty server url")
	}
	token = strings.TrimSpace(token)

	jsonPaths := []string{
		"/api/agent/latest-version",
		"/api/agent/update",
		"/api/agent/version",
	}
	var lastErr error
	if token != "" {
		for _, p := range jsonPaths {
			r, err := getJSONRelease(ctx, client, serverURL+p, token)
			if err != nil {
				lastErr = err
				continue
			}
			if r.Version == "" {
				continue
			}
			if r.BinaryURL == "" {
				r.BinaryURL = serverURL + "/downloads/" + AgentBinaryFilename()
			}
			if r.Source == "" {
				r.Source = "server"
			}
			return r, nil
		}
	}

	ver, err := getPlainVersion(ctx, client, serverURL+"/downloads/agent-version.txt")
	if err != nil {
		if lastErr != nil {
			return Release{}, fmt.Errorf("server latest-version: %v; agent-version.txt: %w", lastErr, err)
		}
		return Release{}, err
	}
	return Release{
		Version:   ver,
		BinaryURL: serverURL + "/downloads/" + AgentBinaryFilename(),
		Source:    "server-downloads",
	}, nil
}

func fetchGitHubRelease(ctx context.Context, client *http.Client, repo string) (Release, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, githubLatestURL(repo), nil)
	if err != nil {
		return Release{}, err
	}
	req.Header.Set("Accept", "application/vnd.github+json")
	req.Header.Set("User-Agent", "NerdyAgent-updater")
	resp, err := client.Do(req)
	if err != nil {
		return Release{}, err
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(io.LimitReader(resp.Body, 2<<20))
	if err != nil {
		return Release{}, err
	}
	if resp.StatusCode >= 300 {
		return Release{}, fmt.Errorf("github releases: status %d", resp.StatusCode)
	}
	var raw struct {
		TagName string `json:"tag_name"`
		Assets  []struct {
			Name               string `json:"name"`
			BrowserDownloadURL string `json:"browser_download_url"`
			Digest             string `json:"digest"`
		} `json:"assets"`
	}
	if err := json.Unmarshal(body, &raw); err != nil {
		return Release{}, err
	}
	ver := normalizeVersion(raw.TagName)
	if ver == "" {
		return Release{}, fmt.Errorf("github releases: empty tag_name")
	}
	wantBin := AgentBinaryFilename()
	wantTray := TrayBinaryFilename()
	r := Release{Version: ver, Source: "github"}
	sumsURL := ""
	for _, a := range raw.Assets {
		name := strings.TrimSpace(a.Name)
		switch {
		case name == wantBin:
			r.BinaryURL = strings.TrimSpace(a.BrowserDownloadURL)
			r.SHA256 = parseDigestSHA256(a.Digest)
		case name == wantTray:
			r.TrayURL = strings.TrimSpace(a.BrowserDownloadURL)
			r.TraySHA256 = parseDigestSHA256(a.Digest)
		case strings.EqualFold(name, "SHA256SUMS") || strings.EqualFold(name, "sha256sums.txt"):
			sumsURL = strings.TrimSpace(a.BrowserDownloadURL)
		}
	}
	if r.BinaryURL == "" {
		r.BinaryURL = fmt.Sprintf("%s/v%s/%s", fmt.Sprintf(githubDownloadBase, repo), ver, wantBin)
	}
	if r.SHA256 == "" && sumsURL != "" {
		if sums, err := getChecksumMap(ctx, client, sumsURL); err == nil {
			r.SHA256 = sums[wantBin]
			if r.TraySHA256 == "" {
				r.TraySHA256 = sums[wantTray]
			}
		}
	}
	return r, nil
}

func getJSONRelease(ctx context.Context, client *http.Client, url, token string) (Release, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return Release{}, err
	}
	req.Header.Set("Accept", "application/json")
	req.Header.Set("User-Agent", "NerdyAgent-updater")
	req.Header.Set("Authorization", "Bearer "+token)
	resp, err := client.Do(req)
	if err != nil {
		return Release{}, err
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if err != nil {
		return Release{}, err
	}
	if resp.StatusCode >= 300 {
		return Release{}, fmt.Errorf("%s: status %d", url, resp.StatusCode)
	}
	return parseReleaseJSON(body)
}

func parseReleaseJSON(body []byte) (Release, error) {
	var raw map[string]interface{}
	if err := json.Unmarshal(body, &raw); err != nil {
		return Release{}, err
	}
	r := Release{Source: "server"}
	r.Version = firstString(raw, "version", "latestVersion", "tag", "tag_name", "agentVersion")
	r.BinaryURL = firstString(raw, "binaryUrl", "binary_url", "url", "downloadUrl", "download_url", "browser_download_url")
	r.SHA256 = parseDigestSHA256(firstString(raw, "sha256", "checksum", "digest", "hash"))
	r.TrayURL = firstString(raw, "trayUrl", "tray_url", "trayBinaryUrl")
	r.TraySHA256 = parseDigestSHA256(firstString(raw, "traySha256", "tray_sha256"))
	r.ServiceName = firstString(raw, "serviceName", "service_name")
	if nested, ok := raw["payload"].(map[string]interface{}); ok {
		if r.Version == "" {
			r.Version = firstString(nested, "version", "latestVersion")
		}
		if r.BinaryURL == "" {
			r.BinaryURL = firstString(nested, "binaryUrl", "binary_url", "url")
		}
		if r.SHA256 == "" {
			r.SHA256 = parseDigestSHA256(firstString(nested, "sha256", "checksum", "digest"))
		}
		if r.ServiceName == "" {
			r.ServiceName = firstString(nested, "serviceName")
		}
	}
	r.Version = normalizeVersion(r.Version)
	if r.Version == "" {
		return Release{}, fmt.Errorf("latest-version payload missing version")
	}
	return r, nil
}

func getPlainVersion(ctx context.Context, client *http.Client, url string) (string, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return "", err
	}
	req.Header.Set("User-Agent", "NerdyAgent-updater")
	resp, err := client.Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()
	if resp.StatusCode >= 300 {
		return "", fmt.Errorf("%s: status %d", url, resp.StatusCode)
	}
	b, err := io.ReadAll(io.LimitReader(resp.Body, 256))
	if err != nil {
		return "", err
	}
	v := normalizeVersion(string(b))
	if v == "" {
		return "", fmt.Errorf("%s: empty version", url)
	}
	return v, nil
}

func getChecksumMap(ctx context.Context, client *http.Client, url string) (map[string]string, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("User-Agent", "NerdyAgent-updater")
	resp, err := client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode >= 300 {
		return nil, fmt.Errorf("checksums: status %d", resp.StatusCode)
	}
	b, err := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if err != nil {
		return nil, err
	}
	return parseSHA256SUMS(string(b)), nil
}

func parseSHA256SUMS(text string) map[string]string {
	out := map[string]string{}
	for _, line := range strings.Split(text, "\n") {
		line = strings.TrimSpace(line)
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		fields := strings.Fields(line)
		if len(fields) < 2 {
			continue
		}
		sum := strings.ToLower(strings.TrimPrefix(fields[0], "sha256:"))
		name := strings.TrimPrefix(fields[len(fields)-1], "*")
		if i := strings.LastIndex(name, "/"); i >= 0 {
			name = name[i+1:]
		}
		if sum != "" && name != "" {
			out[name] = sum
		}
	}
	return out
}

func parseDigestSHA256(v string) string {
	v = strings.TrimSpace(strings.ToLower(v))
	v = strings.TrimPrefix(v, "sha256:")
	v = strings.TrimPrefix(v, "sha-256:")
	if len(v) != 64 {
		return ""
	}
	for _, r := range v {
		if !((r >= '0' && r <= '9') || (r >= 'a' && r <= 'f')) {
			return ""
		}
	}
	return v
}

func firstString(m map[string]interface{}, keys ...string) string {
	for _, k := range keys {
		if v, ok := m[k]; ok {
			switch t := v.(type) {
			case string:
				if s := strings.TrimSpace(t); s != "" {
					return s
				}
			}
		}
	}
	return ""
}

// AgentBinaryFilename is the GitHub/server asset name for this OS/arch.
func AgentBinaryFilename() string {
	if runtime.GOOS == "windows" {
		if runtime.GOARCH == "arm64" {
			return "nerdyrmm-agent-windows-arm64.exe"
		}
		return "nerdyrmm-agent-windows-amd64.exe"
	}
	switch runtime.GOARCH {
	case "arm64":
		return "nerdyrmm-agent-linux-arm64"
	case "arm":
		return "nerdyrmm-agent-linux-armv7"
	default:
		return "nerdyrmm-agent-linux-amd64"
	}
}

// TrayBinaryFilename is an optional dedicated tray asset. The agent binary
// also accepts --tray, so this file is not required to update.
func TrayBinaryFilename() string {
	if runtime.GOOS == "windows" {
		if runtime.GOARCH == "arm64" {
			return "nerdyrmm-agent-tray-windows-arm64.exe"
		}
		return "nerdyrmm-agent-tray-windows-amd64.exe"
	}
	switch runtime.GOARCH {
	case "arm64":
		return "nerdyrmm-agent-tray-linux-arm64"
	case "arm":
		return "nerdyrmm-agent-tray-linux-armv7"
	default:
		return "nerdyrmm-agent-tray-linux-amd64"
	}
}
