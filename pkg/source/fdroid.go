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
	return &FDroid{name: name, baseURL: strings.TrimRight(baseURL, "/"), cacheDir: cacheDir}
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

func (f *FDroid) cacheOK() bool {
	info, err := os.Stat(f.cachePath())
	return err == nil && time.Since(info.ModTime()) < 24*time.Hour
}

func (f *FDroid) load() error {
	if f.index != nil {
		return nil
	}
	if f.cacheOK() {
		data, err := os.ReadFile(f.cachePath())
		if err == nil {
			var idx fdroidIndex
			if json.Unmarshal(data, &idx) == nil {
				f.index = &idx
				return nil
			}
		}
	}
	return f.fetch()
}

func (f *FDroid) fetch() error {
	fmt.Fprintf(os.Stderr, "fetching %s index...\n", f.name)
	resp, err := http.Get(f.baseURL + "/index-v1.json")
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode != 200 {
		return fmt.Errorf("%s: http %d", f.name, resp.StatusCode)
	}
	data, err := io.ReadAll(resp.Body)
	if err != nil {
		return err
	}
	var idx fdroidIndex
	if err := json.Unmarshal(data, &idx); err != nil {
		return err
	}
	os.MkdirAll(f.cacheDir, 0755)
	os.WriteFile(f.cachePath(), data, 0644)
	f.index = &idx
	return nil
}

func (f *FDroid) Refresh() error {
	f.index = nil
	os.Remove(f.cachePath())
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
		return nil, err
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
		return nil, err
	}
	for _, a := range f.index.Apps {
		if a.PackageName == pkg {
			r := f.resolve(a)
			if r.APKURL == "" {
				return nil, fmt.Errorf("no apk for %s", pkg)
			}
			return &r, nil
		}
	}
	return nil, nil
}

func (f *FDroid) Download(app App, dest string) (string, error) {
	if app.APKURL == "" {
		return "", fmt.Errorf("no url for %s", app.PackageName)
	}
	resp, err := http.Get(app.APKURL)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()
	if resp.StatusCode != 200 {
		return "", fmt.Errorf("download: http %d", resp.StatusCode)
	}
	os.MkdirAll(dest, 0755)
	path := filepath.Join(dest, fmt.Sprintf("%s-%s.apk", app.PackageName, app.Version))
	out, err := os.Create(path)
	if err != nil {
		return "", err
	}
	defer out.Close()
	io.Copy(out, resp.Body)
	return path, nil
}
