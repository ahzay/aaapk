package source

type App struct {
	PackageName string
	Name        string
	Version     string
	VersionCode int
	Summary     string
	APKURL      string
	Size        int64
	License     string
	Source      string
	Added       int64
}

type Source interface {
	Name() string
	Search(query string) ([]App, error)
	Resolve(pkg string) (*App, error)
	Download(app App, dest string) (string, error)
	Refresh() error
}
