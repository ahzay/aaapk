package main

import (
	"fmt"
	"os"
	"strings"
	"time"

	"github.com/ahzay/aaapk/pkg/adb"
	"github.com/ahzay/aaapk/pkg/config"
	"github.com/ahzay/aaapk/pkg/ledger"
	"github.com/ahzay/aaapk/pkg/source"
	"github.com/charmbracelet/log"
	fuzzyfinder "github.com/ktr0731/go-fuzzyfinder"
	"github.com/urfave/cli/v2"
)

var logger = log.NewWithOptions(os.Stderr, log.Options{
	ReportTimestamp: false,
})

func sources() []source.Source {
	cfg := config.Load()
	var out []source.Source
	for _, r := range cfg.Enabled() {
		switch r.Type {
		case "fdroid":
			out = append(out, source.NewFDroid(r.Name, r.URL, config.CacheDir()))
		}
	}
	return out
}

func search(q string) []source.App {
	var all []source.App
	for _, s := range sources() {
		hits, err := s.Search(q)
		if err != nil {
			logger.Warn("search failed", "repo", s.Name(), "err", err)
			continue
		}
		all = append(all, hits...)
	}
	return all
}

func pickInstall(apps []source.App, installed map[string]bool) (*source.App, error) {
	if len(apps) == 0 {
		return nil, fmt.Errorf("no results")
	}
	idx, err := fuzzyfinder.Find(apps,
		func(i int) string {
			a := apps[i]
			name := a.Name
			if name == "" {
				name = a.PackageName
			}
			if installed[a.PackageName] {
				return fmt.Sprintf("* %-30s  %s", name, a.Version)
			}
			return fmt.Sprintf("  %-30s  %s", name, a.Version)
		},
		fuzzyfinder.WithPreviewWindow(func(i, w, h int) string {
			if i == -1 {
				return ""
			}
			a := apps[i]
			name := a.Name
			if name == "" {
				name = a.PackageName
			}

			sz := "?"
			if a.Size > 0 {
				sz = fmt.Sprintf("%.1f MB", float64(a.Size)/(1024*1024))
			}
			date := "?"
			if a.Added > 0 {
				date = time.Unix(a.Added/1000, 0).Format("2006-01-02")
			}
			status := "not installed"
			if installed[a.PackageName] {
				status = "INSTALLED"
			}

			var b strings.Builder
			b.WriteString(name)
			b.WriteString("\n")
			b.WriteString(strings.Repeat("-", len(name)))
			b.WriteString("\n\n")

			b.WriteString(fmt.Sprintf("  %-12s %s\n", "package:", a.PackageName))
			b.WriteString(fmt.Sprintf("  %-12s %s\n", "version:", a.Version))
			b.WriteString(fmt.Sprintf("  %-12s %s\n", "date:", date))
			b.WriteString(fmt.Sprintf("  %-12s %s\n", "size:", sz))
			b.WriteString(fmt.Sprintf("  %-12s %s\n", "license:", a.License))
			b.WriteString(fmt.Sprintf("  %-12s %s\n", "repo:", a.Source))
			b.WriteString(fmt.Sprintf("  %-12s %s\n", "status:", status))

			if a.Summary != "" {
				b.WriteString("\n")
				b.WriteString(wrap(a.Summary, w-4))
				b.WriteString("\n")
			}

			return b.String()
		}),
	)
	if err != nil {
		return nil, err
	}
	return &apps[idx], nil
}

func pickUpdate(candidates []updateCandidate) ([]int, error) {
	return fuzzyfinder.FindMulti(candidates,
		func(i int) string {
			c := candidates[i]
			name := c.latest.Name
			if name == "" {
				name = c.pkg
			}
			return fmt.Sprintf("  %-30s  %s -> %s", name, c.current.Version, c.latest.Version)
		},
		fuzzyfinder.WithPreviewWindow(func(i, w, h int) string {
			if i == -1 {
				return ""
			}
			c := candidates[i]
			name := c.latest.Name
			if name == "" {
				name = c.pkg
			}

			sz := "?"
			if c.latest.Size > 0 {
				sz = fmt.Sprintf("%.1f MB", float64(c.latest.Size)/(1024*1024))
			}

			var b strings.Builder
			b.WriteString(name)
			b.WriteString("\n")
			b.WriteString(strings.Repeat("-", len(name)))
			b.WriteString("\n\n")

			b.WriteString(fmt.Sprintf("  %-12s %s\n", "package:", c.pkg))
			b.WriteString(fmt.Sprintf("  %-12s %s\n", "installed:", c.current.Version))
			b.WriteString(fmt.Sprintf("  %-12s %s\n", "available:", c.latest.Version))
			b.WriteString(fmt.Sprintf("  %-12s %s\n", "size:", sz))
			b.WriteString(fmt.Sprintf("  %-12s %s\n", "repo:", c.latest.Source))

			if c.latest.Summary != "" {
				b.WriteString("\n")
				b.WriteString(wrap(c.latest.Summary, w-4))
				b.WriteString("\n")
			}

			return b.String()
		}),
	)
}

func wrap(text string, width int) string {
	if width <= 0 {
		width = 60
	}
	words := strings.Fields(text)
	var lines []string
	line := "  "
	for _, w := range words {
		if len(line)+len(w)+1 > width && line != "  " {
			lines = append(lines, line)
			line = "  "
		}
		if line == "  " {
			line += w
		} else {
			line += " " + w
		}
	}
	if line != "  " {
		lines = append(lines, line)
	}
	return strings.Join(lines, "\n")
}

func download(app *source.App) (string, error) {
	for _, s := range sources() {
		if s.Name() == app.Source {
			return s.Download(*app, os.TempDir())
		}
	}
	return "", fmt.Errorf("source %q not found", app.Source)
}

func requireDevice() error {
	if !adb.Connected() {
		return fmt.Errorf("no device connected")
	}
	return nil
}

func queryFrom(c *cli.Context, label string) (string, error) {
	q := strings.Join(c.Args().Slice(), " ")
	if q == "" {
		return "", fmt.Errorf("%s what?", label)
	}
	return q, nil
}

func cmdInstall(c *cli.Context) error {
	if err := requireDevice(); err != nil {
		return err
	}
	q, err := queryFrom(c, "install")
	if err != nil {
		return err
	}
	hits := search(q)
	installed, _ := adb.InstalledPackages()
	a, err := pickInstall(hits, installed)
	if err != nil {
		return err
	}
	logger.Info("downloading", "pkg", a.PackageName, "ver", a.Version)
	path, err := download(a)
	if err != nil {
		return err
	}
	defer os.Remove(path)
	logger.Info("installing", "pkg", a.PackageName)
	if err := adb.Install(path); err != nil {
		return err
	}
	l, err := ledger.Load()
	if err != nil {
		logger.Warn("ledger load failed", "err", err)
	} else {
		l.Set(a.PackageName, a.Version, a.Source, a.VersionCode)
	}
	logger.Info("done", "pkg", a.PackageName)
	return nil
}

type updateCandidate struct {
	pkg     string
	current ledger.Entry
	latest  source.App
}

func cmdUpdate(c *cli.Context) error {
	if err := requireDevice(); err != nil {
		return err
	}
	l, err := ledger.Load()
	if err != nil {
		return fmt.Errorf("ledger: %w", err)
	}
	if len(l) == 0 {
		logger.Info("nothing managed")
		return nil
	}

	var candidates []updateCandidate

	srcs := sources()
	for pkg, entry := range l {
		for _, s := range srcs {
			latest, err := s.Resolve(pkg)
			if err != nil {
				logger.Warn("resolve failed", "pkg", pkg, "repo", s.Name(), "err", err)
				continue
			}
			if latest == nil {
				continue
			}
			if latest.VersionCode > entry.VersionCode {
				candidates = append(candidates, updateCandidate{pkg: pkg, current: entry, latest: *latest})
			}
			break
		}
	}

	if len(candidates) == 0 {
		logger.Info("everything up to date")
		return nil
	}

	idxs, err := pickUpdate(candidates)
	if err != nil {
		return err
	}

	for _, idx := range idxs {
		cand := candidates[idx]
		logger.Info("downloading", "pkg", cand.pkg, "ver", cand.latest.Version)
		path, err := download(&cand.latest)
		if err != nil {
			logger.Error("download failed", "pkg", cand.pkg, "err", err)
			continue
		}
		logger.Info("installing", "pkg", cand.pkg)
		if err := adb.Install(path); err != nil {
			logger.Error("install failed", "pkg", cand.pkg, "err", err)
			os.Remove(path)
			continue
		}
		os.Remove(path)
		l.Set(cand.pkg, cand.latest.Version, cand.latest.Source, cand.latest.VersionCode)
		logger.Info("updated", "pkg", cand.pkg)
	}
	return nil
}

func cmdRefresh(c *cli.Context) error {
	for _, s := range sources() {
		logger.Info("refreshing", "repo", s.Name())
		if err := s.Refresh(); err != nil {
			logger.Error("refresh failed", "repo", s.Name(), "err", err)
		} else {
			logger.Info("ok", "repo", s.Name())
		}
	}
	return nil
}

func cmdRepoList(c *cli.Context) error {
	cfg := config.Load()
	for _, r := range cfg.Repos {
		status := "on"
		if !r.Enabled {
			status = "off"
		}
		fmt.Printf("%-3s  %-15s  %s  %s\n", status, r.Name, r.Type, r.URL)
	}
	return nil
}

func cmdRepoAdd(c *cli.Context) error {
	if c.NArg() < 2 {
		return fmt.Errorf("usage: repo add <name> <url>")
	}
	name := c.Args().Get(0)
	url := c.Args().Get(1)
	if !strings.HasPrefix(url, "http://") && !strings.HasPrefix(url, "https://") {
		return fmt.Errorf("url must start with http:// or https://")
	}
	cfg := config.Load()
	cfg.Add(name, "fdroid", url)
	return cfg.Save()
}

func cmdRepoRm(c *cli.Context) error {
	name := c.Args().First()
	if name == "" {
		return fmt.Errorf("rm what?")
	}
	cfg := config.Load()
	if !cfg.Remove(name) {
		return fmt.Errorf("not found: %s", name)
	}
	return cfg.Save()
}

func cmdRepoToggle(enabled bool) cli.ActionFunc {
	return func(c *cli.Context) error {
		name := c.Args().First()
		if name == "" {
			return fmt.Errorf("which repo?")
		}
		cfg := config.Load()
		if !cfg.SetEnabled(name, enabled) {
			return fmt.Errorf("not found: %s", name)
		}
		return cfg.Save()
	}
}

func main() {
	app := &cli.App{
		Name:  "aaapk",
		Usage: "android package manager over adb",
		Commands: []*cli.Command{
			{Name: "install", Aliases: []string{"i"}, ArgsUsage: "<query>", Usage: "search, pick, download, install", Action: cmdInstall},
			{Name: "update", Aliases: []string{"u"}, Usage: "check and update managed packages", Action: cmdUpdate},
			{Name: "refresh", Usage: "re-fetch all repo indexes", Action: cmdRefresh},
			{Name: "repo", Usage: "manage repos", Subcommands: []*cli.Command{
				{Name: "list", Aliases: []string{"ls"}, Action: cmdRepoList},
				{Name: "add", ArgsUsage: "<name> <url>", Usage: "add an fdroid repo", Action: cmdRepoAdd},
				{Name: "rm", ArgsUsage: "<name>", Action: cmdRepoRm},
				{Name: "enable", ArgsUsage: "<name>", Action: cmdRepoToggle(true)},
				{Name: "disable", ArgsUsage: "<name>", Action: cmdRepoToggle(false)},
			}},
		},
	}

	if err := app.Run(os.Args); err != nil {
		logger.Fatal(err)
	}
}
