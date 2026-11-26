package platform

import (
	"fmt"
	"os/exec"
	"runtime"
)

// OpenDirectory 用系统文件管理器打开目录。
// 命令仅启动不等待（explorer/xdg-open 的退出码不可靠），启动失败才返回错误。
func OpenDirectory(path string) error {
	if path == "" {
		return fmt.Errorf("目录不能为空")
	}
	var cmd *exec.Cmd
	switch runtime.GOOS {
	case "windows":
		cmd = exec.Command("explorer", path)
	case "darwin":
		cmd = exec.Command("open", path)
	default:
		cmd = exec.Command("xdg-open", path)
	}
	if err := cmd.Start(); err != nil {
		return err
	}
	// 回收子进程，避免僵尸进程；退出码不作为打开失败的依据。
	go func() { _ = cmd.Wait() }()
	return nil
}
