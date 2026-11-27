package ui

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/ssk-wh/gitc/internal/clipboard"
	"github.com/ssk-wh/gitc/internal/config"
	"github.com/ssk-wh/gitc/internal/git"
	"github.com/ssk-wh/gitc/internal/graph"
	"github.com/ssk-wh/gitc/internal/i18n"
	"github.com/ssk-wh/gitc/internal/sanitize"
	"github.com/ssk-wh/gitc/internal/update"
	"github.com/ssk-wh/gitc/internal/workspace"
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
	auto    bool
	status  git.Status
	refs    []git.Ref
	state   git.RepoState
	stashes []git.Stash
	err     error

	statusErr  error
	refsErr    error
	stateErr   error
	stashesErr error
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
	id   int
	name string
	out  string
	err  error
}

// compareMsg 是分支对比结果。
type compareMsg struct {
	gen     int
	key     string
	title   string
	summary git.CompareSummary
	err     error
}

// compareBranchCmd 在详情浮层展示所选分支相对当前分支的独有提交与差异统计。
// 这是只读操作，不占用 busy 状态。
func (a *App) compareBranchCmd(ref git.Ref) tea.Cmd {
	base := a.status.Branch
	if ref.Kind == git.RefLocal && ref.Name == base {
		a.setToast(i18n.S("已在当前分支，无需对比", "already on this branch"), false)
		return nil
	}
	baseName := base
	if base == "" {
		base = "HEAD"
	} else {
		base = "refs/heads/" + base
	}
	target := refIdentity(ref)
	title := fmt.Sprintf(i18n.S("对比 %s..%s", "compare %s..%s"), baseName, ref.Name)
	key := "compare:" + base + ".." + target
	gen := a.beginDetail(key)
	a.overlay = overlayDetail
	a.detail = detailModel{loading: true, kind: "compare", title: title}
	svc := a.svc
	return func() tea.Msg {
		ctx, cancel := context.WithTimeout(context.Background(), readTimeout)
		defer cancel()
		sum, err := svc.CompareBranches(ctx, base, target, 0)
		return compareMsg{gen: gen, key: key, title: title, summary: sum, err: err}
	}
}

// detailMsg 是详情加载结果。
type detailMsg struct {
	gen    int
	key    string
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
			return a.setBranchFilter(refIdentity(r), "", r.Name)
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
	return a.refreshCmdWith(quick, false)
}

// autoRefreshCmd 是定时轮询触发的刷新。
func (a *App) autoRefreshCmd() tea.Cmd {
	// 定时检查需要包含未跟踪文件，才能发现外部新增文件；无变化时不会重绘。
	return a.refreshCmdWith(false, true)
}

// autoRefreshTickMsg 触发一次外部变更检查。
type autoRefreshTickMsg struct{}

func autoRefreshTickCmd() tea.Cmd {
	return tea.Tick(3*time.Second, func(time.Time) tea.Msg { return autoRefreshTickMsg{} })
}

func (a *App) refreshCmdWith(quick, auto bool) tea.Cmd {
	a.refreshGen++
	gen := a.refreshGen
	svc := a.svc
	return func() tea.Msg {
		ctx, cancel := context.WithTimeout(context.Background(), readTimeout)
		defer cancel()
		m := refreshMsg{gen: gen, quick: quick, auto: auto}
		// 并行读取状态与引用；State 走文件系统检测，Stashes 仅在完整刷新时读取。
		var wg sync.WaitGroup
		wg.Add(2)
		go func() {
			defer wg.Done()
			m.status, m.statusErr = svc.Status(ctx, !quick)
		}()
		go func() {
			defer wg.Done()
			m.refs, m.refsErr = svc.Refs(ctx)
		}()
		if !quick {
			wg.Add(1)
			go func() {
				defer wg.Done()
				m.stashes, m.stashesErr = svc.Stashes(ctx)
			}()
		}
		wg.Wait()
		m.state, m.stateErr = svc.State(ctx)
		m.err = m.statusErr
		if m.err == nil {
			m.err = m.refsErr
		}
		if m.err == nil {
			m.err = m.stateErr
		}
		if m.err == nil {
			m.err = m.stashesErr
		}
		return m
	}
}

// reloadLogCmd 重新流式加载提交历史。
// reloadLogCmdPreserve 后台重载但保留当前画面，收到首批数据后再替换。
func (a *App) reloadLogCmdPreserve() tea.Cmd {
	a.clearCommitSelection()
	a.logGen++
	a.logPreserve = true
	gen := a.logGen
	a.rememberSelection()
	if a.stream != nil {
		a.stream.cancel()
	}
	a.stream = newLogStream(a.svc, maxCommits, a.branchFilter, a.branchFilterScope)
	a.loadingLog = true
	a.truncated = false
	return readBatchCmd(a.stream, gen)
}

func (a *App) reloadLogCmd() tea.Cmd {
	a.clearCommitSelection()
	a.logPreserve = false
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
	if a.busy {
		return a.busyToast()
	}
	a.busy = true
	a.opID++
	id := a.opID
	a.taskName = name
	ctx, cancel := context.WithTimeout(context.Background(), opTimeout)
	a.opCancel = cancel
	return tea.Batch(func() tea.Msg {
		out, err := fn(ctx)
		if ctx.Err() != nil {
			err = ctx.Err()
		}
		cancel()
		return opDoneMsg{id: id, name: name, out: out, err: err}
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
	a.searchResults = nil
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

// copyLabel 生成提示里展示的短文本。
func copyLabel(text string) string {
	label := strings.ReplaceAll(text, "\n", " ")
	runes := []rune(label)
	if len(runes) > 24 {
		label = string(runes[:24]) + "…"
	}
	return label
}

// copyText 复制文本到系统剪贴板。
func copyText(text string) tea.Cmd {
	label := copyLabel(text)
	return func() tea.Msg {
		return copyMsg{label: label, err: clipboard.Copy(text)}
	}
}

// copyToClipboard 立即显示“已复制”提示，再异步写剪贴板。
// 剪贴板写入在部分平台较慢，若等命令返回才提示，界面会一直停留在上一条消息。
func (a *App) copyToClipboard(text string) tea.Cmd {
	a.setToast(i18n.S("已复制: ", "copied: ")+copyLabel(text), false)
	return copyText(text)
}

// commitMessageMsg 是读取到的完整提交信息。
type commitMessageMsg struct {
	sha     string
	message string
	err     error
}

// loadCommitMessageCmd 读取提交原始信息，随后打开编辑框。
func (a *App) loadCommitMessageCmd(sha string) tea.Cmd {
	svc := a.svc
	return func() tea.Msg {
		ctx, cancel := context.WithTimeout(context.Background(), readTimeout)
		defer cancel()
		msg, err := svc.CommitMessage(ctx, sha)
		return commitMessageMsg{sha: sha, message: msg, err: err}
	}
}

// editCommitMessagePrompt 打开提交信息编辑框并确认改写历史。
func (a *App) editCommitMessagePrompt(sha, msg string) {
	a.promptText(i18n.S("编辑提交信息 "+shortSHA(sha)+"（改写历史）", "edit commit message "+shortSHA(sha)), "", msg, func(a *App, v string) tea.Cmd {
		v = strings.TrimSpace(v)
		if v == "" {
			a.setToast(i18n.S("提交信息不能为空", "commit message required"), true)
			return nil
		}
		a.confirm(i18n.S("改写提交信息", "rewrite commit message"),
			i18n.S("会重写该提交及其后续提交；已推送时需强推。", "Rewrites this commit and descendants; a pushed branch needs force-push."),
			"git rebase --onto <new> "+shortSHA(sha), true, func(a *App) tea.Cmd {
				return a.runOp("edit message "+shortSHA(sha), func(ctx context.Context) (string, error) {
					return a.svc.EditCommitMessage(ctx, sha, v)
				})
			})
		return nil
	})
}

// squashPlan 校验多选区间并返回合并所需的提交与父提交。
func (a *App) squashPlan() (hashes []string, head, parent, msg string, err error) {
	if a.filter != "" || a.searchResults != nil {
		return nil, "", "", "", fmt.Errorf("%s", i18n.S("搜索/过滤状态下不能合并提交", "cannot squash while filtering"))
	}
	if a.status.Detached {
		return nil, "", "", "", fmt.Errorf("%s", i18n.S("分离 HEAD 状态下不能合并提交", "cannot squash on detached HEAD"))
	}
	if len(a.status.Staged) > 0 {
		return nil, "", "", "", fmt.Errorf("%s", i18n.S("存在已暂存改动，请先提交或取消暂存", "unstage changes before squashing"))
	}
	lo, hi, ok := a.commitSelectionRows()
	if !ok {
		return nil, "", "", "", fmt.Errorf("%s", i18n.S("请先用 Ctrl 选择至少两个连续提交", "select at least two consecutive commits first"))
	}
	if lo != a.dirtyOffset() {
		return nil, "", "", "", fmt.Errorf("%s", i18n.S("只能合并从最新提交（HEAD）开始的一段", "squash must start from HEAD"))
	}
	commits := a.visibleCommits()
	i0, i1 := lo-a.dirtyOffset(), hi-a.dirtyOffset()
	if i0 < 0 || i1 >= len(commits) {
		return nil, "", "", "", fmt.Errorf("%s", i18n.S("所选提交已失效，请重新选择", "selection is stale"))
	}
	selected := commits[i0 : i1+1]
	head = selected[0].Hash
	if a.status.OID != "" && head != a.status.OID {
		return nil, "", "", "", fmt.Errorf("%s", i18n.S("只能合并当前 HEAD 开始的提交", "squash must start from HEAD"))
	}
	for k := 0; k < len(selected)-1; k++ {
		if len(selected[k].Parents) != 1 || selected[k].Parents[0] != selected[k+1].Hash {
			return nil, "", "", "", fmt.Errorf("%s", i18n.S("所选提交不是一条线性历史（含合并提交）", "selection is not a linear chain"))
		}
	}
	oldest := selected[len(selected)-1]
	if len(oldest.Parents) == 0 {
		return nil, "", "", "", fmt.Errorf("%s", i18n.S("不能合并到根提交", "cannot squash the root commit"))
	}
	parent = oldest.Parents[0]
	for _, c := range selected {
		hashes = append(hashes, c.Hash)
	}
	var lines []string
	for i := len(selected) - 1; i >= 0; i-- {
		lines = append(lines, selected[i].Subject)
	}
	msg = strings.Join(lines, string(rune(10)))
	return hashes, head, parent, msg, nil
}

// squashMessagesMsg 是选中提交的完整信息集合。
type squashMessagesMsg struct {
	hashes []string
	head   string
	parent string
	msg    string
	err    error
}

// loadSquashMessagesCmd 异步读取所有选中提交的完整提交信息（标题 + 正文）。
func (a *App) loadSquashMessagesCmd(hashes []string, head, parent string) tea.Cmd {
	svc := a.svc
	return func() tea.Msg {
		ctx, cancel := context.WithTimeout(context.Background(), readTimeout)
		defer cancel()
		parts := make([]string, 0, len(hashes))
		for i := len(hashes) - 1; i >= 0; i-- {
			msg, err := svc.CommitMessage(ctx, hashes[i])
			if err != nil {
				return squashMessagesMsg{err: err}
			}
			if msg = strings.TrimSpace(msg); msg != "" {
				parts = append(parts, msg)
			}
		}
		return squashMessagesMsg{hashes: hashes, head: head, parent: parent, msg: strings.Join(parts, string(rune(10))+string(rune(10)))}
	}
}

// squashPrompt 合并多选提交：先读取完整提交信息，再打开编辑框。
func (a *App) squashPrompt() tea.Cmd {
	hashes, head, parent, _, err := a.squashPlan()
	if err != nil {
		a.setToast(err.Error(), true)
		return nil
	}
	return a.loadSquashMessagesCmd(hashes, head, parent)
}

// openSquashPrompt 用完整提交信息集合打开合并编辑框。
func (a *App) openSquashPrompt(hashes []string, head, parent, msg string) {
	n := len(hashes)
	a.promptText(fmt.Sprintf(i18n.S("合并 %d 个提交（改写历史，可编辑提交信息）", "squash %d commits"), n), "", msg, func(a *App, v string) tea.Cmd {
		v = strings.TrimSpace(v)
		if v == "" {
			a.setToast(i18n.S("提交信息不能为空", "commit message required"), true)
			return nil
		}
		a.confirm(fmt.Sprintf(i18n.S("确认合并 %d 个提交", "confirm squash of %d commits"), n),
			fmt.Sprintf(i18n.S("将 %s 到 %s 合并为一个提交；已推送时需强推。", "Squash %s..%s; a pushed branch needs force-push."), shortSHA(hashes[n-1]), shortSHA(head)),
			"git reset --soft "+shortSHA(parent)+" && git commit", true, func(a *App) tea.Cmd {
				return a.runOp(fmt.Sprintf("squash %d commits", n), func(ctx context.Context) (string, error) {
					return a.svc.Squash(ctx, head, parent, v)
				})
			})
		return nil
	})
}

// updateCheckMsg 是启动时检查更新的结果。
type updateCheckMsg struct {
	tag   string
	newer bool
	err   error
}

// checkUpdateCmd 在 Release 构建启动时检查是否有新版本（失败静默）。
func (a *App) checkUpdateCmd() tea.Cmd {
	if buildVersion == "" {
		return nil
	}
	cur := buildVersion
	return func() tea.Msg {
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		tag, newer, err := update.Check(ctx, cur, os.Getenv("GITC_REPO"), os.Getenv("GITC_MIRROR"))
		return updateCheckMsg{tag: tag, newer: newer, err: err}
	}
}

// saveWorkspace 把当前项目列表写入当前仓库的 .gitc-workspace。
func (a *App) saveWorkspace() {
	root := cleanProjectPath(a.currentProjectPath)
	if root == "" {
		root = cleanProjectPath(a.svc.RootPath())
	}
	if root == "" {
		return
	}
	if err := workspace.Save(root, a.ProjectPaths()); err != nil {
		a.setToast(i18n.S("写入 .gitc-workspace 失败: ", "write .gitc-workspace failed: ")+firstLine(err.Error()), true)
	}
}

// saveWorkspaceAll 把当前项目组写入所有项目根目录的 .gitc-workspace。
func (a *App) saveWorkspaceAll(extra ...string) {
	paths := a.ProjectPaths()
	seen := map[string]bool{}
	all := append([]string(nil), extra...)
	for _, p := range a.projects {
		all = append(all, p.Path)
	}
	for _, root := range all {
		root = cleanProjectPath(root)
		if root == "" || seen[root] {
			continue
		}
		seen[root] = true
		if err := workspace.Save(root, paths); err != nil && root == a.currentProjectPath {
			a.setToast(i18n.S("写入 .gitc-workspace 失败: ", "write .gitc-workspace failed: ")+firstLine(err.Error()), true)
		}
	}
}

// loadInitialProjects 启动时优先读取当前仓库的 .gitc-workspace。
func (a *App) loadInitialProjects(c config.File) {
	root := a.svc.RootPath()
	paths := c.Projects
	if ws, err := workspace.Load(root); err == nil && len(ws.Projects) > 0 {
		paths = ws.Projects
	}
	a.SetProjects(a.resolveWorkspacePaths(root, paths), root)
}

// resolveWorkspacePaths 把 workspace 中的相对路径按仓库根目录解析。
func (a *App) resolveWorkspacePaths(root string, paths []string) []string {
	out := make([]string, 0, len(paths))
	for _, p := range paths {
		p = strings.TrimSpace(p)
		if p == "" {
			continue
		}
		if !filepath.IsAbs(p) {
			p = filepath.Join(root, p)
		}
		out = append(out, p)
	}
	return out
}

const maxCachedCommits = 20000

// saveProjectCache 保存当前项目的界面快照。
func (a *App) saveProjectCache() {
	if a.currentPath == "" {
		return
	}
	commits := a.commits
	if len(commits) > maxCachedCommits {
		commits = commits[:maxCachedCommits]
	}
	copyMap := func(m map[string]int) map[string]int {
		out := make(map[string]int, len(m))
		for k, v := range m {
			out[k] = v
		}
		return out
	}
	a.projectCache[a.currentPath] = &projectCache{
		commits:      append([]git.Commit(nil), commits...),
		refs:         append([]git.Ref(nil), a.refs...),
		status:       a.status,
		state:        a.state,
		stashes:      append([]git.Stash(nil), a.stashes...),
		layout:       a.layout,
		commitColor:  copyMap(a.commitColor),
		branchColor:  copyMap(a.branchColor),
		graphSel:     a.graphSel,
		graphOff:     a.graphOff,
		sideSelKey:   a.sideSelKey,
		branchFilter: a.branchFilter,
		filterScope:  a.branchFilterScope,
		filterLabel:  a.branchFilterLabel,
		truncated:    a.truncated,
	}
}

// restoreProjectCache 用缓存立即渲染项目，返回需要异步加载的文件命令。
func (a *App) restoreProjectCache(root string) (tea.Cmd, bool) {
	c, ok := a.projectCache[root]
	if !ok || (len(c.commits) == 0 && len(c.refs) == 0) {
		return nil, false
	}
	a.commits = append([]git.Commit(nil), c.commits...)
	a.refs = append([]git.Ref(nil), c.refs...)
	a.status = c.status
	a.state = c.state
	a.stashes = append([]git.Stash(nil), c.stashes...)
	a.commitColor = c.commitColor
	a.branchColor = c.branchColor
	a.branchFilter, a.branchFilterScope, a.branchFilterLabel = c.branchFilter, c.filterScope, c.filterLabel
	a.graphSel, a.graphOff = c.graphSel, c.graphOff
	a.truncated = c.truncated
	a.builder = graph.NewBuilder()
	for _, commit := range a.commits {
		a.builder.Add(graph.Commit{Hash: commit.Hash, Parents: commit.Parents})
	}
	a.builder.SetColorOf(a.commitColor)
	if c.layout != nil {
		a.layout = c.layout
	} else {
		a.layout = a.builder.Layout()
	}
	a.sideSelKey = c.sideSelKey
	a.rebuildSidebar()
	a.clampSelection()
	a.rebuildChanges()
	a.invalidateView()
	var cmd tea.Cmd
	if a.onDirtyRow() {
		cmd = a.loadWorkingFiles()
	} else {
		cmd = a.loadFilesForSelection()
	}
	return cmd, true
}

// switchToWorktree 切换当前仓库上下文到某个 worktree，不改变项目列表。
func (a *App) switchToWorktree(projectPath, wtPath string) tea.Cmd {
	if wtPath == "" || wtPath == a.currentPath {
		return nil
	}
	if a.busy {
		return a.busyToast()
	}
	a.saveProjectCache()
	svc, err := a.openRepo(wtPath)
	if err != nil {
		a.setToast(i18n.S("打开 worktree 失败: ", "open worktree failed: ")+firstLine(err.Error()), true)
		return nil
	}
	if a.stream != nil {
		a.stream.cancel()
		a.stream = nil
	}
	if a.opCancel != nil {
		a.opCancel()
		a.opCancel = nil
	}
	if r, ok := svc.(*git.Repo); ok {
		r.SetCommandHook(a.LogCommand)
	}
	a.svc = svc
	root := cleanProjectPath(svc.RootPath())
	if root == "" {
		root = cleanProjectPath(wtPath)
	}
	a.currentPath = root
	a.currentProjectPath = cleanProjectPath(projectPath)
	a.resetForProject()
	fileCmd, restored := a.restoreProjectCache(root)
	a.projectSel = a.projectRowIndex(func(r projectRow) bool { return r.worktree != nil && r.worktree.Path == root })
	a.ensureProjectVisible()
	base := a.loadWorktreesCmd()
	if restored {
		a.verifyAfterRestore = true
		return tea.Batch(a.refreshCmd(true), fileCmd, base)
	}
	return tea.Batch(a.refreshCmd(true), a.reloadLogCmd(), base)
}

// projectRowIndex 返回满足条件的项目树行下标。
func (a *App) projectRowIndex(match func(projectRow) bool) int {
	for i, r := range a.projectRows() {
		if match(r) {
			return i
		}
	}
	return 0
}

// switchProject 打开并切换到指定项目，重置该仓库的界面状态。
func (a *App) switchProject(idx int) tea.Cmd {
	if idx < 0 || idx >= len(a.projects) {
		return nil
	}
	p := a.projects[idx]
	if p.Path == a.currentPath {
		return nil
	}
	if a.busy {
		return a.busyToast()
	}
	a.saveProjectCache()
	svc, err := a.openRepo(p.Path)
	if err != nil {
		a.setToast(i18n.S("打开项目失败: ", "open project failed: ")+firstLine(err.Error()), true)
		return nil
	}
	if a.stream != nil {
		a.stream.cancel()
		a.stream = nil
	}
	if a.opCancel != nil {
		a.opCancel()
		a.opCancel = nil
	}
	if r, ok := svc.(*git.Repo); ok {
		r.SetCommandHook(a.LogCommand)
	}
	a.svc = svc
	root := cleanProjectPath(svc.RootPath())
	if root == "" {
		root = p.Path
	}
	paths := a.ProjectPaths()
	if ws, err := workspace.Load(root); err == nil && len(ws.Projects) > 0 {
		paths = ws.Projects
	}
	a.currentPath = root
	a.currentProjectPath = root
	a.resetForProject()
	a.SetProjects(paths, root)
	fileCmd, restored := a.restoreProjectCache(root)
	a.projectSel = a.projectRowIndex(func(r projectRow) bool { return r.worktree == nil && r.project.Path == root })
	a.ensureProjectVisible()
	a.saveConfig()
	if restored {
		// 先用缓存渲染，由随后的一次刷新校验是否有外部变化。
		a.verifyAfterRestore = true
		return tea.Batch(a.refreshCmd(true), fileCmd, a.loadProjectBranchesCmd(), a.loadWorktreesCmd())
	}
	a.sideSelKey = "p:" + root
	a.rebuildSidebar()
	a.projectSel = a.projectRowIndex(func(r projectRow) bool { return r.worktree == nil && r.project.Path == root })
	a.ensureProjectVisible()
	return tea.Batch(a.refreshCmd(true), a.reloadLogCmd(), a.loadProjectBranchesCmd(), a.loadWorktreesCmd())
}

// resetForProject 清理切换仓库后不应保留的状态。
func (a *App) resetForProject() {
	a.refreshGen++
	a.logGen++
	a.searchGen++
	a.detailGen++
	a.detailKey = ""
	a.filesGen++
	a.contentGen++
	a.busy = false
	a.taskName = ""
	a.loadingLog = false
	a.truncated = false
	a.commits = nil
	a.refs = nil
	a.status = git.Status{}
	a.state = git.RepoState{}
	a.stashes = nil
	a.builder = graph.NewBuilder()
	a.layout = a.builder.Layout()
	a.commitColor = map[string]int{}
	a.branchColor = map[string]int{}
	a.files = nil
	a.fileTree = nil
	a.fileRows = nil
	a.fileSel, a.fileOff, a.filesOwner = 0, 0, ""
	a.changes = nil
	a.changesTree = nil
	a.changesRows = nil
	a.changeSel, a.changeOff = 0, 0
	a.content = detailModel{}
	a.detail = detailModel{}
	a.overlay = overlayNone
	a.modal = nil
	a.searchActive = false
	a.filter, a.searchQuery = "", ""
	a.searchResults = nil
	a.searching = false
	a.branchFilter, a.branchFilterScope, a.branchFilterLabel = "", "", ""
	a.clearCommitSelection()
	a.graphSel, a.graphOff, a.selHash = 0, 0, ""
	a.sideItems = nil
	a.sideSel, a.sideOff, a.sideSelKey = 0, 0, ""
	a.commands = nil
	a.logs = nil
	a.cmdOffset = 0
	a.infoOff = 0
	a.expanded = map[string]bool{}
	a.changesExpanded = map[string]bool{}
	a.collapsed = map[string]bool{}
	a.invalidateView()
}

// dirPickedMsg 是系统目录选择框的结果。
type dirPickedMsg struct {
	path string
	err  error
}

// pickProjectDirCmd 调用系统目录选择框。
func (a *App) pickProjectDirCmd() tea.Cmd {
	pick := a.pickDir
	return func() tea.Msg {
		path, err := pick()
		return dirPickedMsg{path: path, err: err}
	}
}

// worktreesMsg 是各项目 worktree 列表的加载结果。
type worktreesMsg struct {
	list map[string][]git.Worktree
}

// loadWorktreesCmd 异步读取所有项目的 worktree。
func (a *App) loadWorktreesCmd() tea.Cmd {
	open := a.openRepo
	projects := append([]projectEntry(nil), a.projects...)
	return func() tea.Msg {
		out := map[string][]git.Worktree{}
		for _, p := range projects {
			ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
			svc, err := open(p.Path)
			if err == nil {
				if wts, err := svc.Worktrees(ctx); err == nil {
					valid := make([]git.Worktree, 0, len(wts))
					stale := false
					for _, wt := range wts {
						if worktreeInvalid(wt) {
							stale = true
							continue
						}
						valid = append(valid, wt)
					}
					if stale {
						// 目录已删除但元数据残留：静默清理，避免继续列出失效 worktree
						_, _ = svc.PruneWorktrees(ctx)
					}
					out[p.Path] = valid
				}
			}
			cancel()
		}
		return worktreesMsg{list: out}
	}
}

// projectBranchesMsg 是各项目当前分支的加载结果。
type projectBranchesMsg struct {
	branches map[string]string
}

// loadProjectBranchesCmd 异步读取所有项目的当前分支。
func (a *App) loadProjectBranchesCmd() tea.Cmd {
	if len(a.projects) <= 1 {
		return nil
	}
	open := a.openRepo
	projects := append([]projectEntry(nil), a.projects...)
	return func() tea.Msg {
		out := map[string]string{}
		for _, p := range projects {
			ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
			svc, err := open(p.Path)
			if err == nil {
				if b := svc.CurrentBranch(ctx); b != "" {
					out[p.Path] = b
				}
			}
			cancel()
		}
		return projectBranchesMsg{branches: out}
	}
}

// openProjectPicker 打开内置目录选择对话框（系统选择框不可用时兜底）。
func (a *App) openProjectPicker() {
	dir, err := os.UserHomeDir()
	if err != nil || dir == "" {
		dir = "."
	}
	a.modal = &modal{kind: modalDirPicker, title: i18n.S("选择项目目录", "choose project directory")}
	a.loadProjectDir(dir)
}

// loadProjectDir 读取目录下的子目录。
func (a *App) loadProjectDir(dir string) {
	m := a.modal
	if m == nil || m.kind != modalDirPicker {
		return
	}
	dir = cleanProjectPath(dir)
	if dir == "" {
		dir = "."
	}
	entries, err := os.ReadDir(dir)
	m.dir = dir
	m.dirs = nil
	m.err = ""
	m.sel = 0
	m.listOff = 0
	if err != nil {
		m.err = firstLine(err.Error())
		return
	}
	for _, e := range entries {
		if !e.IsDir() || strings.HasPrefix(e.Name(), ".") {
			continue
		}
		m.dirs = append(m.dirs, e.Name())
	}
	sort.Strings(m.dirs)
}

// sameProjectDir 判断两个路径是否指向同一目录，兼容符号链接与 Windows 大小写。
func sameProjectDir(a, b string) bool {
	ai, aerr := os.Stat(a)
	bi, berr := os.Stat(b)
	if aerr == nil && berr == nil {
		return os.SameFile(ai, bi)
	}
	return cleanProjectPath(a) == cleanProjectPath(b)
}

// chooseProjectDir 校验并添加目录对应的仓库。
func (a *App) chooseProjectDir(dir string) tea.Cmd {
	p := cleanProjectPath(dir)
	if p == "" {
		return nil
	}
	svc, err := a.openRepo(p)
	if err != nil {
		if a.modal != nil && a.modal.kind == modalDirPicker {
			a.modal.err = i18n.S("不是有效的 git 仓库", "not a git repository")
		} else {
			a.setToast(i18n.S("不是有效的 git 仓库: ", "not a git repository: ")+firstLine(err.Error()), true)
		}
		return nil
	}
	root := cleanProjectPath(svc.RootPath())
	if root != "" && !sameProjectDir(p, root) {
		msg := i18n.S("所选目录不是 Git 仓库根目录（仓库根目录：", "selected directory is not a Git repository root (root: ") + root + ")"
		if a.modal != nil && a.modal.kind == modalDirPicker {
			a.modal.err = msg
		} else {
			a.setToast(msg, true)
		}
		return nil
	}
	if root != "" {
		p = root
	}
	if a.projectIndex(p) >= 0 {
		if a.modal != nil && a.modal.kind == modalDirPicker {
			a.modal.err = i18n.S("项目已在列表中", "project already added")
		} else {
			a.setToast(i18n.S("项目已在列表中", "project already added"), false)
		}
		return nil
	}
	a.projects = append(a.projects, projectEntry{Name: filepath.Base(p), Path: p})
	a.saveConfig()
	a.saveWorkspaceAll()
	a.rebuildSidebar()
	a.modal = nil
	a.setToast(i18n.S("已添加项目 "+p, "project added: "+p), false)
	return tea.Batch(a.loadProjectBranchesCmd(), a.loadWorktreesCmd())
}

// removeProject 从列表移除项目；移除当前项目时自动切换到剩余第一个。
func (a *App) removeProject(p projectEntry) tea.Cmd {
	if len(a.projects) <= 1 {
		a.setToast(i18n.S("至少保留一个项目", "keep at least one project"), true)
		return nil
	}
	wasCurrent := p.Path == a.currentProjectPath
	out := make([]projectEntry, 0, len(a.projects))
	for _, e := range a.projects {
		if e.Path != p.Path {
			out = append(out, e)
		}
	}
	a.projects = out
	a.saveConfig()
	a.saveWorkspaceAll(p.Path)
	a.rebuildSidebar()
	a.setToast(i18n.S("已移除项目 "+p.Name, "project removed: "+p.Name), false)
	if wasCurrent {
		return a.switchProject(0)
	}
	return nil
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
	if len(choices) == 0 {
		return // 没有可用操作时不弹空菜单
	}
	a.modal = &modal{kind: modalChoice, title: title, choices: choices}
}

// —— 详情加载 ——

func (a *App) beginDetail(key string) int {
	a.detailGen++
	a.detailKey = key
	return a.detailGen
}

func (a *App) closeDetail() {
	a.overlay = overlayNone
	a.detailGen++
	a.detailKey = ""
}

func (a *App) openCommitDetail(sha string) tea.Cmd {
	key := "commit:" + sha
	gen := a.beginDetail(key)
	a.overlay = overlayDetail
	a.detail = detailModel{loading: true, sha: sha, kind: "commit", title: sha}
	svc := a.svc
	return func() tea.Msg {
		ctx, cancel := context.WithTimeout(context.Background(), readTimeout)
		defer cancel()
		d, err := svc.CommitDetail(ctx, sha)
		if err != nil {
			return detailMsg{gen: gen, key: key, sha: sha, kind: "commit", err: err}
		}
		diff, derr := svc.CommitDiff(ctx, sha, "")
		if derr != nil {
			diff = i18n.S("（无法读取 diff）", "(failed to load diff)")
		}
		return detailMsg{gen: gen, key: key, sha: sha, kind: "commit", title: d.Subject, detail: d, diff: diff}
	}
}

func (a *App) openFileDiff(title, path string, staged, untracked bool) tea.Cmd {
	key := fmt.Sprintf("file:%t:%t:%s", staged, untracked, path)
	gen := a.beginDetail(key)
	a.overlay = overlayDetail
	a.detail = detailModel{loading: true, kind: "file", title: title}
	svc := a.svc
	return func() tea.Msg {
		ctx, cancel := context.WithTimeout(context.Background(), readTimeout)
		defer cancel()
		diff, err := svc.WorkingDiff(ctx, path, staged, untracked)
		return detailMsg{gen: gen, key: key, sha: "", kind: "file", title: title, diff: diff, err: err}
	}
}

func (a *App) openStashDiff(st git.Stash) tea.Cmd {
	key := "stash:" + st.Hash
	gen := a.beginDetail(key)
	a.overlay = overlayDetail
	a.detail = detailModel{loading: true, kind: "stash", title: st.Subject}
	svc := a.svc
	return func() tea.Msg {
		ctx, cancel := context.WithTimeout(context.Background(), readTimeout)
		defer cancel()
		diff, err := svc.StashDiff(ctx, st.Hash)
		return detailMsg{gen: gen, key: key, sha: "", kind: "stash", title: st.Subject, diff: diff, err: err}
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
	sameOwner := a.filesOwner == "work"
	a.filesOwner = "work"
	a.setFiles(a.workingFileEntries())
	if !sameOwner {
		a.content = detailModel{title: i18n.S("工作区改动", "working tree")}
	}
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
	if a.filesOwner != owner {
		a.files = nil
		a.fileTree, a.fileRows = nil, nil
		a.fileSel, a.fileOff = 0, 0
	}
	a.filesOwner = owner
	a.commitBody = ""
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
	if working && strings.HasSuffix(f.path, "/") {
		a.content = detailModel{title: title, entry: f, working: true, lines: []string{
			i18n.S("未跟踪目录：空格暂存整个目录，d 丢弃", "untracked directory: space to stage, d to discard"),
		}}
		return nil
	}
	owner := "work"
	if !working {
		owner = strings.TrimPrefix(a.filesOwner, "c:")
	}
	offset := 0
	if a.content.entry.path == f.path && a.content.working == working && a.content.sha == owner {
		offset = a.content.offset
	}
	a.content = detailModel{loading: true, title: title, sha: owner, entry: f, working: working, offset: offset}
	if working {
		path, staged, untracked := f.path, f.staged, f.untracked
		return func() tea.Msg {
			ctx, cancel := context.WithTimeout(context.Background(), readTimeout)
			defer cancel()
			diff, err := svc.WorkingDiff(ctx, path, staged, untracked)
			return contentMsg{gen: gen, path: path, title: title, sha: owner, working: true, diff: diff, err: err}
		}
	}
	path := f.path
	return func() tea.Msg {
		ctx, cancel := context.WithTimeout(context.Background(), readTimeout)
		defer cancel()
		diff, err := svc.CommitDiff(ctx, owner, path)
		return contentMsg{gen: gen, path: path, title: title, sha: owner, diff: diff, err: err}
	}
}

func (a *App) retryFileView() tea.Cmd {
	switch a.content.retry {
	case "content":
		f := a.content.entry
		if f.path == "" {
			if selected := a.selectedFile(); selected != nil {
				f = *selected
			}
		}
		if f.path != "" {
			return a.loadFileContent(f, a.content.working)
		}
	case "files":
		sha := strings.TrimPrefix(a.filesOwner, "c:")
		for i := range a.commits {
			if a.commits[i].Hash == sha {
				return a.loadCommitFiles(&a.commits[i])
			}
		}
	}
	return nil
}

// commandMsg 是一条已执行命令记录。
type commandMsg struct {
	at   time.Time
	line string
}

// LogCommand 供 git 层回调：记录命令到右下角命令历史（线程安全）。
func (a *App) LogCommand(line string) {
	line = sanitize.Secrets(sanitize.Line(line))
	select {
	case a.msgs <- commandMsg{at: time.Now(), line: line}:
	default:
	}
}
