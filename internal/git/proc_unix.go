//go:build !windows

package git

import (
	"os/exec"
	"syscall"
)

// prepareProcess 让 git 子进程进入独立进程组，便于整组终止。
func prepareProcess(c *exec.Cmd) {
	c.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
}

// terminateTree 终止整个进程组（含 ssh、git-remote-* 等子进程）。
func terminateTree(c *exec.Cmd) {
	if c.Process == nil {
		return
	}
	_ = syscall.Kill(-c.Process.Pid, syscall.SIGKILL)
	_ = c.Process.Kill()
}
