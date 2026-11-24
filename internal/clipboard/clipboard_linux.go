//go:build linux

package clipboard

import (
	"errors"
	"os/exec"
	"strings"
)

// Copy 依次尝试 wl-copy / xclip / xsel 写入剪贴板。
func Copy(text string) error {
	candidates := [][]string{
		{"wl-copy"},
		{"xclip", "-selection", "clipboard"},
		{"xsel", "--clipboard", "--input"},
	}
	var lastErr error
	for _, argv := range candidates {
		path, err := exec.LookPath(argv[0])
		if err != nil {
			continue
		}
		cmd := exec.Command(path, argv[1:]...)
		cmd.Stdin = strings.NewReader(text)
		if err := cmd.Run(); err == nil {
			return nil
		}
		lastErr = err
	}
	if lastErr == nil {
		lastErr = errors.New("未找到 wl-copy/xclip/xsel，请安装剪贴板工具")
	}
	return lastErr
}
