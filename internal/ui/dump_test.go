package ui

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/ssk-wh/gitc/internal/git"
)

// TestDumpScreens 在设置 GITC_DUMP_DIR 时导出各状态界面（去 ANSI），用于人工审查。
func TestDumpScreens(t *testing.T) {
	dir := os.Getenv("GITC_DUMP_DIR")
	if dir == "" {
		t.Skip("未设置 GITC_DUMP_DIR")
	}
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	dump := func(name string, a *App) {
		t.Helper()
		if err := os.WriteFile(filepath.Join(dir, name+".txt"), []byte(plainView(a)), 0o644); err != nil {
			t.Fatal(err)
		}
	}

	a, _ := newFakeApp(t)
	a.commands = []commandEntry{
		{at: time.Now().Add(-5 * time.Minute), line: "git status --porcelain=v2 --branch"},
		{at: time.Now().Add(-4 * time.Minute), line: "git for-each-ref --format=..."},
		{at: time.Now().Add(-2 * time.Minute), line: "git checkout feature/x"},
		{at: time.Now().Add(-time.Minute), line: "git log --topo-order"},
	}
	dump("01-main-120x40", a)

	// 80x24
	a.Update(tea.WindowSizeMsg{Width: 80, Height: 24})
	dump("02-main-80x24", a)

	// 侧栏分支选中（feature/x）
	a.Update(tea.WindowSizeMsg{Width: 120, Height: 40})
	sendKey(a, tea.KeyMsg{Type: tea.KeyTab})
	for {
		it := a.selectedSideItem()
		if it != nil && it.kind == sideBranch && it.ref.Name == "feature/x" {
			break
		}
		a.moveSidebar(1)
	}
	dump("03-sidebar-branch", a)

	// 确认框
	sendKey(a, tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("d")})
	dump("04-confirm-delete", a)
	sendKey(a, tea.KeyMsg{Type: tea.KeyEsc})

	// 搜索态
	sendKey(a, tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("/")})
	sendRunes(a, "摄像")
	dump("05-search-inline", a)
	sendKey(a, tea.KeyMsg{Type: tea.KeyEsc})

	// 帮助
	sendKey(a, tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("?")})
	dump("06-help", a)
	sendKey(a, tea.KeyMsg{Type: tea.KeyEsc})

	// 详情
	a.pane = PaneCommits
	a.graphSel = a.dirtyOffset()
	a.overlay = overlayDetail
	a.Update(detailMsg{
		sha: "aaaaaaaaaaaa1111", kind: "commit", title: "修复切换音频崩溃",
		detail: git.CommitDetail{
			Hash: "aaaaaaaaaaaa1111", Author: "张三", AuthorEmail: "z@example.com",
			Subject: "修复切换音频崩溃", Body: "修复了设备切换时的空指针。\n补充了回归测试。",
			Files: []git.CommitFile{{Status: "M", Path: "src/audio.cpp"}, {Status: "A", Path: "tests/audio_test.cpp"}},
		},
		diff: "diff --git a/src/audio.cpp b/src/audio.cpp\nindex 111..222 100644\n--- a/src/audio.cpp\n+++ b/src/audio.cpp\n@@ -10,7 +10,9 @@ void switchDevice() {\n-    old->stop();\n+    if (old) old->stop();\n+    new->start();\n",
	})
	dump("07-commit-detail", a)

	// 空仓库
	f := &fakeSvc{status: git.Status{Branch: "main"}}
	e := New(f)
	e.Update(tea.WindowSizeMsg{Width: 100, Height: 20})
	pump(t, e, e.refreshCmd(false))
	pump(t, e, e.reloadLogCmd())
	dump("08-empty-repo", e)

	// 窄终端
	a.Update(tea.WindowSizeMsg{Width: 60, Height: 16})
	a.overlay = overlayNone
	dump("09-narrow-60x16", a)

	// 分支右键菜单
	a.Update(tea.WindowSizeMsg{Width: 120, Height: 40})
	a.pane = PaneBranches
	a.sideSel = 0
	a.sideSelKey = a.sideItems[0].key
	a.modalAnchorX, a.modalAnchorY = 10, 4
	_ = a.openContextMenu()
	dump("10-branch-menu", a)
	a.modal = nil

	// 提交右键菜单
	a.pane = PaneCommits
	a.graphSel = a.dirtyOffset() + 1
	a.modalAnchorX, a.modalAnchorY = 60, 6
	_ = a.openContextMenu()
	dump("11-commit-menu", a)
	a.modal = nil

	// 文件区与内容区聚焦
	a.pane = PaneCommits
	a.graphSel = a.dirtyOffset() + 1
	pump(t, a, a.loadFilesForSelection())
	a.pane = PaneFiles
	a.fileSel = 0
	dump("12-files-focus", a)
}
