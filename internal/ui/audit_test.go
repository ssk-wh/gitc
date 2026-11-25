package ui

import (
	"strings"
	"testing"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/x/ansi"

	"github.com/ssk-wh/gitc/internal/git"
)

func TestAltFocusNavigation(t *testing.T) {
	a, _ := newFakeApp(t)
	a.pane = PaneCommits
	a.altFocus("left")
	if a.pane != PaneBranches {
		t.Errorf("Alt+Left => %v", a.pane)
	}
	a.altFocus("down")
	if a.pane != PaneInfo {
		t.Errorf("Alt+Down 第二次 => %v", a.pane)
	}
	a.altFocus("right")
	if a.pane != PaneLog {
		t.Errorf("Alt+Right 从 Info => %v", a.pane)
	}
	a.altFocus("up")
	if a.pane != PaneFiles {
		t.Errorf("Alt+Up 从 Log => %v", a.pane)
	}
	a.altFocus("up")
	if a.pane != PaneCommits {
		t.Errorf("Alt+Up 第二次 => %v", a.pane)
	}
}

func TestMenuOutsideClickCloses(t *testing.T) {
	a, _ := newFakeApp(t)
	a.modalAnchorX, a.modalAnchorY = 20, 5
	a.choose("test", []choice{{label: "x"}})
	// 点在菜单框外（左上角）
	a.Update(tea.MouseMsg{Action: tea.MouseActionPress, Button: tea.MouseButtonLeft, X: 0, Y: 0})
	if a.modal != nil {
		t.Fatal("点击菜单外应关闭")
	}
}

func TestMenuBorderWidthsEqual(t *testing.T) {
	a, _ := newFakeApp(t)
	a.modalAnchorX, a.modalAnchorY = 10, 4
	a.choose("一个比较长的标题 用于测试边框宽度", []choice{
		{label: "第一项"},
		{label: "第二项很长很长很长很长很长很长很长很长很长很长很长很长"},
		{label: "第三项", desc: "描述"},
	})
	lines, _, _ := a.choiceMenuBox()
	w := strWidth(lines[0])
	for i, l := range lines {
		if strWidth(l) != w {
			t.Errorf("第 %d 行宽度 %d != %d: %q", i, strWidth(l), w, l)
		}
	}
	// 标题右侧必须用横线填充，不能用空格凑宽度（否则标题行会缺横线）。
	top := lines[0]
	if !strings.HasPrefix(top, "╭─ ") || !strings.HasSuffix(top, "─╮") {
		t.Errorf("标题行首尾不符合边框样式: %q", top)
	}
	if mid := strings.TrimSuffix(strings.TrimPrefix(top, "╭─ "), "─╮"); strings.Contains(mid, "  ") {
		t.Errorf("标题行出现空格填充（应为横线）: %q", top)
	}
}

// 带分页信息的菜单标题也不能把边框撑宽。
func TestMenuBorderWidthsEqualPaged(t *testing.T) {
	a, _ := newFakeApp(t)
	choices := make([]choice, 0, 50)
	for i := 0; i < 50; i++ {
		choices = append(choices, choice{label: "选项"})
	}
	a.choose("一个很长很长很长很长很长很长很长的菜单标题用于测试分页", choices)
	lines, _, _ := a.choiceMenuBox()
	if len(lines) < 2 {
		t.Fatal("菜单未渲染")
	}
	w := strWidth(lines[0])
	for i, l := range lines {
		if strWidth(l) != w {
			t.Errorf("第 %d 行宽度 %d != %d: %q", i, strWidth(l), w, l)
		}
	}
	if !strings.Contains(lines[0], "/50") {
		t.Errorf("分页信息缺失: %q", lines[0])
	}
}

func TestInlineInputRenderedInFooter(t *testing.T) {
	a, _ := newFakeApp(t)
	a.prompt("重命名分支", "new-name", "main", nil)
	v := plainView(a)

	if !strings.Contains(v, "重命名分支") {
		t.Errorf("行内输入标题缺失:\n%s", v)
	}
	if !strings.Contains(v, "⏎ 确认") {
		t.Errorf("行内输入提示缺失")
	}
	if strings.Contains(v, "╭─ 重命名") {
		t.Errorf("单行输入不应弹窗:\n%s", v)
	}
}

func TestIMETextIgnored(t *testing.T) {
	a, _ := newFakeApp(t)
	a.pane = PaneCommits
	before := a.graphSel
	a.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("中文输入")})
	if a.graphSel != before {
		t.Errorf("IME 文本不应改变选择")
	}
	if a.toast == "" {
		t.Errorf("应提示输入法注意事项")
	}
}

func TestBranchWheelScrollAudit(t *testing.T) {
	a, _ := newFakeApp(t)
	// 造出超过可视高度的分支列表
	a.sideItems = nil
	for i := 0; i < 60; i++ {
		a.sideItems = append(a.sideItems, sideItem{kind: sideBranch, label: "  b", key: "b", section: "branches"})
	}
	a.sideOff = 0
	a.Update(tea.MouseMsg{Action: tea.MouseActionPress, Button: tea.MouseButtonWheelDown, X: 2, Y: 5})
	if a.sideOff == 0 {
		t.Error("分支区滚轮应滚动视口")
	}
}

func TestTriPaneAndInfoPanel(t *testing.T) {
	a, _ := newFakeApp(t)
	a.commands = []commandEntry{{at: time.Now(), line: "git status"}}
	a.Update(tea.WindowSizeMsg{Width: 140, Height: 46})
	a.pane = PaneCommits
	a.graphSel = a.dirtyOffset() + 1
	pump(t, a, a.loadFilesForSelection())
	// 文件列表加载会回填 detail.Body，这里在加载后注入长正文验证 INFO 自动换行。
	a.commitBody = "第一段提交正文，包含比较长的中文内容用于验证自动换行是否正确。\n第二段正文。"
	v := plainView(a)
	for _, want := range []string{"分支", "提交信息", "提交", "命令日志"} {
		if !strings.Contains(v, want) {
			t.Errorf("三分区布局缺少 %q:\n%s", want, v)
		}
	}
	if !strings.Contains(v, "第一段提交正文") {
		t.Errorf("提交正文未显示")
	}
}

func TestSevenPaneCycle(t *testing.T) {
	a, _ := newFakeApp(t)
	a.commands = []commandEntry{{at: time.Now(), line: "git status"}}
	a.pane = PaneBranches
	seen := map[Pane]bool{}
	for i := 0; i < 7; i++ {
		seen[a.pane] = true
		a.nextPane(1)
	}
	if len(seen) != 7 {
		t.Errorf("Tab 循环覆盖 %d 个区域，期望 7", len(seen))
	}
	if seen[PaneChanges] {
		t.Error("已移除的改动区不应进入 Tab 循环")
	}
	if !seen[PaneProjects] {
		t.Error("Tab 循环应包含项目区")
	}
}

func TestBranchSortByTipTime(t *testing.T) {
	a, _ := newFakeApp(t)
	a.commits[0].When = time.Now().Add(-10 * time.Minute) // aaaa 最新
	a.commits[1].When = time.Now().Add(-2 * time.Hour)
	a.refs = []git.Ref{
		{Name: "old", Kind: git.RefLocal, Target: "bbbbbbbbbbbb2222"},
		{Name: "new", Kind: git.RefLocal, Target: "aaaaaaaaaaaa1111"},
	}
	a.rebuildSidebar()
	if len(a.sideItems) < 2 || a.sideItems[0].ref.Name != "new" {
		t.Errorf("分支应按 tip 时间倒序: %+v", a.sideItems)
	}
}

func TestCommitBranchColors(t *testing.T) {
	a, _ := newFakeApp(t)
	a.rebuildCommitColors()
	if a.commitColor["aaaaaaaaaaaa1111"] != a.refColorIndex(git.RefLocal, "main") {
		t.Errorf("tip 颜色错误: %v", a.commitColor)
	}
	if a.commitColor["bbbbbbbbbbbb2222"] != a.refColorIndex(git.RefLocal, "feature/x") {
		t.Errorf("feature tip 颜色错误: %v", a.commitColor)
	}
	// 父提交继承最近分支颜色
	if _, ok := a.commitColor["cccccccccccc3333"]; !ok {
		t.Errorf("父提交缺少颜色")
	}
}

func TestDividerHighlightNoDuplication(t *testing.T) {
	a, _ := newFakeApp(t)
	a.Update(tea.WindowSizeMsg{Width: 120, Height: 30})
	line := "[38;2;61;169;252m中文文本 abc[0m" + strings.Repeat("x", 20)
	rows := make([]string, a.bodyHeight())
	for i := range rows {
		rows[i] = line
	}
	out := a.dividerHighlight(strings.Join(rows, string(rune(10))), 1, false)
	plainOut := ansi.Strip(out)
	if strings.Count(plainOut, "中文文本") != len(rows) {
		t.Errorf("高亮不应复制文本: 出现 %d 次", strings.Count(plainOut, "中文文本"))
	}
	for i, l := range strings.Split(plainOut, string(rune(10))) {
		if w := strWidth(l); w > a.width {
			t.Errorf("第 %d 行宽 %d > %d", i, w, a.width)
		}
	}
}
func TestHoverAndClicksNoCorruption(t *testing.T) {
	a, _ := newFakeApp(t)
	a.Update(tea.WindowSizeMsg{Width: 160, Height: 46})
	l := a.panelLayout()
	// 鼠标移动到左侧分隔线并停留
	a.Update(tea.MouseMsg{Action: tea.MouseActionMotion, X: l.leftW, Y: 8})
	if a.hoverDiv == 0 {
		t.Fatal("应识别分隔线悬停")
	}
	// 连续点击几个提交
	for row := 0; row < 3; row++ {
		y := 2 + row
		a.Update(tea.MouseMsg{Action: tea.MouseActionPress, Button: tea.MouseButtonLeft, X: l.leftW + 5, Y: y})
		_ = plainView(a)
	}
	v := plainView(a)
	// 同一行内容不应被复制（悬停手柄曾因 ANSI 切片错误导致整行重复）
	seen := map[string]int{}
	for _, line := range strings.Split(v, string(rune(10))) {
		trimmed := strings.TrimSpace(line)
		if strWidth(trimmed) < 8 {
			continue
		}
		if strings.Trim(trimmed, "─│┃║═╭╮╰╯ ") == "" {
			continue
		}
		seen[trimmed]++
		if seen[trimmed] > 1 {
			t.Errorf("整行重复: %q", trimmed)
		}
	}
	for i, line := range strings.Split(v, string(rune(10))) {
		if w := strWidth(line); w > a.width {
			t.Errorf("第 %d 行宽 %d > %d", i, w, a.width)
		}
	}
}

func TestLongCJKTextWidthSafe(t *testing.T) {
	a, _ := newFakeApp(t)
	a.Update(tea.WindowSizeMsg{Width: 120, Height: 40})
	long := strings.Repeat("很长的中文分支名称", 6)
	a.refs = []git.Ref{
		{Name: long, Kind: git.RefLocal, Target: "aaaaaaaaaaaa1111", Head: true},
		{Name: strings.Repeat("标签", 20), Kind: git.RefTag, Target: "cccccccccccc3333"},
	}
	a.commits[0].Subject = strings.Repeat("提交信息非常长", 12)
	a.commits[1].Author = strings.Repeat("作者", 20)
	a.status.Untracked = []git.FileChange{{Path: strings.Repeat("深层目录/", 12) + "x.txt", Untracked: true}}
	a.rebuildSidebar()
	a.rebuildChanges()
	a.commitBody = strings.Repeat("正文内容很长", 30)
	// 悬停分隔线 + 进入信息区
	l := a.panelLayout()
	a.Update(tea.MouseMsg{Action: tea.MouseActionMotion, X: l.leftW, Y: 6})
	for i, line := range strings.Split(plainView(a), string(rune(10))) {
		if w := strWidth(line); w > a.width {
			t.Errorf("第 %d 行宽 %d > %d: %q", i, w, a.width, line)
		}
	}
}

func TestCommitWheelDoesNotChangeSelection(t *testing.T) {
	a, _ := newFakeApp(t)
	a.pane = PaneCommits
	a.graphSel = 0
	a.graphOff = 0
	// 造出足够多的提交以允许滚动
	for i := 0; i < 100; i++ {
		a.commits = append(a.commits, git.Commit{Hash: string(rune(0x4e00 + i)), Short: "x"})
	}
	a.Update(tea.MouseMsg{Action: tea.MouseActionPress, Button: tea.MouseButtonWheelDown, X: 60, Y: 5})
	if a.graphSel != 0 {
		t.Errorf("滚轮不应改变选中提交，实际 %d", a.graphSel)
	}
	if a.graphOff == 0 {
		t.Errorf("滚轮应滚动视口")
	}
}

func TestViewAlwaysTerminalSized(t *testing.T) {
	a, _ := newFakeApp(t)
	a.Update(tea.WindowSizeMsg{Width: 100, Height: 30})
	a.pane = PaneCommits
	a.graphSel = a.dirtyOffset() + 1
	pump(t, a, a.loadFilesForSelection())
	// 正文与 diff 里塞入 tab 和超长中文
	a.commitBody = "\t缩进代码\n" + strings.Repeat("超长正文内容", 40)
	a.content.lines = []string{"\t+" + strings.Repeat("很长的差异行", 30)}
	a.content.offset = 0
	v := a.View()
	lines := strings.Split(v, string(rune(10)))
	if len(lines) != a.height {
		t.Errorf("渲染行数 = %d, 期望 %d", len(lines), a.height)
	}
	for i, l := range lines {
		if w := strWidth(l); w > a.width {
			t.Errorf("第 %d 行宽 %d > %d", i, w, a.width)
		}
	}
}

func TestMenuOverlayKeepsBackgroundText(t *testing.T) {
	a, _ := newFakeApp(t)
	a.Update(tea.WindowSizeMsg{Width: 120, Height: 40})
	a.modalAnchorX, a.modalAnchorY = 30, 5
	a.choose("测试菜单", []choice{{label: "一"}, {label: "二"}})
	box, x, y := a.choiceMenuBox()
	base := make([]string, 40)
	for i := range base {
		base[i] = "左" + strings.Repeat("=", 90) + "右"
	}
	out := plainView(a) // 只用于确保布局
	_ = out
	spliced := a.viewChoiceOverlay(strings.Join(base, string(rune(10))))
	lines := strings.Split(spliced, string(rune(10)))
	row := lines[y]
	if !strings.HasPrefix(row, "左") {
		t.Errorf("菜单左侧背景被清空: %q", row)
	}
	if !strings.HasSuffix(strings.TrimRight(row, " "), "右") {
		t.Errorf("菜单右侧背景被清空: %q", row)
	}
	for i, l := range box {
		if strWidth(l) != strWidth(box[0]) {
			t.Errorf("菜单第 %d 行宽度不一致", i)
		}
		_ = x
	}
}

func TestAltRightFilesToContent(t *testing.T) {
	a, _ := newFakeApp(t)
	a.pane = PaneFiles
	a.altFocus("right")
	if a.pane != PaneContent {
		t.Errorf("文件区 Alt+Right = %v, 期望内容区", a.pane)
	}
	a.altFocus("left")
	if a.pane != PaneFiles {
		t.Errorf("内容区 Alt+Left = %v, 期望文件区", a.pane)
	}
	a.altFocus("left")
	if a.pane != PaneBranches {
		t.Errorf("文件区再次 Alt+Left = %v, 期望分支区", a.pane)
	}
}

func TestDividerHoverAndDragHighlight(t *testing.T) {
	a, _ := newFakeApp(t)
	a.Update(tea.WindowSizeMsg{Width: 120, Height: 40})
	l := a.panelLayout()
	// hover 竖分隔线：整列 ┃ + 提示
	a.Update(tea.MouseMsg{Action: tea.MouseActionMotion, X: l.leftW, Y: 5})
	if a.hoverDiv != 1 {
		t.Fatalf("hoverDiv = %d", a.hoverDiv)
	}
	v := plainView(a)
	if !strings.Contains(v, "║") || !strings.Contains(v, "拖动调整大小") {
		t.Errorf("hover 分隔线未高亮或缺少提示")
	}
	// 拖动：底栏显示尺寸
	a.Update(tea.MouseMsg{Action: tea.MouseActionPress, Button: tea.MouseButtonLeft, X: l.leftW, Y: 5})
	a.Update(tea.MouseMsg{Action: tea.MouseActionMotion, X: l.leftW + 2, Y: 5})
	v = plainView(a)
	if !strings.Contains(v, "拖动中：左栏") {
		t.Errorf("拖动时缺少尺寸提示")
	}
	// hover 横分隔线：整行 ━
	a.dragMode = 0
	a.hoverDiv = 0
	l = a.panelLayout()
	a.Update(tea.MouseMsg{Action: tea.MouseActionMotion, X: 4, Y: l.projH + 1 + l.leftTopH})
	if a.hoverDiv != 4 {
		t.Fatalf("hoverDiv = %d, 期望 4", a.hoverDiv)
	}
	if !strings.Contains(plainView(a), "═") {
		t.Errorf("横分隔线未高亮")
	}
}

func TestCommitInfoOrder(t *testing.T) {
	a, _ := newFakeApp(t)
	a.pane = PaneCommits
	a.graphSel = a.dirtyOffset() + 1
	pump(t, a, a.loadFilesForSelection())
	a.commitBody = "正文"
	lines := a.infoLines(60)
	joined := strings.Join(lines, string(rune(10)))
	hashIdx := strings.Index(joined, "Commit")
	authorIdx := strings.Index(joined, "Author")
	timeIdx := strings.Index(joined, "Date")
	subjIdx := strings.Index(joined, "支持摄像头分辨率")
	if hashIdx < 0 || authorIdx < 0 || timeIdx < 0 || subjIdx < 0 {
		t.Fatalf("信息缺失: %q", joined)
	}
	if !(hashIdx < authorIdx && authorIdx < timeIdx && timeIdx < subjIdx) {
		t.Errorf("顺序应为 Commit -> Author -> Date -> 标题，实际 %d/%d/%d/%d", hashIdx, authorIdx, timeIdx, subjIdx)
	}
}

func TestFileTreeOnlyBasename(t *testing.T) {
	a, _ := newFakeApp(t)
	a.setFiles([]fileEntry{{path: "src/deep/a.cpp", status: "M"}})
	a.rebuildFileRows()
	lines := a.fileColumnLines(60, 10)
	for _, l := range lines {
		if strings.Contains(l, "a.cpp") {
			if strings.Contains(ansi.Strip(l), "src/deep") {
				t.Errorf("文件行不应重复显示目录: %q", l)
			}
			return
		}
	}
	t.Fatal("未找到文件行")
}

func TestCommitInfoShowsTags(t *testing.T) {
	a, _ := newFakeApp(t)
	a.pane = PaneCommits
	a.graphSel = a.dirtyOffset() + 2 // 初始提交，带 v1.0 tag
	pump(t, a, a.loadFilesForSelection())
	joined := strings.Join(a.infoLines(80), string(rune(10)))
	if !strings.Contains(joined, "tag:v1.0") {
		t.Errorf("提交信息应显示 tag: %q", joined)
	}
}
