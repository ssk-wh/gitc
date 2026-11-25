//go:build windows

package platform

import (
	"errors"
	"os/exec"
	"strings"
)

func pickDirectory() (string, error) {
	// 系统“选择文件夹”对话框（BrowseForFolder 新版样式，可直接输入路径、新建文件夹）。
	script := `[Console]::OutputEncoding=[System.Text.Encoding]::UTF8; ` +
		`$shell=New-Object -ComObject Shell.Application; ` +
		`$start=[Environment]::GetFolderPath('UserProfile'); ` +
		`$folder=$shell.BrowseForFolder(0,'选择项目目录',0x40,$start); ` +
		`if($folder){[Console]::Out.Write($folder.Self.Path)}`
	out, err := exec.Command("powershell", "-NoProfile", "-STA", "-WindowStyle", "Hidden", "-Command", script).Output()
	if err != nil {
		var ee *exec.Error
		if errors.As(err, &ee) {
			return "", ErrUnavailable
		}
		return "", ErrCanceled
	}
	p := strings.TrimSpace(string(out))
	if p == "" {
		return "", ErrCanceled
	}
	return p, nil
}
