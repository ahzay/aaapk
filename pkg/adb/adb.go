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

func Install(path string) error {
	out, err := run("install", path)
	if err != nil {
		return err
	}
	if strings.Contains(out, "Failure") {
		return fmt.Errorf(out)
	}
	return nil
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

func InstalledPackages() (map[string]bool, error) {
	out, err := run("shell", "pm", "list", "packages", "-3")
	if err != nil {
		return nil, err
	}
	pkgs := make(map[string]bool)
	for _, line := range strings.Split(out, "\n") {
		line = strings.TrimSpace(line)
		if name, ok := strings.CutPrefix(line, "package:"); ok {
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
