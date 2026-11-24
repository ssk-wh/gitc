//go:build windows

package clipboard

import (
	"os/exec"
	"strings"
)

// Copy 使用 PowerShell Set-Clipboard 写入剪贴板（原生 Unicode，避免编码问题）。
func Copy(text string) error {
	script := "Set-Clipboard -Value '" + strings.ReplaceAll(text, "'", "''") + "'"
	cmd := exec.Command("powershell", "-NoProfile", "-NonInteractive", "-Command", script)
	return cmd.Run()
}
