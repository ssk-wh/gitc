//go:build linux

package platform

import (
	"errors"
	"os"
	"os/exec"
	"strings"
)

func pickDirectory() (string, error) {
	home, _ := os.UserHomeDir()
	if home == "" {
		home = "/"
	}
	type cmd struct {
		name string
		args []string
	}
	candidates := []cmd{
		{"zenity", []string{"--file-selection", "--directory", "--title=选择项目目录"}},
		{"kdialog", []string{"--getexistingdirectory", home, "--title", "选择项目目录"}},
		{"yad", []string{"--file-selection", "--directory", "--title=选择项目目录"}},
	}
	for _, c := range candidates {
		path, err := exec.LookPath(c.name)
		if err != nil {
			continue
		}
		out, err := exec.Command(path, c.args...).CombinedOutput()
		if err != nil {
			var ee *exec.ExitError
			if errors.As(err, &ee) {
				msg := strings.ToLower(string(out))
				if strings.Contains(msg, "cannot open display") || strings.Contains(msg, "unable to init server") {
					return "", ErrUnavailable
				}
				return "", ErrCanceled
			}
			return "", err
		}
		p := strings.TrimSpace(string(out))
		if p == "" {
			return "", ErrCanceled
		}
		return p, nil
	}
	return "", ErrUnavailable
}
