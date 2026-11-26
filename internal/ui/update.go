package ui

import (
	"context"
	"errors"
	"fmt"
	"path/filepath"
	"strings"
	"time"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/ssk-wh/gitc/internal/git"
	"github.com/ssk-wh/gitc/internal/graph"
	"github.com/ssk-wh/gitc/internal/i18n"
	"github.com/ssk-wh/gitc/internal/platform"
)

// progressMsg 是一条实时进度（fetch/push/pull）。
type progressMsg struct{ line string }

// Update 处理消息。
func (a *App) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch m := msg.(type) {
	case tea.WindowSizeMsg:
		a.width, a.height = m.Width, m.Height
		a.clampSelection()
		a.ensureSideVisible()
		return a, nil

	case tea.KeyMsg:
		return a.handleKey(m)

	case tea.MouseMsg:
		return a.handleMouse(m)

	case spinnerTickMsg:
		if a.busy || a.loadingLog || a.searching {
			a.spinner++
			a.ticking = true
			return a, spinnerTickCmd()
		}
		a.ticking = false
		return a, nil

	case commandMsg:
		a.commands = append(a.commands, commandEntry{at: m.at, line: m.line})
		if len(a.commands) > 300 {
			a.commands = a.commands[len(a.commands)-300:]
		}
		a.clampCmdOffset()
		return a, nil

	case copyMsg:
		if m.err != nil {
			a.setToast(i18n.S("复制失败: ", "copy failed: ")+firstLine(m.err.Error()), true)
		} else {
			a.setToast(i18n.S("已复制: ", "copied: ")+m.label, false)
		}
		return a, nil

	case progressMsg:
		line := strings.TrimSpace(m.line)
		if line != "" {
			a.setToast(line, false)
		}
		return a, nil

	case refreshMsg:
		if m.gen != a.refreshGen {
			return a, nil
		}
		if m.err != nil {
			a.setToast(firstLine(m.err.Error()), true)
			return a, nil
		}
		a.status = m.status
		if a.currentPath != "" {
			a.projectBranch[a.currentPath] = m.status.Branch
		}
		a.refs = m.refs
		a.state = m.state
		a.stashes = m.stashes
		a.rebuildCommitColors()
		if len(a.commits) > 0 {
			a.rebuildLayout()
		}
		a.rebuildSidebar()
		a.rebuildChanges()
		a.clampSelection()
		a.invalidateView()
		var extra tea.Cmd
		if a.onDirtyRow() {
			extra = a.loadWorkingFiles()
		}
		if m.quick {
			return a, tea.Batch(a.refreshCmd(false), extra)
		}
		return a, extra

	case logBatchMsg:
		if m.gen != a.logGen {
			return a, nil
		}
		if a.logPreserve {
			// 首批数据到达：用新流替换缓存画面
			a.logPreserve = false
			a.commits = nil
			a.commitColor = map[string]int{}
			a.builder = graph.NewBuilder()
			a.layout = a.builder.Layout()
			a.invalidateView()
		}
		a.loadingLog = !m.done
		if len(m.commits) > 0 {
			first := a.builder.Len() == 0
			a.commits = append(a.commits, m.commits...)
			a.builder.SetColorOf(a.commitColor)
			for _, c := range m.commits {
				a.builder.Add(graph.Commit{Hash: c.Hash, Parents: c.Parents})
			}
			a.layout = a.builder.Layout()
			a.invalidateView()
			if len(a.commits) >= maxCommits && !m.done {
				a.truncated = true
			}
			// 颜色/分支排序较重：首批与结束（或用户刷新）时计算，流式过程中节流
			if first || m.done {
				a.rebuildCommitColors()
				a.rebuildSidebar()
			}
			if m.done {
				a.rebuildLayout()
			}
			a.restoreSelection()
		}
		extra := a.loadFilesForSelection()
		if m.err != nil && !errors.Is(m.err, context.Canceled) {
			a.setToast(firstLine(m.err.Error()), true)
		}
		if m.done {
			return a, extra
		}
		cmd := readBatchN(a.stream, a.logGen, 1000)
		if !a.ticking {
			a.ticking = true
			return a, tea.Batch(cmd, extra, spinnerTickCmd())
		}
		return a, tea.Batch(cmd, extra)

	case opDoneMsg:
		a.busy = false
		a.taskName = ""
		a.opCancel = nil
		if m.out != "" {
			a.logLine(m.out)
		}
		if m.err != nil {
			msg := firstLine(m.err.Error())
			lower := strings.ToLower(m.err.Error())
			switch {
			case strings.Contains(lower, "non-fast-forward"), strings.Contains(lower, "fetch first"), strings.Contains(lower, "rejected"):
				msg += i18n.S(" — 建议先按 p 拉取", " — try pulling first (p)")
			case strings.Contains(lower, "no upstream"), strings.Contains(lower, "has no upstream branch"):
				msg += i18n.S(" — 按 P 推送并设置上游", " — press P to push and set upstream")
			case strings.Contains(lower, "index.lock"):
				msg += i18n.S(" — 有 git 进程占用，检查 .git/index.lock", " — .git/index.lock is held by another process")
			case strings.Contains(lower, "already checked out at"):
				msg += i18n.S(" — 该分支已在其他工作树检出，不能重复检出", " — branch is already checked out in another worktree")
			case strings.Contains(lower, "conflict"), strings.Contains(lower, "resolve all conflicts"):
				msg += i18n.S(" — 按 M 处理冲突", " — press M to resolve")
			}
			a.setToast(msg, true)
			a.logLine(m.err.Error())
		} else {
			a.setToast(i18n.S("完成: ", "done: ")+m.name, false)
		}
		return a, tea.Batch(a.refreshCmd(false), a.reloadLogCmd())

	case remoteTagMsg:
		if m.err == nil {
			a.remoteTagCache[m.name] = m.exists
		}
		if a.modal == nil || a.modal != a.tagMenuModal || a.tagMenuName != m.name {
			return a, nil
		}
		var ref git.Ref
		found := false
		for _, r := range a.refs {
			if r.Kind == git.RefTag && r.Name == m.name {
				ref, found = r, true
				break
			}
		}
		if !found {
			return a, nil
		}
		pushed := m.exists && m.err == nil
		a.modal.choices = a.tagMenuChoices(ref, pushed)
		if a.modal.sel >= len(a.modal.choices) {
			a.modal.sel = len(a.modal.choices) - 1
		}
		if a.modal.sel < 0 {
			a.modal.sel = 0
		}
		if m.err != nil {
			a.setToast(i18n.S("检查远端失败，仍可尝试推送: ", "remote check failed, push is still available: ")+firstLine(m.err.Error()), true)
		}
		return a, nil

	case dirPickedMsg:
		switch {
		case errors.Is(m.err, platform.ErrCanceled):
			return a, nil
		case errors.Is(m.err, platform.ErrUnavailable):
			a.setToast(i18n.S("系统目录选择不可用，使用内置选择器", "system picker unavailable, using built-in picker"), false)
			a.openProjectPicker()
		case m.err != nil:
			a.setToast(i18n.S("选择目录失败: ", "pick directory failed: ")+firstLine(m.err.Error()), true)
		case m.path != "":
			return a, a.chooseProjectDir(m.path)
		}
		return a, nil

	case switchReloadMsg:
		if strings.TrimSpace(m.path) != a.currentPath {
			return a, nil
		}
		return a, a.reloadLogCmdPreserve()

	case worktreesMsg:
		for p, list := range m.list {
			a.worktrees[p] = list
			if len(list) > 0 && list[0].Branch != "" {
				a.projectBranch[p] = list[0].Branch
			}
		}
		a.projectSel = a.projectRowIndex(func(r projectRow) bool {
			if r.worktree != nil {
				return r.worktree.Path == a.currentPath
			}
			return r.project.Path == a.currentProjectPath && a.currentPath == r.project.Path
		})
		a.ensureProjectVisible()
		return a, nil

	case projectBranchesMsg:
		for p, b := range m.branches {
			a.projectBranch[p] = b
		}
		if a.currentPath != "" && a.status.Branch != "" {
			a.projectBranch[a.currentPath] = a.status.Branch
		}
		return a, nil

	case updateCheckMsg:
		if m.err == nil && m.newer && m.tag != "" {
			a.latestVersion = m.tag
			a.updateAvailable = true
		}
		return a, nil

	case commitMessageMsg:
		if m.err != nil {
			a.setToast(firstLine(m.err.Error()), true)
			return a, nil
		}
		a.clearCommitSelection()
		a.editCommitMessagePrompt(m.sha, m.message)
		return a, nil

	case detailMsg:
		if a.overlay != overlayDetail {
			return a, nil
		}
		a.detail.loading = false
		if m.err != nil {
			a.detail.err = m.err.Error()
			a.detail.lines = nil
			return a, nil
		}
		if m.kind == "commit" {
			a.detail.title = m.title
			a.detail.lines = buildCommitDetailLines(m.detail, m.diff)
		} else {
			a.detail.title = m.title
			a.detail.lines = buildPlainDiffLines(m.diff)
		}
		a.detail.offset = 0
		return a, nil

	case filesMsg:
		if m.gen != a.filesGen {
			return a, nil
		}
		a.setFiles(m.files)
		a.commitBody = m.detail.Body
		a.commitAuthor = m.detail.Author
		if m.err != nil {
			a.setToast(firstLine(m.err.Error()), true)
		}
		if f := a.selectedFile(); f != nil {
			return a, a.loadFileContent(*f, a.filesOwner == "work")
		}
		if len(a.files) == 0 {
			a.content = detailModel{loading: false, title: i18n.S("文件列表", "files"),
				lines: []string{i18n.S("（该提交没有文件变更）", "(no file changes in this commit)")}}
		}
		return a, nil

	case contentMsg:
		if m.gen != a.contentGen {
			return a, nil
		}
		a.content.loading = false
		a.content.title = m.title
		a.content.sha = m.sha
		a.content.offset = 0
		if m.err != nil {
			a.content.err = firstLine(m.err.Error())
			a.content.lines = nil
		} else {
			a.content.err = ""
			a.content.lines = buildPlainDiffLines(m.diff)
		}
		a.clampContentScroll()
		return a, nil

	case searchDoneMsg:
		if m.gen != a.searchGen {
			return a, nil
		}
		a.searching = false
		if m.err != nil {
			a.setToast(firstLine(m.err.Error()), true)
			return a, nil
		}
		a.searchResults = m.commits
		a.filter = ""
		a.selHash = ""
		a.graphSel = 0
		a.graphOff = 0
		a.invalidateView()
		a.clampSelection()
		filesCmd := a.loadFilesForSelection()
		if m.truncated {
			a.setToast(fmt.Sprintf(i18n.S("搜索结果过多，仅显示前 %d 条", "too many results, showing first %d"), maxSearchResult), false)
		} else {
			a.setToast(fmt.Sprintf(i18n.S("全历史搜索：%d 条结果", "full-history search: %d results"), len(m.commits)), false)
		}
		return a, filesCmd
	}
	return a, nil
}

func firstLine(s string) string {
	if i := strings.IndexByte(s, '\n'); i >= 0 {
		return s[:i]
	}
	return s
}

// handleKey 处理按键。
func (a *App) handleKey(k tea.KeyMsg) (tea.Model, tea.Cmd) {
	if a.modal != nil {
		return a.handleModalKey(k)
	}
	switch a.overlay {
	case overlayHelp, overlayLog:
		switch k.String() {
		case "esc", "?", "q", "L":
			a.overlay = overlayNone
		case "down":
			a.detail.offset++
		case "up":
			a.detail.offset--
		case "pgdown":
			a.detail.offset += a.pageSize()
		case "pgup":
			a.detail.offset -= a.pageSize()
		case "g":
			a.detail.offset = 0
		case "G":
			a.detail.offset = 1 << 30
		}
		a.clampOverlayScroll()
		return a, nil
	case overlayDetail:
		switch k.String() {
		case "esc", "q":
			a.overlay = overlayNone
		case "down":
			a.detail.offset++
		case "up":
			a.detail.offset--
		case "pgdown":
			a.detail.offset += a.pageSize()
		case "pgup":
			a.detail.offset -= a.pageSize()
		case "ctrl+d":
			a.detail.offset += a.height / 2
		case "ctrl+u":
			a.detail.offset -= a.height / 2
		case "g":
			a.detail.offset = 0
		case "G":
			a.detail.offset = 1 << 30
		}
		a.clampOverlayScroll()
		return a, nil
	}

	// 键盘操作时清除鼠标拖动/悬停高亮残留
	a.hoverDiv = 0
	a.dragMode = 0
	if a.searchActive {
		return a.handleSearchKey(k)
	}
	switch k.String() {
	case "ctrl+c", "q":
		return a, tea.Quit
	case "esc":
		if a.busy && a.opCancel != nil {
			a.opCancel()
			a.opCancel = nil
			a.setToast(i18n.S("已取消", "cancelled"), false)
			return a, nil
		}
		a.clearCommitSelection()
		a.filter = ""
		a.searchResults = nil
		a.searchQuery = ""
		a.invalidateView()
		a.clampSelection()
		return a, nil
	case "?":
		a.overlay = overlayHelp
		a.detail.offset = 0
		return a, nil
	case "L":
		a.overlay = overlayLog
		a.detail.offset = 1 << 30
		a.clampOverlayScroll()
		return a, nil
	case "R":
		a.setToast(i18n.S("刷新中…", "refreshing…"), false)
		return a, tea.Batch(a.refreshCmd(false), a.reloadLogCmd())
	case "tab", "right":
		a.nextPane(1)
		return a, nil
	case "shift+tab", "left":
		a.nextPane(-1)
		return a, nil
	case "alt+up":
		a.altFocus("up")
		return a, nil
	case "alt+down":
		a.altFocus("down")
		return a, nil
	case "alt+left":
		a.altFocus("left")
		return a, nil
	case "alt+right":
		a.altFocus("right")
		return a, nil
	case "/":
		a.openSearchInline()
		return a, nil
	case ":":
		a.prompt(i18n.S("git 参数（如 push --force-with-lease）", "git args (e.g. push --force-with-lease)"), "", "", func(a *App, v string) tea.Cmd {
			args := git.SplitArgs(v)
			if len(args) == 0 {
				return nil
			}
			return a.runOp("git "+v, func(ctx context.Context) (string, error) {
				return a.svc.RunGitArgs(ctx, args)
			})
		})
		return a, nil
	case "f":
		return a, a.fetch(false)
	case "F":
		return a, a.fetch(true)
	case "p":
		return a, a.pull()
	case "P":
		if a.pane == PaneBranches {
			if it := a.selectedSideItem(); it != nil && it.kind == sideTag {
				return a, a.pushTag(it.ref)
			}
		}
		return a, a.push()
	case "1":
		a.jumpToSection("branches")
		return a, nil
	case "2":
		a.jumpToSection("remote")
		return a, nil
	case "3":
		a.jumpToSection("tags")
		return a, nil
	case "4":
		a.jumpToSection("stash")
		return a, nil
	case "5":
		a.pane = PaneFiles
		a.ensureFileVisible()
		return a, nil
	case "M":
		if a.state.InProgress() {
			a.openStateMenu()
			return a, nil
		}
		a.setToast(i18n.S("当前没有进行中的合并/变基", "no operation in progress"), false)
		return a, nil
	case "T":
		a.cycleTheme()
		return a, nil
	case "b":
		a.anchorMenu()
		return a, a.openContextMenu()
	case "v":
		a.anchorMenu()
		a.openBranchFilterMenu()
		return a, nil
	case "z":
		return a, a.stashPrompt()
	case "Z":
		a.jumpToSection("stash")
		return a, nil
	case "c":
		return a, a.commitPrompt(false)
	case "A":
		return a, a.commitPrompt(true)
	case "a":
		if a.busy {
			return a, a.busyToast()
		}
		return a, a.runOp("add -A", func(ctx context.Context) (string, error) { return a.svc.StageAll(ctx) })
	case "u":
		if a.busy {
			return a, a.busyToast()
		}
		return a, a.runOp("reset HEAD", func(ctx context.Context) (string, error) { return a.svc.UnstageAll(ctx) })
	case "down":
		return a, a.move(1)
	case "up":
		return a, a.move(-1)
	case "ctrl+d", "pgdown":
		delta := a.currentViewHeight() / 2
		if a.pane == PaneContent {
			delta = a.currentViewHeight()
		}
		return a, a.move(delta)
	case "ctrl+u", "pgup":
		delta := a.currentViewHeight() / 2
		if a.pane == PaneContent {
			delta = a.currentViewHeight()
		}
		return a, a.move(-delta)
	case "g", "home":
		return a, a.moveToEdge(false)
	case "G", "end":
		return a, a.moveToEdge(true)
	}

	if k.Type == tea.KeyRunes && (len(k.Runes) > 1 || k.Runes[0] > 127) {
		a.setToast(i18n.S("浏览模式：中文输入法可能吞掉按键，请用方向键或切换英文输入法", "browsing mode: use arrow keys or switch to an English IME"), true)
		return a, nil
	}
	switch a.pane {
	case PaneProjects:
		switch k.String() {
		case "enter":
			return a, a.activateProjectRow()
		case "b":
			a.anchorMenu()
			return a, a.openContextMenu()
		}
	case PaneBranches:
		if cmd, handled := a.handleSidebarKey(k); handled {
			return a, cmd
		}
	case PaneChanges:
		if cmd, handled := a.handleChangesKey(k); handled {
			return a, cmd
		}
	case PaneCommits:
		if cmd, handled := a.handleGraphKey(k); handled {
			return a, cmd
		}
	case PaneFiles:
		if cmd, handled := a.handleFilesKey(k); handled {
			return a, cmd
		}
	case PaneLog:
		if cmd, handled := a.handleLogKey(k); handled {
			return a, cmd
		}
	case PaneInfo:
		if cmd, handled := a.handleInfoKey(k); handled {
			return a, cmd
		}
	}
	return a, nil
}

func (a *App) busyToast() tea.Cmd {
	a.setToast(i18n.S("上一个操作尚未完成，按 Esc 取消", "previous operation still running, press Esc to cancel"), true)
	return nil
}

// currentViewHeight 返回当前面板可视行数。
func (a *App) currentViewHeight() int {
	switch a.pane {
	case PaneProjects:
		return a.projectViewHeight()
	case PaneBranches:
		return a.branchViewHeight()
	case PaneChanges:
		return a.changesViewHeight()
	case PaneFiles:
		return a.fileViewHeight()
	case PaneContent:
		return a.contentViewHeight()
	}
	return a.commitViewHeight()
}

// move 按面板移动选择并触发对应的异步加载。
func (a *App) move(delta int) tea.Cmd {
	switch a.pane {
	case PaneProjects:
		a.projectSel += delta
		a.ensureProjectVisible()
		return nil
	case PaneBranches:
		a.moveSidebar(delta)
		return a.selectBranchTip()
	case PaneChanges:
		return a.moveChange(delta)
	case PaneFiles:
		return a.moveFile(delta)
	case PaneContent:
		a.content.offset += delta
		a.clampContentScroll()
		return nil
	case PaneLog:
		a.cmdOffset -= delta
		a.clampCmdOffset()
		return nil
	case PaneInfo:
		a.infoOff += delta
		a.clampInfoOffset()
		return nil
	case PaneCommits:
		a.clearCommitSelection()
		a.graphSel += delta
		a.clampSelection()
		if c := a.selectedCommit(); c != nil {
			a.selHash = c.Hash
		}
		return a.loadFilesForSelection()
	default:
		a.clearCommitSelection()
		a.graphSel += delta
		a.clampSelection()
		if c := a.selectedCommit(); c != nil {
			a.selHash = c.Hash
		}
		return a.loadFilesForSelection()
	}
}

// moveFile 在文件区移动并加载内容。
func (a *App) moveFile(delta int) tea.Cmd {
	if len(a.fileRows) == 0 {
		return nil
	}
	a.fileSel += delta
	if a.fileSel < 0 {
		a.fileSel = 0
	}
	if a.fileSel >= len(a.fileRows) {
		a.fileSel = len(a.fileRows) - 1
	}
	a.ensureFileVisible()
	if f := a.selectedFile(); f != nil {
		return a.loadFileContent(*f, a.filesOwner == "work")
	}
	if n := a.selectedFileNode(); n != nil && n.dir {
		a.content = detailModel{title: n.path + "/", lines: []string{i18n.S("（目录）按 Enter 展开/折叠", "(directory) press Enter to expand/collapse")}}
	}
	return nil
}

// moveChange 在左下改动区移动并加载内容。
func (a *App) moveChange(delta int) tea.Cmd {
	if len(a.changesRows) == 0 {
		return nil
	}
	a.changeSel += delta
	if a.changeSel < 0 {
		a.changeSel = 0
	}
	if a.changeSel >= len(a.changesRows) {
		a.changeSel = len(a.changesRows) - 1
	}
	a.ensureChangeVisible()
	if f := a.selectedChangeEntry(); f != nil {
		return a.loadFileContent(*f, true)
	}
	if n := a.selectedChangeNode(); n != nil && n.dir {
		a.content = detailModel{title: n.path + "/", lines: []string{i18n.S("（目录）按 Enter 展开/折叠", "(directory) press Enter to expand/collapse")}}
	}
	return nil
}

// rebuildLayout 用当前完整颜色映射重建提交图（分支着色）。
func (a *App) rebuildLayout() {
	b := graph.NewBuilder()
	b.SetColorOf(a.commitColor)
	for _, c := range a.commits {
		b.Add(graph.Commit{Hash: c.Hash, Parents: c.Parents})
	}
	a.builder = b
	a.layout = b.Layout()
	a.invalidateView()
}

// selectBranchTip 选中分支后，把提交区定位到其 tip 并加载文件。
func (a *App) selectBranchTip() tea.Cmd {
	it := a.selectedSideItem()
	if it == nil {
		return nil
	}
	target := it.ref.Target
	if it.kind == sideStash {
		target = it.stash.Hash
	}
	if target == "" {
		return nil
	}
	for i, c := range a.visibleCommits() {
		if c.Hash == target {
			a.graphSel = i + a.dirtyOffset()
			a.clampSelection()
			a.selHash = c.Hash
			return a.loadFilesForSelection()
		}
	}
	return nil
}

// moveToEdge 跳到当前面板首/尾。
func (a *App) moveToEdge(last bool) tea.Cmd {
	switch a.pane {
	case PaneBranches:
		if len(a.sideItems) > 0 {
			if last {
				a.sideSel = len(a.sideItems) - 1
			} else {
				a.sideSel = 0
			}
			a.sideSelKey = a.sideItems[a.sideSel].key
			a.ensureSideVisible()
			return a.selectBranchTip()
		}
	case PaneChanges:
		if len(a.changesRows) > 0 {
			if last {
				a.changeSel = len(a.changesRows) - 1
			} else {
				a.changeSel = 0
			}
			a.ensureChangeVisible()
			if f := a.selectedChangeEntry(); f != nil {
				return a.loadFileContent(*f, true)
			}
		}
	case PaneFiles:
		if len(a.files) > 0 {
			if last {
				a.fileSel = len(a.files) - 1
			} else {
				a.fileSel = 0
			}
			a.ensureFileVisible()
			if f := a.selectedFile(); f != nil {
				return a.loadFileContent(*f, a.filesOwner == "work")
			}
		}
	case PaneContent:
		if last {
			a.content.offset = len(a.content.lines)
		} else {
			a.content.offset = 0
		}
		a.clampContentScroll()
	case PaneLog:
		if last {
			a.cmdOffset = 0
		} else {
			a.cmdOffset = len(a.commands)
		}
		a.clampCmdOffset()
	default:
		a.graphSel = a.totalRows() - 1
		if !last {
			a.graphSel = 0
		}
		a.clampSelection()
		if c := a.selectedCommit(); c != nil {
			a.selHash = c.Hash
		}
		return a.loadFilesForSelection()
	}
	return nil
}

// handleInfoKey 处理提交信息区按键。
func (a *App) handleInfoKey(k tea.KeyMsg) (tea.Cmd, bool) {
	switch k.String() {
	case "b":
		a.openInfoHeaderMenu()
		return nil, true
	}
	return nil, false
}

// handleLogKey 处理命令历史区按键。
func (a *App) handleLogKey(k tea.KeyMsg) (tea.Cmd, bool) {
	switch k.String() {
	case "b":
		a.openCommandsHeaderMenu()
		return nil, true
	}
	return nil, false
}

// handleChangesKey 处理左下工作区改动区按键。
func (a *App) handleChangesKey(k tea.KeyMsg) (tea.Cmd, bool) {
	f := a.selectedChangeEntry()
	n := a.selectedChangeNode()
	switch k.String() {
	case "enter":
		if n != nil && n.dir {
			a.toggleChangeDir(n)
			return nil, true
		}
		if f != nil {
			a.pane = PaneContent
			a.clampContentScroll()
			return a.loadFileContent(*f, true), true
		}
		return nil, true
	case " ":
		if n != nil && n.dir {
			a.toggleChangeDir(n)
			return nil, true
		}
		if f == nil {
			return nil, true
		}
		if a.busy {
			return a.busyToast(), true
		}
		if f.staged {
			path := f.path
			return a.runOp("unstage "+path, func(ctx context.Context) (string, error) {
				return a.svc.Unstage(ctx, path)
			}), true
		}
		path := f.path
		return a.runOp("stage "+path, func(ctx context.Context) (string, error) {
			return a.svc.Stage(ctx, path)
		}), true
	case "d":
		if f != nil {
			return a.discardFilePrompt(*f), true
		}
	case "D":
		a.discardAllPrompt()
		return nil, true
	case "b":
		return a.openContextMenu(), true
	}
	return nil, false
}

// handleFilesKey 处理文件区按键。
func (a *App) handleFilesKey(k tea.KeyMsg) (tea.Cmd, bool) {
	f := a.selectedFile()
	n := a.selectedFileNode()
	switch k.String() {
	case "enter":
		if n != nil && n.dir {
			a.toggleFileDir(n)
			return nil, true
		}
		a.pane = PaneContent
		a.clampContentScroll()
		return nil, true
	case " ":
		if n != nil && n.dir {
			a.toggleFileDir(n)
			return nil, true
		}
		if f == nil || a.filesOwner != "work" {
			a.setToast(i18n.S("只能暂存工作区文件", "only working-tree files can be staged"), true)
			return nil, true
		}
		if a.busy {
			return a.busyToast(), true
		}
		if f.staged {
			path := f.path
			return a.runOp("unstage "+path, func(ctx context.Context) (string, error) {
				return a.svc.Unstage(ctx, path)
			}), true
		}
		path := f.path
		return a.runOp("stage "+path, func(ctx context.Context) (string, error) {
			return a.svc.Stage(ctx, path)
		}), true
	case "d":
		if f != nil && a.filesOwner == "work" {
			return a.discardFilePrompt(*f), true
		}
	case "D":
		if a.filesOwner == "work" {
			a.discardAllPrompt()
			return nil, true
		}
	case "b":
		return a.openContextMenu(), true
	}
	return nil, false
}

// handleGraphKey 处理图区按键。
func (a *App) handleGraphKey(k tea.KeyMsg) (cmd tea.Cmd, handled bool) {
	switch k.String() {
	case "enter":
		if a.onDirtyRow() {
			a.pane = PaneFiles
			a.ensureFileVisible()
			return nil, true
		}
		c := a.selectedCommit()
		if c == nil {
			return nil, true
		}
		return a.openCommitDetail(c.Hash), true
	case "y":
		c := a.selectedCommit()
		if c != nil {
			return a.copyToClipboard(c.Hash), true
		}
		return nil, true
	case "C":
		c := a.selectedCommit()
		if c == nil {
			return nil, true
		}
		a.confirm(i18n.S("cherry-pick 提交", "cherry-pick commit"),
			fmt.Sprintf("%s %s", c.Short, c.Subject),
			"git cherry-pick "+c.Hash, false,
			func(a *App) tea.Cmd {
				return a.runOp("cherry-pick", func(ctx context.Context) (string, error) {
					return a.svc.CherryPick(ctx, c.Hash)
				})
			})
		return nil, true
	case "V":
		c := a.selectedCommit()
		if c == nil {
			return nil, true
		}
		a.confirm(i18n.S("revert 提交", "revert commit"),
			fmt.Sprintf("%s %s", c.Short, c.Subject),
			"git revert --no-edit "+c.Hash, false,
			func(a *App) tea.Cmd {
				return a.runOp("revert", func(ctx context.Context) (string, error) {
					return a.svc.Revert(ctx, c.Hash)
				})
			})
		return nil, true
	case "X":
		c := a.selectedCommit()
		if c == nil {
			return nil, true
		}
		a.choose(i18n.S("reset 当前分支到 "+c.Short, "reset current branch to "+c.Short), []choice{
			{label: "soft", desc: i18n.S("保留索引与工作区", "keep index and worktree"), action: func(a *App) tea.Cmd {
				return a.doReset(c.Hash, git.ResetSoft)
			}},
			{label: "mixed", desc: i18n.S("保留工作区，清空索引", "keep worktree, reset index"), action: func(a *App) tea.Cmd {
				return a.doReset(c.Hash, git.ResetMixed)
			}},
			{label: "hard", desc: i18n.S("丢弃索引与工作区改动（危险）", "discard all changes (dangerous)"), action: func(a *App) tea.Cmd {
				return a.doReset(c.Hash, git.ResetHard)
			}},
		})
		return nil, true
	case "t":
		c := a.selectedCommit()
		if c == nil {
			return nil, true
		}
		a.prompt(i18n.S("标签名（name 或 name:附注）", "tag name (name or name:message)"), "v1.0", "", func(a *App, v string) tea.Cmd {
			name, msg := v, ""
			if i := strings.IndexByte(v, ':'); i > 0 {
				name, msg = v[:i], strings.TrimSpace(v[i+1:])
			}
			name = strings.TrimSpace(name)
			if name == "" {
				return nil
			}
			return a.runOp("tag "+name, func(ctx context.Context) (string, error) {
				return a.svc.CreateTag(ctx, name, c.Hash, msg)
			})
		})
		return nil, true
	case "b":
		return a.openContextMenu(), true
	case " ":
		if a.onDirtyRow() {
			return a.openContextMenu(), true
		}
		return nil, true
	case "d":
		if a.onDirtyRow() {
			return a.openContextMenu(), true
		}
		return nil, true
	case "n":
		c := a.selectedCommit()
		if c != nil {
			a.newBranchPrompt(c.Hash)
			return nil, true
		}
		return nil, true
	}
	return nil, false
}

func (a *App) commitActions(c *git.Commit) []choice {
	var out []choice
	if c == nil {
		return out
	}
	out = append(out,
		choice{label: i18n.S("在此提交新建分支并检出", "new branch here & checkout"), action: func(a *App) tea.Cmd {
			a.newBranchPrompt(c.Hash)
			return nil
		}},
		choice{label: i18n.S("检出此提交（分离 HEAD）", "checkout this commit (detached)"), action: func(a *App) tea.Cmd {
			a.confirm(i18n.S("检出 "+c.Short, "checkout "+c.Short),
				i18n.S("将进入分离 HEAD 状态，可随时检出其他分支返回。", "Detached HEAD; checkout a branch to return."),
				"git checkout "+c.Hash, false, func(a *App) tea.Cmd {
					return a.runOp("checkout "+c.Short, func(ctx context.Context) (string, error) {
						return a.svc.Checkout(ctx, c.Hash)
					})
				})
			return nil
		}},
		choice{label: i18n.S("在此提交打标签", "tag this commit"), action: func(a *App) tea.Cmd {
			a.handleGraphKey(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'t'}})
			return nil
		}},
		choice{label: i18n.S("复制完整 sha", "copy full sha"), action: func(a *App) tea.Cmd {
			return a.copyToClipboard(c.Hash)
		}},
	)
	return out
}

func (a *App) newBranchPrompt(start string) {
	a.prompt(i18n.S("新分支名（基于 "+shortSHA(start)+" 创建并检出）", "new branch name (from "+shortSHA(start)+")"), "feature/x", "", func(a *App, v string) tea.Cmd {
		name := strings.TrimSpace(v)
		if name == "" {
			return nil
		}
		return a.runOp("checkout -b "+name, func(ctx context.Context) (string, error) {
			return a.svc.CreateBranch(ctx, name, start, true)
		})
	})
}

func shortSHA(s string) string {
	if len(s) > 7 && !strings.ContainsAny(s, "/ ") {
		return s[:7]
	}
	return s
}

func (a *App) doReset(sha string, mode git.ResetMode) tea.Cmd {
	cmd := "git reset " + string(mode) + " " + sha
	run := func(a *App) tea.Cmd {
		return a.runOp("reset "+string(mode), func(ctx context.Context) (string, error) {
			return a.svc.Reset(ctx, sha, mode)
		})
	}
	if mode == git.ResetHard {
		a.confirm(i18n.S("reset --hard（危险）", "reset --hard (dangerous)"),
			i18n.S("将丢弃工作区与索引中的全部改动。可用 reflog 找回提交，但未提交改动无法恢复。",
				"All worktree and index changes will be lost. Commits can be recovered via reflog."),
			cmd, true, run)
		return nil
	}
	a.confirm(i18n.S("reset 当前分支", "reset current branch"),
		i18n.S("将移动当前分支指针；可用 reflog 恢复。", "Move current branch pointer; recoverable via reflog."),
		cmd, false, run)
	return nil
}

// handleSidebarKey 处理侧栏按键。
func (a *App) handleSidebarKey(k tea.KeyMsg) (tea.Cmd, bool) {
	it := a.selectedSideItem()
	switch k.String() {
	case "]", "l":
		a.jumpSection(1)
		return nil, true
	case "[", "h":
		a.jumpSection(-1)
		return nil, true
	case "-":
		if it := a.selectedSideItem(); it != nil {
			a.collapsed[it.section] = true
			a.toggleSection(it.section)
			a.toggleSection(it.section)
		}
		return nil, true
	case "+", "=":
		if it := a.selectedSideItem(); it != nil {
			a.collapsed[it.section] = false
		}
		return nil, true
	case "enter":
		return a.sidebarEnter(it), true
	case " ":
		if it != nil && it.kind == sideStash {
			return a.runOp("stash apply", func(ctx context.Context) (string, error) {
				return a.svc.StashApply(ctx, it.stash.Index)
			}), true
		}
		return a.sidebarSpace(it), true
	case "d":
		return a.sidebarDelete(it), true
	case "D":
		if it != nil && it.kind == sideBranch && !it.ref.Head {
			name := it.ref.Name
			a.confirm(i18n.S("强制删除分支 "+name+"（危险）", "force delete branch "+name+" (dangerous)"),
				i18n.S("即使分支未合并也会删除。可用 reflog 恢复。", "Deletes even if unmerged. Recoverable via reflog."),
				"git branch -D "+name, true, func(a *App) tea.Cmd {
					return a.runOp("branch -D "+name, func(ctx context.Context) (string, error) {
						return a.svc.DeleteBranch(ctx, name, true)
					})
				})
			return nil, true
		}
	case "r":
		if it != nil && it.kind == sideBranch {
			a.renameBranchPrompt(it.ref)
			return nil, true
		}
	case "m":
		if it != nil && (it.kind == sideBranch || it.kind == sideRemote) {
			a.mergePrompt(it.ref)
			return nil, true
		}
	case "B":
		if it != nil && (it.kind == sideBranch || it.kind == sideRemote) {
			a.rebasePrompt(it.ref)
			return nil, true
		}
	case "o":
		if it != nil && it.kind == sideStash {
			return a.stashAction(it.stash, "pop"), true
		}
	case "n":
		a.newBranchPrompt("HEAD")
		return nil, true
	}
	return nil, false
}

func (a *App) sidebarEnter(it *sideItem) tea.Cmd {
	if it == nil {
		return nil
	}
	switch it.kind {
	case sideProject:
		return a.switchProject(a.projectIndex(it.project.Path))
	case sideBranch:
		if it.ref.Head {
			a.setToast(i18n.S("已在当前分支", "already on this branch"), false)
			return nil
		}
		return a.runOp("checkout "+it.ref.Name, func(ctx context.Context) (string, error) {
			return a.svc.Checkout(ctx, it.ref.Name)
		})
	case sideRemote:
		local := remoteLocalName(it.ref.Name)
		for _, r := range a.refs {
			if r.Kind == git.RefLocal && r.Name == local {
				return a.runOp("checkout "+local, func(ctx context.Context) (string, error) {
					return a.svc.Checkout(ctx, local)
				})
			}
		}
		return a.runOp("checkout "+local, func(ctx context.Context) (string, error) {
			return a.svc.CheckoutRemoteBranch(ctx, it.ref.Name, local)
		})
	case sideTag:
		return a.runOp("checkout "+it.ref.Name, func(ctx context.Context) (string, error) {
			return a.svc.Checkout(ctx, it.ref.Name)
		})
	case sideStash:
		return a.openStashDiff(it.stash)
	case sideFile:
		return a.openFileDiff(fileTitle(it), it.file.Path, it.staged, it.untracked)
	}
	return nil
}

func fileTitle(it *sideItem) string {
	label := it.file.Label(it.staged)
	return label + " " + it.file.Path
}

func remoteLocalName(full string) string {
	if i := strings.IndexByte(full, '/'); i >= 0 {
		return full[i+1:]
	}
	return full
}

func (a *App) sidebarSpace(it *sideItem) tea.Cmd {
	if it == nil || it.kind != sideFile {
		return nil
	}
	if a.busy {
		return a.busyToast()
	}
	if it.staged {
		path := it.file.Path
		return a.runOp("unstage "+path, func(ctx context.Context) (string, error) {
			return a.svc.Unstage(ctx, path)
		})
	}
	path := it.file.Path
	return a.runOp("stage "+path, func(ctx context.Context) (string, error) {
		return a.svc.Stage(ctx, path)
	})
}

func (a *App) sidebarDelete(it *sideItem) tea.Cmd {
	if it == nil {
		return nil
	}
	switch it.kind {
	case sideProject:
		return a.switchProject(a.projectIndex(it.project.Path))
	case sideBranch:
		if it.ref.Head {
			a.setToast(i18n.S("不能删除当前分支", "cannot delete current branch"), true)
			return nil
		}
		name := it.ref.Name
		a.confirm(i18n.S("删除分支 "+name, "delete branch "+name),
			i18n.S("将删除本地分支；未合并时需二次确认。误删可用 reflog 恢复。",
				"Delete local branch. Unmerged branches need extra confirmation. Recoverable via reflog."),
			"git branch -d "+name, true, func(a *App) tea.Cmd {
				return a.runOp("branch -d "+name, func(ctx context.Context) (string, error) {
					out, err := a.svc.DeleteBranch(ctx, name, false)
					if err != nil {
						return out, err
					}
					return out, nil
				})
			})
		return nil
	case sideRemote:
		remote, branch := splitRemote(it.ref.Name)
		if remote == "" {
			return nil
		}
		a.confirm(i18n.S("删除远端分支 "+it.ref.Name, "delete remote branch "+it.ref.Name),
			i18n.S("将在远端仓库删除该分支。", "This deletes the branch on the remote."),
			"git push "+remote+" --delete "+branch, true, func(a *App) tea.Cmd {
				return a.runOp("push --delete "+branch, func(ctx context.Context) (string, error) {
					return a.svc.DeleteRemoteBranch(ctx, remote, branch)
				})
			})
		return nil
	case sideTag:
		a.confirm(i18n.S("删除标签 "+it.ref.Name, "delete tag "+it.ref.Name),
			i18n.S("仅删除本地标签。", "Deletes the local tag only."),
			"git tag -d "+it.ref.Name, true, func(a *App) tea.Cmd {
				return a.runOp("tag -d "+it.ref.Name, func(ctx context.Context) (string, error) {
					return a.svc.DeleteTag(ctx, it.ref.Name)
				})
			})
		return nil
	case sideStash:
		return a.stashAction(it.stash, "drop")
	case sideFile:
		path := it.file.Path
		untracked := it.untracked
		a.confirm(i18n.S("丢弃改动 "+path, "discard changes "+path),
			i18n.S("将永久丢弃该文件的未提交改动，无法恢复。", "Uncommitted changes to this file will be lost permanently."),
			discardCommand(path, untracked), true, func(a *App) tea.Cmd {
				return a.runOp("discard "+path, func(ctx context.Context) (string, error) {
					return a.svc.Discard(ctx, untracked, path)
				})
			})
		return nil
	}
	return nil
}

func discardCommand(path string, untracked bool) string {
	if untracked {
		return "git clean -f -d -- " + path
	}
	return "git checkout -- " + path
}

func splitRemote(name string) (string, string) {
	if i := strings.IndexByte(name, '/'); i > 0 {
		return name[:i], name[i+1:]
	}
	return "", ""
}

func (a *App) stashAction(st git.Stash, action string) tea.Cmd {
	if action == "drop" {
		a.confirm(i18n.S("删除 stash "+st.Selector, "drop stash "+st.Selector),
			st.Subject, "git stash drop "+st.Selector, true, func(a *App) tea.Cmd {
				return a.runOp("stash drop", func(ctx context.Context) (string, error) {
					return a.svc.StashDrop(ctx, st.Index)
				})
			})
		return nil
	}
	if action == "pop" {
		return a.runOp("stash pop", func(ctx context.Context) (string, error) {
			return a.svc.StashPop(ctx, st.Index)
		})
	}
	return nil
}

func (a *App) renameBranchPrompt(ref git.Ref) {
	a.prompt(i18n.S("重命名分支 "+ref.Name, "rename branch "+ref.Name), "new-name", ref.Name, func(a *App, v string) tea.Cmd {
		name := strings.TrimSpace(v)
		if name == "" || name == ref.Name {
			return nil
		}
		return a.runOp("branch -m "+name, func(ctx context.Context) (string, error) {
			return a.svc.RenameBranch(ctx, ref.Name, name)
		})
	})
}

func (a *App) mergePrompt(ref git.Ref) {
	a.confirm(i18n.S("合并 "+ref.Name+" 到当前分支", "merge "+ref.Name+" into current"),
		i18n.S("冲突时会在顶部显示冲突态，可在 M 菜单继续或终止。", "On conflicts use the M menu to continue or abort."),
		"git merge --no-edit "+ref.Name, false, func(a *App) tea.Cmd {
			return a.runOp("merge "+ref.Name, func(ctx context.Context) (string, error) {
				return a.svc.Merge(ctx, ref.Name)
			})
		})
}

func (a *App) rebasePrompt(ref git.Ref) {
	a.confirm(i18n.S("将当前分支 rebase 到 "+ref.Name, "rebase current onto "+ref.Name),
		i18n.S("会改写当前分支历史。冲突时可在 M 菜单继续或终止。", "This rewrites current branch history. Use the M menu on conflicts."),
		"git rebase "+ref.Name, true, func(a *App) tea.Cmd {
			return a.runOp("rebase "+ref.Name, func(ctx context.Context) (string, error) {
				return a.svc.Rebase(ctx, ref.Name)
			})
		})
}

func (a *App) jumpToSection(section string) {
	if a.collapsed[section] {
		a.collapsed[section] = false
	}
	for i, it := range a.sideItems {
		if it.section == section {
			a.sideSel = i
			a.sideSelKey = it.key
			a.pane = PaneBranches
			a.ensureSideVisible()
			return
		}
	}
	if section == "stash" {
		a.setToast(i18n.S("没有 stash", "no stashes"), false)
	}
}

// —— 弹窗按键 ——

func (a *App) handleModalKey(k tea.KeyMsg) (tea.Model, tea.Cmd) {
	m := a.modal
	switch m.kind {
	case modalConfirm:
		switch k.String() {
		case "y", "Y", "enter":
			a.modal = nil
			if m.onConfirm != nil {
				return a, m.onConfirm(a)
			}
		case "n", "N", "esc", "q":
			a.modal = nil
		}
		return a, nil
	case modalChoice:
		switch k.String() {
		case "down":
			m.sel++
			if m.sel >= len(m.choices) {
				m.sel = 0
			}
		case "up":
			m.sel--
			if m.sel < 0 {
				m.sel = len(m.choices) - 1
			}
		case "enter", " ":
			a.modal = nil
			if m.sel < len(m.choices) && m.choices[m.sel].action != nil {
				return a, m.choices[m.sel].action(a)
			}
		case "esc":
			a.modal = nil
		default:
			s := k.String()
			if len(s) == 1 && s[0] >= '1' && s[0] <= '9' {
				idx := int(s[0] - '1')
				if idx < len(m.choices) {
					a.modal = nil
					if m.choices[idx].action != nil {
						return a, m.choices[idx].action(a)
					}
				}
			}
		}
		return a, nil
	case modalDirPicker:
		entries := 2 + len(m.dirs)
		switch k.String() {
		case "esc", "q":
			a.modal = nil
		case "up":
			if m.sel > 0 {
				m.sel--
			}
		case "down":
			if m.sel < entries-1 {
				m.sel++
			}
		case "pgup":
			m.sel -= 8
			if m.sel < 0 {
				m.sel = 0
			}
		case "pgdown":
			m.sel += 8
			if m.sel >= entries {
				m.sel = entries - 1
			}
		case "left", "backspace":
			parent := filepath.Dir(m.dir)
			if parent != m.dir {
				a.loadProjectDir(parent)
			}
		case "s":
			return a, a.chooseProjectDir(m.dir)
		case "enter", " ":
			switch {
			case m.sel == 0:
				return a, a.chooseProjectDir(m.dir)
			case m.sel == 1:
				if parent := filepath.Dir(m.dir); parent != m.dir {
					a.loadProjectDir(parent)
				}
			case m.sel-2 < len(m.dirs):
				a.loadProjectDir(filepath.Join(m.dir, m.dirs[m.sel-2]))
			}
		}
		return a, nil
	case modalInput:
		switch k.String() {
		case "esc":
			a.modal = nil
			if m.onCancel != nil {
				return a, m.onCancel(a)
			}
			return a, nil
		case "enter":
			v := m.input.Value()
			a.modal = nil
			if m.onSubmit != nil {
				return a, m.onSubmit(a, v)
			}
			return a, nil
		}
		var cmd tea.Cmd
		m.input, cmd = m.input.Update(k)
		if m.onChange != nil {
			if c := m.onChange(a, m.input.Value()); c != nil {
				cmd = tea.Batch(cmd, c)
			}
		}
		return a, cmd
	case modalText:
		switch k.String() {
		case "esc":
			a.modal = nil
			if m.onCancel != nil {
				return a, m.onCancel(a)
			}
			return a, nil
		case "ctrl+s":
			v := m.text.Value()
			a.modal = nil
			if m.onSubmit != nil {
				return a, m.onSubmit(a, v)
			}
			return a, nil
		}
		var cmd tea.Cmd
		m.text, cmd = m.text.Update(k)
		return a, cmd
	}
	return a, nil
}

// —— 各类动作 ——

func (a *App) fetch(prune bool) tea.Cmd {
	if a.busy {
		return a.busyToast()
	}
	name := "fetch"
	if prune {
		name = "fetch --prune"
	}
	return a.runProgressOp(name, func(ctx context.Context, progress func(string)) (string, error) {
		return a.svc.Fetch(ctx, prune, progress)
	})
}

func (a *App) pull() tea.Cmd {
	if a.busy {
		return a.busyToast()
	}
	return a.runProgressOp("pull", func(ctx context.Context, progress func(string)) (string, error) {
		return a.svc.Pull(ctx, false, progress)
	})
}

func (a *App) push() tea.Cmd {
	if a.busy {
		return a.busyToast()
	}
	branch := a.status.Branch
	if a.status.Upstream == "" {
		local := branch
		if local == "" {
			local = "HEAD"
		}
		a.confirm(i18n.S("当前分支没有上游", "no upstream configured"),
			i18n.S("是否推送到 origin 并设置上游？", "Push to origin and set upstream?"),
			"git push -u origin "+local, false, func(a *App) tea.Cmd {
				return a.runProgressOp("push -u", func(ctx context.Context, progress func(string)) (string, error) {
					return a.svc.Push(ctx, git.PushOptions{SetUpstream: true}, branch, progress)
				})
			})
		return nil
	}
	return a.runProgressOp("push", func(ctx context.Context, progress func(string)) (string, error) {
		return a.svc.Push(ctx, git.PushOptions{}, branch, progress)
	})
}

func (a *App) pushTag(ref git.Ref) tea.Cmd {
	if a.busy {
		return a.busyToast()
	}
	a.confirm(i18n.S("推送标签 "+ref.Name, "push tag "+ref.Name),
		i18n.S("推送到 origin。", "Push to origin."),
		"git push origin refs/tags/"+ref.Name, false, func(a *App) tea.Cmd {
			return a.runProgressOp("push tag", func(ctx context.Context, progress func(string)) (string, error) {
				return a.svc.PushTag(ctx, "origin", ref.Name, progress)
			})
		})
	return nil
}

func (a *App) runProgressOp(name string, fn func(ctx context.Context, progress func(string)) (string, error)) tea.Cmd {
	a.busy = true
	a.taskName = name
	msgs := a.msgs
	return tea.Batch(func() tea.Msg {
		ctx, cancel := context.WithTimeout(context.Background(), opTimeout)
		defer cancel()
		a.opCancel = cancel
		progress := func(line string) {
			select {
			case msgs <- progressMsg{line: line}:
			default:
			}
		}
		out, err := fn(ctx, progress)
		return opDoneMsg{name: name, out: out, err: err}
	}, spinnerTickCmd())
}

func (a *App) stashPrompt() tea.Cmd {
	if a.busy {
		return a.busyToast()
	}
	if !a.status.Dirty() {
		a.setToast(i18n.S("工作区没有改动", "no local changes"), false)
		return nil
	}
	a.prompt(i18n.S("stash 消息（可留空）", "stash message (optional)"), "", "", func(a *App, v string) tea.Cmd {
		msg := strings.TrimSpace(v)
		return a.runOp("stash", func(ctx context.Context) (string, error) {
			return a.svc.Stash(ctx, msg, true)
		})
	})
	return nil
}

func (a *App) commitPrompt(amend bool) tea.Cmd {
	if a.busy {
		return a.busyToast()
	}
	if !amend && len(a.status.Staged) == 0 {
		a.setToast(i18n.S("没有已暂存的改动（空格键暂存文件）", "nothing staged (press space on a file)"), true)
		return nil
	}
	initial := ""
	title := i18n.S("提交信息（Ctrl+S 提交，Esc 取消）", "commit message (Ctrl+S commit, Esc cancel)")
	if amend {
		title = i18n.S("amend（留空保留原提交信息）", "amend (leave empty to keep message)")
	}
	a.promptText(title, i18n.S("标题", "subject"), initial, func(a *App, v string) tea.Cmd {
		msg := strings.TrimRight(v, "\n")
		if strings.TrimSpace(msg) == "" && !amend {
			a.setToast(i18n.S("提交信息不能为空", "commit message required"), true)
			return nil
		}
		return a.runOp("commit", func(ctx context.Context) (string, error) {
			return a.svc.Commit(ctx, msg, amend)
		})
	})
	return nil
}

func (a *App) openStateMenu() {
	kind := a.state.Kind
	label := map[string]string{
		git.StateMerge:      "merge",
		git.StateRebase:     "rebase",
		git.StateCherryPick: "cherry-pick",
		git.StateRevert:     "revert",
	}[kind]
	a.choose(fmt.Sprintf(i18n.S("%s 进行中", "%s in progress"), label), []choice{
		{label: i18n.S("继续（continue）", "continue"), desc: i18n.S("解决冲突并暂存后继续", "resolve & stage, then continue"), action: func(a *App) tea.Cmd {
			return a.runOp(label+" --continue", func(ctx context.Context) (string, error) {
				return a.svc.ContinueOperation(ctx, kind)
			})
		}},
		{label: i18n.S("终止（abort）", "abort"), desc: i18n.S("回到操作前的状态", "return to the previous state"), action: func(a *App) tea.Cmd {
			a.confirm(i18n.S("确认终止 "+label, "confirm abort "+label), i18n.S("将放弃本次操作。", "This abandons the operation."),
				"git "+label+" --abort", true, func(a *App) tea.Cmd {
					return a.runOp(label+" --abort", func(ctx context.Context) (string, error) {
						return a.svc.AbortOperation(ctx, kind)
					})
				})
			return nil
		}},
	})
}

// openSearchInline 进入行内搜索（`/`），结果实时展示在主列表。
func (a *App) openSearchInline() {
	a.clearCommitSelection()
	a.searchActive = true
	a.searchResults = nil
	a.searchQuery = ""
	ti := newTextInput(i18n.S("输入 hash 或提交信息", "hash or commit message"))
	ti.SetValue(a.filter)
	ti.CursorEnd()
	a.searchInput = ti
}

// handleSearchKey 处理行内搜索按键。
func (a *App) handleSearchKey(k tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch k.String() {
	case "esc":
		a.searchActive = false
		a.filter = ""
		a.searchResults = nil
		a.searchQuery = ""
		a.clearCommitSelection()
		a.invalidateView()
		a.graphSel, a.graphOff = 0, 0
		return a, nil
	case "enter":
		q := strings.TrimSpace(a.searchInput.Value())
		a.searchActive = false
		if q == "" {
			a.filter = ""
			a.searchResults = nil
			a.invalidateView()
			return a, nil
		}
		a.filter = q
		a.invalidateView()
		return a, a.runSearch(q)
	case "ctrl+c":
		return a, tea.Quit
	}
	var cmd tea.Cmd
	a.searchInput, cmd = a.searchInput.Update(k)
	a.filter = strings.TrimSpace(a.searchInput.Value())
	a.searchResults = nil
	a.searchQuery = ""
	a.invalidateView()
	a.graphSel, a.graphOff = 0, 0
	return a, cmd
}

// isDoubleClick 判断同一目标 600ms 内的第二次点击。
func (a *App) isDoubleClick(key string) bool {
	now := time.Now()
	dbl := now.Sub(a.lastClick) < 600*time.Millisecond && a.lastClickKey == key
	a.lastClick = now
	a.lastClickKey = key
	return dbl
}

// handleMouse 处理鼠标点击、双击、右键菜单与滚轮。
func (a *App) handleMouse(m tea.MouseMsg) (tea.Model, tea.Cmd) {
	if a.modal != nil {
		if a.modal.kind == modalChoice {
			switch {
			case m.Action == tea.MouseActionMotion:
				if idx, ok := a.choiceIndexAt(m.X, m.Y); ok {
					a.modal.sel = idx
				}
				return a, nil
			case m.Action == tea.MouseActionPress && m.Button == tea.MouseButtonLeft:
				a.modalPress = a.modal
				return a, a.modalClick(m)
			case m.Button == tea.MouseButtonWheelUp:
				if a.modal.sel > 0 {
					a.modal.sel--
				}
				return a, nil
			case m.Button == tea.MouseButtonWheelDown:
				if a.modal.sel < len(a.modal.choices)-1 {
					a.modal.sel++
				}
				return a, nil
			}
		}
		if m.Action == tea.MouseActionPress && m.Button == tea.MouseButtonLeft {
			return a, a.modalClick(m)
		}
		// 部分终端在滚轮后只上报 release：仅当当前仍是同一个选择菜单时才允许关闭，
		// 避免 Press 已执行动作并弹出确认框/输入框后，随后的 Release 把它误关。
		if m.Action == tea.MouseActionRelease && m.Button == tea.MouseButtonLeft {
			if a.modal.kind == modalChoice && (a.modalPress == nil || a.modal == a.modalPress) {
				if _, ok := a.choiceIndexAt(m.X, m.Y); !ok {
					a.modal = nil
				}
			}
			a.modalPress = nil
		}
		return a, nil
	}
	if a.overlay != overlayNone {
		switch m.Button {
		case tea.MouseButtonWheelUp:
			a.detail.offset -= 3
		case tea.MouseButtonWheelDown:
			a.detail.offset += 3
		default:
			return a, nil
		}
		a.clampOverlayScroll()
		return a, nil
	}
	if m.Action == tea.MouseActionMotion {
		hint := updateHintText(a.latestVersion)
		inside := a.updateAvailable && !a.searchActive && a.updateBadgeW > 0 &&
			m.Y == a.height-1 && m.X >= a.updateBadgeX && m.X < a.updateBadgeX+a.updateBadgeW && m.X < a.width
		if inside {
			a.setToast(hint, false)
			a.updateHintOn = true
		} else if a.updateHintOn {
			if a.toast == hint {
				a.toast = ""
			}
			a.updateHintOn = false
		}
	}
	if a.panelLayout().split {
		l := a.panelLayout()
		ly := m.Y
		switch m.Action {
		case tea.MouseActionMotion:
			if a.dragMode != 0 {
				a.dragTo(m.X, m.Y)
				return a, nil
			}
			a.hoverX, a.hoverY = m.X, m.Y
			switch {
			case m.X == l.leftW:
				a.hoverDiv = 1
			case m.X < l.leftW && ly == l.projH+1+l.leftTopH:
				a.hoverDiv = 4
			case m.X > l.leftW && ly == l.rightTopH:
				a.hoverDiv = 2
			case l.cmdH > 0 && m.X > l.leftW && ly == l.rightTopH+1+l.drawerH:
				a.hoverDiv = 5
			case m.X == l.leftW+1+a.drawerSplit(l.rightW) && ly > l.rightTopH && ly < l.rightTopH+1+l.drawerH:
				a.hoverDiv = 3
			default:
				a.hoverDiv = 0
			}
			return a, nil
		case tea.MouseActionRelease:
			if a.dragMode != 0 {
				a.dragMode = 0
				a.hoverDiv = 0
				a.saveConfig()
				return a, nil
			}
		case tea.MouseActionPress:
			if m.Button != tea.MouseButtonLeft {
				break
			}
			detect := func(mode int, key string, reset func()) bool {
				if a.isDoubleClick(key) {
					reset()
					a.dragMode = 0
					a.saveConfig()
					return true
				}
				a.dragMode = mode
				a.hoverDiv = 0
				a.dragTo(m.X, m.Y)
				return true
			}
			switch {
			case m.X == l.leftW:
				if detect(1, "div:left", func() { a.leftW = 0 }) {
					return a, nil
				}
			case m.X < l.leftW && ly == l.projH+1+l.leftTopH:
				if detect(4, "div:lefttop", func() { a.leftTopH = 0 }) {
					return a, nil
				}
			case m.X > l.leftW && ly == l.rightTopH:
				if detect(2, "div:top", func() { a.rightTopH = 0 }) {
					return a, nil
				}
			case l.cmdH > 0 && m.X > l.leftW && ly == l.rightTopH+1+l.drawerH:
				if detect(5, "div:cmd", func() { a.cmdH = 0 }) {
					return a, nil
				}
			case m.X == l.leftW+1+a.drawerSplit(l.rightW) && ly > l.rightTopH && ly < l.rightTopH+1+l.drawerH:
				if detect(3, "div:vert", func() { a.drawerLeftW = 0 }) {
					return a, nil
				}
			}
		}
	}
	switch m.Button {
	case tea.MouseButtonWheelUp:
		pane := a.pane
		if p, _, ok := a.hitTest(m.X, m.Y); ok {
			pane = p
		}
		return a, a.scrollPane(pane, -3)
	case tea.MouseButtonWheelDown:
		pane := a.pane
		if p, _, ok := a.hitTest(m.X, m.Y); ok {
			pane = p
		}
		return a, a.scrollPane(pane, 3)
	}
	if m.Action != tea.MouseActionPress {
		return a, nil
	}
	switch m.Button {
	case tea.MouseButtonLeft:
		cmd, _ := a.mouseClick(m, false)
		return a, cmd
	case tea.MouseButtonRight:
		a.modalAnchorX, a.modalAnchorY = m.X, m.Y
		cmd, _ := a.mouseClick(m, true)
		pane, row, ok := a.hitTest(m.X, m.Y)
		if !ok {
			return a, cmd
		}
		a.pane = pane
		if pane == PaneBranches && row >= 0 {
			lines := a.sidebarLines()
			idx := a.sideOff + row
			if idx >= 0 && idx < len(lines) && lines[idx].itemIdx < 0 {
				a.openSectionMenu(lines[idx].section)
				return a, cmd
			}
		}
		if row < 0 {
			switch pane {
			case PaneProjects:
				a.openProjectMenu(-1)
			case PaneBranches:
				a.openBranchesHeaderMenu()
			case PaneChanges:
				a.choose(i18n.S("工作区改动（全部）", "working changes (all)"), a.uncommittedMenuChoices())
			case PaneFiles:
				a.openFilesHeaderMenu()
			case PaneContent:
				a.openContentHeaderMenu()
			case PaneLog:
				a.openCommandsHeaderMenu()
			case PaneInfo:
				a.openInfoHeaderMenu()
			}
			return a, cmd
		}
		menu := a.openContextMenu()
		if menu == nil {
			return a, cmd
		}
		return a, tea.Batch(cmd, menu)
	}
	return a, nil
}

// choiceMenuBox 手工绘制等宽浮动菜单（保证边框对齐）。
func (a *App) choiceMenuBox() ([]string, int, int) {
	m := a.modal
	if m == nil {
		return nil, 0, 0
	}
	w := 52
	if a.width-6 < w {
		w = a.width - 6
	}
	if w < 30 {
		w = 30
	}
	cap := a.height - 4
	if cap < 1 {
		cap = 1
	}
	n := len(m.choices)
	start := 0
	if n > cap {
		start = m.sel - cap/2
		if start < 0 {
			start = 0
		}
		if start+cap > n {
			start = n - cap
		}
	}
	a.modalChoiceStart = start
	end := start + cap
	if end > n {
		end = n
	}
	// 标题与分页信息共用可用宽度：先给分页留位置再截断标题，避免撑宽边框。
	title := m.title
	if n > cap {
		page := fmt.Sprintf(" (%d-%d/%d)", start+1, end, n)
		title = truncate(title, w-4-strWidth(page)) + page
	} else {
		title = truncate(title, w-4)
	}
	// 标题右侧用横线填充（而非空格），与区域边框的 ─── 样式一致。
	fillTitle := w - 4 - strWidth(title)
	if fillTitle < 0 {
		fillTitle = 0
	}
	top := "╭─ " + title + " " + strings.Repeat("─", fillTitle) + "─╮"
	lines := []string{top}
	for i := start; i < end; i++ {
		c := m.choices[i]
		marker := "  "
		if i == m.sel {
			marker = "› "
		}
		label := fmt.Sprintf("%s%d. %s", marker, i+1, c.label)
		if c.desc != "" && w >= 56 {
			label += "  " + c.desc
		}
		row := "│ " + padRight(truncate(label, w-2), w-1) + "│"
		if i == m.sel {
			row = styleSel.Render(row)
		}
		lines = append(lines, row)
	}
	// 条目超过可视区时在右边框绘制滚动条
	if start2, thumb, ok := scrollThumb(start, n, cap, cap); ok {
		for i := 0; i < thumb; i++ {
			idx := 1 + start2 + i
			if idx > 0 && idx < len(lines) {
				lines[idx] = replaceCellStyled(lines[idx], w+1, styleAccent.Render("┃"))
			}
		}
	}
	lines = append(lines, "╰"+strings.Repeat("─", w)+"╯")
	boxW := w + 2
	x := a.modalAnchorX
	y := a.modalAnchorY + 1
	if x+boxW > a.width {
		x = a.width - boxW
	}
	if x < 0 {
		x = 0
	}
	if y+len(lines) > a.height-1 {
		y = a.height - len(lines) - 1
	}
	if y < 1 {
		y = 1
	}
	return lines, x, y
}

// choiceIndexAt 返回坐标对应的菜单项下标。
func (a *App) choiceIndexAt(x, y int) (int, bool) {
	lines, bx, by := a.choiceMenuBox()
	if len(lines) == 0 || y < by || y >= by+len(lines) {
		return 0, false
	}
	boxW := 0
	for _, l := range lines {
		if lw := strWidth(l); lw > boxW {
			boxW = lw
		}
	}
	if x < bx || x >= bx+boxW {
		return 0, false
	}
	// 菜单首行是边框，条目从 by+1 开始。
	idx := y - by - 1 + a.modalChoiceStart
	if idx < 0 || idx >= len(a.modal.choices) {
		return 0, false
	}
	return idx, true
}

// modalClick 处理模态菜单的鼠标点击。
func (a *App) modalClick(m tea.MouseMsg) tea.Cmd {
	if a.modal == nil || a.modal.kind != modalChoice {
		return nil
	}
	idx, ok := a.choiceIndexAt(m.X, m.Y)
	if !ok {
		a.modal = nil
		return nil
	}
	a.modal.sel = idx
	action := a.modal.choices[idx].action
	a.modal = nil
	if action != nil {
		return action(a)
	}
	return nil
}

// anchorMenu 为键盘打开的菜单设置锚点（靠近当前面板）。
func (a *App) anchorMenu() {
	l := a.panelLayout()
	switch a.pane {
	case PaneBranches:
		a.modalAnchorX, a.modalAnchorY = 2, 2
	case PaneChanges:
		a.modalAnchorX, a.modalAnchorY = 2, l.projH+1+l.leftTopH+2
	case PaneCommits:
		a.modalAnchorX, a.modalAnchorY = l.leftW+3, 2
	case PaneFiles:
		a.modalAnchorX, a.modalAnchorY = l.leftW+3, l.rightTopH+2
	case PaneContent:
		a.modalAnchorX, a.modalAnchorY = l.leftW+3+a.drawerSplit(l.rightW), l.rightTopH+2
	case PaneLog:
		a.modalAnchorX, a.modalAnchorY = l.leftW+3, l.rightTopH+2+l.drawerH+2
	}
}

// mouseClick 选中鼠标所在行；right 为 true 时只选中不执行双击动作。
// activateProjectRow 展开/折叠项目节点或切换到 worktree。
func (a *App) activateProjectRow() tea.Cmd {
	rows := a.projectRows()
	if a.projectSel < 0 || a.projectSel >= len(rows) {
		return nil
	}
	r := rows[a.projectSel]
	if r.worktree != nil {
		return a.switchToWorktree(r.project.Path, r.worktree.Path)
	}
	if len(a.worktrees[r.project.Path]) > 1 {
		a.projectCollapsed[r.project.Path] = !a.projectCollapsed[r.project.Path]
		a.ensureProjectVisible()
		return nil
	}
	return a.switchProject(a.projectIndex(r.project.Path))
}

func (a *App) mouseClick(m tea.MouseMsg, right bool) (tea.Cmd, bool) {
	pane, row, ok := a.hitTest(m.X, m.Y)
	if !ok {
		return nil, false
	}
	a.pane = pane
	switch pane {
	case PaneBranches:
		a.ensureSideVisible()
		lines := a.sidebarLines()
		idx := a.sideOff + row
		if idx < 0 || idx >= len(lines) {
			return nil, true
		}
		ln := lines[idx]
		if ln.itemIdx < 0 {
			if !right {
				a.toggleSection(ln.section)
			}
			return nil, true
		}
		a.sideSel = ln.itemIdx
		a.sideSelKey = a.sideItems[a.sideSel].key
		if it := a.sideItems[a.sideSel]; it.kind == sideProject {
			if !right {
				return a.switchProject(a.projectIndex(it.project.Path)), true
			}
			return nil, true
		}
		cmd := a.selectBranchTip()
		if !right && a.isDoubleClick("side:"+a.sideSelKey) {
			return a.sidebarEnter(&a.sideItems[a.sideSel]), true
		}
		return cmd, true
	case PaneProjects:
		if row < 0 {
			return nil, true
		}
		rows := a.projectRows()
		idx := a.projectOff + row
		if idx < 0 || idx >= len(rows) {
			return nil, true
		}
		a.pane = PaneProjects
		a.projectSel = idx
		a.ensureProjectVisible()
		r := rows[idx]
		if right {
			if r.worktree != nil {
				a.openWorktreeMenu(r.project.Path, *r.worktree)
			} else {
				a.openProjectMenu(a.projectIndex(r.project.Path))
			}
			return nil, true
		}
		return a.activateProjectRow(), true
	case PaneCommits:
		if row < 0 {
			// 只有左键点击标题栏才打开分支筛选；点边框/右键不弹菜单
			if b, ok := a.paneBoxOf(PaneCommits); !right && ok && m.Y == b.y {
				a.modalAnchorX, a.modalAnchorY = m.X, m.Y
				a.openBranchFilterMenu()
			}
			return nil, true
		}
		idx := a.graphOff + row
		if idx < 0 || idx >= a.totalRows() {
			return nil, true
		}
		switch {
		case idx < a.dirtyOffset():
			// 未提交行不参与多选
			a.clearCommitSelection()
			a.graphSel = idx
		case m.Ctrl && !right:
			if a.commitAnchor < 0 {
				anchor := a.graphSel
				if anchor < a.dirtyOffset() || anchor >= a.totalRows() || anchor == idx {
					anchor = idx
				}
				a.commitAnchor = anchor
			}
			a.graphSel = idx
		case right && a.commitAnchor >= 0 && a.rowInCommitSelection(idx):
			// 右键点在已选区间内：保留区间，不改变活动行
		default:
			a.clearCommitSelection()
			a.graphSel = idx
		}
		a.clampSelection()
		cmd := a.loadFilesForSelection()
		key := "row:" + i18n.S("dirty", "dirty")
		if c := a.selectedCommit(); c != nil {
			a.selHash = c.Hash
			key = "commit:" + c.Hash
		}
		if !right && !m.Ctrl && a.isDoubleClick(key) {
			if a.onDirtyRow() {
				a.pane = PaneFiles
				a.ensureFileVisible()
				return cmd, true
			}
			if c := a.selectedCommit(); c != nil {
				return a.openCommitDetail(c.Hash), true
			}
		}
		return cmd, true
	case PaneChanges:
		idx := a.changeOff + row
		if idx < 0 || idx >= len(a.changesRows) {
			return nil, true
		}
		a.changeSel = idx
		a.ensureChangeVisible()
		n := a.changesRows[idx].node
		if !right && a.isDoubleClick("chg:"+n.path) {
			if n.dir {
				a.toggleChangeDir(n)
				return nil, true
			}
			if f := a.selectedChangeEntry(); f != nil {
				a.pane = PaneContent
				return a.loadFileContent(*f, true), true
			}
		}
		if !n.dir {
			if f := a.selectedChangeEntry(); f != nil {
				return a.loadFileContent(*f, true), true
			}
		}
		return nil, true
	case PaneFiles:
		idx := a.fileOff + row
		if idx < 0 || idx >= len(a.fileRows) {
			return nil, true
		}
		a.fileSel = idx
		a.ensureFileVisible()
		n := a.fileRows[idx].node
		if n.dir {
			if !right {
				a.toggleFileDir(n)
			}
			return nil, true
		}
		f := a.selectedFile()
		if f == nil {
			return nil, true
		}
		cmd := a.loadFileContent(*f, a.filesOwner == "work")
		if !right && a.isDoubleClick("file:"+f.path) {
			a.pane = PaneContent
			a.content.offset = 0
			a.clampContentScroll()
		}
		return cmd, true
	case PaneContent:
		a.clampContentScroll()
		return nil, true
	case PaneLog:
		a.clampCmdOffset()
		return nil, true
	case PaneInfo:
		a.clampInfoOffset()
		return nil, true
	}
	return nil, false
}

// hitTest 根据坐标判断所在区域与内容行号（已扣除边框）。
// paneBoxOf 返回指定区域的矩形。
func (a *App) paneBoxOf(p Pane) (paneBox, bool) {
	for _, b := range a.paneBoxes() {
		if b.pane == p {
			return b, true
		}
	}
	return paneBox{}, false
}

func (a *App) hitTest(x, y int) (Pane, int, bool) {
	for _, b := range a.paneBoxes() {
		if x < b.x || x >= b.x+b.w || y < b.y || y >= b.y+b.h {
			continue
		}
		if x == b.x || x == b.x+b.w-1 || y == b.y || y == b.y+b.h-1 {
			return b.pane, -1, true
		}
		return b.pane, y - b.y - 1, true
	}
	return a.pane, -1, false
}

// scrollPane 处理滚轮：列表面板滚动视口，内容区滚动文本。
func (a *App) scrollPane(pane Pane, delta int) tea.Cmd {
	a.pane = pane
	clamp := func(v, max int) int {
		if v > max {
			v = max
		}
		if v < 0 {
			v = 0
		}
		return v
	}
	switch pane {
	case PaneProjects:
		a.projectOff = clamp(a.projectOff+delta, len(a.projectRows())-a.projectViewHeight())
		return nil
	case PaneBranches:
		h := a.branchViewHeight()
		max := len(a.sidebarLines()) - h
		a.sideOff = clamp(a.sideOff+delta, max)
		return nil
	case PaneChanges:
		a.changeOff = clamp(a.changeOff+delta, len(a.changesRows)-a.changesViewHeight())
		return nil
	case PaneFiles:
		a.fileOff = clamp(a.fileOff+delta, len(a.fileRows)-a.fileViewHeight())
		return nil
	case PaneLog:
		a.cmdOffset -= delta
		a.clampCmdOffset()
		return nil
	case PaneInfo:
		a.infoOff += delta
		a.clampInfoOffset()
		return nil
	case PaneCommits:
		// 滚轮只滚动视口，不改变选中提交，避免滚动时反复加载详情
		max := a.totalRows() - a.commitViewHeight()
		if max < 0 {
			max = 0
		}
		a.graphOff = clamp(a.graphOff+delta, max)
		return nil
	}
	return a.move(delta)
}

// drawerSplit 返回抽屉内文件列宽度（含边框）。
func (a *App) drawerSplit(width int) int {
	f := a.drawerLeftW
	if f <= 0 {
		f = width / 3
	}
	if f < 14 {
		f = 14
	}
	if max := width - 16; f > max {
		f = max
	}
	return f
}

// dragTo 根据拖动模式调整布局尺寸。
func (a *App) dragTo(x, y int) {
	l := a.panelLayout()
	clamp := func(v, lo, hi int) int {
		if v < lo {
			v = lo
		}
		if v > hi {
			v = hi
		}
		return v
	}
	switch a.dragMode {
	case 1:
		a.leftW = clamp(x, 22, a.width-40)
	case 4:
		a.leftTopH = clamp(y-(l.projH+1), 4, l.bodyH-l.projH-7)
	case 2:
		a.rightTopH = clamp(y, 4, l.bodyH-8)
	case 5:
		drawerH := y - (l.rightTopH + 1)
		cmdH := l.bodyH - (l.rightTopH + 1 + drawerH) - 1
		if cmdH < 2 {
			cmdH = 2
		}
		if max := l.bodyH - l.rightTopH - 6; cmdH > max {
			cmdH = max
		}
		a.cmdH = cmdH
	case 3:
		w := x - (l.leftW + 1)
		a.drawerLeftW = clamp(w, 14, l.rightW-16)
	}
}
