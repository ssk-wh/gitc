//go:build windows

package platform

import (
	"errors"
	"os/exec"
	"strings"
)

func pickDirectory() (string, error) {
	// 使用“保存文件”风格的现代选择框：进入目标文件夹后点“保存”，取所在目录。
	script := `Add-Type -AssemblyName System.Windows.Forms; ` +
		`[Console]::OutputEncoding=[System.Text.Encoding]::UTF8; ` +
		`$d=New-Object System.Windows.Forms.SaveFileDialog; ` +
		`$d.Title='选择项目目录：进入目标文件夹后点“保存”'; ` +
		`$d.FileName='选择当前文件夹'; ` +
		`$d.Filter='文件夹|*.*'; ` +
		`$d.ValidateNames=$false; ` +
		`$d.CheckPathExists=$true; ` +
		`$d.OverwritePrompt=$false; ` +
		`$d.AddExtension=$false; ` +
		`$d.RestoreDirectory=$true; ` +
		`$d.InitialDirectory=[Environment]::GetFolderPath('UserProfile'); ` +
		`if($d.ShowDialog() -eq [System.Windows.Forms.DialogResult]::OK){[Console]::Out.Write((Split-Path -Parent $d.FileName))}`
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
