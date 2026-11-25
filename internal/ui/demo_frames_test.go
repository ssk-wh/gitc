package ui

import (
	"fmt"
	"os"
	"path/filepath"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/muesli/termenv"

	"github.com/ssk-wh/gitc/internal/git"
	"github.com/ssk-wh/gitc/internal/i18n"
)

// TestDumpRealFrames 用真实仓库导出带 ANSI 颜色的界面帧，供 README 示意图/GIF 使用。
// 设置 GITC_DUMP_ANSI_DIR 与 GITC_DEMO_REPO 时生效。
func TestDumpRealFrames(t *testing.T) {
	dir := os.Getenv("GITC_DUMP_ANSI_DIR")
	repoPath := os.Getenv("GITC_DEMO_REPO")
	if dir == "" || repoPath == "" {
		t.Skip("未设置 GITC_DUMP_ANSI_DIR / GITC_DEMO_REPO")
	}
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	svc, err := git.Open(repoPath)
	if err != nil {
		t.Fatal(err)
	}
	i18n.Set("zh")
	SetVersion("v1.0.0")
	defer SetVersion("")
	lipgloss.SetColorProfile(termenv.TrueColor)
	lipgloss.SetHasDarkBackground(true)
	a := New(svc)
	a.Update(tea.WindowSizeMsg{Width: 118, Height: 36})
	pump(t, a, a.Init())
	frame := func(name string) {
		t.Helper()
		if err := os.WriteFile(filepath.Join(dir, name+".ansi"), []byte(a.View()), 0o644); err != nil {
			t.Fatal(err)
		}
	}

	// 选中第一条正常提交，加载文件与 diff。
	a.pane = PaneCommits
	a.graphSel = a.dirtyOffset()
	a.clampSelection()
	if c := a.selectedCommit(); c != nil {
		a.selHash = c.Hash
		pump(t, a, a.loadFilesForSelection())
	}
	frame("01-main")

	// 提交右键菜单
	if a.selectedCommit() != nil {
		a.modalAnchorX, a.modalAnchorY = 58, 8
		_ = a.openContextMenu()
		frame("02-menu")
		a.modal = nil
	}

	// 行内搜索：逐字输入，每步一帧
	pump(t, a, sendKey(a, tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("/")}))
	frame("03-search-empty")
	for i, r := range []rune("发布") {
		pump(t, a, sendRunes(a, string(r)))
		frame(fmt.Sprintf("04-search-%d", i+1))
	}
	pump(t, a, sendKey(a, tea.KeyMsg{Type: tea.KeyEsc}))

	// 提交详情
	a.pane = PaneCommits
	a.graphSel = a.dirtyOffset()
	a.clampSelection()
	if c := a.selectedCommit(); c != nil {
		pump(t, a, a.openCommitDetail(c.Hash))
		frame("05-detail")
		a.overlay = overlayNone
	}

	// 复制提示
	text := "copied commit message"
	if c := a.selectedCommit(); c != nil {
		text = c.Subject
	}
	a.copyToClipboard(text)
	a.Update(copyMsg{label: copyLabel(text)})
	frame("06-copied")
}
