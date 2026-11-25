package ui

import (
	"fmt"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/x/ansi"
	"github.com/mattn/go-runewidth"

	"github.com/ssk-wh/gitc/internal/git"
	"github.com/ssk-wh/gitc/internal/graph"
	"github.com/ssk-wh/gitc/internal/i18n"
	"github.com/ssk-wh/gitc/internal/sanitize"
)

// View 渲染界面，并强制约束为终端尺寸（避免超宽换行/超高校验错位）。
func (a *App) View() string {
	if a.width <= 0 || a.height <= 0 {
		return ""
	}
	return a.finalizeView(a.viewRaw())
}

// finalizeView 把所有行裁剪到终端宽度并补齐到终端高度。
func (a *App) finalizeView(s string) string {
	lines := strings.Split(s, string(rune(10)))
	if len(lines) > a.height {
		lines = lines[:a.height]
	}
	for i, l := range lines {
		w := strWidth(l)
		if w > a.width {
			lines[i] = ansi.Truncate(l, a.width, "")
		} else if w < a.width {
			// 短行必须补满宽度，否则终端会残留上一帧的内容
			lines[i] = padRight(l, a.width)
		}
	}
	for len(lines) < a.height {
		lines = append(lines, strings.Repeat(" ", a.width))
	}
	return strings.Join(lines, string(rune(10)))
}

// statusLine 渲染左侧顶部的仓库/分支状态行。
func (a *App) statusLine(width int) string {
	branch := sanitize.Line(a.status.Branch)
	if a.status.Detached {
		branch = i18n.S("分离 HEAD", "detached")
	}
	line := styleTitle.Render("gitc") + styleDim.Render(" · ") + styleAccent.Render(sanitize.Line(a.svc.Name())) + styleDim.Render(" · ") + styleGood.Render(branch)
	if a.status.Ahead > 0 || a.status.Behind > 0 {
		line += styleCyan.Render(fmt.Sprintf("  ↑%d ↓%d", a.status.Ahead, a.status.Behind))
	}
	if n := a.status.Total(); n > 0 {
		line += styleWarn.Render(fmt.Sprintf("  ✱%d", n))
	}
	return truncate(line, width)
}

// viewRaw 渲染原始界面内容。
// pageSize 是浮层整页翻页的行数。
func (a *App) pageSize() int {
	n := a.height - 2
	if n < 1 {
		n = 1
	}
	return n
}

func (a *App) viewRaw() string {
	if a.overlay == overlayHelp {
		return a.viewHelp()
	}
	if a.overlay == overlayLog {
		return a.viewLog()
	}
	if a.overlay == overlayDetail {
		return a.viewDetailOverlay()
	}
	if a.modal != nil {
		base := a.baseView()
		switch a.modal.kind {
		case modalChoice:
			return a.viewChoiceOverlay(base)
		}
		return a.viewModal()
	}
	return a.baseView()
}

// baseView 渲染主界面（不含模态菜单）。
func (a *App) baseView() string {
	var b strings.Builder
	body := a.viewBody()
	if a.panelLayout().split {
		div := a.hoverDiv
		if a.dragMode != 0 {
			div = a.dragMode
		}
		if div != 0 {
			body = a.dividerHighlight(body, div, a.dragMode != 0)
		}
	}
	b.WriteString(body)
	b.WriteByte('\n')
	b.WriteString(styleDim.Render(strings.Repeat("─", a.width)))
	b.WriteByte('\n')
	b.WriteString(a.viewFooter())
	return b.String()
}

// viewChoiceOverlay 把右键/选择菜单浮在当前界面之上。
func (a *App) viewChoiceOverlay(base string) string {
	box, x, y := a.choiceMenuBox()
	if len(box) == 0 {
		return base
	}
	lines := strings.Split(base, "\n")
	for i, bl := range box {
		yy := y + i
		if yy < 0 || yy >= len(lines) {
			continue
		}
		// 在纯文本层按显示列拼接：保留菜单左右两侧内容，同时保证边框不偏移
		plain := ansi.Strip(lines[yy])
		left := runewidth.Truncate(plain, x, "")
		leftW := runewidth.StringWidth(left)
		left += strings.Repeat(" ", maxInt(0, x-leftW))
		endCol := x + strWidth(bl)
		tail, skipped := stripColumnPrefix(plain, endCol)
		if skipped < endCol {
			tail = strings.Repeat(" ", endCol-skipped) + tail
		}
		lines[yy] = padRight(truncate(left+bl+tail, a.width), a.width)
	}
	return strings.Join(lines, "\n")
}

// dividerHighlight 绘制拖动反馈：hover 为鼠标附近短手柄，拖动中整条绿色高亮。
func (a *App) dividerHighlight(body string, div int, dragging bool) string {
	lines := strings.Split(body, string(rune(10)))
	style := styleAccent.Bold(true)
	if dragging {
		style = styleGood.Bold(true)
	}
	l := a.panelLayout()
	horiz := div == 2 || div == 4 || div == 5
	ch := "║"
	if horiz {
		ch = "═"
	}
	if dragging {
		// 整条高亮
		switch div {
		case 1:
			for y := 0; y < len(lines) && y < l.bodyH; y++ {
				lines[y] = replaceColumn(lines[y], l.leftW, style.Render(ch), a.width)
			}
		case 3:
			for y := l.rightTopH + 1; y < l.rightTopH+1+l.drawerH && y < len(lines); y++ {
				lines[y] = replaceColumn(lines[y], l.leftW+1+a.drawerSplit(l.rightW), style.Render(ch), a.width)
			}
		case 4:
			lines = replaceRowSegment(lines, l.projH+1+l.leftTopH, 0, l.leftW, style.Render(strings.Repeat(ch, l.leftW)), a.width)
		case 2:
			lines = replaceRowSegment(lines, l.rightTopH, l.leftW+1, l.rightW, style.Render(strings.Repeat(ch, l.rightW)), a.width)
		case 5:
			lines = replaceRowSegment(lines, l.rightTopH+1+l.drawerH, l.leftW+1, l.rightW, style.Render(strings.Repeat(ch, l.rightW)), a.width)
		}
		return strings.Join(lines, string(rune(10)))
	}
	// hover：鼠标附近一小段手柄
	const handle = 6
	if horiz {
		y := a.hoverY
		x0, w := 0, l.leftW
		if div == 2 || div == 5 {
			x0, w = l.leftW+1, l.rightW
		}
		sx := a.hoverX - handle/2
		if sx < x0+1 {
			sx = x0 + 1
		}
		if sx+handle > x0+w-1 {
			sx = x0 + w - 1 - handle
		}
		lines = replaceRowSegment(lines, y, sx, handle, style.Render(strings.Repeat(ch, handle)), a.width)
	} else {
		x := l.leftW
		if div == 3 {
			x = l.leftW + 1 + a.drawerSplit(l.rightW)
		}
		sr := a.hoverY - handle/2
		if sr < 1 {
			sr = 1
		}
		for y := sr; y < sr+handle && y < len(lines) && y < l.bodyH-1; y++ {
			lines[y] = replaceColumn(lines[y], x, style.Render(ch), a.width)
		}
	}
	return strings.Join(lines, string(rune(10)))
}

// replaceColumn 用纯文本层替换某一显示列。
func replaceColumn(line string, col int, seg string, width int) string {
	plain := ansi.Strip(line)
	left := runewidth.Truncate(plain, col, "")
	left += strings.Repeat(" ", maxInt(0, col-runewidth.StringWidth(left)))
	tail, _ := stripColumnPrefix(plain, col+1)
	return padRight(truncate(left+seg+tail, width), width)
}

// replaceRowSegment 用纯文本层替换某一行的一段列区间。
func replaceRowSegment(lines []string, row, x0, w int, seg string, width int) []string {
	if row < 0 || row >= len(lines) || w <= 0 {
		return lines
	}
	plain := ansi.Strip(lines[row])
	left := runewidth.Truncate(plain, x0, "")
	left += strings.Repeat(" ", maxInt(0, x0-runewidth.StringWidth(left)))
	tail, _ := stripColumnPrefix(plain, x0+w)
	lines[row] = padRight(truncate(left+seg+tail, width), width)
	return lines
}

func strWidth(s string) int { return ansi.StringWidth(s) }

func truncate(s string, w int) string {
	if w <= 0 {
		return ""
	}
	if strWidth(s) <= w {
		return s
	}
	return ansi.Truncate(s, w, "…")
}

func padRight(s string, w int) string {
	if w <= 0 {
		return ""
	}
	d := w - strWidth(s)
	if d <= 0 {
		return truncate(s, w)
	}
	return s + strings.Repeat(" ", d)
}

// updateHintText 是新版本徽标的悬停提示。
func updateHintText(v string) string {
	return i18n.S("新版本 "+v+" 可用：运行 gitc --update 自动升级", "new version "+v+" available: run gitc --update")
}

// viewHeader 渲染顶部信息栏。
// buildVersion 是 Release 构建注入的 tag；本地构建为空。
var buildVersion string

// SetVersion 设置界面显示的版本号（空字符串表示不显示）。
func SetVersion(v string) { buildVersion = v }

func (a *App) viewHeader() string {
	parts := []string{styleTitle.Render("gitc")}
	if v := strings.TrimSpace(buildVersion); v != "" {
		if !strings.HasPrefix(v, "v") {
			v = "v" + v
		}
		parts = append(parts, styleDim.Render(v))
	}
	if a.updateAvailable && a.latestVersion != "" {
		sep := styleDim.Render("  ·  ")
		a.updateBadgeX = strWidth(strings.Join(parts, sep)) + strWidth(sep)
		badge := styleWarn.Bold(true).Render("⬆ " + a.latestVersion)
		a.updateBadgeW = strWidth(badge)
		parts = append(parts, badge)
	} else {
		a.updateBadgeX, a.updateBadgeW = 0, 0
	}
	if a.status.Ahead > 0 || a.status.Behind > 0 {
		parts = append(parts, styleCyan.Render(fmt.Sprintf("↑%d ↓%d", a.status.Ahead, a.status.Behind)))
	}
	if n := a.status.Total(); n > 0 {
		parts = append(parts, styleWarn.Render(fmt.Sprintf("✱%d", n)))
	}
	if a.state.InProgress() {
		parts = append(parts, styleBad.Bold(true).Render(strings.ToUpper(a.state.Kind)))
	}
	if a.busy {
		parts = append(parts, styleAccent.Render(spinnerFrame(a.spinner)+" "+a.taskName))
	} else if a.loadingLog {
		parts = append(parts, styleDim.Render(spinnerFrame(a.spinner)+" "+i18n.S("加载历史…", "loading history…")))
	}
	if a.truncated {
		parts = append(parts, styleWarn.Render(i18n.S("已截断", "truncated")))
	}
	line := strings.Join(parts, styleDim.Render("  ·  "))
	return truncate(line, a.width)
}

func spinnerFrame(i int) string {
	frames := []string{"⠋", "⠙", "⠹", "⠸", "⠼", "⠴", "⠦", "⠧", "⠇", "⠏"}
	return frames[i%len(frames)]
}

// viewBody 渲染工作台：每个区域一个完整边框（lazygit 风格）。
func (a *App) viewBody() string {
	h := a.bodyHeight()
	lines := make([]string, h)
	for i := range lines {
		lines[i] = strings.Repeat(" ", a.width)
	}
	for _, b := range a.paneBoxes() {
		box := a.renderPaneBox(b)
		for i := 0; i < len(box) && i < b.h; i++ {
			y := b.y + i
			if y < 0 || y >= h {
				break
			}
			left := ansi.Cut(lines[y], 0, b.x)
			right := ansi.Cut(lines[y], b.x+strWidth(box[i]), a.width)
			lines[y] = left + box[i] + right
		}
	}
	return strings.Join(lines, string(rune(10)))
}

// panelHeader 渲染带填充分割线的面板标题。
func panelHeader(title string, width int, focused bool) string {
	if width <= 0 {
		return ""
	}
	label := " " + title + " "
	line := "─" + label
	if fill := width - strWidth(line); fill > 0 {
		line += strings.Repeat("─", fill)
	}
	line = truncate(line, width)
	if focused {
		return styleAccent.Bold(true).Render(line)
	}
	return styleHeader.Render(line)
}

// viewProjectsPanel 渲染左上角项目区。
func (a *App) viewProjectsPanel(width, height int) string {
	header := panelHeader(i18n.S("项目", "PROJECTS"), width, a.pane == PaneProjects)
	if height <= 1 {
		return header
	}
	avail := height - 1
	if a.projectSel >= len(a.projects) {
		a.projectSel = len(a.projects) - 1
	}
	if a.projectSel < 0 {
		a.projectSel = 0
	}
	if a.projectSel < a.projectOff {
		a.projectOff = a.projectSel
	}
	if a.projectSel >= a.projectOff+avail {
		a.projectOff = a.projectSel - avail + 1
	}
	if a.projectOff < 0 {
		a.projectOff = 0
	}
	out := []string{header}
	end := a.projectOff + avail
	if end > len(a.projects) {
		end = len(a.projects)
	}
	if len(a.projects) == 0 {
		out = append(out, styleDim.Render(i18n.S("  （没有项目）", "  (no projects)")))
	}
	for i := a.projectOff; i < end; i++ {
		p := a.projects[i]
		marker := "  "
		style := styleAccent
		if a.pane == PaneProjects && i == a.projectSel {
			marker = "› "
			style = styleSel
		}
		cur := "  "
		if p.Path == a.currentPath {
			cur = "* "
			if a.pane != PaneProjects || i != a.projectSel {
				style = styleGood
			}
		}
		line := marker + cur + sanitize.Line(p.Name)
		if p.Path == a.currentPath {
			branch := sanitize.Line(a.status.Branch)
			if a.status.Detached {
				branch = i18n.S("分离 HEAD", "detached")
			}
			if branch != "" {
				line += "  " + branch
			}
		}
		out = append(out, truncate(style.Render(line), width))
	}
	for len(out) < height {
		out = append(out, "")
	}
	return strings.Join(out[:height], string(rune(10)))
}

// viewBranchesPanel 渲染左侧分支区（全高）。
func (a *App) viewBranchesPanel(width, height int) string {
	header := panelHeader("BRANCHES", width, a.pane == PaneBranches)
	if height <= 1 {
		return header
	}
	avail := height - 1
	lines := a.sidebarLines()
	if a.sideOff > len(lines) {
		a.sideOff = len(lines)
	}
	out := []string{header}
	end := a.sideOff + avail
	if end > len(lines) {
		end = len(lines)
	}
	for i := a.sideOff; i < end; i++ {
		ln := lines[i]
		if ln.header {
			out = append(out, truncate(styleHeader.Render(ln.text), width))
			continue
		}
		it := a.sideItems[ln.itemIdx]
		marker := " "
		style := lipgloss.NewStyle().Foreground(colorFg)
		if ln.itemIdx == a.sideSel && a.pane == PaneBranches {
			marker = "›"
			style = styleSel
		}
		name := strings.TrimLeft(it.label, " ")
		switch it.kind {
		case sideProject:
			if it.project.Path == a.currentPath {
				style = styleGood.Bold(true)
			} else if ln.itemIdx != a.sideSel {
				style = styleAccent
			}
		case sideBranch, sideRemote:
			col := a.refColor(it.ref.Kind, it.ref.Name)
			if it.ref.Head {
				style = lipgloss.NewStyle().Foreground(colorGood).Bold(true)
			} else if ln.itemIdx != a.sideSel {
				style = lipgloss.NewStyle().Foreground(col)
			}
		case sideTag:
			if ln.itemIdx != a.sideSel {
				style = styleWarn
			}
		case sideStash:
			if ln.itemIdx != a.sideSel {
				style = styleCyan
			}
		}
		if ln.itemIdx == a.sideSel {
			style = style.Bold(true)
		}
		text := truncate(" "+name, width-2)
		out = append(out, style.Render(marker+" "+text))
	}
	for len(out) < height {
		out = append(out, "")
	}
	return strings.Join(out[:height], "\n")
}

// viewFilesPanel 渲染文件列（窄屏时占满面板）。
func (a *App) viewFilesPanel(width, height int) string {
	header := panelHeader(a.filesTitle(), width, a.pane == PaneFiles)
	if height <= 1 {
		return header
	}
	body := a.fileColumnLines(width, height-1)
	out := append([]string{header}, body...)
	for len(out) < height {
		out = append(out, "")
	}
	return strings.Join(out[:height], "\n")
}

// viewContentPanel 渲染内容列（窄屏时占满面板）。
func (a *App) viewContentPanel(width, height int) string {
	header := panelHeader(a.contentTitle(), width, a.pane == PaneContent)
	if height <= 1 {
		return header
	}
	body := a.contentColumnLines(width, height-1)
	out := append([]string{header}, body...)
	for len(out) < height {
		out = append(out, "")
	}
	return strings.Join(out[:height], "\n")
}

// viewInfoPanel 渲染左下提交信息区。
func (a *App) viewInfoPanel(width, height int) string {
	header := panelHeader(i18n.S("提交信息", "COMMIT"), width, a.pane == PaneInfo)
	if height <= 1 {
		return header
	}
	avail := height - 1
	lines := a.infoLines(width)
	maxOff := len(lines) - avail
	if maxOff < 0 {
		maxOff = 0
	}
	if a.infoOff > maxOff {
		a.infoOff = maxOff
	}
	if a.infoOff < 0 {
		a.infoOff = 0
	}
	out := []string{header}
	end := a.infoOff + avail
	if end > len(lines) {
		end = len(lines)
	}
	for _, l := range lines[a.infoOff:end] {
		out = append(out, truncate(l, width))
	}
	for len(out) < height {
		out = append(out, "")
	}
	return strings.Join(out[:height], "\n")
}

// infoLines 生成提交信息面板内容行（按面板宽度换行）。
func (a *App) infoLines(width int) []string {
	if width < 10 {
		width = 10
	}
	styled := func(text string, st lipgloss.Style) []string {
		var out []string
		for _, l := range strings.Split(wrapText(text, width), string(rune(10))) {
			out = append(out, st.Render(l))
		}
		return out
	}
	add := func(out []string, text string, st lipgloss.Style) []string {
		return append(out, styled(text, st)...)
	}
	return a.infoLinesWith(width, add)
}

// infoLinesWith 用给定的换行/样式函数构造信息行，便于复用。
func (a *App) infoLinesWith(width int, add func([]string, string, lipgloss.Style) []string) []string {
	if a.onDirtyRow() {
		s := a.status
		var out []string
		out = add(out, i18n.S("未提交的改动", "uncommitted changes"), styleWarn.Bold(true))
		out = add(out, fmt.Sprintf("+%d staged  ~%d unstaged  ?%d untracked", len(s.Staged), len(s.Unstaged), len(s.Untracked)), styleDim)
		out = append(out, "")
		out = add(out, i18n.S("space 暂存 · d 丢弃 · D 全部丢弃", "space stage · d discard · D discard all"), styleDim)
		return out
	}
	c := a.selectedCommit()
	if c == nil {
		return add(nil, i18n.S("（没有选中的提交）", "(no commit selected)"), styleDim)
	}
	var out []string
	// 头部固定三行两列对齐：Commit / Author / Date
	out = add(out, fmt.Sprintf("%-8s%s", "Commit", c.Short), styleBold)
	author := sanitize.Line(c.Author)
	if a.commitAuthor != "" {
		author = sanitize.Line(a.commitAuthor)
	}
	out = add(out, fmt.Sprintf("%-8s%s", "Author", author), styleBold)
	out = add(out, fmt.Sprintf("%-8s%s", "Date", c.When.Format("02 Jan 2006 15:04")), styleBold)
	var refNames []string
	for _, r := range a.refsAt(c.Hash) {
		switch r.Kind {
		case "local":
			name := sanitize.Line(r.Name)
			if r.Head {
				name = "*" + name
			}
			refNames = append(refNames, name)
		case "remote":
			refNames = append(refNames, sanitize.Line(r.Name))
		case "tag":
			refNames = append(refNames, "tag:"+sanitize.Line(r.Name))
		}
	}
	// 按引用名整体换行，避免把 origin/master 从中间截断
	line := ""
	for _, n := range refNames {
		if line == "" {
			line = n
			continue
		}
		if strWidth(line)+2+strWidth(n) > width {
			out = add(out, line, styleMagenta)
			line = n
		} else {
			line += "  " + n
		}
	}
	if line != "" {
		out = add(out, line, styleMagenta)
	}
	out = append(out, "")
	out = add(out, sanitize.Line(c.Subject), styleAccent)
	if body := strings.TrimSpace(sanitize.Text(a.commitBody)); body != "" {
		out = append(out, "")
		out = add(out, body, styleDim)
	}
	return out
}

// viewCommandPanel 渲染右下角命令历史。
func (a *App) viewCommandPanel(width, height int) string {
	header := panelHeader(i18n.S("COMMANDS", "COMMANDS"), width, a.pane == PaneLog)
	if height <= 1 {
		return header
	}
	avail := height - 1
	body := make([]string, 0, avail)
	n := len(a.commands)
	end := n - a.cmdOffset
	if end < 0 {
		end = 0
	}
	start := end - avail
	if start < 0 {
		start = 0
	}
	for i := start; i < end; i++ {
		e := a.commands[i]
		line := e.at.Format("15:04:05") + "  " + sanitize.Line(e.line)
		body = append(body, truncate(styleDim.Render(truncate(line, width)), width))
	}
	for len(body) < avail {
		body = append([]string{""}, body...)
	}
	out := append([]string{header}, body...)
	for len(out) < height {
		out = append(out, "")
	}
	return strings.Join(out[:height], "\n")
}

// viewChangesPanel 渲染左下工作区改动区（树形）。
func (a *App) viewChangesPanel(width, height int) string {
	title := fmt.Sprintf("%s (%d)", i18n.S("工作区改动", "CHANGES"), len(a.changes))
	if width > 40 {
		title += i18n.S("  [a 暂存] [u 取消] [D 丢弃全部]", "  [a stage] [u unstage] [D discard all]")
	} else if width > 20 {
		title += "  a/u/D"
	}
	header := panelHeader(title, width, a.pane == PaneChanges)
	if height <= 1 {
		return header
	}
	body := treeLines(a.changesRows, a.changesExpanded, a.changeSel, &a.changeOff, width, height-1, a.pane == PaneChanges)
	out := append([]string{header}, body...)
	for len(out) < height {
		out = append(out, "")
	}
	return strings.Join(out[:height], "\n")
}

// viewCommitsPanel 渲染提交区。
func (a *App) viewCommitsPanel(width, height int) string {
	title := "COMMITS"
	if a.branchFilterLabel != "" {
		title += " · " + sanitize.Line(a.branchFilterLabel)
	}
	title += " ▾"
	header := panelHeader(title, width, a.pane == PaneCommits)
	if height <= 1 {
		return header
	}
	avail := height - 1
	out := []string{header}
	total := a.totalRows()
	if total == 0 {
		out = append(out,
			"",
			styleWarn.Render(i18n.S("仓库还没有提交", "no commits yet")),
			"",
			styleDim.Render(i18n.S("按 a 暂存全部改动，再按 c 提交", "press a to stage, then c to commit")),
		)
		for len(out) < height {
			out = append(out, "")
		}
		return strings.Join(out[:height], "\n")
	}
	vc := a.visibleCommits()
	layout := a.viewLayout()
	row := a.graphOff
	for len(out)-1 < avail && row < total {
		out = append(out, a.renderRow(row, layout, vc, width))
		row++
	}
	for len(out) < height {
		out = append(out, "")
	}
	out = out[:height]
	return strings.Join(out, "\n")
}

// drawerLines 渲染固定抽屉：左文件列 + 右内容列。
func (a *App) drawerLines(width, height int) []string {
	fw := a.drawerSplit(width)
	cw := width - fw - 1
	if cw < 10 {
		cw = 10
	}
	lines := make([]string, 0, height)
	head := panelHeader(a.filesTitle(), fw, a.pane == PaneFiles) + styleDim.Render("│") + panelHeader(a.contentTitle(), cw, a.pane == PaneContent)
	lines = append(lines, head)
	files := a.fileColumnLines(fw, height-1)
	content := a.contentColumnLines(cw, height-1)
	for i := 0; i < height-1; i++ {
		fl := ""
		cl := ""
		if i < len(files) {
			fl = files[i]
		}
		if i < len(content) {
			cl = content[i]
		}
		lines = append(lines, padRight(fl, fw)+styleDim.Render("│")+cl)
	}
	return lines
}

func (a *App) filesTitle() string {
	if a.filesOwner == "work" {
		return fmt.Sprintf("%s (%d)", i18n.S("工作区改动", "CHANGES"), len(a.files))
	}
	return fmt.Sprintf("FILES (%d)", len(a.files))
}

func (a *App) contentTitle() string {
	title := a.content.title
	if title == "" {
		title = i18n.S("内容", "CONTENT")
	}
	if a.content.loading {
		title += " …"
	}
	return title
}

// fileColumnLines 渲染文件列数据行（树形视图）。
func (a *App) fileColumnLines(width, height int) []string {
	return treeLines(a.fileRows, a.expanded, a.fileSel, &a.fileOff, width, height, a.pane == PaneFiles)
}

// treeLines 渲染树形文件列表的可见行。
func treeLines(rows []fileRow, expanded map[string]bool, sel int, off *int, width, height int, focused bool) []string {
	if *off < 0 {
		*off = 0
	}
	if sel < *off {
		*off = sel
	}
	if sel >= *off+height {
		*off = sel - height + 1
	}
	out := make([]string, 0, height)
	if len(rows) == 0 {
		out = append(out, styleDim.Render(i18n.S("（没有文件）", "  (no files)")))
		for len(out) < height {
			out = append(out, "")
		}
		return out
	}
	end := *off + height
	if end > len(rows) {
		end = len(rows)
	}
	for i := *off; i < end; i++ {
		row := rows[i]
		n := row.node
		marker := " "
		if i == sel {
			if focused {
				marker = styleSel.Render("›")
			} else {
				marker = styleAccent.Render("›")
			}
		}
		indent := strings.Repeat("  ", row.depth)
		if n.dir {
			arrow := "▸"
			if expanded[n.path] {
				arrow = "▾"
			}
			label := " " + indent + arrow + " " + n.name + "/"
			// 目录用灰色区分文件名；选中行用强调色加粗。
			style := lipgloss.NewStyle().Foreground(colorDim)
			if i == sel {
				style = styleSel
			}
			out = append(out, truncate(marker+style.Render(truncate(label, width-2)), width))
			continue
		}
		status, staged := nodeStatus(n)
		col := colorWarn
		switch status {
		case "A":
			col = colorGood
		case "D", "U":
			col = colorBad
		case "?":
			col = colorCyan
		case "R":
			col = colorMagenta
		}
		left := indent + n.name
		if staged {
			left += i18n.S(" (已暂存)", " (staged)")
		}
		left = truncate(left, width-4)
		line := marker + " " + left
		line = padRight(line, width-2)
		line += lipgloss.NewStyle().Foreground(col).Render(truncate(status, 1))
		out = append(out, truncate(line, width))
	}
	for len(out) < height {
		out = append(out, "")
	}
	return out
}

// nodeStatus 返回文件节点的状态字母与是否含已暂存条目。
func nodeStatus(n *fileNode) (string, bool) {
	stagedAny := false
	for _, e := range n.entries {
		if e.staged {
			stagedAny = true
			continue
		}
		return e.status, stagedAny
	}
	if len(n.entries) > 0 {
		return n.entries[0].status, stagedAny
	}
	return " ", false
}

// contentColumnLines 渲染内容列数据行。
func (a *App) contentColumnLines(width, height int) []string {
	lines := a.content.lines
	if a.content.err != "" {
		lines = []string{styleBad.Render(a.content.err)}
	}
	if len(lines) == 0 && !a.content.loading {
		lines = []string{styleDim.Render(i18n.S("（无可显示内容）", "(no content)"))}
	}
	if a.content.loading && len(lines) == 0 {
		lines = []string{styleDim.Render(i18n.S("加载中…", "loading…"))}
	}
	maxOff := len(lines) - height
	if maxOff < 0 {
		maxOff = 0
	}
	if a.content.offset > maxOff {
		a.content.offset = maxOff
	}
	if a.content.offset < 0 {
		a.content.offset = 0
	}
	out := make([]string, 0, height)
	end := a.content.offset + height
	if end > len(lines) {
		end = len(lines)
	}
	for _, ln := range lines[a.content.offset:end] {
		out = append(out, colorizeDiffLine(ln, width))
	}
	for len(out) < height {
		out = append(out, "")
	}
	return out
}

// viewGraph 渲染提交图区。
func (a *App) viewGraph(width, height int) string {
	vc := a.visibleCommits()
	layout := a.viewLayout()
	total := a.totalRows()
	if total == 0 {
		lines := []string{
			"",
			styleWarn.Render(i18n.S("仓库还没有提交", "no commits yet")),
			"",
			styleDim.Render(i18n.S("按 a 暂存全部改动，再按 c 提交", "press a to stage, then c to commit")),
		}
		for len(lines) < height {
			lines = append(lines, "")
		}
		return strings.Join(lines[:height], "\n")
	}
	off := a.graphOff
	end := off + height
	if end > total {
		end = total
	}
	var out []string
	for row := off; row < end; row++ {
		out = append(out, a.renderRow(row, layout, vc, width))
	}
	for len(out) < height {
		out = append(out, "")
	}
	return strings.Join(out, "\n")
}

// renderRow 渲染图区的一行。
func (a *App) renderRow(row int, layout *graph.Layout, vc []git.Commit, width int) string {
	selected := row == a.graphSel
	marker := " "
	if selected {
		marker = styleAccent.Render("›")
	}
	off := a.dirtyOffset()
	if row < off {
		n := a.status.Total()
		text := fmt.Sprintf(i18n.S("◐ 未提交变更 (%d)", "◐ uncommitted changes (%d)"), n)
		if len(a.status.Staged) > 0 || len(a.status.Unstaged) > 0 || len(a.status.Untracked) > 0 {
			text += fmt.Sprintf("  +%d ~%d ?%d", len(a.status.Staged), len(a.status.Unstaged), len(a.status.Untracked))
		}
		style := styleWarn.Bold(selected)
		line := marker + " " + style.Render(text)
		line = padRight(truncate(line, width), width)
		if selected {
			return styleRowSel.Render(padRight(ansi.Strip(line), width))
		}
		return line
	}
	idx := row - off
	if idx < 0 || idx >= len(vc) {
		return ""
	}
	c := vc[idx]
	var lr graph.Row
	if idx < len(layout.Rows) {
		lr = layout.Rows[idx]
	} else {
		lr = graph.Compute([]graph.Commit{{Hash: c.Hash, Parents: c.Parents}}).Rows[0]
	}

	graphWidth := layout.MaxLanes*2 - 1
	if graphWidth < 1 {
		graphWidth = 1
	}
	capW := width / 2
	if capW < 20 {
		capW = 20
	}
	if graphWidth > capW {
		graphWidth = capW
	}
	isHead := a.status.OID != "" && c.Hash == a.status.OID
	graphStr := a.renderGraphRow(lr, graphWidth, isHead)
	graphPad := ""
	if dw := graphWidth - strWidth(graphStr); dw > 0 {
		graphPad = strings.Repeat(" ", dw)
	}

	// 引用徽标
	refs := a.refsAt(c.Hash)
	if len(refs) > 4 {
		refs = refs[:4]
	}
	var badgeStyled strings.Builder
	badgeWidth := 0
	if isHead {
		head := styleGood.Bold(true).Render("[HEAD]")
		badgeStyled.WriteString(head + " ")
		badgeWidth += 7
	}
	for _, r := range refs {
		name := sanitize.Line(r.Name)
		if r.Kind == git.RefLocal && r.Head {
			name = "*" + name
		}
		txt := "[" + name + "]"
		w := strWidth(txt)
		if badgeWidth+w+1 > 36 {
			break
		}
		badgeWidth += w + 1
		style := refStyle(r.Kind)
		if r.Kind == git.RefLocal || r.Kind == git.RefRemote {
			style = lipgloss.NewStyle().Foreground(a.refColor(r.Kind, r.Name))
		}
		badgeStyled.WriteString(style.Render(txt) + " ")
	}

	sha := styleDim.Render(c.Short)
	subject := sanitize.Line(c.Subject)
	author := truncate(sanitize.Line(c.Author), 10)
	when := c.When.Format("02 Jan 2006 15:04")
	timeStyle := styleDim
	if isToday(c.When) {
		when = "•" + when
		timeStyle = styleGood.Bold(true)
	}

	// 计算可用宽度
	used := 2 + graphWidth + 1 + 7 + 1 + badgeWidth
	timeW := 19
	authorW := 0
	if width >= 115 {
		authorW = 11
	} else if width >= 100 {
		authorW = 8
	}
	subjW := width - used - timeW - authorW - 1
	if subjW < 8 {
		subjW = 8
	}
	if subjW > 200 {
		subjW = 200
	}

	subjStyled := truncate(subject, subjW)
	if selected {
		subjStyled = styleBold.Foreground(colorFg).Render(subjStyled)
	}
	if c.Parents == nil && len(vc) > 0 && idx == len(vc)-1 {
		subjStyled = styleDim.Render(truncate(subject, subjW))
	}

	var b strings.Builder
	b.WriteString(marker)
	b.WriteByte(' ')
	b.WriteString(graphStr)
	b.WriteString(graphPad)
	b.WriteByte(' ')
	b.WriteString(sha)
	b.WriteByte(' ')
	b.WriteString(badgeStyled.String())
	b.WriteString(subjStyled)
	usedSoFar := 2 + graphWidth + 1 + 7 + 1 + badgeWidth + strWidth(subjStyled)
	if authorW > 0 {
		pad := width - usedSoFar - authorW - timeW - 1
		if pad > 0 {
			b.WriteString(strings.Repeat(" ", pad))
		}
		b.WriteString(styleDim.Render(padRight(author, authorW)))
	} else {
		pad := width - usedSoFar - timeW - 1
		if pad > 0 {
			b.WriteString(strings.Repeat(" ", pad))
		}
	}
	b.WriteString(timeStyle.Render(padRight(when, timeW)))

	line := b.String()
	if strWidth(line) > width {
		line = ansi.Truncate(line, width, "")
	}
	if selected || a.rowInCommitSelection(row) {
		return styleRowSel.Render(padRight(ansi.Strip(line), width))
	}
	return line
}

// renderGraphRow 把布局行渲染为带颜色字符串。
func (a *App) renderGraphRow(r graph.Row, maxWidth int, isHead bool) string {
	var b strings.Builder
	w := 0
	for i, c := range r.LaneChars {
		if i == r.NodeLane && isHead {
			c = '◉'
		}
		if c != ' ' {
			col := -1
			if i < len(r.Colors) {
				col = r.Colors[i]
			}
			style := laneStyle(col)
			if i == r.NodeLane && isHead {
				style = styleGood.Bold(true)
			}
			b.WriteString(style.Render(string(c)))
		} else {
			b.WriteByte(' ')
		}
		w++
		if i < len(r.Links) {
			link := r.Links[i]
			if link != ' ' {
				col := -1
				if i < len(r.LinkColors) && r.LinkColors[i] >= 0 {
					col = r.LinkColors[i]
				}
				b.WriteString(laneStyle(col).Render(string(link)))
			} else {
				b.WriteByte(' ')
			}
			w++
		}
		if w >= maxWidth {
			break
		}
	}
	return b.String()
}

// viewFooter 渲染底部两行。
func (a *App) viewFooter() string {
	// 搜索：行内显示输入框
	if a.searchActive {
		line := styleAccent.Render("/ 搜索: ") + a.searchInput.View() + styleDim.Render(i18n.S("   ⏎ 全历史  esc 清除", "   ⏎ history  esc clear"))
		return truncate(line, a.width)
	}
	var msg string
	if a.toast != "" {
		if a.toastErr {
			msg = styleBad.Render("✘ " + truncate(a.toast, a.width/2))
		} else {
			msg = styleGood.Render("✔ " + truncate(a.toast, a.width/2))
		}
	} else if a.filter != "" {
		f := fmt.Sprintf(i18n.S("过滤: %s（%d 条）", "filter: %s (%d)"), a.filter, len(a.visibleCommits()))
		if a.searching {
			f += "  " + i18n.S("搜索全部历史中…", "searching full history…")
		}
		msg = styleAccent.Render(f)
	} else if a.searchResults != nil {
		msg = styleAccent.Render(fmt.Sprintf(i18n.S("全历史搜索: %s（%d 条，Esc 退出）", "history search: %s (%d, Esc to exit)"), a.searchQuery, len(a.searchResults)))
	}
	hints := styleDim.Render(a.contextHints())
	sep := styleDim.Render("  ·  ")
	head := a.viewHeader()
	// 消息过长时优先压缩消息，保证右侧快捷键提示可见
	if msg != "" {
		// 给快捷键提示预留空间（最多 30 列），空间不足时先压缩消息
		budget := strWidth(hints)
		if budget > 30 {
			budget = 30
		}
		cap := a.width - strWidth(head) - strWidth(sep)*2 - budget
		if cap < 8 {
			cap = 8
		}
		if strWidth(msg) > cap {
			msg = truncate(msg, cap)
		}
		// 剩余空间仍不够时截短提示，而不是直接丢弃
		if avail := a.width - strWidth(head) - strWidth(sep)*2 - strWidth(msg); avail < strWidth(hints) {
			hints = truncate(hints, maxInt(0, avail))
		}
	}
	line := head + sep
	if msg != "" {
		line += msg + sep
	}
	line += hints
	return truncate(line, a.width)
}

func (a *App) contextHints() string {
	if a.modal != nil && a.modal.kind == modalInput {
		return i18n.S("⏎ 确认  esc 取消", "⏎ confirm  esc cancel")
	}
	if a.width < 80 {
		return "↑↓ ⏎ c p/P f / b ? q"
	}
	if a.dragMode != 0 {
		switch a.dragMode {
		case 1:
			return fmt.Sprintf(i18n.S("拖动中：左栏 %d 列", "dragging: left %d cols"), a.panelLayout().leftW)
		case 4:
			return fmt.Sprintf(i18n.S("拖动中：分支区 %d 行", "dragging: branches %d rows"), a.panelLayout().leftTopH)
		case 2:
			return fmt.Sprintf(i18n.S("拖动中：提交区 %d 行", "dragging: commits %d rows"), a.panelLayout().rightTopH)
		case 5:
			return fmt.Sprintf(i18n.S("拖动中：命令区 %d 行", "dragging: commands %d rows"), a.panelLayout().cmdH)
		case 3:
			return fmt.Sprintf(i18n.S("拖动中：文件列 %d 列", "dragging: files %d cols"), a.drawerSplit(a.panelLayout().rightW))
		}
	}
	if a.hoverDiv != 0 {
		return i18n.S("拖动调整大小（双击复位）", "drag to resize (double-click resets)")
	}
	if a.searchActive {
		return i18n.S("输入实时过滤（hash/提交信息）  ⏎ 搜索全部历史  esc 清除", "type to filter (hash/message)  ⏎ full history  esc clear")
	}
	if a.state.InProgress() {
		return i18n.S("冲突/中断态：M 继续或终止；space 暂存已解决文件", "in progress: M to continue/abort; space stages resolved files")
	}
	if a.busy {
		return i18n.S("Esc 取消当前操作", "Esc cancel operation")
	}
	switch a.pane {
	case PaneBranches:
		return i18n.S("↑↓ 选择  ⏎ 检出  b/右键 菜单  Tab 切区  / 搜索  ? 帮助", "↑↓ select  ⏎ checkout  b/right-click menu  Tab switch  / search  ? help")
	case PaneChanges:
		return i18n.S("↑↓ 选择  space 暂存  d 丢弃  D 全部丢弃  a/u 全部  b/右键 菜单", "↑↓ select  space stage  d discard  D discard all  a/u all  b/right-click menu")
	case PaneFiles:
		return i18n.S("↑↓ 切换文件  ⏎ 内容区  space 暂存  d 丢弃  b 菜单", "↑↓ files  ⏎ content  space stage  d discard  b menu")
	case PaneContent:
		return i18n.S("↑↓ 滚动  PgUp/PgDn 翻页  / 搜索  Tab 切区  b 菜单", "↑↓ scroll  PgUp/PgDn page  / search  Tab switch  b menu")
	case PaneLog:
		return i18n.S("↑↓/滚轮 查看历史  G 最新  b/右键 菜单", "↑↓/wheel history  G latest  b/right-click menu")
	case PaneInfo:
		return i18n.S("↑↓/滚轮 阅读提交信息  ⏎ 完整详情  b/右键 菜单", "↑↓/wheel read info  ⏎ details  b/right-click menu")
	default:
		return i18n.S("↑↓ 选择提交  ⏎ 详情  b/右键 菜单  Tab 切区  / 搜索  ? 帮助", "↑↓ select commit  ⏎ details  b/right-click menu  Tab switch  / search  ? help")
	}
}

// isToday 判断是否为本地时间的今天。
func isToday(t time.Time) bool {
	y1, m1, d1 := t.Local().Date()
	y2, m2, d2 := time.Now().Local().Date()
	return y1 == y2 && m1 == m2 && d1 == d2
}

// relativeTime 返回相对时间文本。
func relativeTime(t time.Time) string {
	if t.IsZero() {
		return ""
	}
	d := time.Since(t)
	if d < 0 {
		d = -d
	}
	switch {
	case d < time.Minute:
		return i18n.S("刚刚", "now")
	case d < time.Hour:
		return fmt.Sprintf("%dm", int(d.Minutes()))
	case d < 24*time.Hour:
		return fmt.Sprintf("%dh", int(d.Hours()))
	case d < 30*24*time.Hour:
		return fmt.Sprintf("%dd", int(d.Hours()/24))
	case d < 365*24*time.Hour:
		return fmt.Sprintf("%dmo", int(d.Hours()/(24*30)))
	default:
		return t.Format("2006-01")
	}
}

// clampOverlayScroll 限制覆盖层滚动位置。
func (a *App) clampOverlayScroll() {
	var total int
	switch a.overlay {
	case overlayDetail:
		total = len(a.detail.lines)
	case overlayLog:
		total = len(a.logs)
	case overlayHelp:
		total = len(helpLines())
	}
	max := total - (a.height - 2)
	if max < 0 {
		max = 0
	}
	if a.detail.offset > max {
		a.detail.offset = max
	}
	if a.detail.offset < 0 {
		a.detail.offset = 0
	}
}

// viewDetailOverlay 渲染详情覆盖层。
func (a *App) viewDetailOverlay() string {
	viewH := a.height - 2 // 标题 + 底部提示各占 1 行
	if viewH < 1 {
		viewH = 1
	}
	var lines []string
	if a.detail.loading {
		lines = []string{styleDim.Render(i18n.S("加载中…", "loading…"))}
	} else if a.detail.err != "" {
		lines = []string{styleBad.Render(a.detail.err)}
	} else {
		lines = a.detail.lines
	}
	off := a.detail.offset
	end := off + viewH
	if end > len(lines) {
		end = len(lines)
	}
	if off > len(lines) {
		off = len(lines)
	}
	var b strings.Builder
	title := a.detail.title
	if title == "" {
		title = a.detail.sha
	}
	b.WriteString(styleTitle.Render(truncate(title, a.width)))
	b.WriteByte('\n')
	for _, ln := range lines[off:end] {
		b.WriteString(colorizeDiffLine(ln, a.width))
		b.WriteByte('\n')
	}
	b.WriteString(styleDim.Render(i18n.S("↑/↓ 滚动  PgUp/PgDn 翻页  esc 返回", "↑/↓ scroll  PgUp/PgDn page  esc back")))
	return a.withOverlayScrollbar(b.String(), len(lines), off, viewH)
}

// colorizeDiffLine 为 diff/详情行着色。
func colorizeDiffLine(line string, width int) string {
	l := truncate(line, width)
	switch {
	case strings.HasPrefix(line, "+") && !strings.HasPrefix(line, "+++"):
		return styleGood.Render(l)
	case strings.HasPrefix(line, "-") && !strings.HasPrefix(line, "---"):
		return styleBad.Render(l)
	case strings.HasPrefix(line, "@@"):
		return styleCyan.Render(l)
	case strings.HasPrefix(line, "commit "), strings.HasPrefix(line, "diff --git"), strings.HasPrefix(line, "index "), strings.HasPrefix(line, "+++"), strings.HasPrefix(line, "---"):
		return styleBold.Render(l)
	}
	return l
}

// viewHelp 渲染帮助覆盖层。
func (a *App) viewHelp() string {
	lines := helpLines()
	viewH := a.height - 2
	off := a.detail.offset
	end := off + viewH
	if end > len(lines) {
		end = len(lines)
	}
	if off > len(lines) {
		off = len(lines)
	}
	var b strings.Builder
	b.WriteString(styleTitle.Render(i18n.S("gitc 帮助", "gitc help")))
	b.WriteByte('\n')
	for _, ln := range lines[off:end] {
		b.WriteString(truncate(ln, a.width))
		b.WriteByte('\n')
	}
	b.WriteString(styleDim.Render(i18n.S("↑/↓ 滚动  PgUp/PgDn 翻页  esc 返回", "↑/↓ scroll  PgUp/PgDn page  esc back")))
	return b.String()
}

func helpLines() []string {
	rows := [][2]string{
		{"↑↓", i18n.S("当前区选择/滚动；内容区方向键上下翻页", "select/scroll in current pane")},
		{"PgUp / PgDn", i18n.S("翻页（内容区整页滚动）", "page up/down (full page in content)")},
		{"Tab / ←→", i18n.S("按顺序切换 7 个区域", "cycle 7 panes")},
		{"Alt+方向键", i18n.S("按空间位置切换区域（左3/右4）", "spatial focus across panes")},
		{"space / d / D", i18n.S("工作区文件区：暂存 / 丢弃所选 / 全部丢弃", "worktree files: stage / discard selected / discard all")},
		{"v / 点击 COMMITS 标题", i18n.S("选择要显示的分支（提交视图筛选）", "filter commits by branch")},
		{"右键标题", i18n.S("对整块生效：分支/改动/提交/文件/内容标题各有整体菜单", "right-click titles for overall actions")},
		{"Enter", i18n.S("分支检出 / 提交详情 / 文件聚焦内容区", "checkout / commit details / focus content")},
		{"Space", i18n.S("文件区暂存/取消暂存；未提交伪行打开菜单", "stage/unstage file; menu on uncommitted row")},
		{"c / A", i18n.S("提交 / amend（多行信息 Ctrl+S 提交）", "commit / amend (Ctrl+S submit)")},
		{"a / u", i18n.S("全部暂存 / 全部取消暂存", "stage all / unstage all")},
		{"f / F", i18n.S("fetch / fetch --prune", "fetch / fetch --prune")},
		{"p / P", i18n.S("pull / push", "pull / push")},
		{"z / Z", i18n.S("stash / 跳到 stash 分组", "stash / focus stashes")},
		{"b / 右键", i18n.S("打开当前区上下文菜单（分支/提交/文件）", "context menu for branch/commit/file")},
		{"C / V / X", i18n.S("cherry-pick / revert / reset", "cherry-pick / revert / reset")},
		{"t / y", i18n.S("打标签 / 复制 sha（OSC52）", "tag / copy sha (OSC52)")},
		{"m / B / d / D / r / n", i18n.S("分支：合并 / rebase / 删除 / 强制删除 / 重命名 / 新建", "branch: merge / rebase / delete / force-delete / rename / new")},
		{"o", i18n.S("stash：pop（菜单中还有 apply/drop）", "stash pop (apply/drop in menu)")},
		{"- / +", i18n.S("折叠 / 展开当前分组（也可点击分组标题）", "collapse / expand section (or click title)")},
		{"鼠标拖动", i18n.S("三条分隔线均可拖动：左栏宽度、左侧上下、右侧上下、文件/内容宽度（双击复位）", "drag dividers: left width, left/right split, files/content width (double-click resets)")},
		{"滚轮", i18n.S("在分支/提交/文件区移动，在内容区滚动", "scroll lists and content")},
		{"/", i18n.S("行内搜索（实时过滤，⏎ 搜全部历史）", "inline search (live filter, ⏎ full history)")},
		{":", i18n.S("透传任意 git 参数", "raw git args")},
		{"M", i18n.S("冲突态 continue / abort", "resolve merge/rebase: continue / abort")},
		{"T", i18n.S("切换主题（default/light/ocean/forest/fire/rose/high-contrast）", "cycle theme")},
		{"L / R / ?", i18n.S("命令日志 / 刷新 / 帮助", "command log / refresh / help")},
		{"右键项目区标题", i18n.S("添加项目 / 切换项目（系统目录选择框）", "add/switch projects (system directory picker)")},
		{"q / Ctrl+C", i18n.S("退出", "quit")},
	}
	// 顶部独立区域：项目与 GitHub 信息
	lines := []string{
		"  " + styleTitle.Render(i18n.S("项目信息", "Project")),
		"  " + styleAccent.Render(padRight("GitHub", 12)) + "https://github.com/ssk-wh/gitc",
		"  " + styleAccent.Render(padRight(i18n.S("发布页", "Releases"), 12)) + "https://github.com/ssk-wh/gitc/releases",
		"  " + styleAccent.Render(padRight(i18n.S("问题反馈", "Issues"), 12)) + "https://github.com/ssk-wh/gitc/issues",
		"  " + styleAccent.Render(padRight(i18n.S("升级", "Update"), 12)) + i18n.S("gitc --update（或重新运行安装脚本）", "gitc --update (or rerun the installer)"),
		"",
		"  " + styleDim.Render(strings.Repeat("─", 64)),
		"",
		"  " + styleTitle.Render(i18n.S("快捷键", "Keybindings")),
	}
	for _, r := range rows {
		lines = append(lines, "  "+styleAccent.Render(padRight(r[0], 22))+r[1])
	}
	return lines
}

// viewLog 渲染命令日志覆盖层。
func (a *App) viewLog() string {
	viewH := a.height - 2
	off := a.detail.offset
	end := off + viewH
	if end > len(a.logs) {
		end = len(a.logs)
	}
	if off > len(a.logs) {
		off = len(a.logs)
	}
	var b strings.Builder
	b.WriteString(styleTitle.Render(i18n.S("命令输出", "command output")))
	b.WriteByte('\n')
	for _, ln := range a.logs[off:end] {
		b.WriteString(truncate(ln, a.width))
		b.WriteByte('\n')
	}
	b.WriteString(styleDim.Render(i18n.S("↑/↓ 滚动  PgUp/PgDn 翻页  esc 返回", "↑/↓ scroll  PgUp/PgDn page  esc back")))
	return a.withOverlayScrollbar(b.String(), len(a.logs), off, viewH)
}

// withOverlayScrollbar 为全屏浮层绘制右侧滚动条。
func (a *App) withOverlayScrollbar(s string, total, off, visible int) string {
	lines := strings.Split(s, string(rune(10)))
	start, thumb, ok := scrollThumb(off, total, visible, visible)
	if !ok {
		return s
	}
	for i := range lines {
		lines[i] = padRight(lines[i], a.width)
	}
	for i := 0; i < thumb; i++ {
		idx := 1 + start + i
		// 最后一行是“↑/↓ 滚动 esc 返回”，不要覆盖
		if idx > 0 && idx < len(lines)-1 {
			lines[idx] = replaceCellStyled(lines[idx], a.width-1, styleAccent.Render("┃"))
		}
	}
	return strings.Join(lines, string(rune(10)))
}

// viewModal 渲染模态框。
func (a *App) viewModal() string {
	m := a.modal
	w := a.width - 8
	if w > 64 {
		w = 64
	}
	if w < 24 {
		w = 24
	}
	var body strings.Builder
	body.WriteString(styleTitle.Render(m.title))
	body.WriteString("\n\n")
	switch m.kind {
	case modalConfirm:
		if m.body != "" {
			body.WriteString(wrapText(m.body, w-4))
			body.WriteString("\n")
		}
		if m.command != "" {
			body.WriteString(styleDim.Render(truncate(m.command, w-4)))
			body.WriteString("\n")
		}
		if m.danger {
			body.WriteString(styleBad.Render(i18n.S("此操作不可撤销", "this cannot be undone")))
			body.WriteString("\n")
		}
		body.WriteString("\n" + styleAccent.Render(i18n.S("y 确认    n/esc 取消", "y confirm    n/esc cancel")))
	case modalDirPicker:
		body.WriteString(styleAccent.Render(truncate(m.dir, w-4)))
		body.WriteString(string(rune(10)) + string(rune(10)))
		entries := []string{i18n.S("✓ 使用当前目录", "✓ use this directory"), ".."}
		for _, d := range m.dirs {
			entries = append(entries, d+"/")
		}
		rows := a.height - 12
		if rows < 5 {
			rows = 5
		}
		if rows > 14 {
			rows = 14
		}
		if m.sel < m.listOff {
			m.listOff = m.sel
		}
		if m.sel >= m.listOff+rows {
			m.listOff = m.sel - rows + 1
		}
		if m.listOff < 0 {
			m.listOff = 0
		}
		end := m.listOff + rows
		if end > len(entries) {
			end = len(entries)
		}
		for i := m.listOff; i < end; i++ {
			marker := "  "
			style := lipgloss.NewStyle().Foreground(colorFg)
			if i == m.sel {
				marker = "› "
				style = styleSel
			}
			body.WriteString(style.Render(truncate(marker+entries[i], w-4)))
			body.WriteString(string(rune(10)))
		}
		if m.err != "" {
			body.WriteString(styleBad.Render(truncate(m.err, w-4)))
			body.WriteString(string(rune(10)))
		}
		body.WriteString(string(rune(10)) + styleDim.Render(i18n.S("↑↓ 选择  ⏎ 进入/选择  ← 上级  s 选当前  esc 取消", "↑↓ select  ⏎ enter/choose  ← up  s choose here  esc cancel")))
	case modalChoice:
		for i, c := range m.choices {
			marker := "  "
			style := lipgloss.NewStyle().Foreground(colorFg)
			if i == m.sel {
				marker = "› "
				style = styleSel
			}
			line := fmt.Sprintf("%s%d. %s", marker, i+1, c.label)
			if c.desc != "" {
				line += "  " + styleDim.Render(c.desc)
			}
			body.WriteString(style.Render(truncate(line, w-4)))
			body.WriteString("\n")
		}
		body.WriteString("\n" + styleDim.Render(i18n.S("↑↓ 选择  ⏎ 确认  esc 取消", "↑↓ select  ⏎ confirm  esc cancel")))
	case modalInput:
		body.WriteString(m.input.View())
		body.WriteString("\n\n" + styleDim.Render(i18n.S("⏎ 确认  esc 取消", "⏎ confirm  esc cancel")))
	case modalText:
		body.WriteString(m.text.View())
		body.WriteString("\n" + styleDim.Render(i18n.S("Ctrl+S 提交  esc 取消", "Ctrl+S submit  esc cancel")))
	}
	box := styleBox
	if m.danger {
		box = styleBoxBad
	}
	rendered := box.Width(w).Render(body.String())
	return lipgloss.Place(a.width, a.height, lipgloss.Center, lipgloss.Center, rendered)
}

// wrapText 简单按显示宽度换行（适配中文）。
func wrapText(s string, w int) string {
	if w <= 0 {
		return s
	}
	var out []string
	for _, para := range strings.Split(s, "\n") {
		line := ""
		width := 0
		for _, r := range para {
			if r == '	' {
				for k := 0; k < 4; k++ {
					if width+1 > w {
						out = append(out, line)
						line = ""
						width = 0
					}
					line += " "
					width++
				}
				continue
			}
			rw := runewidth.RuneWidth(r)
			if width+rw > w {
				out = append(out, line)
				line = ""
				width = 0
			}
			line += string(r)
			width += rw
		}
		out = append(out, line)
	}
	return strings.Join(out, "\n")
}

// stripColumnPrefix 删除纯文本前 cols 个显示列，返回剩余文本与实际删除列数。
func stripColumnPrefix(s string, cols int) (string, int) {
	removed := 0
	for i, r := range s {
		if removed >= cols {
			return s[i:], removed
		}
		rw := runewidth.RuneWidth(r)
		if removed+rw > cols {
			// 宽字符跨越边界：整体丢弃，多删的列由调用方补空格
			return s[i+len(string(r)):], removed + rw
		}
		removed += rw
	}
	return "", removed
}

// replaceCellStyled 替换第 col 个显示列上的字符，保留其余 ANSI 样式与宽度。
func replaceCellStyled(s string, col int, seg string) string {
	if col < 0 {
		return s
	}
	var b strings.Builder
	w := 0
	done := false
	for i := 0; i < len(s); {
		if s[i] == 0x1b {
			j := i + 1
			if j < len(s) && s[j] == '[' {
				j++
				for j < len(s) && !(s[j] >= 0x40 && s[j] <= 0x7e) {
					j++
				}
				if j < len(s) {
					j++
				}
			} else {
				j++
			}
			b.WriteString(s[i:j])
			i = j
			continue
		}
		r, size := utf8.DecodeRuneInString(s[i:])
		cw := runewidth.RuneWidth(r)
		if !done && w == col {
			b.WriteString(seg)
			done = true
			i += size
			w += cw
			continue
		}
		b.WriteString(s[i : i+size])
		w += cw
		i += size
	}
	if !done {
		b.WriteString(seg)
	}
	return b.String()
}

// scrollThumb 返回滑块在 rows 行视口中的起始行与高度。
func scrollThumb(off, total, visible, rows int) (int, int, bool) {
	if total <= visible || visible <= 0 || rows <= 0 {
		return 0, 0, false
	}
	thumb := rows * visible / total
	if thumb < 1 {
		thumb = 1
	}
	if thumb > rows {
		thumb = rows
	}
	start := 0
	if maxOff := total - visible; maxOff > 0 {
		start = off * (rows - thumb) / maxOff
	}
	if start < 0 {
		start = 0
	}
	if start+thumb > rows {
		start = rows - thumb
	}
	return start, thumb, true
}
