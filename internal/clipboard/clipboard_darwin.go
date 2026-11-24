//go:build darwin

package clipboard

import (
	"os/exec"
	"strings"
)

// Copy 使用 pbcopy 写入剪贴板。
func Copy(text string) error {
	cmd := exec.Command("pbcopy")
	cmd.Stdin = strings.NewReader(text)
	return cmd.Run()
}
