package ui

import (
	"context"
	"fmt"
	"sort"
	"strings"
	"sync"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/ssk-wh/gitc/internal/clipboard"
	"github.com/ssk-wh/gitc/internal/git"
	"github.com/ssk-wh/gitc/internal/graph"
	"github.com/ssk-wh/gitc/internal/i18n"
	"github.com/ssk-wh/gitc/internal/sanitize"
)

const (
	maxCommits      = 100000
	maxSearchResult = 3000
	opTimeout       = 5 * time.Minute
	readTimeout     = 30 * time.Second
)

// refreshMsg 携带一次完整的状态刷新结果。
type refreshMsg struct {
	gen     int
	quick   bool
	status  git.Status
	refs    []git.Ref
	state   git.RepoState
	stashes []git.Stash
	err     error
}

// logBatchMsg 是一批提交（流式加载）。
type logBatchMsg struct {
	gen     int
	commits []git.Commit
	done    bool
	err     error
}

// opDoneMsg 是一次变更操作的结果。
type opDoneMsg struct {
	name string
	out  string
	err  error
}

// detailMsg 是详情加载结果。
type detailMsg struct {
	sha    string
	kind   string
	title  string
	detail git.CommitDetail
	diff   string
	err    error
}

// searchDoneMsg 是全历史搜索结果。
type searchDoneMsg struct {
	gen       int
	query     string
	commits   []git.Commit
	err       error
	truncated bool
}

type spinnerTickMsg struct{}

// logStream 是流式日志读取器。
type logStream struct {
	ch     chan git.Commit
	err    error
	cancel context.CancelFunc
}

func newLogStream(svc git.Service, max int, ref, scope string) *logStream {
	ctx, cancel := context.WithCancel(context.Background())
	s := &logStream{ch: make(chan git.Commit, 512), cancel: cancel}
	go func() {
		defer close(s.ch)
		consume := func(c git.Commit) bool {
			select {
			case s.ch <- c:
				return true
			case <-ctx.Done():
				return false
			}
		}
		switch {
		case scope != "":
			s.err = svc.StreamLogScope(ctx, scope, max, consume)
		case ref != "":
			s.err = svc.StreamLogRef(ctx, ref, max, consume)
		default:
			s.err = svc.StreamLog(ctx, max, consume)
		}
	}()
	return s
}

// openBranchFilterMenu 打开分支筛选下拉（COMMITS 标题/键 v）。
func (a *App) openBranchFilterMenu() {
	tip := map[string]time.Time{}
	for _, c := range a.commits {
		tip[c.Hash] = c.When
	}
	refs := make([]git.Ref, 0, len(a.refs))
	for _, r := range a.refs {
		if r.Kind == git.RefTag {
			continue
		}
		refs = append(refs, r)
	}
	sort.SliceStable(refs, func(i, j int) bool {
		ti, tj := tip[refs[i].Target], tip[refs[j].Target]
		if !ti.Equal(tj) {
			return ti.After(tj)
		}
		return refs[i].Name < refs[j].Name
	})
	choices := []choice{
		{label: i18n.S("全部引用（all）", "all refs"), desc: i18n.S("显示所有分支/远端的提交", "commits from every ref"), action: func(a *App) tea.Cmd {
			return a.setBranchFilter("", "", i18n.S("全部引用", "all"))
		}},
		{label: i18n.S("仅本地分支（local）", "local branches only"), desc: i18n.S("显示 refs/heads 可达的提交", "commits reachable from refs/heads"), action: func(a *App) tea.Cmd {
			return a.setBranchFilter("", "local", i18n.S("本地分支", "local"))
		}},
		{label: i18n.S("仅远端分支（remote）", "remote branches only"), desc: i18n.S("显示 refs/remotes 可达的提交", "commits reachable from refs/remotes"), action: func(a *App) tea.Cmd {
			return a.setBranchFilter("", "remote", i18n.S("远端分支", "remote"))
		}},
	}
	for _, ref := range refs {
		r := ref
		prefix := ""
		if r.Kind == git.RefRemote {
			prefix = i18n.S("远端 ", "remote ")
		}
		label := prefix + r.Name
		if r.Head {
			label += i18n.S("（当前）", " (current)")
		}
		choices = append(choices, choice{label: label, desc: r.Name, action: func(a *App) tea.Cmd {
			return a.setBranchFilter(r.Name, "", r.Name)
		}})
	}
	a.choose(i18n.S("显示某个分支的提交", "show commits of a branch"), choices)
}

// setBranchFilter 设置分支筛选并重新加载提交。
// scope 为 "local"/"remote" 时按分组筛选；rev 非空时筛选单个分支。
func (a *App) setBranchFilter(rev, scope, label string) tea.Cmd {
	a.branchFilter = rev
	a.branchFilterScope = scope
	a.branchFilterLabel = label
	a.selHash = ""
	a.graphSel, a.graphOff = 0, 0
	a.setToast(fmt.Sprintf(i18n.S("提交视图：%s", "commits: %s"), label), false)
	return a.reloadLogCmd()
}

// readBatchCmd 读取首批提交（200 条，尽快出首帧）。
func readBatchCmd(s *logStream, gen int) tea.Cmd { return readBatchN(s, gen, 200) }

// readBatchN 读取下一批提交。
func readBatchN(s *logStream, gen, size int) tea.Cmd {
	return func() tea.Msg {
		if s == nil {
			return logBatchMsg{gen: gen, done: true}
		}
		batch := make([]git.Commit, 0, size)
		for len(batch) < size {
			c, ok := <-s.ch
			if !ok {
				return logBatchMsg{gen: gen, commits: batch, done: true, err: s.err}
			}
			batch = append(batch, c)
		}
		return logBatchMsg{gen: gen, commits: batch}
	}
}

// refreshCmd 刷新状态（quick=true 时暂不扫描未跟踪文件）。
func (a *App) refreshCmd(quick bool) tea.Cmd {
	a.refreshGen++
	gen := a.refreshGen
	svc := a.svc
	return func() tea.Msg {
		ctx, cancel := context.WithTimeout(context.Background(), readTimeout)
		defer cancel()
		m := refreshMsg{gen: gen, quick: quick}
		// 并行读取状态与引用；State 走文件系统检测，Stashes 仅在完整刷新时读取。
		var wg sync.WaitGroup
		wg.Add(2)
		go func() {
			defer wg.Done()
			m.status, m.err = svc.Status(ctx, !quick)
		}()
		go func() {
			defer wg.Done()
			m.refs, _ = svc.Refs(ctx)
		}()
		if !quick {
			wg.Add(1)
			go func() {
				defer wg.Done()
				m.stashes, _ = svc.Stashes(ctx)
			}()
		}
		wg.Wait()
		m.state, _ = svc.State(ctx)
		return m
	}
}

// reloadLogCmd 重新流式加载提交历史。
func (a *App) reloadLogCmd() tea.Cmd {
	a.logGen++
	gen := a.logGen
	a.rememberSelection()
	if a.stream != nil {
		a.stream.cancel()
	}
	a.stream = newLogStream(a.svc, maxCommits, a.branchFilter, a.branchFilterScope)
	a.commits = nil
	a.commitColor = map[string]int{}
	a.builder = graph.NewBuilder()
	a.layout = a.builder.Layout()
	a.loadingLog = true
	a.truncated = false
	a.invalidateView()
	return readBatchCmd(a.stream, gen)
}

// runOp 执行一次变更操作；结束后刷新状态与日志。
func (a *App) runOp(name string, fn func(ctx context.Context) (string, error)) tea.Cmd {
	a.busy = true
	a.taskName = name
	return tea.Batch(func() tea.Msg {
		ctx, cancel := context.WithTimeout(context.Background(), opTimeout)
		defer cancel()
		a.opCancel = cancel
		out, err := fn(ctx)
		return opDoneMsg{name: name, out: out, err: err}
	}, spinnerTickCmd())
}

func spinnerTickCmd() tea.Cmd {
	return tea.Tick(120*time.Millisecond, func(time.Time) tea.Msg { return spinnerTickMsg{} })
}

// runSearch 全历史搜索（标题 + 作者）。
func (a *App) runSearch(query string) tea.Cmd {
	a.searchGen++
	gen := a.searchGen
	svc := a.svc
	a.searching = true
	a.searchQuery = query
	return func() tea.Msg {
		ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
		defer cancel()
		seen := make(map[string]bool, 1024)
		var list []git.Commit
		add := func(c git.Commit) bool {
			if seen[c.Hash] {
				return true
			}
			seen[c.Hash] = true
			list = append(list, c)
			return len(list) < maxSearchResult
		}
		err := svc.SearchLog(ctx, query, maxSearchResult, add)
		if err != nil {
			return searchDoneMsg{gen: gen, query: query, err: err}
		}
		if len(list) < maxSearchResult {
			_ = svc.SearchAuthorLog(ctx, query, maxSearchResult, add)
		}
		// sha 前缀：回退到直接解析引用
		if len(list) == 0 && isHexPrefix(query) {
			if c, err := svc.LogOne(ctx, query); err == nil && c.Hash != "" {
				add(c)
			}
		}
		sort.SliceStable(list, func(i, j int) bool { return list[i].When.After(list[j].When) })
		return searchDoneMsg{gen: gen, query: query, commits: list, truncated: len(list) >= maxSearchResult}
	}
}

// isHexPrefix 判断是否为 4-40 位十六进制。
func isHexPrefix(s string) bool {
	if len(s) < 4 || len(s) > 40 {
		return false
	}
	for _, c := range s {
		if !(c >= '0' && c <= '9' || c >= 'a' && c <= 'f' || c >= 'A' && c <= 'F') {
			return false
		}
	}
	return true
}

// copyMsg 是复制操作结果。
type copyMsg struct {
	label string
	err   error
}

// copyText 复制文本到系统剪贴板。
func copyText(text string) tea.Cmd {
	label := text
	runes := []rune(label)
	if len(runes) > 24 {
		label = string(runes[:24]) + "…"
	}
	return func() tea.Msg {
		return copyMsg{label: label, err: clipboard.Copy(text)}
	}
}

// —— 弹窗辅助 ——

func (a *App) confirm(title, body, command string, danger bool, action func(*App) tea.Cmd) {
	a.modal = &modal{
		kind:      modalConfirm,
		title:     title,
		body:      body,
		command:   command,
		danger:    danger,
		onConfirm: action,
	}
}

func (a *App) prompt(title, placeholder, initial string, onSubmit func(*App, string) tea.Cmd) {
	ti := newTextInput(placeholder)
	ti.SetValue(initial)
	ti.CursorEnd()
	a.modal = &modal{
		kind:     modalInput,
		title:    title,
		input:    ti,
		onSubmit: onSubmit,
	}
}

func (a *App) promptLive(title, placeholder string, onChange func(*App, string) tea.Cmd, onSubmit func(*App, string) tea.Cmd) {
	ti := newTextInput(placeholder)
	a.modal = &modal{
		kind:     modalInput,
		title:    title,
		input:    ti,
		onChange: onChange,
		onSubmit: onSubmit,
	}
}

func (a *App) promptText(title, placeholder, initial string, onSubmit func(*App, string) tea.Cmd) {
	ta := newTextArea(placeholder)
	ta.SetValue(initial)
	ta.CursorEnd()
	a.modal = &modal{
		kind:     modalText,
		title:    title,
		text:     ta,
		onSubmit: onSubmit,
	}
}

func (a *App) choose(title string, choices []choice) {
	a.modal = &modal{kind: modalChoice, title: title, choices: choices}
}

// —— 详情加载 ——

func (a *App) openCommitDetail(sha string) tea.Cmd {
	a.overlay = overlayDetail
	a.detail = detailModel{loading: true, sha: sha, kind: "commit", title: sha}
	svc := a.svc
	return func() tea.Msg {
		ctx, cancel := context.WithTimeout(context.Background(), readTimeout)
		defer cancel()
		d, err := svc.CommitDetail(ctx, sha)
		if err != nil {
			return detailMsg{sha: sha, kind: "commit", err: err}
		}
		diff, derr := svc.CommitDiff(ctx, sha, "")
		if derr != nil {
			diff = i18n.S("（无法读取 diff）", "(failed to load diff)")
		}
		return detailMsg{sha: sha, kind: "commit", title: d.Subject, detail: d, diff: diff}
	}
}

func (a *App) openFileDiff(title, path string, staged, untracked bool) tea.Cmd {
	a.overlay = overlayDetail
	a.detail = detailModel{loading: true, kind: "file", title: title}
	svc := a.svc
	return func() tea.Msg {
		ctx, cancel := context.WithTimeout(context.Background(), readTimeout)
		defer cancel()
		diff, err := svc.WorkingDiff(ctx, path, staged, untracked)
		return detailMsg{sha: "", kind: "file", title: title, diff: diff, err: err}
	}
}

func (a *App) openStashDiff(st git.Stash) tea.Cmd {
	a.overlay = overlayDetail
	a.detail = detailModel{loading: true, kind: "stash", title: st.Subject}
	svc := a.svc
	return func() tea.Msg {
		ctx, cancel := context.WithTimeout(context.Background(), readTimeout)
		defer cancel()
		diff, err := svc.StashDiff(ctx, st.Index)
		return detailMsg{sha: "", kind: "stash", title: st.Subject, diff: diff, err: err}
	}
}

// opName 生成日志中的操作名。
func opName(format string, args ...any) string { return fmt.Sprintf(format, args...) }

// renderPathList 用于确认框展示路径。
func renderPathList(paths []string, limit int) string {
	if len(paths) <= limit {
		return strings.Join(paths, "\n")
	}
	return strings.Join(paths[:limit], "\n") + fmt.Sprintf("\n… (+%d)", len(paths)-limit)
}

// filesMsg 是所选提交的文件列表加载结果。
type filesMsg struct {
	gen    int
	owner  string
	files  []fileEntry
	detail git.CommitDetail
	err    error
}

// contentMsg 是内容区 diff 加载结果。
type contentMsg struct {
	gen     int
	path    string
	title   string
	sha     string
	diff    string
	working bool
	err     error
}

// loadFilesForSelection 根据当前选中行加载文件列表。
// 选中「未提交变更」伪行时展示工作区文件。
func (a *App) loadFilesForSelection() tea.Cmd {
	if a.onDirtyRow() {
		return a.loadWorkingFiles()
	}
	c := a.selectedCommit()
	if c == nil {
		a.files = nil
		a.filesOwner = ""
		a.content = detailModel{loading: false, lines: []string{i18n.S("（没有选中的提交）", "(no commit selected)")}}
		return nil
	}
	owner := "c:" + c.Hash
	if a.filesOwner == owner {
		return nil
	}
	return a.loadCommitFiles(c)
}

// loadWorkingFiles 展示工作区文件。
func (a *App) loadWorkingFiles() tea.Cmd {
	a.filesGen++
	a.filesOwner = "work"
	a.setFiles(a.workingFileEntries())
	a.content = detailModel{title: i18n.S("工作区改动", "working tree")}
	if len(a.files) == 0 {
		a.content.lines = []string{i18n.S("（工作区没有改动）", "(no local changes)")}
		return nil
	}
	if f := a.selectedFile(); f != nil {
		return a.loadFileContent(*f, true)
	}
	a.content.lines = []string{i18n.S("选择文件查看修改内容", "select a file to view its diff")}
	return nil
}

// loadCommitFiles 异步加载提交文件列表。
func (a *App) loadCommitFiles(c *git.Commit) tea.Cmd {
	a.filesGen++
	gen := a.filesGen
	owner := "c:" + c.Hash
	a.filesOwner = owner
	a.commitBody = ""
	a.files = nil
	a.fileSel, a.fileOff = 0, 0
	a.content = detailModel{loading: true, sha: c.Hash, title: c.Short + " " + sanitize.Line(c.Subject),
		lines: []string{
			"commit " + c.Hash,
			"Author: " + sanitize.Line(c.Author),
			"Date:   " + c.When.Format("2006-01-02 15:04:05"),
			"",
			sanitize.Line(c.Subject),
			"",
			i18n.S("选择左侧文件查看修改内容", "select a file to view its diff"),
		}}
	svc := a.svc
	sha := c.Hash
	return func() tea.Msg {
		ctx, cancel := context.WithTimeout(context.Background(), readTimeout)
		defer cancel()
		d, err := svc.CommitDetail(ctx, sha)
		files := make([]fileEntry, 0, len(d.Files))
		for _, f := range d.Files {
			files = append(files, fileEntry{path: f.Path, origPath: f.OldPath, status: f.Status})
		}
		return filesMsg{gen: gen, owner: owner, files: files, detail: d, err: err}
	}
}

// loadFileContent 异步加载选中文件的内容/diff。
func (a *App) loadFileContent(f fileEntry, working bool) tea.Cmd {
	a.contentGen++
	gen := a.contentGen
	svc := a.svc
	title := f.status + " " + f.path
	if f.staged {
		title += i18n.S("  (已暂存)", "  (staged)")
	}
	a.content = detailModel{loading: true, title: title, sha: a.filesOwner}
	if working {
		path, staged, untracked := f.path, f.staged, f.untracked
		return func() tea.Msg {
			ctx, cancel := context.WithTimeout(context.Background(), readTimeout)
			defer cancel()
			diff, err := svc.WorkingDiff(ctx, path, staged, untracked)
			return contentMsg{gen: gen, path: path, title: title, working: true, diff: diff, err: err}
		}
	}
	owner := strings.TrimPrefix(a.filesOwner, "c:")
	path := f.path
	return func() tea.Msg {
		ctx, cancel := context.WithTimeout(context.Background(), readTimeout)
		defer cancel()
		diff, err := svc.CommitDiff(ctx, owner, path)
		return contentMsg{gen: gen, path: path, title: title, sha: owner, diff: diff, err: err}
	}
}

// commandMsg 是一条已执行命令记录。
type commandMsg struct {
	at   time.Time
	line string
}

// LogCommand 供 git 层回调：记录命令到右下角命令历史（线程安全）。
func (a *App) LogCommand(line string) {
	select {
	case a.msgs <- commandMsg{at: time.Now(), line: line}:
	default:
	}
}
