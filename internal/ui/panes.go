package ui

import (
	"fmt"
	"strings"

	"github.com/charmbracelet/lipgloss"

	"github.com/ssk-wh/gitc/internal/i18n"
	"github.com/ssk-wh/gitc/internal/sanitize"
)

// paneBox 是一个带完整边框的区域。
type paneBox struct {
	pane Pane
	x, y int
	w, h int
}

// paneBoxes 返回当前布局下所有区域的矩形（包含边框）。
func (a *App) paneBoxes() []paneBox {
	l := a.panelLayout()
	if !l.split {
		return []paneBox{{pane: a.pane, x: 0, y: 0, w: a.width, h: l.bodyH}}
	}
	fw := a.drawerSplit(l.rightW)
	rx := l.leftW + 1
	boxes := []paneBox{
		{PaneBranches, 0, 0, l.leftW, l.leftTopH},
		{PaneInfo, 0, l.leftTopH + 1, l.leftW, l.leftBotH},
		{PaneCommits, rx, 0, l.rightW, l.rightTopH},
		{PaneFiles, rx, l.rightTopH + 1, fw, l.drawerH},
		{PaneContent, rx + fw + 1, l.rightTopH + 1, l.rightW - fw - 1, l.drawerH},
	}
	if l.cmdH > 0 {
		boxes = append(boxes, paneBox{PaneLog, rx, l.rightTopH + 1 + l.drawerH + 1, l.rightW, l.cmdH})
	}
	return boxes
}

// paneTitle 返回区域中文标题。
func (a *App) paneTitle(p Pane) string {
	switch p {
	case PaneBranches:
		return i18n.S("分支", "Branches")
	case PaneChanges:
		return i18n.S("改动", "Changes")
	case PaneInfo:
		return i18n.S("提交信息", "Commit")
	case PaneCommits:
		title := i18n.S("提交", "Commits")
		switch a.branchFilterScope {
		case "local":
			title += i18n.S(" · 本地分支", " · local")
		case "remote":
			title += i18n.S(" · 远端分支", " · remote")
		}
		if a.branchFilterLabel != "" && a.branchFilter != "" {
			title += " · " + sanitize.Line(a.branchFilterLabel)
		}
		return title
	case PaneFiles:
		if a.filesOwner == "work" {
			return i18n.S("工作区文件", "Worktree files")
		}
		return i18n.S("文件", "Files")
	case PaneContent:
		title := a.content.title
		if title == "" {
			title = i18n.S("内容", "Content")
		}
		return title
	case PaneLog:
		return i18n.S("命令日志", "Commands")
	}
	return ""
}

// paneInfo 返回区域右下角基本信息。
func (a *App) paneInfo(p Pane) string {
	switch p {
	case PaneBranches:
		if len(a.sideItems) == 0 {
			return "0 of 0"
		}
		return fmt.Sprintf("%d of %d", a.sideSel+1, len(a.sideItems))
	case PaneInfo:
		total := len(a.infoLines(maxInt(10, a.panelLayout().leftW-2)))
		if total == 0 {
			return "0 of 0"
		}
		return fmt.Sprintf("%d of %d", minInt(a.infoOff+1, total), total)
	case PaneCommits:
		total := a.totalRows()
		if total == 0 {
			return "0 of 0"
		}
		return fmt.Sprintf("%d of %d", a.graphSel+1, total)
	case PaneFiles:
		if len(a.fileRows) == 0 {
			return "0 of 0"
		}
		return fmt.Sprintf("%d of %d", a.fileSel+1, len(a.fileRows))
	case PaneContent:
		total := len(a.content.lines)
		if total == 0 {
			return "0 of 0"
		}
		return fmt.Sprintf("%d of %d", minInt(a.content.offset+1, total), total)
	case PaneLog:
		return fmt.Sprintf("%d", len(a.commands))
	}
	return ""
}

// renderPaneBox 渲染一个带边框的区域。
func (a *App) renderPaneBox(b paneBox) []string {
	if b.w < 4 || b.h < 2 {
		return nil
	}
	title := a.paneTitle(b.pane)
	info := a.paneInfo(b.pane)
	innerW := b.w - 2
	innerH := b.h - 2
	content := a.paneContent(b.pane, innerW, innerH)
	lines := drawBox(title, info, innerW, innerH, a.pane == b.pane, content)
	a.drawPaneScrollbar(lines, b)
	return lines
}

// drawPaneScrollbar 在区域右边框绘制 lazygit 风格滚动条（滑块用粗线）。
func (a *App) drawPaneScrollbar(lines []string, b paneBox) {
	off, total, visible := a.paneScrollState(b)
	start, thumb, ok := scrollThumb(off, total, visible, b.h-2)
	if !ok {
		return
	}
	style := styleDim
	if a.pane == b.pane {
		style = styleAccent.Bold(true)
	}
	seg := style.Render("┃")
	for i := 0; i < thumb; i++ {
		idx := 1 + start + i
		if idx > 0 && idx < len(lines) && b.w > 0 {
			lines[idx] = replaceCellStyled(lines[idx], b.w-1, seg)
		}
	}
}

// paneScrollState 返回区域的滚动偏移、总行数与可视行数。
func (a *App) paneScrollState(b paneBox) (off, total, visible int) {
	switch b.pane {
	case PaneBranches:
		return a.sideOff, len(a.sidebarLines()), a.branchViewHeight()
	case PaneCommits:
		return a.graphOff, a.totalRows(), a.commitViewHeight()
	case PaneFiles:
		return a.fileOff, len(a.fileRows), a.fileViewHeight()
	case PaneContent:
		return a.content.offset, len(a.content.lines), a.contentViewHeight()
	case PaneInfo:
		return a.infoOff, len(a.infoLines(maxInt(10, b.w-2))), a.infoViewHeight()
	case PaneLog:
		visible = a.commandViewHeight()
		total = len(a.commands)
		off = total - visible - a.cmdOffset
		if off < 0 {
			off = 0
		}
		return off, total, visible
	}
	return 0, 0, 0
}

// paneContent 调用各区域渲染器并去掉其自带的标题行。
func (a *App) paneContent(p Pane, w, h int) []string {
	if h <= 0 || w <= 0 {
		return nil
	}
	var raw string
	switch p {
	case PaneBranches:
		raw = a.viewBranchesPanel(w, h+1)
	case PaneChanges:
		raw = a.viewChangesPanel(w, h+1)
	case PaneInfo:
		raw = a.viewInfoPanel(w, h+1)
	case PaneCommits:
		raw = a.viewCommitsPanel(w, h+1)
	case PaneFiles:
		raw = a.viewFilesPanel(w, h+1)
	case PaneContent:
		raw = a.viewContentPanel(w, h+1)
	case PaneLog:
		raw = a.viewCommandPanel(w, h+1)
	}
	lines := strings.Split(raw, "\n")
	if len(lines) > 1 {
		lines = lines[1:]
	} else {
		lines = nil
	}
	for len(lines) < h {
		lines = append(lines, "")
	}
	if len(lines) > h {
		lines = lines[:h]
	}
	return lines
}

// drawBox 绘制 lazygit 风格边框：左上角标题、右下角信息、焦点绿色。
func drawBox(title, info string, innerW, innerH int, focused bool, content []string) []string {
	if innerW < 0 {
		innerW = 0
	}
	if innerH < 0 {
		innerH = 0
	}
	border := lipgloss.NewStyle().Foreground(colorDim)
	if focused {
		border = lipgloss.NewStyle().Foreground(colorGood).Bold(true)
	}
	// 上边框：┌─ 标题 ───┐
	titleText := truncate(" "+title+" ", innerW-1)
	if titleText == "" {
		titleText = " "
	}
	fill := innerW - 1 - strWidth(titleText)
	if fill < 0 {
		fill = 0
	}
	top := border.Render("╭") + border.Render("─"+titleText+strings.Repeat("─", fill)) + border.Render("╮")
	// 下边框：└──── 信息 ─┘
	infoText := ""
	if info != "" {
		infoText = truncate(" "+info+" ", innerW)
	}
	fillBottom := innerW - strWidth(infoText)
	if fillBottom < 0 {
		fillBottom = 0
	}
	bottom := border.Render("╰") + border.Render(strings.Repeat("─", fillBottom)) + border.Render(infoText) + border.Render("╯")

	out := make([]string, 0, innerH+2)
	out = append(out, top)
	for i := 0; i < innerH; i++ {
		line := ""
		if i < len(content) {
			line = content[i]
		}
		line = padRight(truncate(line, innerW), innerW)
		out = append(out, border.Render("│")+line+border.Render("│"))
	}
	out = append(out, bottom)
	return out
}

func maxInt(a, b int) int {
	if a > b {
		return a
	}
	return b
}

func minInt(a, b int) int {
	if a < b {
		return a
	}
	return b
}
