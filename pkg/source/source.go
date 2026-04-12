package source

import "github.com/charmbracelet/log"

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

// shared logger setter pattern -- each source gets its own via constructor
type logged struct {
	log *log.Logger
}

func (l *logged) debug(msg string, kv ...interface{}) {
	if l.log != nil {
		l.log.Debug(msg, kv...)
	}
}

func (l *logged) info(msg string, kv ...interface{}) {
	if l.log != nil {
		l.log.Info(msg, kv...)
	}
}

func (l *logged) warn(msg string, kv ...interface{}) {
	if l.log != nil {
		l.log.Warn(msg, kv...)
	}
}
