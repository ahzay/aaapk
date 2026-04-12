package main

import (
	"fmt"
	"os"
	"path/filepath"
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
	Level:           log.InfoLevel,
})

func sources() []source.Source {
	cfg := config.Load()
	var out []source.Source
	for _, r := range cfg.Enabled() {
		switch r.Type {
		case "fdroid":
			out = append(out, source.NewFDroid(r.Name, r.URL, config.CacheDir(), logger))
		case "gplay":
			out = append(out, source.NewGPlay(r.Name, r.Dispenser, logger))
		default:
			logger.Warn("unknown repo type", "repo", r.Name, "type", r.Type)
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

func sourceByName(name string) source.Source {
	for _, s := range sources() {
		if s.Name() == name {
			return s
		}
	}
	return nil
}

func download(app *source.App) (string, error) {
	s := sourceByName(app.Source)
	if s == nil {
		return "", fmt.Errorf("source %q not found", app.Source)
	}
	return s.Download(*app, os.TempDir())
}

// installPath handles both single-file and split-apk directories.
func installPath(path string) error {
	info, err := os.Stat(path)
	if err != nil {
		return err
	}
	if !info.IsDir() {
		return adb.Install(path)
	}
	entries, _ := os.ReadDir(path)
	var apks []string
	for _, e := range entries {
		if !e.IsDir() && strings.HasSuffix(e.Name(), ".apk") {
			apks = append(apks, filepath.Join(path, e.Name()))
		}
	}
	if len(apks) == 0 {
		return fmt.Errorf("no apk files found in %s", path)
	}
	if len(apks) == 1 {
		return adb.Install(apks[0])
	}
	return adb.InstallMultiple(apks)
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

// --- commands ---

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
	defer os.RemoveAll(path)

	logger.Info("installing", "pkg", a.PackageName)
	if err := installPath(path); err != nil {
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

	srcs := sources()
	var candidates []updateCandidate
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
		if err := installPath(path); err != nil {
			logger.Error("install failed", "pkg", cand.pkg, "err", err)
			os.RemoveAll(path)
			continue
		}
		os.RemoveAll(path)
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

func cmdList(c *cli.Context) error {
	if err := requireDevice(); err != nil {
		logger.Error("device not connected")
		return err
	}
	l, err := ledger.Load()
	if err != nil {
		logger.Error("failed to load ledger", "err", err)
		return nil
	}
	if len(l) == 0 {
		logger.Info("no managed packages")
		return nil
	}
	for pkg, entry := range l {
		fmt.Printf("%-40s  %-15s  %s\n", pkg, entry.Version, entry.Source)
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
		detail := r.URL
		if r.Type == "gplay" {
			detail = r.Dispenser
		}
		fmt.Printf("%-3s  %-15s  %-7s  %s\n", status, r.Name, r.Type, detail)
	}
	return nil
}

func cmdRepoAdd(c *cli.Context) error {
	if c.NArg() < 1 {
		return fmt.Errorf("usage: repo add <name> [url or dispenser]")
	}
	name := c.Args().Get(0)
	typ := c.String("type")
	cfg := config.Load()

	switch typ {
	case "fdroid":
		if c.NArg() < 2 {
			return fmt.Errorf("usage: repo add --type fdroid <name> <url>")
		}
		u := c.Args().Get(1)
		if !strings.HasPrefix(u, "http://") && !strings.HasPrefix(u, "https://") {
			return fmt.Errorf("url must start with http:// or https://")
		}
		cfg.Repos = append(cfg.Repos, config.Repo{Name: name, Type: "fdroid", URL: u, Enabled: true})
	case "gplay":
		disp := "https://auroraoss.com/api/auth"
		if c.NArg() >= 2 {
			disp = c.Args().Get(1)
		}
		cfg.Repos = append(cfg.Repos, config.Repo{Name: name, Type: "gplay", Dispenser: disp, Enabled: true})
	default:
		return fmt.Errorf("unknown type %q (fdroid or gplay)", typ)
	}
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

// --- fuzzyfinder UI ---

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
			mark := " "
			if installed[a.PackageName] {
				mark = "*"
			}
			return fmt.Sprintf("%s %-30s  %-10s  %s", mark, name, "["+a.Source+"]", a.Version)
		},
		fuzzyfinder.WithPreviewWindow(func(i, w, h int) string {
			if i == -1 {
				return ""
			}
			return appPreview(apps[i], installed[apps[i].PackageName], w)
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
			return fmt.Sprintf("  %-30s  %-10s  %s -> %s", name, "["+c.latest.Source+"]", c.current.Version, c.latest.Version)
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
			sz := sizeStr(c.latest.Size)
			var b strings.Builder
			b.WriteString(name + "\n" + strings.Repeat("-", len(name)) + "\n\n")
			fmt.Fprintf(&b, "  %-12s %s\n", "package:", c.pkg)
			fmt.Fprintf(&b, "  %-12s %s\n", "installed:", c.current.Version)
			fmt.Fprintf(&b, "  %-12s %s\n", "available:", c.latest.Version)
			fmt.Fprintf(&b, "  %-12s %s\n", "size:", sz)
			fmt.Fprintf(&b, "  %-12s %s\n", "repo:", c.latest.Source)
			if c.latest.Summary != "" {
				b.WriteString("\n" + wrap(c.latest.Summary, w-4) + "\n")
			}
			return b.String()
		}),
	)
}

func appPreview(a source.App, isInstalled bool, w int) string {
	name := a.Name
	if name == "" {
		name = a.PackageName
	}
	status := "not installed"
	if isInstalled {
		status = "INSTALLED"
	}
	var b strings.Builder
	b.WriteString(name + "\n" + strings.Repeat("-", len(name)) + "\n\n")
	b.WriteString(fmt.Sprintf("  %-12s %s\n", "package:", a.PackageName))
	b.WriteString(fmt.Sprintf("  %-12s %s\n", "version:", a.Version))
	b.WriteString(fmt.Sprintf("  %-12s %s\n", "date:", dateStr(a.Added)))
	b.WriteString(fmt.Sprintf("  %-12s %s\n", "size:", sizeStr(a.Size)))
	b.WriteString(fmt.Sprintf("  %-12s %s\n", "license:", a.License))
	b.WriteString(fmt.Sprintf("  %-12s %s\n", "repo:", a.Source))
	b.WriteString(fmt.Sprintf("  %-12s %s\n", "status:", status))
	if a.Summary != "" {
		b.WriteString("\n" + wrap(a.Summary, w-4) + "\n")
	}
	return b.String()
}

func sizeStr(n int64) string {
	if n <= 0 {
		return "?"
	}
	return fmt.Sprintf("%.1f MB", float64(n)/(1024*1024))
}

func dateStr(ms int64) string {
	if ms <= 0 {
		return "?"
	}
	return time.Unix(ms/1000, 0).Format("2006-01-02")
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

// --- entrypoint ---

func main() {
	app := &cli.App{
		Name:  "aaapk",
		Usage: "android package manager over adb",
		Flags: []cli.Flag{
			&cli.BoolFlag{Name: "debug", Aliases: []string{"d"}, Usage: "enable debug logging"},
		},
		Before: func(c *cli.Context) error {
			if c.Bool("debug") {
				logger.SetLevel(log.DebugLevel)
			}
			return nil
		},
		Commands: []*cli.Command{
			{Name: "install", Aliases: []string{"i"}, ArgsUsage: "<query>", Usage: "search, pick, download, install", Action: cmdInstall},
			{Name: "update", Aliases: []string{"u"}, Usage: "check and update managed packages", Action: cmdUpdate},
			{Name: "refresh", Usage: "re-fetch repo indexes", Action: cmdRefresh},
			{Name: "list", Aliases: []string{"ls"}, Usage: "list managed packages", Action: cmdList},
			{Name: "repo", Usage: "manage repos", Subcommands: []*cli.Command{
				{Name: "list", Aliases: []string{"ls"}, Action: cmdRepoList},
				{Name: "add", ArgsUsage: "<name> [url]", Usage: "add a repo",
					Flags: []cli.Flag{
						&cli.StringFlag{Name: "type", Aliases: []string{"t"}, Value: "fdroid", Usage: "fdroid or gplay"},
					},
					Action: cmdRepoAdd,
				},
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
