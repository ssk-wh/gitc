//go:build windows

package platform

import (
	"errors"
	"os/exec"
	"strings"
)

func pickDirectory() (string, error) {
	script := `Add-Type -AssemblyName System.Windows.Forms; ` +
		`[Console]::OutputEncoding=[System.Text.Encoding]::UTF8; ` +
		`$d=New-Object System.Windows.Forms.FolderBrowserDialog; ` +
		`$d.Description='选择项目目录'; ` +
		`$d.ShowNewFolderButton=$true; ` +
		`$d.SelectedPath=[Environment]::GetFolderPath('UserProfile'); ` +
		`if($d.ShowDialog() -eq [System.Windows.Forms.DialogResult]::OK){[Console]::Out.Write($d.SelectedPath)}`
	out, err := exec.Command("powershell", "-NoProfile", "-STA", "-Command", script).Output()
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
