package ui

import (
	"context"
	"fmt"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/charmbracelet/bubbles/textarea"
	"github.com/charmbracelet/bubbles/textinput"
	tea "github.com/charmbracelet/bubbletea"

	"github.com/ssk-wh/gitc/internal/config"
	"github.com/ssk-wh/gitc/internal/git"
	"github.com/ssk-wh/gitc/internal/graph"
	"github.com/ssk-wh/gitc/internal/i18n"
	"github.com/ssk-wh/gitc/internal/platform"
	"github.com/ssk-wh/gitc/internal/sanitize"
)

// Pane 是当前焦点。
type Pane int

const (
	PaneBranches Pane = iota // 左上：分支/引用
	PaneChanges              // 左下：工作区改动
	PaneCommits              // 右上：提交图
	PaneFiles                // 右下抽屉：所选提交的文件
	PaneContent              // 右下抽屉：diff/内容
	PaneLog                  // 右下角：命令历史
	PaneInfo                 // 左下角：提交信息
	PaneProjects             // 左上角：项目切换
)

// overlayKind 是全屏覆盖层。
type overlayKind int

const (
	overlayNone overlayKind = iota
	overlayDetail
	overlayHelp
	overlayLog
)

// modalKind 是模态框类型。
type modalKind int

const (
	modalNone modalKind = iota
	modalConfirm
	modalInput
	modalText
	modalChoice
	modalDirPicker
)

// choice 是选择菜单项。
type choice struct {
	label  string
	desc   string
	action func(*App) tea.Cmd
}

// modal 是当前模态框。
type modal struct {
	kind    modalKind
	title   string
	body    string
	command string
	danger  bool

	input     textinput.Model
	text      textarea.Model
	onChange  func(*App, string) tea.Cmd
	onSubmit  func(*App, string) tea.Cmd
	onCancel  func(*App) tea.Cmd
	onConfirm func(*App) tea.Cmd

	choices []choice
	sel     int

	// 目录选择框状态
	dir     string
	dirs    []string
	listOff int
	err     string
}

// sideKind 是侧栏条目类型。
type sideKind int

const (
	sideBranch sideKind = iota
	sideRemote
	sideTag
	sideStash
	sideFile
	sideProject
)

// sideItem 是侧栏中的一个可选条目。
type sideItem struct {
	kind      sideKind
	label     string
	key       string
	section   string
	ref       git.Ref
	stash     git.Stash
	file      git.FileChange
	untracked bool
	staged    bool
	project   projectEntry
}

// detailModel 是全屏详情视图。
type detailModel struct {
	title   string
	sha     string
	kind    string // commit / file / stash
	loading bool
	lines   []string
	offset  int
	err     string
	entry   fileEntry
	working bool
	retry   string
}

// fileEntry 是文件区的一行（提交文件或工作区文件）。
type fileEntry struct {
	path      string
	origPath  string
	status    string
	staged    bool
	untracked bool
	unmerged  bool
}

// commandEntry 是已执行命令记录。
type commandEntry struct {
	at   time.Time
	line string
}

// fileNode 是文件树节点（目录或文件）。
type fileNode struct {
	name     string
	path     string
	dir      bool
	children []*fileNode
	entries  []fileEntry
}

// fileRow 是树形视图中的一行。
type fileRow struct {
	node  *fileNode
	depth int
}

// projectEntry 是一个可切换的项目仓库。
type projectEntry struct {
	Name string
	Path string
}

// projectCache 是切换项目时保留的快照，用于先渲染缓存再后台刷新。
type projectCache struct {
	commits      []git.Commit
	refs         []git.Ref
	status       git.Status
	state        git.RepoState
	stashes      []git.Stash
	layout       *graph.Layout
	commitColor  map[string]int
	branchColor  map[string]int
	graphSel     int
	graphOff     int
	sideSelKey   string
	branchFilter string
	filterScope  string
	filterLabel  string
	truncated    bool
	logComplete  bool
}

// projectRow 是项目区树形视图中的一行。
type projectRow struct {
	project  projectEntry
	worktree *git.Worktree // nil 表示项目节点
}

// projectRows 返回项目区的扁平树行（存在多个 worktree 且未折叠时展开子行）。
func (a *App) projectRows() []projectRow {
	var rows []projectRow
	for _, p := range a.projects {
		rows = append(rows, projectRow{project: p})
		wts := a.worktrees[p.Path]
		if len(wts) <= 1 || a.projectCollapsed[p.Path] {
			continue
		}
		for i := range wts {
			wt := wts[i]
			rows = append(rows, projectRow{project: p, worktree: &wt})
		}
	}
	return rows
}

// App 是根模型。
type App struct {
	svc    git.Service
	width  int
	height int

	commits []git.Commit
	builder *graph.Builder
	layout  *graph.Layout

	refs       []git.Ref
	stashes    []git.Stash
	status     git.Status
	state      git.RepoState
	refreshErr string // 上次自动刷新失败的信息，用于避免每 3 秒重复提示

	pane       Pane
	graphSel   int
	graphOff   int
	sideSel    int
	sideOff    int
	sideItems  []sideItem
	sideSelKey string

	// 文件区与内容区
	files        []fileEntry
	fileTree     []*fileNode
	fileRows     []fileRow
	expanded     map[string]bool
	fileSel      int
	fileOff      int
	filesOwner   string // "work" 或 "c:<sha>"
	commitBody   string
	commitAuthor string
	commitColor  map[string]int
	branchColor  map[string]int
	content      detailModel

	filesGen   int
	contentGen int

	filter        string
	searchQuery   string
	searchResults []git.Commit
	searching     bool
	searchActive  bool
	searchInput   textinput.Model

	// 过滤/搜索结果的缓存
	viewCache  []git.Commit
	graphCache *graph.Layout

	overlay   overlayKind
	detail    detailModel
	logs      []string
	commands  []commandEntry
	cmdOffset int
	hoverDiv  int
	hoverX    int
	hoverY    int

	modalAnchorX int
	modalAnchorY int

	modalChoiceStart int
	modal            *modal
	modalPress       *modal

	busy       bool
	opID       int
	taskName   string
	spinner    int
	ticking    bool
	loadingLog bool
	logFailed  bool
	truncated  bool

	refreshGen int
	logGen     int
	searchGen  int
	detailGen  int
	projectGen int
	detailKey  string
	stream     *logStream

	toast    string
	toastErr bool
	toastGen int
	selHash  string
	quit     bool

	msgs     chan tea.Msg
	opCancel context.CancelFunc

	lastClick    time.Time
	lastClickKey string

	leftW              int // 左栏宽度（0 表示默认）
	leftTopH           int // 左侧分支区高度（0 表示默认比例）
	leftMidH           int // 左侧改动区高度（0 表示默认比例）
	infoOff            int
	cmdH               int // 命令历史高度（0 表示默认）
	changes            []fileEntry
	changesTree        []*fileNode
	changesRows        []fileRow
	changesExpanded    map[string]bool
	changeSel          int
	changeOff          int
	commitAnchor       int // Ctrl 多选锚点（-1 表示未多选）
	latestVersion      string
	updateAvailable    bool
	updateBadgeX       int
	updateBadgeW       int
	updateHintOn       bool
	projects           []projectEntry
	projectSel         int
	projectOff         int
	projectBranch      map[string]string
	projectCache       map[string]*projectCache
	worktreesGen       int
	worktreesLoading   bool
	projectBranchesGen int
	autoTicks          int
	verifyAfterRestore bool
	worktrees          map[string][]git.Worktree
	projectCollapsed   map[string]bool
	currentProjectPath string
	logPreserve        bool
	currentPath        string
	projectH           int
	openRepo           func(string) (git.Service, error)
	pickDir            func() (string, error)
	openDir            func(string) error // 用系统文件管理器打开目录
	drawerLeftW        int                // 抽屉文件列宽度（0 表示默认比例）
	rightTopH          int                // 提交区高度（0 表示默认比例）
	dragMode           int                // 0 无 / 1 左分隔线 / 2 横向分隔线 / 3 抽屉纵向分隔线
	collapsed          map[string]bool

	branchFilter      string
	branchFilterScope string // "local"/"remote" 表示按本地/远端分组筛选
	branchFilterLabel string

	cfg         config.File
	persist     bool
	themeName   string
	themeColors map[string]string
	themeBad    []string
}

// cleanProjectPath 返回规范化的绝对路径。
func cleanProjectPath(p string) string {
	p = strings.TrimSpace(p)
	if p == "" {
		return ""
	}
	abs, err := filepath.Abs(p)
	if err != nil {
		return ""
	}
	return filepath.Clean(abs)
}

// SetProjects 设置项目列表；当前仓库始终包含在列表中。
func (a *App) SetProjects(paths []string, current string) {
	a.worktreesGen++
	a.projectBranchesGen++
	cur := cleanProjectPath(current)
	seen := map[string]bool{}
	var out []projectEntry
	for _, p := range paths {
		p = cleanProjectPath(p)
		if p == "" || seen[p] {
			continue
		}
		seen[p] = true
		out = append(out, projectEntry{Name: filepath.Base(p), Path: p})
	}
	if cur != "" && !seen[cur] {
		out = append(out, projectEntry{Name: filepath.Base(cur), Path: cur})
		seen[cur] = true
	}
	a.projects = out
	a.currentPath = cur
	a.projectSel = a.projectIndex(cur)
	a.rebuildSidebar()
}

// projectIndex 返回路径对应的项目下标。
func (a *App) projectIndex(path string) int {
	for i, p := range a.projects {
		if p.Path == path {
			return i
		}
	}
	return -1
}

// ProjectPaths 返回项目路径列表（用于持久化）。
func (a *App) ProjectPaths() []string {
	out := make([]string, 0, len(a.projects))
	for _, p := range a.projects {
		out = append(out, p.Path)
	}
	return out
}

// New 创建应用。
func New(svc git.Service) *App {
	a := &App{
		svc:              svc,
		builder:          graph.NewBuilder(),
		msgs:             make(chan tea.Msg, 256),
		pane:             PaneCommits,
		collapsed:        map[string]bool{},
		themeName:        "default",
		expanded:         map[string]bool{},
		commitColor:      map[string]int{},
		branchColor:      map[string]int{},
		commitAnchor:     -1,
		projectBranch:    map[string]string{},
		projectCache:     map[string]*projectCache{},
		worktrees:        map[string][]git.Worktree{},
		projectCollapsed: map[string]bool{},
		openRepo:         func(path string) (git.Service, error) { return git.Open(path) },
		pickDir:          platform.PickDirectory,
		openDir:          platform.OpenDirectory,
		changesExpanded:  map[string]bool{},
	}
	a.layout = a.builder.Layout()
	if cur := cleanProjectPath(svc.RootPath()); cur != "" {
		a.projects = []projectEntry{{Name: filepath.Base(cur), Path: cur}}
		a.currentPath = cur
		a.currentProjectPath = cur
	}
	a.toast = i18n.S("按 ? 查看帮助", "press ? for help")
	return a
}

// Msgs 返回用于接收异步进度消息的通道。
func (a *App) Msgs() <-chan tea.Msg { return a.msgs }

// Init 启动首屏加载。
func (a *App) Init() tea.Cmd {
	cmds := []tea.Cmd{a.refreshCmd(true), a.reloadLogCmd(), a.checkUpdateCmd(), a.loadProjectBranchesCmd(), a.loadWorktreesCmd(), autoRefreshTickCmd()}
	if a.toast != "" {
		cmds = append(cmds, toastExpireCmd(a.toastGen))
	}
	return tea.Batch(cmds...)
}

// 尺寸计算
func (a *App) graphHeight() int {
	h := a.height - 2
	if h < 3 {
		h = 3
	}
	return h
}

func (a *App) sidebarVisible() bool { return a.width >= 90 && a.height >= 15 }

func (a *App) sidebarWidth() int {
	w := a.width / 4
	if w < 24 {
		w = 24
	}
	if w > 34 {
		w = 34
	}
	return w
}

func (a *App) graphWidth() int {
	w := a.width
	if a.sidebarVisible() {
		w -= a.sidebarWidth() + 1
	}
	if w < 20 {
		w = 20
	}
	return w
}

func (a *App) bodyHeight() int { return a.graphHeight() }

// dirtyOffset 返回图区顶部未提交伪行占用的行数。
func (a *App) dirtyOffset() int {
	if a.status.Dirty() {
		return 1
	}
	return 0
}

// clearCommitSelection 清除 Ctrl 多选区间。
func (a *App) clearCommitSelection() { a.commitAnchor = -1 }

// commitSelectionRows 返回多选区间 [lo,hi]（至少两行才有效）。
func (a *App) commitSelectionRows() (lo, hi int, ok bool) {
	if a.commitAnchor < 0 {
		return 0, 0, false
	}
	lo, hi = a.commitAnchor, a.graphSel
	if lo > hi {
		lo, hi = hi, lo
	}
	if lo < a.dirtyOffset() || hi >= a.totalRows() || hi <= lo {
		return 0, 0, false
	}
	return lo, hi, true
}

// commitSelectionCount 返回多选提交数（未多选返回 0）。
func (a *App) commitSelectionCount() int {
	lo, hi, ok := a.commitSelectionRows()
	if !ok {
		return 0
	}
	return hi - lo + 1
}

// rowInCommitSelection 判断提交行是否在多选区间内。
func (a *App) rowInCommitSelection(row int) bool {
	lo, hi, ok := a.commitSelectionRows()
	return ok && row >= lo && row <= hi
}

func (a *App) totalRows() int { return len(a.visibleCommits()) + a.dirtyOffset() }

func (a *App) onDirtyRow() bool { return a.graphSel == 0 && a.dirtyOffset() == 1 }

// visibleCommits 返回当前应展示的提交（搜索结果 / 本地过滤 / 全部）。
func (a *App) visibleCommits() []git.Commit {
	if a.searchResults != nil {
		return a.searchResults
	}
	if a.filter == "" {
		return a.commits
	}
	return a.filteredCommits()
}

func (a *App) filteredCommits() []git.Commit {
	if a.viewCache == nil {
		q := strings.ToLower(a.filter)
		res := make([]git.Commit, 0, len(a.commits))
		for _, c := range a.commits {
			if strings.Contains(strings.ToLower(c.Subject), q) ||
				strings.Contains(strings.ToLower(c.Author), q) ||
				strings.HasPrefix(c.Hash, q) {
				res = append(res, c)
			}
		}
		a.viewCache = res
	}
	return a.viewCache
}

// viewLayout 返回当前可见提交的图布局（过滤时重算）。
func (a *App) viewLayout() *graph.Layout {
	if a.searchResults == nil && a.filter == "" {
		return a.layout
	}
	if a.graphCache == nil {
		b := graph.NewBuilder()
		b.SetColorOf(a.commitColor)
		for _, c := range a.visibleCommits() {
			b.Add(graph.Commit{Hash: c.Hash, Parents: c.Parents})
		}
		a.graphCache = b.Layout()
	}
	return a.graphCache
}

// selectedCommit 返回当前选中的提交；选中伪行时返回 nil。
func (a *App) selectedCommit() *git.Commit {
	vc := a.visibleCommits()
	idx := a.graphSel - a.dirtyOffset()
	if idx < 0 || idx >= len(vc) {
		return nil
	}
	return &vc[idx]
}

// clampSelection 修正选中行与滚动偏移。
func (a *App) clampSelection() {
	total := a.totalRows()
	if total == 0 {
		a.graphSel, a.graphOff = 0, 0
		return
	}
	if a.graphSel >= total {
		a.graphSel = total - 1
	}
	if a.graphSel < 0 {
		a.graphSel = 0
	}
	a.ensureGraphVisible()
}

func (a *App) ensureGraphVisible() {
	h := a.commitViewHeight()
	if a.graphSel < a.graphOff {
		a.graphOff = a.graphSel
	}
	if a.graphSel >= a.graphOff+h {
		a.graphOff = a.graphSel - h + 1
	}
	if a.graphOff < 0 {
		a.graphOff = 0
	}
}

// restoreSelection 在重载后按 sha 恢复选中行。
func (a *App) restoreSelection() {
	if a.selHash == "" {
		a.clampSelection()
		return
	}
	vc := a.visibleCommits()
	for i, c := range vc {
		if c.Hash == a.selHash {
			a.graphSel = i + a.dirtyOffset()
			a.ensureGraphVisible()
			return
		}
	}
	a.clampSelection()
}

// rememberSelection 记录当前选中提交。
func (a *App) rememberSelection() {
	if c := a.selectedCommit(); c != nil {
		a.selHash = c.Hash
	} else {
		a.selHash = ""
	}
}

// rebuildSidebar 重新构建侧栏并保持选中项稳定。
func (a *App) rebuildSidebar() {
	key := a.sideSelKey
	var items []sideItem
	add := func(it sideItem) { items = append(items, it) }

	// 分支按 tip 提交时间倒序（用户要求默认按最新提交时间排序）。
	tipTime := make(map[string]time.Time, len(a.commits))
	for _, c := range a.commits {
		tipTime[c.Hash] = c.When
	}
	sortRefs := func(refs []git.Ref) {
		sort.SliceStable(refs, func(i, j int) bool {
			ti, tj := tipTime[refs[i].Target], tipTime[refs[j].Target]
			if !ti.Equal(tj) {
				return ti.After(tj)
			}
			return refs[i].Name < refs[j].Name
		})
	}
	locals := make([]git.Ref, 0, 8)
	for _, ref := range a.refs {
		if ref.Kind == git.RefLocal {
			locals = append(locals, ref)
		}
	}
	sortRefs(locals)
	for _, ref := range locals {
		mark := "  "
		if ref.Head {
			mark = "* "
		}
		add(sideItem{kind: sideBranch, label: mark + sanitize.Line(ref.Name), key: "b:" + ref.Name, section: "branches", ref: ref})
	}
	remotes := make([]git.Ref, 0, 8)
	for _, ref := range a.refs {
		if ref.Kind == git.RefRemote {
			remotes = append(remotes, ref)
		}
	}
	sortRefs(remotes)
	for _, ref := range remotes {
		add(sideItem{kind: sideRemote, label: "  " + sanitize.Line(ref.Name), key: "r:" + ref.Name, section: "remote", ref: ref})
	}
	tags := make([]git.Ref, 0, 8)
	for _, ref := range a.refs {
		if ref.Kind == git.RefTag {
			tags = append(tags, ref)
		}
	}
	sortRefs(tags)
	for _, ref := range tags {
		add(sideItem{kind: sideTag, label: "  " + sanitize.Line(ref.Name), key: "t:" + ref.Name, section: "tags", ref: ref})
	}
	for _, st := range a.stashes {
		label := "  " + st.Selector
		subject := sanitize.Line(st.Subject)
		if i := strings.Index(subject, ":"); i >= 0 && strings.HasPrefix(subject, "WIP on") {
			subject = strings.TrimSpace(subject[i+1:])
		}
		if subject != "" {
			label += " " + subject
		}
		add(sideItem{kind: sideStash, label: label, key: "s:" + st.Selector, section: "stash", stash: st})
	}

	a.sideItems = items
	if key != "" {
		for i, it := range items {
			if it.key == key {
				a.sideSel = i
				return
			}
		}
	}
	if a.sideSel >= len(items) {
		a.sideSel = len(items) - 1
	}
	if a.sideSel < 0 {
		a.sideSel = 0
	}
}

// workingFileEntries 把工作区状态转为文件区条目。
func (a *App) workingFileEntries() []fileEntry {
	var out []fileEntry
	for _, f := range a.status.Staged {
		out = append(out, fileEntry{path: f.Path, origPath: f.OrigPath, status: f.Label(true), staged: true})
	}
	for _, f := range a.status.Unstaged {
		out = append(out, fileEntry{path: f.Path, origPath: f.OrigPath, status: f.Label(false), unmerged: f.Unmerged})
	}
	for _, f := range a.status.Untracked {
		out = append(out, fileEntry{path: f.Path, status: "?", untracked: true})
	}
	return out
}

// selectedFileRow 返回文件区当前选中行。
func (a *App) selectedFileRow() *fileRow {
	if a.fileSel < 0 || a.fileSel >= len(a.fileRows) {
		return nil
	}
	return &a.fileRows[a.fileSel]
}

// selectedFileNode 返回当前选中的文件/目录节点。
func (a *App) selectedFileNode() *fileNode {
	row := a.selectedFileRow()
	if row == nil {
		return nil
	}
	return row.node
}

// selectedFile 返回文件区当前选中的文件条目；目录或空返回 nil。
func (a *App) selectedFile() *fileEntry {
	n := a.selectedFileNode()
	if n == nil || n.dir || len(n.entries) == 0 {
		return nil
	}
	// 优先展示工作区（未暂存）条目
	for i := range n.entries {
		if !n.entries[i].staged {
			return &n.entries[i]
		}
	}
	return &n.entries[0]
}

// setFiles 更新文件列表并重建树形视图。
func (a *App) setFiles(files []fileEntry) {
	selPath, off := "", a.fileOff
	if n := a.selectedFileNode(); n != nil {
		selPath = n.path
	}
	a.files = files
	a.rebuildFileTree()
	a.restoreFileSelectionAt(selPath, off)
}

// buildFileTree 依据文件路径构建目录树（同名文件合并条目）并按展开状态生成可见行。
func buildFileTree(files []fileEntry, expanded map[string]bool) ([]*fileNode, []fileRow) {
	return buildFileTreeDefault(files, expanded, false)
}

// buildFileTreeDefault 构建树；defaultExpand 控制未记录过的目录是否默认展开。
func buildFileTreeDefault(files []fileEntry, expanded map[string]bool, defaultExpand bool) ([]*fileNode, []fileRow) {
	roots := []*fileNode{}
	dirs := map[string]*fileNode{}
	for _, fe := range files {
		raw := strings.ReplaceAll(fe.path, "\\", "/")
		if raw == "" {
			continue
		}
		if strings.HasSuffix(raw, "/") {
			// git 对整目录未跟踪时只给一个 "docs/" 占位：显示为可操作的目录条目，
			// 不再生成空文件名；可整目录暂存/丢弃。
			name := strings.Trim(raw, "/") + "/"
			roots = append(roots, &fileNode{name: name, path: raw, entries: []fileEntry{fe}})
			continue
		}
		path := strings.Trim(raw, "/")
		parts := strings.Split(path, "/")
		cur := &roots
		curPath := ""
		for i, part := range parts {
			if i == len(parts)-1 {
				found := false
				for _, n := range *cur {
					if !n.dir && n.path == fe.path {
						n.entries = append(n.entries, fe)
						found = true
						break
					}
				}
				if !found {
					*cur = append(*cur, &fileNode{name: part, path: fe.path, entries: []fileEntry{fe}})
				}
				break
			}
			if curPath == "" {
				curPath = part
			} else {
				curPath += "/" + part
			}
			n := dirs[curPath]
			if n == nil {
				n = &fileNode{name: part, path: curPath, dir: true}
				dirs[curPath] = n
				*cur = append(*cur, n)
			}
			cur = &n.children
		}
	}
	sortFileTree(roots)
	var rows []fileRow
	var walk func(nodes []*fileNode, depth int)
	walk = func(nodes []*fileNode, depth int) {
		for _, n := range nodes {
			rows = append(rows, fileRow{node: n, depth: depth})
			if n.dir {
				exp, seen := expanded[n.path]
				if !seen {
					exp = defaultExpand
				}
				if exp {
					walk(n.children, depth+1)
				}
			}
		}
	}
	walk(roots, 0)
	return roots, rows
}

// sortFileTree 目录优先、名称排序。
func sortFileTree(nodes []*fileNode) {
	sort.SliceStable(nodes, func(i, j int) bool {
		if nodes[i].dir != nodes[j].dir {
			return nodes[i].dir
		}
		return nodes[i].name < nodes[j].name
	})
	for _, n := range nodes {
		if n.dir {
			sortFileTree(n.children)
		}
	}
}

// rebuildFileTree 重建右侧抽屉文件树。
func (a *App) rebuildFileTree() {
	a.fileTree, a.fileRows = buildFileTreeDefault(a.files, a.expanded, true)
}

// rebuildFileRows 按当前展开状态刷新抽屉可见行。
func (a *App) rebuildFileRows() {
	a.fileTree, a.fileRows = buildFileTreeDefault(a.files, a.expanded, true)
}

// rebuildChanges 重建左下工作区改动树。
func (a *App) rebuildChanges() {
	var selPath string
	if n := a.selectedChangeNode(); n != nil {
		selPath = n.path
	}
	a.changes = a.workingFileEntries()
	a.changesTree, a.changesRows = buildFileTreeDefault(a.changes, a.changesExpanded, true)
	a.changeSel = 0
	if selPath != "" {
		for i, r := range a.changesRows {
			if r.node.path == selPath {
				a.changeSel = i
				break
			}
		}
	}
	a.ensureChangeVisible()
}

// infoViewHeight 是左下提交信息区可显示行数。
func (a *App) infoViewHeight() int {
	l := a.panelLayout()
	h := l.bodyH - 2
	if l.split {
		h = l.leftBotH - 2
	}
	if h < 1 {
		h = 1
	}
	return h
}

// clampInfoOffset 限制提交信息滚动。
func (a *App) clampInfoOffset() {
	max := a.infoLineCount() - a.infoViewHeight()
	if max < 0 {
		max = 0
	}
	if a.infoOff > max {
		a.infoOff = max
	}
	if a.infoOff < 0 {
		a.infoOff = 0
	}
}

// infoLineCount 返回提交信息面板（按当前宽度换行后）的行数。
func (a *App) infoLineCount() int {
	w := a.panelLayout().leftW
	if w < 10 {
		w = 10
	}
	return len(a.infoLines(w))
}

// branchViewHeight 是左侧分支区可显示行数（宽窄屏分别取分支面板高度）。
func (a *App) branchViewHeight() int {
	l := a.panelLayout()
	h := l.bodyH - 2
	if l.split {
		h = l.leftTopH - 2
	}
	if h < 1 {
		h = 1
	}
	return h
}

// changesViewHeight 是左下改动区可显示行数。
func (a *App) changesViewHeight() int {
	l := a.panelLayout()
	h := l.bodyH - 2
	if l.split {
		h = l.leftMidH - 2
	}
	if h < 1 {
		h = 1
	}
	return h
}

// selectedChangeRow 返回改动区选中行。
func (a *App) selectedChangeRow() *fileRow {
	if a.changeSel < 0 || a.changeSel >= len(a.changesRows) {
		return nil
	}
	return &a.changesRows[a.changeSel]
}

// selectedChangeNode 返回改动区选中节点。
func (a *App) selectedChangeNode() *fileNode {
	row := a.selectedChangeRow()
	if row == nil {
		return nil
	}
	return row.node
}

// selectedChangeEntry 返回改动区选中文件条目。
func (a *App) selectedChangeEntry() *fileEntry {
	n := a.selectedChangeNode()
	if n == nil || n.dir || len(n.entries) == 0 {
		return nil
	}
	for i := range n.entries {
		if !n.entries[i].staged {
			return &n.entries[i]
		}
	}
	return &n.entries[0]
}

// ensureChangeVisible 修正改动区滚动。
func (a *App) ensureChangeVisible() {
	h := a.changesViewHeight()
	if a.changeSel >= len(a.changesRows) {
		a.changeSel = len(a.changesRows) - 1
	}
	if a.changeSel < 0 {
		a.changeSel = 0
	}
	if a.changeSel < a.changeOff {
		a.changeOff = a.changeSel
	}
	if a.changeSel >= a.changeOff+h {
		a.changeOff = a.changeSel - h + 1
	}
	if a.changeOff < 0 {
		a.changeOff = 0
	}
}

// toggleChangeDir 展开/折叠改动区目录。
func (a *App) toggleChangeDir(n *fileNode) {
	if n == nil || !n.dir {
		return
	}
	cur, seen := a.changesExpanded[n.path]
	if !seen {
		cur = true
	}
	a.changesExpanded[n.path] = !cur
	a.changesTree, a.changesRows = buildFileTreeDefault(a.changes, a.changesExpanded, true)
	for i, r := range a.changesRows {
		if r.node.path == n.path {
			a.changeSel = i
			break
		}
	}
	a.ensureChangeVisible()
}

// restoreFileSelection 按当前选中节点恢复位置。
func (a *App) restoreFileSelection() {
	selPath, off := "", a.fileOff
	if n := a.selectedFileNode(); n != nil {
		selPath = n.path
	}
	a.restoreFileSelectionAt(selPath, off)
}

func (a *App) restoreFileSelectionAt(selPath string, off int) {
	if len(a.fileRows) == 0 {
		a.fileSel, a.fileOff = 0, 0
		return
	}
	a.fileSel = 0
	found := false
	if selPath != "" {
		for i, r := range a.fileRows {
			if r.node.path == selPath {
				a.fileSel = i
				found = true
				break
			}
		}
	}
	if !found {
		for i, r := range a.fileRows {
			if !r.node.dir {
				a.fileSel = i
				break
			}
		}
	}
	maxOff := len(a.fileRows) - a.fileViewHeight()
	if maxOff < 0 {
		maxOff = 0
	}
	a.fileOff = maxInt(0, minInt(off, maxOff))
	a.ensureFileVisible()
}

// toggleFileDir 展开/折叠目录。
func (a *App) toggleFileDir(n *fileNode) {
	if n == nil || !n.dir {
		return
	}
	cur, seen := a.expanded[n.path]
	if !seen {
		cur = true
	}
	a.expanded[n.path] = !cur
	a.rebuildFileRows()
	for i, r := range a.fileRows {
		if r.node.path == n.path {
			a.fileSel = i
			break
		}
	}
	a.ensureFileVisible()
}

// panelLayout 计算布局：左侧分支区（全高）+ 右侧提交区/固定抽屉。
type panelLayout struct {
	split     bool
	leftW     int
	rightW    int
	bodyH     int
	projH     int
	leftTopH  int
	leftMidH  int
	leftBotH  int
	rightTopH int
	rightBotH int
	drawerH   int
	cmdH      int
}

func (a *App) panelLayout() panelLayout {
	bodyH := a.bodyHeight()
	if a.width < 100 {
		return panelLayout{split: false, leftW: a.width, bodyH: bodyH}
	}
	lw := a.leftW
	if lw <= 0 {
		lw = a.width / 4
	}
	if lw < 22 {
		lw = 22
	}
	maxW := a.width - 40
	if maxW < 22 {
		maxW = 22
	}
	if lw > maxW {
		lw = maxW
	}
	clampTop := func(v int) int {
		if v <= 0 {
			v = bodyH * 55 / 100
		}
		if v < 6 {
			v = 6
		}
		if v > bodyH-8 {
			v = bodyH - 8
		}
		if v < 3 {
			v = 3
		}
		return v
	}
	top := clampTop(a.rightTopH)
	bot := bodyH - top
	cmdH := 0
	if len(a.commands) > 0 {
		cmdH = a.cmdH
		if cmdH <= 0 {
			cmdH = 6
			if bot < 12 {
				cmdH = 3
			}
		}
		if cmdH > bot-5 {
			cmdH = bot - 5
		}
		if cmdH < 2 {
			cmdH = 2
		}
	}
	drawerH := bot - 1 - cmdH
	if cmdH > 0 {
		drawerH--
	}
	if drawerH < 3 {
		drawerH = 3
	}
	// 左上角项目区：项目 + 展开的 worktree；支持手动拖动高度
	projH := a.projectH
	if projH <= 0 {
		projH = len(a.projectRows()) + 2
		if projH < 3 {
			projH = 3
		}
		if projH > 10 {
			projH = 10
		}
	}
	if projH > bodyH-10 {
		projH = bodyH - 10
	}
	if projH < 3 {
		projH = 3
	}
	avail := bodyH - projH - 1
	// 左侧分支 + 提交信息；提交信息默认高度与文件区（抽屉）一致
	ltop := a.leftTopH
	if ltop <= 0 {
		ltop = avail - drawerH - 1
	}
	if ltop < 4 {
		ltop = 4
	}
	if ltop > avail-4 {
		ltop = avail - 4
	}
	linfo := avail - ltop - 1
	if linfo < 4 {
		linfo = 4
		ltop = avail - linfo - 1
	}
	return panelLayout{
		split:     true,
		leftW:     lw,
		rightW:    a.width - lw - 1,
		bodyH:     bodyH,
		projH:     projH,
		leftTopH:  ltop,
		leftBotH:  linfo,
		rightTopH: top,
		rightBotH: bot,
		drawerH:   drawerH,
		cmdH:      cmdH,
	}
}

// commandViewHeight 是命令历史区可显示行数。
func (a *App) commandViewHeight() int {
	l := a.panelLayout()
	h := l.bodyH - 2
	if l.split && l.cmdH > 0 {
		h = l.cmdH - 2
	}
	if h < 1 {
		h = 1
	}
	return h
}

// clampCmdOffset 限制命令历史滚动。
func (a *App) clampCmdOffset() {
	avail := a.commandViewHeight()
	max := len(a.commands) - avail
	if max < 0 {
		max = 0
	}
	if a.cmdOffset > max {
		a.cmdOffset = max
	}
	if a.cmdOffset < 0 {
		a.cmdOffset = 0
	}
}

// SetLeftWidth 设置分支区宽度（来自配置）。
func (a *App) SetLeftWidth(w int) { a.leftW = w }

// SetDrawerLeft 设置抽屉文件列宽度（来自配置）。
func (a *App) SetDrawerLeft(w int) { a.drawerLeftW = w }

// SetRightTop 设置提交区高度（来自配置）。
func (a *App) SetRightTop(h int) { a.rightTopH = h }

// SetLeftTop 设置左侧分支区高度（来自配置）。
func (a *App) SetLeftTop(h int) { a.leftTopH = h }

// SetLeftMid 设置左侧改动区高度（来自配置）。
func (a *App) SetLeftMid(h int) { a.leftMidH = h }

// LeftWidth 返回当前分支区宽度（供保存配置）。
func (a *App) LeftWidth() int { return a.panelLayout().leftW }

// commitViewHeight 是提交区可显示行数（去掉面板标题）。
func (a *App) commitViewHeight() int {
	l := a.panelLayout()
	h := l.bodyH - 2
	if l.split {
		h = l.rightTopH - 2
	}
	if h < 1 {
		h = 1
	}
	return h
}

// drawerHeight 是抽屉总高度（含双列标题行）。
func (a *App) drawerHeight() int {
	if a.panelLayout().split {
		h := a.panelLayout().drawerH
		if h < 3 {
			h = 3
		}
		return h
	}
	h := a.panelLayout().bodyH - 7
	if h < 3 {
		h = 3
	}
	return h
}

// drawerBodyHeight 是抽屉内容行数（去掉标题行）。
func (a *App) drawerBodyHeight() int {
	h := a.drawerHeight() - 2
	if h < 1 {
		h = 1
	}
	return h
}

// fileViewHeight 是文件列可显示行数。
func (a *App) fileViewHeight() int { return a.drawerBodyHeight() }

// contentViewHeight 是内容列可显示行数。
func (a *App) contentViewHeight() int { return a.drawerBodyHeight() }

// nextPane 循环切换焦点。
func (a *App) nextPane(delta int) {
	order := []Pane{PaneProjects, PaneBranches, PaneInfo, PaneCommits, PaneFiles, PaneContent, PaneLog}
	idx := -1
	for i, p := range order {
		if p == a.pane {
			idx = i
			break
		}
	}
	for i := 0; i < len(order); i++ {
		if idx < 0 {
			idx = 0
		} else {
			idx = (idx + delta + len(order)) % len(order)
		}
		p := order[idx]
		if p == PaneLog && len(a.commands) == 0 {
			continue
		}
		if p == PaneProjects && !a.panelLayout().split {
			continue
		}
		a.pane = p
		break
	}
	a.ensurePaneVisible()
}

// altFocus 处理 Alt+方向键的区域切换。
// 上/下按空间行移动；文件与内容同排时用左/右切换，日志区在其下方。
func (a *App) altFocus(dir string) {
	if !a.panelLayout().split {
		return
	}
	hasLog := len(a.commands) > 0
	switch dir {
	case "up":
		switch a.pane {
		case PaneInfo:
			a.pane = PaneBranches
		case PaneBranches:
			a.pane = PaneProjects
		case PaneFiles, PaneContent:
			a.pane = PaneCommits
		case PaneLog:
			a.pane = PaneFiles
		}
	case "down":
		switch a.pane {
		case PaneProjects:
			a.pane = PaneBranches
		case PaneBranches:
			a.pane = PaneInfo
		case PaneCommits:
			a.pane = PaneFiles
		case PaneFiles, PaneContent:
			if hasLog {
				a.pane = PaneLog
			}
		}
	case "left":
		switch a.pane {
		case PaneCommits:
			a.pane = PaneBranches
		case PaneFiles:
			a.pane = PaneInfo
		case PaneContent:
			a.pane = PaneFiles
		case PaneLog:
			a.pane = PaneInfo
		}
	case "right":
		switch a.pane {
		case PaneProjects, PaneBranches:
			a.pane = PaneCommits
		case PaneInfo:
			a.pane = PaneFiles
		case PaneFiles:
			a.pane = PaneContent
		}
	}
	a.ensurePaneVisible()
}

// ensurePaneVisible 让各区的选中项滚入可视范围。
func (a *App) ensurePaneVisible() {
	switch a.pane {
	case PaneProjects:
		a.ensureProjectVisible()
	case PaneBranches:
		a.ensureSideVisible()
	case PaneChanges:
		a.ensureChangeVisible()
	case PaneCommits:
		a.ensureGraphVisible()
	case PaneFiles:
		a.ensureFileVisible()
	case PaneContent:
		a.clampContentScroll()
	case PaneLog:
		a.clampCmdOffset()
	case PaneInfo:
		a.clampInfoOffset()
	}
}

// projectViewHeight 返回项目区可视行数。
func (a *App) projectViewHeight() int {
	h := a.panelLayout().projH - 2
	if h < 1 {
		h = 1
	}
	return h
}

// ensureProjectVisible 修正项目区滚动。
func (a *App) ensureProjectVisible() {
	rows := a.projectRows()
	if len(rows) == 0 {
		a.projectSel, a.projectOff = 0, 0
		return
	}
	if a.projectSel >= len(rows) {
		a.projectSel = len(rows) - 1
	}
	if a.projectSel < 0 {
		a.projectSel = 0
	}
	h := a.projectViewHeight()
	if a.projectSel < a.projectOff {
		a.projectOff = a.projectSel
	}
	if a.projectSel >= a.projectOff+h {
		a.projectOff = a.projectSel - h + 1
	}
	if a.projectOff < 0 {
		a.projectOff = 0
	}
}

// ensureFileVisible 修正文件区滚动。
func (a *App) ensureFileVisible() {
	h := a.fileViewHeight()
	if a.fileSel >= len(a.fileRows) {
		a.fileSel = len(a.fileRows) - 1
	}
	if a.fileSel < 0 {
		a.fileSel = 0
	}
	if a.fileSel < a.fileOff {
		a.fileOff = a.fileSel
	}
	if a.fileSel >= a.fileOff+h {
		a.fileOff = a.fileSel - h + 1
	}
	if a.fileOff < 0 {
		a.fileOff = 0
	}
}

// clampContentScroll 限制内容区滚动。
func (a *App) clampContentScroll() {
	max := len(a.content.lines) - a.contentViewHeight()
	if max < 0 {
		max = 0
	}
	if a.content.offset > max {
		a.content.offset = max
	}
	if a.content.offset < 0 {
		a.content.offset = 0
	}
}

// selectedSideItem 返回当前侧栏条目。
func (a *App) selectedSideItem() *sideItem {
	if a.sideSel < 0 || a.sideSel >= len(a.sideItems) {
		return nil
	}
	return &a.sideItems[a.sideSel]
}

// moveSidebar 移动侧栏选中项并同步 key。
func (a *App) moveSidebar(delta int) {
	if len(a.sideItems) == 0 {
		return
	}
	a.sideSel += delta
	if a.sideSel < 0 {
		a.sideSel = 0
	}
	if a.sideSel >= len(a.sideItems) {
		a.sideSel = len(a.sideItems) - 1
	}
	a.sideSelKey = a.sideItems[a.sideSel].key
	a.ensureSideVisible()
}

// jumpSection 在侧栏中跳到上一个/下一个非空分组。
func (a *App) jumpSection(dir int) {
	if len(a.sideItems) == 0 {
		return
	}
	sections := []string{}
	for _, it := range a.sideItems {
		if len(sections) == 0 || sections[len(sections)-1] != it.section {
			sections = append(sections, it.section)
		}
	}
	cur := ""
	if it := a.selectedSideItem(); it != nil {
		cur = it.section
	}
	idx := 0
	for i, s := range sections {
		if s == cur {
			idx = i
		}
	}
	idx += dir
	if idx < 0 {
		idx = 0
	}
	if idx >= len(sections) {
		idx = len(sections) - 1
	}
	target := sections[idx]
	for i, it := range a.sideItems {
		if it.section == target {
			a.sideSel = i
			break
		}
	}
	a.sideSelKey = a.sideItems[a.sideSel].key
	a.ensureSideVisible()
}

// sidebarLines 生成侧栏显示行（含分组标题）。
type sideLine struct {
	text    string
	header  bool
	itemIdx int    // -1 表示标题
	section string // 所属分组（标题行也带）
}

func (a *App) sidebarLines() []sideLine {
	var lines []sideLine
	for _, sec := range []string{"branches", "remote", "tags", "stash"} {
		n := a.sectionCount(sec)
		if n == 0 && sec != "branches" {
			continue
		}
		collapsed := a.collapsed[sec]
		arrow := "▾"
		if collapsed {
			arrow = "▸"
		}
		lines = append(lines, sideLine{text: arrow + " " + sectionTitle(sec, n), header: true, itemIdx: -1, section: sec})
		if collapsed {
			continue
		}
		for i, it := range a.sideItems {
			if it.section == sec {
				lines = append(lines, sideLine{text: it.label, itemIdx: i, section: sec})
			}
		}
	}
	if len(lines) == 0 {
		lines = append(lines, sideLine{text: i18n.S("  （无引用）", "  (no refs)"), header: true, itemIdx: -1, section: "branches"})
	}
	return lines
}

// toggleSection 折叠/展开分组。
func (a *App) toggleSection(section string) {
	if section == "" {
		return
	}
	a.collapsed[section] = !a.collapsed[section]
	// 折叠后若选中项被隐藏，移动到可见项
	if a.collapsed[section] {
		if it := a.selectedSideItem(); it != nil && it.section == section {
			lines := a.sidebarLines()
			for _, ln := range lines {
				if ln.itemIdx >= 0 {
					a.sideSel = ln.itemIdx
					a.sideSelKey = a.sideItems[a.sideSel].key
					break
				}
			}
		}
	}
	a.ensureSideVisible()
}

func (a *App) sectionCount(section string) int {
	n := 0
	for _, it := range a.sideItems {
		if it.section == section {
			n++
		}
	}
	return n
}

func sectionTitle(section string, n int) string {
	name := strings.ToUpper(section)
	switch section {
	case "branches":
		name = "LOCAL"
	case "remote":
		name = "REMOTE"
	case "tags":
		name = "TAGS"
	case "stash":
		name = "STASH"
	case "projects":
		name = i18n.S("项目", "PROJECTS")
	case "changes":
		name = i18n.S("CHANGES", "CHANGES")
	}
	if n > 0 {
		return fmt.Sprintf("%s (%d)", name, n)
	}
	return name
}

func (a *App) ensureSideVisible() {
	lines := a.sidebarLines()
	if len(lines) == 0 {
		return
	}
	target := 0
	for i, ln := range lines {
		if ln.itemIdx == a.sideSel {
			target = i
			break
		}
	}
	h := a.branchViewHeight()
	if target < a.sideOff {
		a.sideOff = target
	}
	if target >= a.sideOff+h {
		a.sideOff = target - h + 1
	}
	if a.sideOff < 0 {
		a.sideOff = 0
	}
}

// refsByTarget 建立提交 -> 引用 的映射。
func (a *App) refsAt(hash string) []git.Ref {
	var out []git.Ref
	for _, r := range a.refs {
		if r.Target == hash {
			out = append(out, r)
		}
	}
	// 本地分支优先
	sort.SliceStable(out, func(i, j int) bool { return refOrder(out[i].Kind) < refOrder(out[j].Kind) })
	return out
}

func refOrder(kind string) int {
	switch kind {
	case git.RefLocal:
		return 0
	case git.RefRemote:
		return 1
	case git.RefTag:
		return 2
	case git.RefStash:
		return 3
	}
	return 4
}

// refBadge 是提交行上的一个引用徽标；本地分支与同名远端合并为 branch|remote。
type refBadge struct {
	label string
	kind  string
	name  string
}

// refBadges 返回提交的徽标，顺序为本地分支、远程分支、tag。
func (a *App) refBadges(hash string) []refBadge {
	var locals, remotes, tags []git.Ref
	for _, r := range a.refsAt(hash) {
		switch r.Kind {
		case git.RefLocal:
			locals = append(locals, r)
		case git.RefRemote:
			remotes = append(remotes, r)
		case git.RefTag:
			tags = append(tags, r)
		}
	}
	sort.SliceStable(locals, func(i, j int) bool { return locals[i].Name < locals[j].Name })
	sort.SliceStable(remotes, func(i, j int) bool { return remotes[i].Name < remotes[j].Name })
	sort.SliceStable(tags, func(i, j int) bool { return tags[i].Name < tags[j].Name })
	merged := make([]bool, len(remotes))
	out := make([]refBadge, 0, len(locals)+len(remotes)+len(tags))
	for _, l := range locals {
		label := l.Name
		if l.Head {
			label = "*" + label
		}
		parts := []string{label}
		for i, r := range remotes {
			if strings.HasSuffix(r.Name, "/"+l.Name) {
				if remote := strings.TrimSuffix(r.Name, "/"+l.Name); remote != "" {
					parts = append(parts, remote)
					merged[i] = true
				}
			}
		}
		out = append(out, refBadge{label: strings.Join(parts, "|"), kind: git.RefLocal, name: l.Name})
	}
	for i, r := range remotes {
		if !merged[i] {
			out = append(out, refBadge{label: r.Name, kind: git.RefRemote, name: r.Name})
		}
	}
	for _, t := range tags {
		out = append(out, refBadge{label: t.Name, kind: git.RefTag, name: t.Name})
	}
	return out
}

// logLine 追加一条命令日志。
func (a *App) logLine(s string) {
	for _, line := range strings.Split(strings.TrimRight(s, "\n"), "\n") {
		if strings.TrimSpace(line) == "" {
			continue
		}
		a.logs = append(a.logs, sanitize.Secrets(sanitize.Text(line)))
	}
	if len(a.logs) > 500 {
		a.logs = a.logs[len(a.logs)-500:]
	}
}

// setToast 设置状态栏消息；世代号用于让旧定时器失效。
func (a *App) setToast(s string, isErr bool) {
	a.toast = sanitize.Secrets(sanitize.Line(s))
	a.toastErr = isErr
	a.toastGen++
}

// textInput 创建单行输入框。
func newTextInput(placeholder string) textinput.Model {
	ti := textinput.New()
	ti.Placeholder = placeholder
	ti.Focus()
	ti.CharLimit = 200
	ti.Width = 50
	return ti
}

// newTextArea 创建多行输入框。
func newTextArea(placeholder string) textarea.Model {
	ta := textarea.New()
	ta.Placeholder = placeholder
	ta.Focus()
	ta.CharLimit = 8000
	ta.SetWidth(60)
	ta.SetHeight(6)
	return ta
}

// invalidateView 清空过滤/搜索布局缓存。
func (a *App) invalidateView() {
	a.viewCache = nil
	a.graphCache = nil
}

func contextTimeout(ctx context.Context) context.Context { return ctx }
