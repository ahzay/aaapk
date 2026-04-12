package adb

import (
	"fmt"
	"os/exec"
	"strings"
)

func run(args ...string) (string, error) {
	out, err := exec.Command("adb", args...).CombinedOutput()
	s := strings.TrimSpace(string(out))
	if err != nil {
		return s, fmt.Errorf("%s", s)
	}
	return s, nil
}

func Connected() bool {
	out, _ := run("devices")
	for _, line := range strings.Split(out, "\n")[1:] {
		if strings.Contains(line, "device") {
			return true
		}
	}
	return false
}

func Install(path string) error {
	out, err := run("install", path)
	if err != nil {
		return err
	}
	if strings.Contains(out, "Failure") {
		return fmt.Errorf("%s", out)
	}
	return nil
}

func InstallMultiple(paths []string) error {
	args := append([]string{"install-multiple"}, paths...)
	out, err := run(args...)
	if err != nil {
		return err
	}
	if strings.Contains(out, "Failure") {
		return fmt.Errorf("%s", out)
	}
	return nil
}

func InstalledPackages() (map[string]bool, error) {
	out, err := run("shell", "pm", "list", "packages", "-3")
	if err != nil {
		return nil, err
	}
	pkgs := make(map[string]bool)
	for line := range strings.SplitSeq(out, "\n") {
		if name, ok := strings.CutPrefix(strings.TrimSpace(line), "package:"); ok {
			pkgs[name] = true
		}
	}
	return pkgs, nil
}

func ReadFile(path string) ([]byte, error) {
	out, err := run("shell", "cat", path)
	if strings.Contains(out, "No such file") {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return []byte(out), nil
}

func WriteFile(path, content string) error {
	cmd := fmt.Sprintf("echo '%s' > %s", content, path)
	_, err := run("shell", cmd)
	return err
}

// --- device introspection (used by gplay source) ---

func GetProperties() (map[string]string, error) {
	out, err := run("shell", "getprop")
	if err != nil {
		return nil, fmt.Errorf("getprop: %w", err)
	}
	m := make(map[string]string)
	for _, line := range strings.Split(out, "\n") {
		line = strings.TrimSpace(line)
		if !strings.HasPrefix(line, "[") {
			continue
		}
		parts := strings.SplitN(line, ": ", 2)
		if len(parts) != 2 {
			continue
		}
		k := strings.Trim(parts[0], "[]")
		v := strings.Trim(parts[1], "[]")
		m[k] = v
	}
	return m, nil
}

func ScreenSize() (width, height string) {
	out, _ := run("shell", "wm", "size")
	for _, line := range strings.Split(out, "\n") {
		if strings.Contains(line, "Physical size") || strings.Contains(line, "Override size") {
			parts := strings.Split(line, ":")
			if len(parts) == 2 {
				wh := strings.Split(strings.TrimSpace(parts[1]), "x")
				if len(wh) == 2 {
					return strings.TrimSpace(wh[0]), strings.TrimSpace(wh[1])
				}
			}
		}
	}
	return "1080", "2400"
}

func ScreenDensity() string {
	out, _ := run("shell", "wm", "density")
	for _, line := range strings.Split(out, "\n") {
		if strings.Contains(line, "Physical density") || strings.Contains(line, "Override density") {
			parts := strings.Split(line, ":")
			if len(parts) == 2 {
				return strings.TrimSpace(parts[1])
			}
		}
	}
	return "420"
}

func Features() []string {
	out, _ := run("shell", "pm", "list", "features")
	var feats []string
	for _, line := range strings.Split(out, "\n") {
		if f, ok := strings.CutPrefix(strings.TrimSpace(line), "feature:"); ok {
			feats = append(feats, f)
		}
	}
	return feats
}

func Libraries() []string {
	out, _ := run("shell", "pm", "list", "libraries")
	var libs []string
	for line := range strings.SplitSeq(out, "\n") {
		if l, ok := strings.CutPrefix(strings.TrimSpace(line), "library:"); ok {
			libs = append(libs, l)
		}
	}
	return libs
}
