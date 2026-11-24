package ui

import (
	"os"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
)

// 临时验收辅助：强制真彩色后导出 ANSI，人工核对焦点边框与选中行背景。
func TestTmpVerifyANSI(t *testing.T) {
	lipgloss.SetColorProfile(2) // 2 = TrueColor? 依赖 termenv 常量；若不符仅作观察
	a, _ := newFakeApp(t)
	a.Update(tea.WindowSizeMsg{Width: 120, Height: 40})
	a.pane = PaneBranches
	os.WriteFile("D:/iflytek_projects/linux/projects/12-终端git工具gitc/tmp/ansi-main.txt", []byte(a.View()), 0o644)
	a.pane = PaneCommits
	a.graphSel = a.dirtyOffset() + 1
	os.WriteFile("D:/iflytek_projects/linux/projects/12-终端git工具gitc/tmp/ansi-commitsel.txt", []byte(a.View()), 0o644)
}
