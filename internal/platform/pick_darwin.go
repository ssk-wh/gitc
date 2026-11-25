//go:build darwin

package platform

import (
	"errors"
	"os/exec"
	"strings"
)

func pickDirectory() (string, error) {
	out, err := exec.Command("osascript", "-e", `POSIX path of (choose folder with prompt "选择项目目录")`).Output()
	if err != nil {
		var ee *exec.ExitError
		if errors.As(err, &ee) {
			return "", ErrCanceled
		}
		return "", ErrUnavailable
	}
	p := strings.TrimSpace(string(out))
	if p == "" {
		return "", ErrCanceled
	}
	return strings.TrimSuffix(p, "/"), nil
}
