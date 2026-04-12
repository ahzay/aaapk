package config

import (
	"encoding/json"
	"os"
	"path/filepath"
)

type Repo struct {
	Name      string `json:"name"`
	Type      string `json:"type"`
	URL       string `json:"url,omitempty"`
	Dispenser string `json:"dispenser,omitempty"`
	Enabled   bool   `json:"enabled"`
}

type Config struct {
	Repos []Repo `json:"repos"`
	path  string
}

func dir() string {
	d, err := os.UserConfigDir()
	if err != nil {
		d = os.TempDir()
	}
	return filepath.Join(d, "aaapk")
}

func CacheDir() string {
	d, err := os.UserCacheDir()
	if err != nil {
		d = os.TempDir()
	}
	return filepath.Join(d, "aaapk")
}

func Load() *Config {
	p := filepath.Join(dir(), "config.json")
	c := &Config{path: p}

	data, err := os.ReadFile(p)
	if err != nil {
		c.Repos = defaults()
		c.Save()
		return c
	}
	json.Unmarshal(data, c)
	c.path = p
	if len(c.Repos) == 0 {
		c.Repos = defaults()
		c.Save()
	}
	return c
}

func defaults() []Repo {
	return []Repo{
		{Name: "f-droid", Type: "fdroid", URL: "https://f-droid.org/repo", Enabled: true},
		{Name: "izzy", Type: "fdroid", URL: "https://apt.izzysoft.de/fdroid/repo", Enabled: true},
		{Name: "play", Type: "gplay", Dispenser: "https://auroraoss.com/api/auth", Enabled: true},
	}
}

func (c *Config) Save() error {
	os.MkdirAll(filepath.Dir(c.path), 0755)
	data, _ := json.MarshalIndent(c, "", "  ")
	return os.WriteFile(c.path, data, 0644)
}

func (c *Config) Remove(name string) bool {
	for i, r := range c.Repos {
		if r.Name == name {
			c.Repos = append(c.Repos[:i], c.Repos[i+1:]...)
			return true
		}
	}
	return false
}

func (c *Config) SetEnabled(name string, enabled bool) bool {
	for i, r := range c.Repos {
		if r.Name == name {
			c.Repos[i].Enabled = enabled
			return true
		}
	}
	return false
}

func (c *Config) Enabled() []Repo {
	var out []Repo
	for _, r := range c.Repos {
		if r.Enabled {
			out = append(out, r)
		}
	}
	return out
}
