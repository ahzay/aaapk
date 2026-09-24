package source

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"
)

type GitHub struct {
	name    string
	owner   string
	repo    string
	apps    []App
	fetched time.Time
}

func NewGitHub(name, repo string) *GitHub {
	owner, r := repo, ""
	if i := strings.IndexByte(repo, '/'); i >= 0 {
		owner, r = repo[:i], repo[i+1:]
	}
	return &GitHub{name: name, owner: owner, repo: r}
}

func (g *GitHub) Name() string { return g.name }

func (g *GitHub) apiURL() string {
	return fmt.Sprintf("https://api.github.com/repos/%s/%s/releases/latest", g.owner, g.repo)
}

type ghRelease struct {
	TagName string    `json:"tag_name"`
	Name    string    `json:"name"`
	Assets  []ghAsset `json:"assets"`
}

type ghAsset struct {
	Name               string `json:"name"`
	BrowserDownloadURL string `json:"browser_download_url"`
	Size               int64  `json:"size"`
}

func (g *GitHub) load() error {
	if g.apps != nil && time.Since(g.fetched) < time.Hour {
		return nil
	}
	if g.repo == "" {
		return fmt.Errorf("github %s: repo not set", g.name)
	}

	req, err := http.NewRequest("GET", g.apiURL(), nil)
	if err != nil {
		return fmt.Errorf("new request: %w", err)
	}
	req.Header.Set("Accept", "application/vnd.github+json")
	req.Header.Set("User-Agent", "aaapk")

	resp, err := (&http.Client{Timeout: 30 * time.Second}).Do(req)
	if err != nil {
		return fmt.Errorf("fetch %s: %w", g.apiURL(), err)
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return fmt.Errorf("read %s: %w", g.apiURL(), err)
	}
	if resp.StatusCode == 404 {
		return fmt.Errorf("github %s: repo or release not found", g.name)
	}
	if resp.StatusCode != 200 {
		return fmt.Errorf("github %s: http %d: %s", g.name, resp.StatusCode, trunc(string(body), 200))
	}

	var rel ghRelease
	if err := json.Unmarshal(body, &rel); err != nil {
		return fmt.Errorf("decode release: %w", err)
	}

	version := rel.TagName
	if version == "" {
		version = "unknown"
	}

	var apps []App
	for _, a := range rel.Assets {
		if !strings.HasSuffix(strings.ToLower(a.Name), ".apk") {
			continue
		}
		name := assetDisplayName(a.Name)
		apps = append(apps, App{
			PackageName: g.pkgName(a.Name),
			Name:        name,
			Version:     version,
			Summary:     g.name + " (GitHub release)",
			APKURL:      a.BrowserDownloadURL,
			Size:        a.Size,
			Source:      g.name,
			Added:       time.Now().UnixMilli(),
		})
	}
	if len(apps) == 0 {
		return fmt.Errorf("github %s: no apk assets in latest release %q", g.name, rel.TagName)
	}
	g.apps = apps
	g.fetched = time.Now()
	return nil
}

// pkgName builds a unique, stable identifier for an asset so the ledger and
// update flow can track each APK independently.
func (g *GitHub) pkgName(assetName string) string {
	base := g.owner + "." + g.repo
	trim := strings.TrimSuffix(assetName, ".apk")
	return base + ":" + trim
}

func assetDisplayName(assetName string) string {
	return strings.TrimSuffix(assetName, ".apk")
}

func (g *GitHub) Search(query string) ([]App, error) {
	if err := g.load(); err != nil {
		return nil, err
	}
	q := strings.ToLower(query)
	var out []App
	for _, a := range g.apps {
		if strings.Contains(strings.ToLower(a.Name), q) ||
			strings.Contains(strings.ToLower(a.Source), q) ||
			strings.Contains(strings.ToLower(a.PackageName), q) {
			out = append(out, a)
		}
	}
	return out, nil
}

func (g *GitHub) Resolve(pkg string) (*App, error) {
	if err := g.load(); err != nil {
		return nil, err
	}
	for i := range g.apps {
		if g.apps[i].PackageName == pkg {
			return &g.apps[i], nil
		}
	}
	return nil, nil
}

func (g *GitHub) Download(app App, dest string) (string, error) {
	if app.APKURL == "" {
		return "", fmt.Errorf("github %s: no url for %s", g.name, app.PackageName)
	}
	req, err := http.NewRequest("GET", app.APKURL, nil)
	if err != nil {
		return "", fmt.Errorf("new request: %w", err)
	}
	req.Header.Set("User-Agent", "aaapk")
	resp, err := (&http.Client{Timeout: 10 * time.Minute}).Do(req)
	if err != nil {
		return "", fmt.Errorf("github download %s: %w", app.PackageName, err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != 200 {
		return "", fmt.Errorf("github download %s: http %d", app.PackageName, resp.StatusCode)
	}
	if err := os.MkdirAll(dest, 0755); err != nil {
		return "", fmt.Errorf("mkdir %s: %w", dest, err)
	}
	path := filepath.Join(dest, fmt.Sprintf("%s-%s.apk", app.PackageName, app.Version))
	out, err := os.Create(path)
	if err != nil {
		return "", fmt.Errorf("create %s: %w", path, err)
	}
	defer out.Close()
	if _, err := io.Copy(out, resp.Body); err != nil {
		return "", fmt.Errorf("write %s: %w", path, err)
	}
	return path, nil
}

func (g *GitHub) Refresh() error {
	g.apps = nil
	return g.load()
}
