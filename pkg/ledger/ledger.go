package ledger

import (
	"encoding/json"

	"github.com/ahzay/aaapk/pkg/adb"
)

const DevicePath = "/sdcard/.aaapk.json"

type Entry struct {
	Version     string `json:"version"`
	VersionCode int    `json:"versionCode"`
	Source      string `json:"source"`
}

type Ledger map[string]Entry

func Load() (Ledger, error) {
	installed, err := adb.InstalledPackages()
	if err != nil {
		return nil, err
	}
	data, err := adb.ReadFile(DevicePath)
	if err != nil {
		return nil, err
	}
	var l Ledger
	if data == nil || json.Unmarshal(data, &l) != nil || l == nil {
		l = make(Ledger)
	}
	changed := false
	for pkg := range l {
		if !installed[pkg] {
			delete(l, pkg)
			changed = true
		}
	}
	if changed {
		l.save()
	}
	return l, nil
}

func (l Ledger) save() error {
	data, err := json.Marshal(l)
	if err != nil {
		return err
	}
	return adb.WriteFile(DevicePath, string(data))
}

func (l Ledger) Set(pkg, version, source string, versionCode int) error {
	l[pkg] = Entry{Version: version, VersionCode: versionCode, Source: source}
	return l.save()
}
