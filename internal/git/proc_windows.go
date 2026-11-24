//go:build windows

package git

import (
	"os/exec"
	"strconv"
)

// prepareProcess Windows 下无需特殊设置，终止时用 taskkill /T 处理进程树。
func prepareProcess(c *exec.Cmd) {}

// terminateTree 使用 taskkill 终止进程树，避免 git-remote-* 残留。
func terminateTree(c *exec.Cmd) {
	if c.Process == nil {
		return
	}
	_ = exec.Command("taskkill", "/F", "/T", "/PID", strconv.Itoa(c.Process.Pid)).Run()
	_ = c.Process.Kill()
}
