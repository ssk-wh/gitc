package ui

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/ssk-wh/gitc/internal/git"
	"github.com/ssk-wh/gitc/internal/i18n"
)

// initRepo 创建带分支与合并历史的临时仓库。
func initRepo(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	run := func(args ...string) string {
		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()
		cmd := exec.CommandContext(ctx, "git", append([]string{"-c", "core.fscache=false", "-C", dir}, args...)...)
		cmd.WaitDelay = 2 * time.Second
		out, err := cmd.CombinedOutput()
		if err != nil {
			t.Fatalf("git %v: %v\n%s", args, err, out)
		}
		return string(out)
	}
	run("init", "-b", "main")
	run("config", "user.name", "测试")
	run("config", "user.email", "t@example.com")
	write := func(name, content string) {
		if err := os.WriteFile(filepath.Join(dir, name), []byte(content), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	write("a.txt", "1\n")
	run("add", "-A")
	run("commit", "-m", "初始提交")
	run("checkout", "-b", "feature/audio")
	write("a.txt", "2\n")
	run("commit", "-am", "修复音频切换崩溃")
	run("checkout", "main")
	write("b.txt", "b\n")
	run("add", "-A")
	run("commit", "-m", "支持摄像头分辨率")
	run("merge", "--no-edit", "feature/audio")
	write("a.txt", "dirty\n")
	return dir
}

func TestRealRepoRender(t *testing.T) {
	i18n.Set("zh")
	dir := initRepo(t)
	repo, err := git.Open(dir)
	if err != nil {
		t.Fatal(err)
	}
	a := New(repo)
	repo.SetCommandHook(a.LogCommand)
	a.Update(tea.WindowSizeMsg{Width: 120, Height: 40})
	pump(t, a, a.Init())
	// 把命令历史消息从通道喂回模型
	for {
		select {
		case msg := <-a.Msgs():
			a.Update(msg)
		default:
			goto drained
		}
	}
drained:

	v := plainView(a)
	t.Logf("\n%s", v)
	for _, want := range []string{"gitc", filepath.Base(dir), "main", "修复音频切换崩溃", "支持摄像头分辨率", "未提交变更"} {
		if !strings.Contains(v, want) {
			t.Errorf("渲染缺少 %q", want)
		}
	}
	// 打开详情（选非合并提交）
	a.graphSel = a.dirtyOffset() + 1
	cmd, _ := a.handleGraphKey(tea.KeyMsg{Type: tea.KeyEnter})
	pump(t, a, cmd)
	dv := plainView(a)
	if !strings.Contains(dv, "diff --git") && !strings.Contains(dv, "文件") {
		t.Errorf("详情视图缺少 diff/文件信息:\n%s", dv)
	}
}

func TestRealRepoSmall(t *testing.T) {
	i18n.Set("zh")
	dir := initRepo(t)
	repo, err := git.Open(dir)
	if err != nil {
		t.Fatal(err)
	}
	a := New(repo)
	a.Update(tea.WindowSizeMsg{Width: 70, Height: 16})
	pump(t, a, a.Init())
	v := plainView(a)
	t.Logf("\n%s", v)
	if strings.Count(v, "\n") > 18 {
		t.Errorf("小终端渲染行数过多")
	}
}
