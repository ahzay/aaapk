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

type FDroid struct {
	name     string
	baseURL  string
	cacheDir string
	index    *fdroidIndex
}

func NewFDroid(name, baseURL, cacheDir string) *FDroid {
	return &FDroid{
		name:     name,
		baseURL:  strings.TrimRight(baseURL, "/"),
		cacheDir: cacheDir,
	}
}

func (f *FDroid) Name() string { return f.name }

type fdroidIndex struct {
	Apps     []fdroidApp                `json:"apps"`
	Packages map[string][]fdroidPackage `json:"packages"`
}

type fdroidApp struct {
	PackageName string            `json:"packageName"`
	Name        string            `json:"name"`
	Summary     string            `json:"summary"`
	License     string            `json:"license"`
	Localized   map[string]locale `json:"localized"`
}

type locale struct {
	Name    string `json:"name"`
	Summary string `json:"summary"`
}

type fdroidPackage struct {
	VersionName string `json:"versionName"`
	VersionCode int    `json:"versionCode"`
	ApkName     string `json:"apkName"`
	Size        int64  `json:"size"`
	Added       int64  `json:"added"`
}

func (f *FDroid) cachePath() string {
	return filepath.Join(f.cacheDir, f.name+"-index.json")
}

func (f *FDroid) cacheValid() bool {
	info, err := os.Stat(f.cachePath())
	return err == nil && time.Since(info.ModTime()) < 24*time.Hour
}

func (f *FDroid) load() error {
	if f.index != nil {
		return nil
	}
	if f.cacheValid() {
		data, err := os.ReadFile(f.cachePath())
		if err != nil {
			return fmt.Errorf("read cache %s: %w", f.cachePath(), err)
		}
		var idx fdroidIndex
		if err := json.Unmarshal(data, &idx); err != nil {
			// cache corrupt, fall through to fetch
			return f.fetch()
		}
		f.index = &idx
		return nil
	}
	return f.fetch()
}

func (f *FDroid) fetch() error {
	u := f.baseURL + "/index-v1.json"
	resp, err := http.Get(u)
	if err != nil {
		return fmt.Errorf("fetch %s: %w", u, err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != 200 {
		return fmt.Errorf("fetch %s: http %d", u, resp.StatusCode)
	}
	data, err := io.ReadAll(resp.Body)
	if err != nil {
		return fmt.Errorf("read body %s: %w", u, err)
	}
	var idx fdroidIndex
	if err := json.Unmarshal(data, &idx); err != nil {
		return fmt.Errorf("unmarshal index %s: %w", f.name, err)
	}
	if err := os.MkdirAll(f.cacheDir, 0755); err != nil {
		return fmt.Errorf("mkdir cache %s: %w", f.cacheDir, err)
	}
	if err := os.WriteFile(f.cachePath(), data, 0644); err != nil {
		return fmt.Errorf("write cache %s: %w", f.cachePath(), err)
	}
	f.index = &idx
	return nil
}

func (f *FDroid) Refresh() error {
	f.index = nil
	_ = os.Remove(f.cachePath()) // best effort, fetch will overwrite anyway
	return f.load()
}

func (f *FDroid) resolve(a fdroidApp) App {
	name, summary := a.Name, a.Summary
	for _, tag := range []string{"en-US", "en"} {
		if loc, ok := a.Localized[tag]; ok {
			if loc.Name != "" {
				name = loc.Name
			}
			if loc.Summary != "" {
				summary = loc.Summary
			}
			break
		}
	}
	out := App{
		PackageName: a.PackageName,
		Name:        name,
		Summary:     summary,
		License:     a.License,
		Source:      f.name,
	}
	if pkgs, ok := f.index.Packages[a.PackageName]; ok && len(pkgs) > 0 {
		p := pkgs[0]
		out.Version = p.VersionName
		out.VersionCode = p.VersionCode
		out.APKURL = f.baseURL + "/" + p.ApkName
		out.Size = p.Size
		out.Added = p.Added
	}
	return out
}

func (f *FDroid) Search(query string) ([]App, error) {
	if err := f.load(); err != nil {
		return nil, fmt.Errorf("fdroid %s search: %w", f.name, err)
	}
	q := strings.ToLower(query)
	var out []App
	for _, a := range f.index.Apps {
		r := f.resolve(a)
		if r.APKURL == "" {
			continue
		}
		if strings.Contains(strings.ToLower(r.Name), q) ||
			strings.Contains(strings.ToLower(r.PackageName), q) ||
			strings.Contains(strings.ToLower(r.Summary), q) {
			out = append(out, r)
		}
	}
	return out, nil
}

func (f *FDroid) Resolve(pkg string) (*App, error) {
	if err := f.load(); err != nil {
		return nil, fmt.Errorf("fdroid %s resolve: %w", f.name, err)
	}
	for _, a := range f.index.Apps {
		if a.PackageName == pkg {
			r := f.resolve(a)
			if r.APKURL == "" {
				return nil, fmt.Errorf("fdroid %s: no apk for %s", f.name, pkg)
			}
			return &r, nil
		}
	}
	return nil, nil
}

func (f *FDroid) Download(app App, dest string) (string, error) {
	if app.APKURL == "" {
		return "", fmt.Errorf("fdroid %s: no url for %s", f.name, app.PackageName)
	}
	resp, err := http.Get(app.APKURL)
	if err != nil {
		return "", fmt.Errorf("fdroid download %s: %w", app.PackageName, err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != 200 {
		return "", fmt.Errorf("fdroid download %s: http %d", app.PackageName, resp.StatusCode)
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
