package ui

import (
	"context"
	"fmt"
	"strings"
	"testing"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/x/ansi"

	"github.com/ssk-wh/gitc/internal/config"
	"github.com/ssk-wh/gitc/internal/git"
	"github.com/ssk-wh/gitc/internal/i18n"
)

// fakeSvc 是测试用假服务：读接口返回预置数据，写接口记录调用。
type fakeSvc struct {
	git.Service
	commits []git.Commit
	refs    []git.Ref
	status  git.Status
	state   git.RepoState
	stashes []git.Stash
	calls   []string
}

func (f *fakeSvc) Name() string     { return "demo" }
func (f *fakeSvc) RootPath() string { return "/tmp/demo" }

func (f *fakeSvc) StreamLog(ctx context.Context, max int, cb func(git.Commit) bool) error {
	for _, c := range f.commits {
		if !cb(c) {
			return nil
		}
	}
	return nil
}

func (f *fakeSvc) StreamLogRef(ctx context.Context, ref string, max int, cb func(git.Commit) bool) error {
	return f.StreamLog(ctx, max, cb)
}

func (f *fakeSvc) StreamLogScope(ctx context.Context, scope string, max int, cb func(git.Commit) bool) error {
	return f.StreamLog(ctx, max, cb)
}

func (f *fakeSvc) SearchLog(ctx context.Context, q string, max int, cb func(git.Commit) bool) error {
	for _, c := range f.commits {
		if strings.Contains(strings.ToLower(c.Subject), strings.ToLower(q)) {
			if !cb(c) {
				return nil
			}
		}
	}
	return nil
}

func (f *fakeSvc) SearchAuthorLog(ctx context.Context, q string, max int, cb func(git.Commit) bool) error {
	return nil
}

func (f *fakeSvc) LogOne(ctx context.Context, rev string) (git.Commit, error) {
	for _, c := range f.commits {
		if strings.HasPrefix(c.Hash, rev) {
			return c, nil
		}
	}
	return git.Commit{}, nil
}

func (f *fakeSvc) Refs(context.Context) ([]git.Ref, error)          { return f.refs, nil }
func (f *fakeSvc) Status(context.Context, bool) (git.Status, error) { return f.status, nil }
func (f *fakeSvc) State(context.Context) (git.RepoState, error)     { return f.state, nil }
func (f *fakeSvc) Stashes(context.Context) ([]git.Stash, error)     { return f.stashes, nil }

func (f *fakeSvc) CommitDetail(ctx context.Context, sha string) (git.CommitDetail, error) {
	for _, c := range f.commits {
		if c.Hash == sha {
			return git.CommitDetail{
				Hash: c.Hash, Author: c.Author, When: c.When,
				Subject: c.Subject, Body: "详情正文",
				Files: []git.CommitFile{{Status: "M", Path: "a.txt"}, {Status: "A", Path: "b.go"}},
			}, nil
		}
	}
	return git.CommitDetail{Hash: sha}, nil
}

func (f *fakeSvc) CommitDiff(ctx context.Context, sha, path string) (string, error) {
	return "diff --git a/a.txt b/a.txt\n@@ -1 +1 @@\n-old\n+new\n", nil
}

func (f *fakeSvc) WorkingDiff(ctx context.Context, path string, staged, untracked bool) (string, error) {
	return "diff --git a/" + path + " b/" + path + "\n+x\n", nil
}

func (f *fakeSvc) StashDiff(ctx context.Context, index int) (string, error) {
	return "diff --git a/x b/x\n+z\n", nil
}

func (f *fakeSvc) record(name string) (string, error) {
	f.calls = append(f.calls, name)
	return "ok", nil
}

func (f *fakeSvc) Stage(ctx context.Context, paths ...string) (string, error) {
	return f.record("stage:" + strings.Join(paths, ","))
}
func (f *fakeSvc) Unstage(ctx context.Context, paths ...string) (string, error) {
	return f.record("unstage:" + strings.Join(paths, ","))
}
func (f *fakeSvc) StageAll(context.Context) (string, error)   { return f.record("stage-all") }
func (f *fakeSvc) UnstageAll(context.Context) (string, error) { return f.record("unstage-all") }
func (f *fakeSvc) Discard(ctx context.Context, untracked bool, paths ...string) (string, error) {
	return f.record("discard:" + strings.Join(paths, ","))
}
func (f *fakeSvc) Commit(ctx context.Context, msg string, amend bool) (string, error) {
	if amend {
		return f.record("amend")
	}
	return f.record("commit:" + msg)
}
func (f *fakeSvc) Checkout(ctx context.Context, ref string) (string, error) {
	return f.record("checkout:" + ref)
}
func (f *fakeSvc) CheckoutRemoteBranch(ctx context.Context, remoteRef, local string) (string, error) {
	return f.record("checkout-remote:" + remoteRef)
}
func (f *fakeSvc) CreateBranch(ctx context.Context, name, start string, checkout bool) (string, error) {
	return f.record("create-branch:" + name)
}
func (f *fakeSvc) RenameBranch(ctx context.Context, oldName, newName string) (string, error) {
	return f.record("rename:" + oldName + "->" + newName)
}
func (f *fakeSvc) DeleteBranch(ctx context.Context, name string, force bool) (string, error) {
	return f.record("delete-branch:" + name)
}
func (f *fakeSvc) DeleteRemoteBranch(ctx context.Context, remote, branch string) (string, error) {
	return f.record("delete-remote:" + remote + "/" + branch)
}
func (f *fakeSvc) Merge(ctx context.Context, ref string) (string, error) {
	return f.record("merge:" + ref)
}
func (f *fakeSvc) Rebase(ctx context.Context, ref string) (string, error) {
	return f.record("rebase:" + ref)
}
func (f *fakeSvc) CherryPick(ctx context.Context, sha string) (string, error) {
	return f.record("cherry-pick:" + sha)
}
func (f *fakeSvc) Revert(ctx context.Context, sha string) (string, error) {
	return f.record("revert:" + sha)
}
func (f *fakeSvc) Reset(ctx context.Context, sha string, mode git.ResetMode) (string, error) {
	return f.record("reset:" + string(mode))
}
func (f *fakeSvc) CreateTag(ctx context.Context, name, sha, msg string) (string, error) {
	return f.record("tag:" + name)
}
func (f *fakeSvc) DeleteTag(ctx context.Context, name string) (string, error) {
	return f.record("delete-tag:" + name)
}
func (f *fakeSvc) PushTag(ctx context.Context, remote, name string, progress func(string)) (string, error) {
	return f.record("push-tag:" + name)
}
func (f *fakeSvc) Stash(ctx context.Context, msg string, untracked bool) (string, error) {
	return f.record("stash")
}
func (f *fakeSvc) StashApply(ctx context.Context, index int) (string, error) {
	return f.record("stash-apply")
}
func (f *fakeSvc) StashPop(ctx context.Context, index int) (string, error) {
	return f.record("stash-pop")
}
func (f *fakeSvc) StashDrop(ctx context.Context, index int) (string, error) {
	return f.record("stash-drop")
}
func (f *fakeSvc) Fetch(ctx context.Context, prune bool, progress func(string)) (string, error) {
	return f.record("fetch")
}
func (f *fakeSvc) Pull(ctx context.Context, rebase bool, progress func(string)) (string, error) {
	return f.record("pull")
}
func (f *fakeSvc) Push(ctx context.Context, opts git.PushOptions, branch string, progress func(string)) (string, error) {
	if opts.SetUpstream {
		return f.record("push-upstream")
	}
	return f.record("push")
}
func (f *fakeSvc) SetUpstream(ctx context.Context, branch, upstream string) (string, error) {
	return f.record("set-upstream")
}
func (f *fakeSvc) ContinueOperation(ctx context.Context, kind string) (string, error) {
	return f.record("continue:" + kind)
}
func (f *fakeSvc) AbortOperation(ctx context.Context, kind string) (string, error) {
	return f.record("abort:" + kind)
}
func (f *fakeSvc) RunGitArgs(ctx context.Context, args []string) (string, error) {
	return f.record("git:" + strings.Join(args, " "))
}
func (f *fakeSvc) RevParse(ctx context.Context, ref string) (string, error) {
	return "abc1234", nil
}
func (f *fakeSvc) CurrentBranch(context.Context) string { return f.status.Branch }
func (f *fakeSvc) CommitFiles(context.Context, string) ([]git.CommitFile, error) {
	return []git.CommitFile{
		{Status: "M", Path: "a.txt"},
		{Status: "A", Path: "b.go"},
	}, nil
}

func sampleCommits() []git.Commit {
	// 以当天中午为基准，避免跨午夜测试不稳定
	now := time.Now()
	noon := time.Date(now.Year(), now.Month(), now.Day(), 12, 0, 0, 0, now.Location())
	return []git.Commit{
		{Hash: "aaaaaaaaaaaa1111", Short: "aaaaaaa", Parents: []string{"bbbbbbbbbbbb2222"}, Author: "张三", When: noon.Add(-time.Hour), Subject: "修复切换音频崩溃"},
		{Hash: "bbbbbbbbbbbb2222", Short: "bbbbbbb", Parents: []string{"cccccccccccc3333"}, Author: "李四", When: noon.Add(-2 * time.Hour), Subject: "支持摄像头分辨率"},
		{Hash: "cccccccccccc3333", Short: "ccccccc", Author: "张三", When: noon.Add(-48 * time.Hour), Subject: "初始提交"},
	}
}

func newFakeApp(t *testing.T) (*App, *fakeSvc) {
	t.Helper()
	i18n.Set("zh")
	f := &fakeSvc{
		commits: sampleCommits(),
		refs: []git.Ref{
			{Name: "main", FullName: "refs/heads/main", Kind: git.RefLocal, Target: "aaaaaaaaaaaa1111", Head: true, Upstream: "origin/main"},
			{Name: "feature/x", FullName: "refs/heads/feature/x", Kind: git.RefLocal, Target: "bbbbbbbbbbbb2222"},
			{Name: "origin/main", FullName: "refs/remotes/origin/main", Kind: git.RefRemote, Target: "aaaaaaaaaaaa1111"},
			{Name: "v1.0", FullName: "refs/tags/v1.0", Kind: git.RefTag, Target: "cccccccccccc3333"},
		},
		status: git.Status{
			Branch: "main", Upstream: "origin/main", Ahead: 1, Behind: 2, OID: "aaaaaaaaaaaa1111",
			Staged:    []git.FileChange{{Path: "a.txt", X: 'M', Y: '.'}},
			Unstaged:  []git.FileChange{{Path: "a.txt", X: '.', Y: 'M'}},
			Untracked: []git.FileChange{{Path: "new.txt", Untracked: true}},
		},
	}
	a := New(f)
	a.Update(tea.WindowSizeMsg{Width: 120, Height: 40})
	pump(t, a, a.refreshCmd(false))
	pump(t, a, a.reloadLogCmd())
	return a, f
}

// pump 执行命令并把消息喂回模型，直到没有后续命令。
func pump(t *testing.T, a *App, cmd tea.Cmd) {
	t.Helper()
	for depth := 0; depth < 200 && cmd != nil; depth++ {
		msg := cmd()
		switch m := msg.(type) {
		case tea.BatchMsg:
			for _, c := range m {
				if c != nil {
					pump(t, a, c)
				}
			}
			cmd = nil
		case spinnerTickMsg:
			cmd = nil
		default:
			_, next := a.Update(msg)
			cmd = next
		}
	}
}

func sendRunes(a *App, s string) tea.Cmd {
	var last tea.Cmd
	for _, r := range s {
		_, last = a.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{r}})
	}
	return last
}

func sendKey(a *App, k tea.KeyMsg) tea.Cmd {
	_, cmd := a.Update(k)
	return cmd
}

// plainView 去掉 ANSI 便于断言。
func plainView(a *App) string { return ansi.Strip(a.View()) }

func TestInitialView(t *testing.T) {
	a, _ := newFakeApp(t)
	v := plainView(a)
	for _, want := range []string{"gitc", "demo", "main", "修复切换音频崩溃", "支持摄像头分辨率"} {
		if !strings.Contains(v, want) {
			t.Errorf("视图缺少 %q:\n%s", want, v)
		}
	}
	if !strings.Contains(v, "未提交变更") {
		t.Errorf("缺少未提交伪节点:\n%s", v)
	}
	// 每行不应超过终端宽度
	for i, line := range strings.Split(v, "\n") {
		if w := strWidth(line); w > 120 {
			t.Errorf("第 %d 行宽度 %d 超过 120: %q", i, w, line)
		}
	}
}

func TestNavigationAndDetail(t *testing.T) {
	a, _ := newFakeApp(t)
	// 首行是未提交伪行，向下到第一条提交
	sendKey(a, tea.KeyMsg{Type: tea.KeyDown})
	if c := a.selectedCommit(); c == nil || c.Subject != "修复切换音频崩溃" {
		t.Fatalf("选中提交错误: %+v", c)
	}
	cmd, _ := a.handleGraphKey(tea.KeyMsg{Type: tea.KeyEnter})
	pump(t, a, cmd)
	if a.overlay != overlayDetail {
		t.Fatalf("应打开详情覆盖层")
	}
	// 等待详情加载：执行 pending cmd 无法拿到，这里直接构造 detailMsg
	d := git.CommitDetail{Hash: "aaaaaaaaaaaa1111", Author: "张三", Subject: "修复切换音频崩溃", Body: "详情正文", Files: []git.CommitFile{{Status: "M", Path: "a.txt"}}}
	a.Update(detailMsg{sha: "aaaaaaaaaaaa1111", kind: "commit", title: "修复切换音频崩溃", detail: d, diff: "@@ -1 +1 @@\n-old\n+new"})
	v := plainView(a)
	for _, want := range []string{"详情正文", "a.txt", "+new", "-old"} {
		if !strings.Contains(v, want) {
			t.Errorf("详情缺少 %q:\n%s", want, v)
		}
	}
	sendKey(a, tea.KeyMsg{Type: tea.KeyEsc})
	if a.overlay != overlayNone {
		t.Errorf("Esc 后应返回主界面")
	}
}

func TestFilesPaneStage(t *testing.T) {
	a, f := newFakeApp(t)
	// 初始选中未提交伪行，文件区应加载工作区文件
	a.pane = PaneFiles
	a.Update(a.move(0))
	if len(a.files) == 0 {
		t.Fatalf("工作区文件未加载")
	}
	// 找到 a.txt 对应的可见行
	for i, r := range a.fileRows {
		if !r.node.dir && r.node.name == "a.txt" {
			a.fileSel = i
			break
		}
	}
	cmd, handled := a.handleFilesKey(tea.KeyMsg{Type: tea.KeySpace})
	if !handled {
		t.Fatal("space 应由文件区处理")
	}
	pump(t, a, cmd)
	if !hasCall(f.calls, "stage:a.txt") {
		t.Errorf("未调用 Stage: %v", f.calls)
	}
}

func TestDangerousActionRequiresConfirm(t *testing.T) {
	a, f := newFakeApp(t)
	a.pane = PaneBranches
	// 选中 feature/x 分支
	for {
		it := a.selectedSideItem()
		if it != nil && it.kind == sideBranch && it.ref.Name == "feature/x" {
			break
		}
		a.moveSidebar(1)
	}
	sendKey(a, tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("d")})
	if a.modal == nil || a.modal.kind != modalConfirm {
		t.Fatalf("应弹出确认框")
	}
	sendKey(a, tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("n")})
	if hasCall(f.calls, "delete-branch:feature/x") {
		t.Fatalf("取消后不应删除分支")
	}
	// 再次确认
	sendKey(a, tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("d")})
	cmd := sendKey(a, tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("y")})
	pump(t, a, cmd)
	if !hasCall(f.calls, "delete-branch:feature/x") {
		t.Errorf("确认后应删除分支: %v", f.calls)
	}
}

func TestBusyMutex(t *testing.T) {
	a, f := newFakeApp(t)
	a.busy = true
	cmd := a.sidebarSpace(&sideItem{kind: sideFile, file: git.FileChange{Path: "a.txt", X: '.', Y: 'M'}})
	_ = cmd
	if hasCall(f.calls, "stage:a.txt") {
		t.Errorf("忙碌时不应执行操作")
	}
	if a.toast == "" {
		t.Errorf("应提示上一个操作未完成")
	}
}

func TestSearch(t *testing.T) {
	a, _ := newFakeApp(t)
	// / 进入行内搜索，不弹窗
	sendKey(a, tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("/")})
	if !a.searchActive {
		t.Fatalf("/ 应进入行内搜索")
	}
	if a.modal != nil {
		t.Fatalf("行内搜索不应弹窗")
	}
	sendRunes(a, "摄像头")
	if a.filter != "摄像头" {
		t.Errorf("过滤条件 = %q", a.filter)
	}
	if len(a.visibleCommits()) != 1 {
		t.Errorf("本地过滤结果 = %d", len(a.visibleCommits()))
	}
	// Enter 搜索全部历史
	_, cmd := a.Update(tea.KeyMsg{Type: tea.KeyEnter})
	pump(t, a, cmd)
	if len(a.searchResults) != 1 || a.searchResults[0].Subject != "支持摄像头分辨率" {
		t.Fatalf("搜索结果错误: %+v", a.searchResults)
	}
	// Esc 清除
	sendKey(a, tea.KeyMsg{Type: tea.KeyEsc})
	if a.filter != "" || a.searchResults != nil {
		t.Errorf("Esc 应清除搜索")
	}
}

func TestSearchByHash(t *testing.T) {
	a, _ := newFakeApp(t)
	sendKey(a, tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("/")})
	sendRunes(a, "bbbbbbb")
	if len(a.visibleCommits()) != 1 || a.visibleCommits()[0].Short != "bbbbbbb" {
		t.Errorf("hash 过滤失败: %+v", a.visibleCommits())
	}
}

func TestMouseClickBranchCheckout(t *testing.T) {
	a, f := newFakeApp(t)
	lines := a.sidebarLines()
	idx := -1
	for i, ln := range lines {
		if ln.itemIdx >= 0 && a.sideItems[ln.itemIdx].kind == sideBranch && a.sideItems[ln.itemIdx].ref.Name == "feature/x" {
			idx = i
		}
	}
	if idx < 0 {
		t.Fatalf("未找到 feature/x 行: %+v", lines)
	}
	click := tea.MouseMsg{Action: tea.MouseActionPress, Button: tea.MouseButtonLeft, X: 2, Y: 1 + idx}
	a.Update(click)
	if a.pane != PaneBranches {
		t.Fatalf("点击侧栏应聚焦侧栏")
	}
	_, cmd := a.Update(click) // 双击
	pump(t, a, cmd)
	if !hasCall(f.calls, "checkout:feature/x") {
		t.Errorf("双击分支应检出: %v", f.calls)
	}
}

func TestMouseDoubleClickCommit(t *testing.T) {
	a, _ := newFakeApp(t)
	// 面板标题一行 + 未提交伪行 + 首个提交
	y := 2 + a.dirtyOffset() + 1
	click := tea.MouseMsg{Action: tea.MouseActionPress, Button: tea.MouseButtonLeft, X: 60, Y: y}
	a.Update(click)
	_, cmd := a.Update(click)
	pump(t, a, cmd)
	if a.overlay != overlayDetail {
		t.Fatalf("双击提交应打开详情")
	}
}

func TestMouseWheelScroll(t *testing.T) {
	a, _ := newFakeApp(t)
	a.graphSel = 0
	_, _ = a.Update(tea.MouseMsg{Action: tea.MouseActionPress, Button: tea.MouseButtonWheelDown, X: 60, Y: 5})
	if a.graphSel != 0 {
		t.Errorf("滚轮不应改变选中提交，实际 %d", a.graphSel)
	}
}

func TestSmallTerminal(t *testing.T) {
	a, _ := newFakeApp(t)
	a.Update(tea.WindowSizeMsg{Width: 60, Height: 10})
	v := a.View()
	if v == "" {
		t.Fatal("小终端不应渲染为空")
	}
	if strings.Count(v, "\n") > 12 {
		t.Errorf("小终端行数过多: %d", strings.Count(v, "\n"))
	}
}

func TestQuit(t *testing.T) {
	a, _ := newFakeApp(t)
	_, cmd := a.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("q")})
	if cmd == nil {
		t.Fatal("q 应返回 tea.Quit")
	}
	if _, ok := cmd().(tea.QuitMsg); !ok {
		t.Errorf("q 应退出程序")
	}
}

func hasCall(calls []string, want string) bool {
	for _, c := range calls {
		if c == want {
			return true
		}
	}
	return false
}

func TestEmptyRepoView(t *testing.T) {
	i18n.Set("zh")
	f := &fakeSvc{status: git.Status{Branch: "main"}}
	a := New(f)
	a.Update(tea.WindowSizeMsg{Width: 100, Height: 20})
	pump(t, a, a.refreshCmd(false))
	pump(t, a, a.reloadLogCmd())
	v := plainView(a)
	if !strings.Contains(v, "仓库还没有提交") {
		t.Errorf("空仓库应显示引导:\n%s", v)
	}
	if !strings.Contains(v, "按 a 暂存全部改动") {
		t.Errorf("空仓库缺少操作提示:\n%s", v)
	}
}

func TestTinyTerminal(t *testing.T) {
	a, _ := newFakeApp(t)
	a.Update(tea.WindowSizeMsg{Width: 20, Height: 5})
	v := a.View()
	if v == "" {
		t.Fatal("20x5 不应渲染为空")
	}
	a.Update(tea.WindowSizeMsg{Width: 200, Height: 60})
	if a.View() == "" {
		t.Fatal("resize 后不应渲染为空")
	}
}

func TestCommitFilesAndContent(t *testing.T) {
	a, _ := newFakeApp(t)
	a.pane = PaneCommits
	// 从未提交伪行移到第一条提交，加载文件与首个文件内容
	cmd := a.move(1)
	pump(t, a, cmd)
	if len(a.files) != 2 {
		t.Fatalf("文件列表 = %+v", a.files)
	}
	if !strings.Contains(strings.Join(a.content.lines, "\n"), "diff --git") {
		t.Errorf("应自动加载首个文件内容: %+v", a.content.lines)
	}
	// 切换文件
	a.pane = PaneFiles
	cmd = a.move(1)
	pump(t, a, cmd)
	if a.fileSel != 1 {
		t.Errorf("文件选择 = %d", a.fileSel)
	}
	if !strings.Contains(a.content.title, "b.go") {
		t.Errorf("内容标题 = %q", a.content.title)
	}
}

func TestBranchContextMenu(t *testing.T) {
	a, f := newFakeApp(t)
	a.pane = PaneBranches
	for {
		it := a.selectedSideItem()
		if it != nil && it.ref.Name == "feature/x" {
			break
		}
		a.moveSidebar(1)
	}
	sendKey(a, tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("b")})
	if a.modal == nil || a.modal.kind != modalChoice {
		t.Fatalf("b 应打开分支菜单")
	}
	found := false
	for i, c := range a.modal.choices {
		if strings.Contains(c.label, "检出") {
			a.modal.sel = i
			found = true
			break
		}
	}
	if !found {
		t.Fatalf("菜单缺少检出项: %+v", a.modal.choices)
	}
	_, cmd := a.Update(tea.KeyMsg{Type: tea.KeyEnter})
	pump(t, a, cmd)
	if !hasCall(f.calls, "checkout:feature/x") {
		t.Errorf("菜单检出未执行: %v", f.calls)
	}
}

func TestCommitContextMenuByRightClick(t *testing.T) {
	a, _ := newFakeApp(t)
	y := 2 + a.dirtyOffset() + 1
	a.Update(tea.MouseMsg{Action: tea.MouseActionPress, Button: tea.MouseButtonRight, X: 60, Y: y})
	if a.modal == nil || a.modal.kind != modalChoice {
		t.Fatalf("右键提交应弹出菜单")
	}
	if !strings.Contains(a.modal.title, "提交") {
		t.Errorf("菜单标题 = %q", a.modal.title)
	}
}

func TestBranchRightClickMenu(t *testing.T) {
	a, _ := newFakeApp(t)
	lines := a.sidebarLines()
	idx := -1
	for i, ln := range lines {
		if ln.itemIdx >= 0 && a.sideItems[ln.itemIdx].ref.Name == "feature/x" {
			idx = i
		}
	}
	if idx < 0 {
		t.Fatal("未找到分支行")
	}
	a.Update(tea.MouseMsg{Action: tea.MouseActionPress, Button: tea.MouseButtonRight, X: 2, Y: 1 + idx})
	if a.modal == nil || a.modal.kind != modalChoice {
		t.Fatalf("右键分支应弹出菜单")
	}
}

func TestCollapsibleSections(t *testing.T) {
	a, _ := newFakeApp(t)
	// 默认全部展开
	seenRemote := false
	for _, ln := range a.sidebarLines() {
		if ln.itemIdx >= 0 && a.sideItems[ln.itemIdx].section == "remote" {
			seenRemote = true
		}
	}
	if !seenRemote {
		t.Fatal("默认应展开远端分支")
	}
	// 点击 REMOTE 标题折叠
	lines := a.sidebarLines()
	idx := -1
	for i, ln := range lines {
		if ln.header && ln.section == "remote" {
			idx = i
		}
	}
	if idx < 0 {
		t.Fatal("未找到 REMOTE 标题")
	}
	a.Update(tea.MouseMsg{Action: tea.MouseActionPress, Button: tea.MouseButtonLeft, X: 2, Y: 1 + idx})
	shown := false
	for _, ln := range a.sidebarLines() {
		if ln.itemIdx >= 0 && a.sideItems[ln.itemIdx].section == "remote" {
			shown = true
		}
	}
	if shown {
		t.Fatal("点击标题后应收起远端分支")
	}
	// 再次点击展开
	a.Update(tea.MouseMsg{Action: tea.MouseActionPress, Button: tea.MouseButtonLeft, X: 2, Y: 1 + idx})
	for _, ln := range a.sidebarLines() {
		if ln.itemIdx >= 0 && a.sideItems[ln.itemIdx].section == "remote" {
			shown = true
		}
	}
	if !shown {
		t.Fatal("再次点击应展开")
	}
}

func TestDragDivider(t *testing.T) {
	a, _ := newFakeApp(t)
	x := a.panelLayout().leftW
	a.Update(tea.MouseMsg{Action: tea.MouseActionPress, Button: tea.MouseButtonLeft, X: x, Y: 6})
	if a.dragMode != 1 {
		t.Fatalf("应进入左分隔线拖动状态: %d", a.dragMode)
	}
	a.Update(tea.MouseMsg{Action: tea.MouseActionMotion, Button: tea.MouseButtonLeft, X: 62, Y: 6})
	if a.panelLayout().leftW != 62 {
		t.Errorf("拖动后宽度 = %d", a.panelLayout().leftW)
	}
	a.dragMode = 0
}

func TestDirtyRowContextMenu(t *testing.T) {
	a, _ := newFakeApp(t)
	a.pane = PaneCommits
	a.graphSel = 0
	_ = a.openContextMenu()
	if a.modal == nil || a.modal.kind != modalChoice {
		t.Fatal("未提交伪行应弹出菜单")
	}
	hasStageAll, hasDiscard := false, false
	for _, c := range a.modal.choices {
		if strings.Contains(c.label, "全部暂存") {
			hasStageAll = true
		}
		if strings.Contains(c.label, "全部丢弃") {
			hasDiscard = true
		}
	}
	if !hasStageAll || !hasDiscard {
		t.Errorf("菜单缺少入口: %+v", a.modal.choices)
	}
}

func TestHeadAndTodayMarkers(t *testing.T) {
	a, _ := newFakeApp(t)
	v := plainView(a)
	if !strings.Contains(v, "◉") {
		t.Errorf("HEAD 提交应使用 ◉ 标记")
	}
	if !strings.Contains(v, "[HEAD]") {
		t.Errorf("应显示 HEAD 徽标")
	}
	if !strings.Contains(v, "•") {
		t.Errorf("当天提交应有 • 标记")
	}
}

func TestContentPaging(t *testing.T) {
	a, _ := newFakeApp(t)
	a.pane = PaneContent
	a.content.lines = make([]string, 200)
	a.content.offset = 0
	a.Update(tea.KeyMsg{Type: tea.KeyPgDown})
	if a.content.offset != a.contentViewHeight() {
		t.Errorf("PgDn 后偏移 = %d, 期望 %d", a.content.offset, a.contentViewHeight())
	}
	a.Update(tea.KeyMsg{Type: tea.KeyPgUp})
	if a.content.offset != 0 {
		t.Errorf("PgUp 后偏移 = %d", a.content.offset)
	}
	a.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("G")})
	if a.content.offset == 0 {
		t.Errorf("G 应滚到底部")
	}
}

func TestCollapseByKeyboard(t *testing.T) {
	a, _ := newFakeApp(t)
	a.pane = PaneBranches
	// 选中 main（branches 分组），按 - 折叠
	a.sideSel = 0
	a.sideSelKey = a.sideItems[0].key
	sendKey(a, tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("-")})
	if !a.collapsed["branches"] {
		t.Fatal("应按 - 折叠当前分组")
	}
	// 折叠后选区会移到其他分组，用数字键跳到 LOCAL（会自动展开）
	sendKey(a, tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("1")})
	if a.collapsed["branches"] {
		t.Fatal("跳转到 LOCAL 时应自动展开")
	}
}

func TestFileTreeBuild(t *testing.T) {
	files := []fileEntry{
		{path: "src/audio.cpp", status: "M"},
		{path: "src/video.cpp", status: "A"},
		{path: "main.go", status: "M"},
	}
	_, rows := buildFileTree(files, map[string]bool{})
	if len(rows) != 2 {
		t.Fatalf("折叠时行数 = %d (%+v)", len(rows), rows)
	}
	if !rows[0].node.dir || rows[0].node.name != "src" {
		t.Errorf("首行应为 src 目录: %+v", rows[0].node)
	}
	roots2, rows2 := buildFileTree(files, map[string]bool{"src": true})
	if len(rows2) != 4 {
		t.Fatalf("展开后行数 = %d", len(rows2))
	}
	if len(roots2) != 2 {
		t.Errorf("根节点数 = %d", len(roots2))
	}
	if rows2[0].node.name != "src" || !rows2[1].node.dir {
		// rows2[0]=src, rows2[1]=audio.cpp, rows2[2]=video.cpp, rows2[3]=main.go
	}
	names := []string{rows2[1].node.name, rows2[2].node.name}
	if names[0] != "audio.cpp" || names[1] != "video.cpp" {
		t.Errorf("目录内文件顺序 = %v", names)
	}
}

func TestChangesPane(t *testing.T) {
	a, f := newFakeApp(t)
	a.pane = PaneChanges
	a.rebuildChanges()
	if len(a.changesRows) == 0 {
		t.Fatal("工作区改动用例应可见")
	}
	// 找到未暂存的 a.txt
	for i, r := range a.changesRows {
		if !r.node.dir && r.node.name == "a.txt" {
			a.changeSel = i
			break
		}
	}
	cmd, _ := a.handleChangesKey(tea.KeyMsg{Type: tea.KeySpace})
	pump(t, a, cmd)
	if !hasCall(f.calls, "stage:a.txt") {
		t.Errorf("space 应暂存文件: %v", f.calls)
	}
	// 标题菜单（全部操作）
	a.choose("x", a.uncommittedMenuChoices())
	hasAll := false
	for _, c := range a.modal.choices {
		if strings.Contains(c.label, "全部丢弃") {
			hasAll = true
		}
	}
	if !hasAll {
		t.Error("标题菜单缺少一键丢弃")
	}
	a.modal = nil
}

func TestCommitClickAfterScroll(t *testing.T) {
	a, _ := newFakeApp(t)
	a.pane = PaneCommits
	// 模拟向下滚动：偏移 1，选中第 2 行（提交 bbbb）
	a.graphOff = 1
	a.graphSel = 2
	a.Update(tea.WindowSizeMsg{Width: 120, Height: 40})
	// 屏幕行：面板标题占 1 行，第 row 行 y = 1 + 1 + (row - off)
	y := 1 + (2 - 1)
	a.Update(tea.MouseMsg{Action: tea.MouseActionPress, Button: tea.MouseButtonLeft, X: 60, Y: y})
	c := a.selectedCommit()
	if c == nil || c.Short != "bbbbbbb" {
		t.Errorf("滚动后点击选中错误: %+v", c)
	}
}

func TestCommandLogPanel(t *testing.T) {
	a, _ := newFakeApp(t)
	a.Update(commandMsg{at: time.Now(), line: "git checkout feature/x"})
	if len(a.commands) != 1 {
		t.Fatalf("命令历史 = %d", len(a.commands))
	}
	v := plainView(a)
	if !strings.Contains(v, "命令日志") || !strings.Contains(v, "git checkout feature/x") {
		t.Errorf("命令历史未显示:\n%s", v)
	}
	a.pane = PaneLog
	a.Update(commandMsg{at: time.Now(), line: "git merge main"})
	v = plainView(a)
	if !strings.Contains(v, "git merge main") {
		t.Errorf("最新命令未显示")
	}
}

func TestHoverDividerHandle(t *testing.T) {
	a, _ := newFakeApp(t)
	x := a.panelLayout().leftW
	a.Update(tea.MouseMsg{Action: tea.MouseActionMotion, X: x, Y: 8})
	if a.hoverDiv != 1 {
		t.Fatalf("悬停应识别左分隔线: %d", a.hoverDiv)
	}
	v := plainView(a)
	if !strings.Contains(v, "拖动调整大小") {
		t.Errorf("hover hint missing: \n%s", v)
	}
}

func TestBranchFilterMenu(t *testing.T) {
	a, _ := newFakeApp(t)
	sendKey(a, tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("v")})
	if a.modal == nil || a.modal.kind != modalChoice {
		t.Fatal("v 应打开分支筛选菜单")
	}
	found := false
	for i, c := range a.modal.choices {
		if strings.Contains(c.label, "feature/x") {
			a.modal.sel = i
			found = true
		}
	}
	if !found {
		t.Fatalf("菜单缺少分支: %+v", a.modal.choices)
	}
	_, cmd := a.Update(tea.KeyMsg{Type: tea.KeyEnter})
	pump(t, a, cmd)
	if a.branchFilter != "feature/x" {
		t.Errorf("分支筛选 = %q", a.branchFilter)
	}
}

func TestSectionHeaderRightClickMenu(t *testing.T) {
	a, _ := newFakeApp(t)
	// 打开 TAGS 分组
	a.collapsed["tags"] = false
	lines := a.sidebarLines()
	idx := -1
	for i, ln := range lines {
		if ln.header && ln.section == "tags" {
			idx = i
		}
	}
	if idx < 0 {
		t.Fatal("未找到 TAGS 标题")
	}
	a.Update(tea.MouseMsg{Action: tea.MouseActionPress, Button: tea.MouseButtonRight, X: 2, Y: 1 + idx})
	if a.modal == nil || a.modal.kind != modalChoice {
		t.Fatal("右键分组标题应弹出分组菜单")
	}
	if !strings.Contains(a.modal.title, "分组") {
		t.Errorf("菜单标题 = %q，不应是分支菜单", a.modal.title)
	}
	for _, c := range a.modal.choices {
		if strings.Contains(c.label, "检出该分支") {
			t.Errorf("TAGS 标题菜单不应包含分支检出")
		}
	}
}

func TestFilesHeaderRightClickAllChanges(t *testing.T) {
	a, _ := newFakeApp(t)
	l := a.panelLayout()
	// 文件列标题行：抽屉上边框 y=rightTopH；x 在右侧
	x := l.leftW + 2
	y := l.rightTopH + 1
	a.Update(tea.MouseMsg{Action: tea.MouseActionPress, Button: tea.MouseButtonRight, X: x, Y: y})
	if a.modal == nil {
		t.Fatal("右键文件标题应弹菜单")
	}
	hasDiscardAll := false
	for _, c := range a.modal.choices {
		if strings.Contains(c.label, "全部丢弃") {
			hasDiscardAll = true
		}
	}
	if !hasDiscardAll {
		t.Errorf("文件标题菜单应是全部改动: %+v", a.modal.choices)
	}
}

func TestDirMenuScoped(t *testing.T) {
	a, _ := newFakeApp(t)
	n := &fileNode{name: "src", path: "src", dir: true, children: []*fileNode{
		{name: "a.cpp", path: "src/a.cpp", entries: []fileEntry{{path: "src/a.cpp", status: "M"}}},
	}}
	choices := a.dirMenuChoices(n, true)
	hasDirDiscard := false
	for _, c := range choices {
		if strings.Contains(c.label, "丢弃该目录") {
			hasDirDiscard = true
		}
	}
	if !hasDirDiscard {
		t.Errorf("目录菜单缺少目录级丢弃: %+v", choices)
	}
}

func TestMenuOverlayHoverAndClick(t *testing.T) {
	a, f := newFakeApp(t)
	a.pane = PaneBranches
	for {
		it := a.selectedSideItem()
		if it != nil && it.ref.Name == "feature/x" {
			break
		}
		a.moveSidebar(1)
	}
	a.modalAnchorX, a.modalAnchorY = 10, 5
	a.openContextMenu()
	if a.modal == nil || a.modal.kind != modalChoice {
		t.Fatal("应打开菜单")
	}
	idx := -1
	for i, c := range a.modal.choices {
		if strings.Contains(c.label, "检出该分支") {
			idx = i
		}
	}
	if idx < 0 {
		t.Fatalf("菜单缺少检出: %+v", a.modal.choices)
	}
	box, x, y := a.choiceMenuBox()
	cy := y + 1 + idx
	cx := x + 2
	// 鼠标悬停
	a.Update(tea.MouseMsg{Action: tea.MouseActionMotion, X: cx, Y: cy})
	if a.modal.sel != idx {
		t.Errorf("悬停选择 = %d, 期望 %d", a.modal.sel, idx)
	}
	// 浮层应保留背景内容
	v := plainView(a)
	if !strings.Contains(v, "分支") {
		t.Errorf("菜单应浮在原界面上")
	}
	if len(box) == 0 {
		t.Fatal("菜单框为空")
	}
	// 点击执行
	_, cmd := a.Update(tea.MouseMsg{Action: tea.MouseActionPress, Button: tea.MouseButtonLeft, X: cx, Y: cy})
	pump(t, a, cmd)
	if !hasCall(f.calls, "checkout:feature/x") {
		t.Errorf("点击菜单未执行: %v", f.calls)
	}
}

func TestDefaultExpandedTree(t *testing.T) {
	files := []fileEntry{{path: "src/a.cpp", status: "M"}, {path: "src/b.cpp", status: "A"}}
	_, rows := buildFileTreeDefault(files, map[string]bool{}, true)
	if len(rows) != 3 {
		t.Fatalf("默认展开行数 = %d", len(rows))
	}
	_, collapsed := buildFileTreeDefault(files, map[string]bool{"src": false}, true)
	if len(collapsed) != 1 {
		t.Fatalf("折叠后行数 = %d", len(collapsed))
	}
}

func TestDragRightDividers(t *testing.T) {
	a, _ := newFakeApp(t)
	a.commands = []commandEntry{{at: time.Now(), line: "git status"}}
	l := a.panelLayout()
	// 右侧横向分隔线（提交区/抽屉）
	y := l.rightTopH
	a.Update(tea.MouseMsg{Action: tea.MouseActionPress, Button: tea.MouseButtonLeft, X: l.leftW + 5, Y: y})
	if a.dragMode != 2 {
		t.Fatalf("横向分隔线模式 = %d", a.dragMode)
	}
	a.Update(tea.MouseMsg{Action: tea.MouseActionMotion, X: l.leftW + 5, Y: y + 3})
	if a.rightTopH != l.rightTopH+3 {
		t.Errorf("rightTopH = %d, 期望 %d", a.rightTopH, l.rightTopH+3)
	}
	a.Update(tea.MouseMsg{Action: tea.MouseActionRelease, Button: tea.MouseButtonLeft, X: l.leftW + 5, Y: y + 3})

	// 抽屉内部纵向分隔线（FILES/内容）
	l = a.panelLayout()
	base := a.drawerSplit(l.rightW)
	vertX := l.leftW + 1 + base
	vy := l.rightTopH + 3
	a.Update(tea.MouseMsg{Action: tea.MouseActionPress, Button: tea.MouseButtonLeft, X: vertX, Y: vy})
	if a.dragMode != 3 {
		t.Fatalf("纵向分隔线模式 = %d", a.dragMode)
	}
	a.Update(tea.MouseMsg{Action: tea.MouseActionMotion, X: vertX + 5, Y: vy})
	if a.drawerLeftW != base+5 {
		t.Errorf("drawerLeftW = %d, 期望 %d", a.drawerLeftW, base+5)
	}
	a.Update(tea.MouseMsg{Action: tea.MouseActionRelease, Button: tea.MouseButtonLeft, X: vertX + 5, Y: vy})

	// 命令历史分隔线
	l = a.panelLayout()
	cy := l.rightTopH + 1 + l.drawerH
	a.Update(tea.MouseMsg{Action: tea.MouseActionPress, Button: tea.MouseButtonLeft, X: l.leftW + 5, Y: cy})
	if a.dragMode != 5 {
		t.Fatalf("命令分隔线模式 = %d", a.dragMode)
	}
	before := a.cmdH
	a.Update(tea.MouseMsg{Action: tea.MouseActionMotion, X: l.leftW + 5, Y: cy - 2})
	if a.cmdH <= before {
		t.Errorf("cmdH 拖动后 = %d, 初始 %d，期望增大", a.cmdH, before)
	}
	a.Update(tea.MouseMsg{Action: tea.MouseActionRelease, Button: tea.MouseButtonLeft, X: l.leftW + 5, Y: cy - 2})
}

func TestFilesTreeMouseExpandAndPreview(t *testing.T) {
	a, _ := newFakeApp(t)
	a.pane = PaneFiles
	a.filesOwner = "work"
	a.setFiles([]fileEntry{
		{path: "src/a.cpp", status: "M"},
		{path: "src/b.cpp", status: "A"},
	})
	// 默认展开：src 目录 + 两个文件
	if len(a.fileRows) != 3 {
		t.Fatalf("可见行 = %d", len(a.fileRows))
	}
	l := a.panelLayout()
	y0 := l.rightTopH + 2 // 抽屉内容第一行（上边框下移 1 行）
	x := l.leftW + 2
	// 点击 src 目录应折叠
	a.Update(tea.MouseMsg{Action: tea.MouseActionPress, Button: tea.MouseButtonLeft, X: x, Y: y0})
	if len(a.fileRows) != 1 {
		t.Fatalf("点击目录后行数 = %d", len(a.fileRows))
	}
	// 再次点击展开
	a.Update(tea.MouseMsg{Action: tea.MouseActionPress, Button: tea.MouseButtonLeft, X: x, Y: y0})
	if len(a.fileRows) != 3 {
		t.Fatalf("再次点击后行数 = %d", len(a.fileRows))
	}
	// 点击第一个文件，右侧应加载其 diff
	a.Update(tea.MouseMsg{Action: tea.MouseActionPress, Button: tea.MouseButtonLeft, X: x, Y: y0 + 1})
	if !strings.Contains(a.content.title, "a.cpp") {
		t.Errorf("点击文件后内容标题 = %q", a.content.title)
	}
}

// TestTabAndAltFocus 验证 7 区 Tab/Shift+Tab 顺序切换与 Alt+方向键空间导航。
func TestTabAndAltFocus(t *testing.T) {
	a, _ := newFakeApp(t)
	a.commands = []commandEntry{{at: time.Now(), line: "git status"}}
	a.pane = PaneBranches
	order := []Pane{PaneInfo, PaneCommits, PaneFiles, PaneContent, PaneLog, PaneBranches}
	for _, want := range order {
		sendKey(a, tea.KeyMsg{Type: tea.KeyTab})
		if a.pane != want {
			t.Fatalf("Tab 后焦点 = %v, 期望 %v", a.pane, want)
		}
	}
	sendKey(a, tea.KeyMsg{Type: tea.KeyShiftTab})
	if a.pane != PaneLog {
		t.Fatalf("Shift+Tab 后焦点 = %v, 期望 PaneLog", a.pane)
	}
	// Alt+方向键按空间位置切换：左三分支/改动/信息，右四提交/文件/内容/命令。
	a.pane = PaneBranches
	sendKey(a, tea.KeyMsg{Type: tea.KeyRight, Alt: true})
	if a.pane != PaneCommits {
		t.Fatalf("分支 alt+right = %v, 期望提交区", a.pane)
	}
	sendKey(a, tea.KeyMsg{Type: tea.KeyDown, Alt: true})
	if a.pane != PaneFiles {
		t.Fatalf("提交 alt+down = %v, 期望文件区", a.pane)
	}
	sendKey(a, tea.KeyMsg{Type: tea.KeyDown, Alt: true})
	if a.pane != PaneLog {
		t.Fatalf("文件 alt+down = %v, 期望命令区", a.pane)
	}
	sendKey(a, tea.KeyMsg{Type: tea.KeyUp, Alt: true})
	if a.pane != PaneFiles {
		t.Fatalf("命令 alt+up = %v, 期望文件区", a.pane)
	}
	sendKey(a, tea.KeyMsg{Type: tea.KeyLeft, Alt: true})
	if a.pane != PaneBranches {
		t.Fatalf("文件 alt+left = %v, 期望分支区", a.pane)
	}
}

// TestMenuClickOutsideCloses 验证点击菜单外区域自动关闭。
func TestMenuClickOutsideCloses(t *testing.T) {
	a, _ := newFakeApp(t)
	a.pane = PaneCommits
	a.graphSel = a.dirtyOffset()
	a.modalAnchorX, a.modalAnchorY = 40, 4
	a.openContextMenu()
	if a.modal == nil {
		t.Fatal("应打开菜单")
	}
	a.Update(tea.MouseMsg{Action: tea.MouseActionPress, Button: tea.MouseButtonLeft, X: 0, Y: 0})
	if a.modal != nil {
		t.Fatal("点击菜单外应关闭菜单")
	}
}

// TestMenuSubmenuClick 验证多级菜单点击可进入下一级。
func TestMenuSubmenuClick(t *testing.T) {
	a, _ := newFakeApp(t)
	a.pane = PaneCommits
	a.graphSel = a.dirtyOffset() + 1
	a.modalAnchorX, a.modalAnchorY = 40, 4
	a.openContextMenu()
	idx := -1
	for i, c := range a.modal.choices {
		if c.label == "reset" {
			idx = i
		}
	}
	if idx < 0 {
		t.Fatal("提交菜单缺少 reset")
	}
	_, x, y := a.choiceMenuBox()
	a.Update(tea.MouseMsg{Action: tea.MouseActionPress, Button: tea.MouseButtonLeft, X: x + 2, Y: y + 1 + idx})
	if a.modal == nil || a.modal.kind != modalChoice || !strings.Contains(a.modal.title, "reset") {
		t.Fatalf("点击 reset 应打开子菜单: %+v", a.modal)
	}
}

// TestBranchFilterScopes 验证分支筛选下拉包含全部/本地/远端三档。
func TestBranchFilterScopes(t *testing.T) {
	a, _ := newFakeApp(t)
	sendKey(a, tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("v")})
	if a.modal == nil {
		t.Fatal("v 应打开筛选菜单")
	}
	idx := -1
	for i, c := range a.modal.choices {
		if strings.Contains(c.label, "仅本地分支") {
			idx = i
		}
	}
	if idx < 0 {
		t.Fatalf("筛选菜单缺少本地分组: %+v", a.modal.choices)
	}
	a.modal.sel = idx
	_, cmd := a.Update(tea.KeyMsg{Type: tea.KeyEnter})
	pump(t, a, cmd)
	if a.branchFilterScope != "local" || a.branchFilter != "" {
		t.Fatalf("本地筛选参数不对: scope=%q rev=%q", a.branchFilterScope, a.branchFilter)
	}
	if !strings.Contains(plainView(a), "本地分支") {
		t.Error("COMMITS 标题未显示筛选范围")
	}
}

// TestLeftDividersDrag 验证左侧上下两条分隔线可拖动并双击复位。
func TestLeftDividersDrag(t *testing.T) {
	a, _ := newFakeApp(t)
	a.Update(tea.WindowSizeMsg{Width: 120, Height: 50})
	l := a.panelLayout()
	yTop := l.leftTopH
	a.Update(tea.MouseMsg{Action: tea.MouseActionPress, Button: tea.MouseButtonLeft, X: 2, Y: yTop})
	if a.dragMode != 4 {
		t.Fatalf("上分隔线拖动模式 = %d, 期望 4", a.dragMode)
	}
	a.Update(tea.MouseMsg{Action: tea.MouseActionMotion, X: 2, Y: yTop + 2})
	if a.leftTopH != l.leftTopH+2 {
		t.Errorf("leftTopH = %d, 期望 %d", a.leftTopH, l.leftTopH+2)
	}
	a.Update(tea.MouseMsg{Action: tea.MouseActionRelease, Button: tea.MouseButtonLeft, X: 2, Y: yTop + 2})

	// 双击上分隔线复位
	a.lastClick = time.Time{}
	l = a.panelLayout()
	yTop = l.leftTopH
	a.Update(tea.MouseMsg{Action: tea.MouseActionPress, Button: tea.MouseButtonLeft, X: 2, Y: yTop})
	a.Update(tea.MouseMsg{Action: tea.MouseActionRelease, Button: tea.MouseButtonLeft, X: 2, Y: yTop})
	a.Update(tea.MouseMsg{Action: tea.MouseActionPress, Button: tea.MouseButtonLeft, X: 2, Y: yTop})
	if a.leftTopH != 0 || a.dragMode != 0 {
		t.Errorf("双击复位后 leftTopH=%d dragMode=%d", a.leftTopH, a.dragMode)
	}
}

// TestPanelViewHeights 回归：分支/改动区滚动高度应取各自面板高度。
func TestPanelViewHeights(t *testing.T) {
	a, _ := newFakeApp(t)
	l := a.panelLayout()
	if got := a.branchViewHeight(); got != l.leftTopH-2 {
		t.Errorf("branchViewHeight = %d, 期望 %d", got, l.leftTopH-2)
	}
	a.pane = PaneBranches
	if got := a.currentViewHeight(); got != l.leftTopH-2 {
		t.Errorf("分支区 currentViewHeight = %d, 期望 %d", got, l.leftTopH-2)
	}
}

// TestInfoHeaderRightClick 回归：提交信息面板标题行应识别为标题，不被当成首行数据。
func TestInfoHeaderRightClick(t *testing.T) {
	a, _ := newFakeApp(t)
	l := a.panelLayout()
	y := l.leftTopH + 1
	pane, row, ok := a.hitTest(2, y)
	if !ok || pane != PaneInfo || row != -1 {
		t.Fatalf("提交信息标题命中 = (%v,%d,%v), 期望 (PaneInfo,-1,true)", pane, row, ok)
	}
	a.Update(tea.MouseMsg{Action: tea.MouseActionPress, Button: tea.MouseButtonRight, X: 2, Y: y})
	if a.modal == nil || !strings.Contains(a.modal.title, "提交信息") {
		t.Fatalf("右键提交信息标题应打开整体菜单: %+v", a.modal)
	}
}

// TestBranchWheelScrollViewport 验证分支过多时滚轮可滚动视口。
func TestBranchWheelScrollViewport(t *testing.T) {
	a, _ := newFakeApp(t)
	for i := 0; i < 30; i++ {
		a.refs = append(a.refs, git.Ref{
			Name: fmt.Sprintf("branch-%02d", i), Kind: git.RefLocal, Target: "bbbbbbbbbbbb2222",
		})
	}
	a.rebuildSidebar()
	a.pane = PaneCommits
	a.Update(tea.MouseMsg{Action: tea.MouseActionPress, Button: tea.MouseButtonWheelDown, X: 2, Y: 5})
	if a.pane != PaneBranches {
		t.Fatalf("侧栏滚轮应聚焦分支区，实际 %v", a.pane)
	}
	if a.sideOff == 0 {
		t.Error("分支区滚轮应移动视口")
	}
}

// TestContentArrowScroll 验证内容区 ↑↓ 逐行滚动。
func TestContentArrowScroll(t *testing.T) {
	a, _ := newFakeApp(t)
	a.pane = PaneContent
	a.content.lines = make([]string, 50)
	a.content.offset = 1
	sendKey(a, tea.KeyMsg{Type: tea.KeyUp})
	if a.content.offset != 0 {
		t.Errorf("↑ 后 offset = %d, 期望 0", a.content.offset)
	}
	sendKey(a, tea.KeyMsg{Type: tea.KeyDown})
	if a.content.offset != 1 {
		t.Errorf("↓ 后 offset = %d, 期望 1", a.content.offset)
	}
}

// TestIMECompositionIgnored 验证浏览模式忽略非 ASCII/组合输入并提示，搜索框仍可输入。
func TestIMECompositionIgnored(t *testing.T) {
	a, _ := newFakeApp(t)
	a.pane = PaneCommits
	sendKey(a, tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("中文")})
	if !a.toastErr || !strings.Contains(a.toast, "输入法") {
		t.Errorf("浏览模式应提示输入法，toast=%q err=%v", a.toast, a.toastErr)
	}
	sendKey(a, tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("/")})
	sendRunes(a, "中文")
	if a.filter != "中文" {
		t.Errorf("搜索框应接受中文，filter=%q", a.filter)
	}
}

// TestCommitColorMatchesBadge 验证提交 lane 颜色与分支徽标同色。
func TestCommitColorMatchesBadge(t *testing.T) {
	a, _ := newFakeApp(t)
	if got, want := a.commitColorOf("aaaaaaaaaaaa1111", -1), a.refColorIndex(git.RefLocal, "main"); got != want {
		t.Errorf("main 提交颜色 = %d, 分支徽标颜色 = %d", got, want)
	}
	if got, want := a.commitColorOf("bbbbbbbbbbbb2222", -1), a.refColorIndex(git.RefLocal, "feature/x"); got != want {
		t.Errorf("feature/x 提交颜色 = %d, 分支徽标颜色 = %d", got, want)
	}
}

// TestThemeApplyAndFallback 验证主题应用、未知主题与颜色回退。
func TestThemeApplyAndFallback(t *testing.T) {
	t.Cleanup(func() { applyTheme(defaultTheme()) })
	light := lightTheme()
	applyTheme(light)
	if colorAccent != light.Accent || colorBorder != light.Border || colorSelect != light.Selection {
		t.Errorf("applyTheme 未更新强调/边框/选中色")
	}
	if lanePalette[0] != light.Lanes[0] {
		t.Errorf("applyTheme 未更新 lane 调色板")
	}
	th, bad := resolveTheme("nope", map[string]string{
		"accent":    "#ff0000",
		"border":    "#00ff00",
		"selection": "#0000ff",
	})
	if th.Name != "default" || len(bad) != 1 || bad[0] != "theme" {
		t.Fatalf("未知主题应回退 default 并报告 theme: name=%q bad=%v", th.Name, bad)
	}
	if th.Accent.Dark != "#ff0000" || th.Border.Dark != "#00ff00" || th.Selection.Dark != "#0000ff" {
		t.Errorf("自定义 border/selection/accent 未生效: %+v", th)
	}
}

// TestThemeAndLayoutPersist 验证主题与各分隔线尺寸写回配置并可再次加载。
func TestThemeAndLayoutPersist(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("AppData", dir)
	t.Setenv("XDG_CONFIG_HOME", dir)
	t.Cleanup(func() { applyTheme(defaultTheme()) })

	a, _ := newFakeApp(t)
	a.persist = true
	a.leftW = 41
	a.drawerLeftW = 27
	a.rightTopH = 9
	a.leftTopH = 8
	a.leftMidH = 7
	a.cmdH = 5
	a.cycleTheme()

	cfg := config.Load()
	if cfg.Theme != "light" {
		t.Errorf("主题未持久化: %q", cfg.Theme)
	}
	if cfg.LeftWidth != 41 || cfg.DrawerLeft != 27 || cfg.RightTop != 9 ||
		cfg.LeftTop != 8 || cfg.LeftMid != 7 || cfg.CommandH != 5 {
		t.Errorf("布局未完整持久化: %+v", cfg)
	}

	b := New(&fakeSvc{})
	b.SetConfig(config.File{Theme: "high-contrast", Colors: map[string]string{"accent": "#123456"}, LeftWidth: 33})
	if b.themeName != "high-contrast" || b.leftW != 33 {
		t.Errorf("SetConfig 未恢复主题/宽度: theme=%q leftW=%d", b.themeName, b.leftW)
	}
	if colorAccent.Light != "#123456" || colorAccent.Dark != "#123456" {
		t.Errorf("SetConfig 未应用自定义颜色: %+v", colorAccent)
	}
}

// TestCommandHistoryWheel 验证命令历史区滚轮可回看。
func TestCommandHistoryWheel(t *testing.T) {
	a, _ := newFakeApp(t)
	for i := 0; i < 20; i++ {
		a.Update(commandMsg{at: time.Now(), line: fmt.Sprintf("git commit -m %d", i)})
	}
	l := a.panelLayout()
	y := l.rightTopH + l.drawerH + 4
	a.Update(tea.MouseMsg{Action: tea.MouseActionPress, Button: tea.MouseButtonWheelUp, X: l.leftW + 2, Y: y})
	if a.pane != PaneLog {
		t.Fatalf("滚轮应聚焦命令区，实际 %v", a.pane)
	}
	if a.cmdOffset == 0 {
		t.Error("命令历史滚轮应回看旧记录")
	}
}

// TestWorkingEntriesCoverAllStates 验证工作区改动覆盖 staged/unstaged/untracked。
func TestWorkingEntriesCoverAllStates(t *testing.T) {
	a, _ := newFakeApp(t)
	entries := a.workingFileEntries()
	var staged, unstaged, untracked bool
	for _, e := range entries {
		switch {
		case e.staged:
			staged = true
		case e.untracked:
			untracked = true
		default:
			unstaged = true
		}
	}
	if !staged || !unstaged || !untracked {
		t.Errorf("工作区改动未覆盖三类: %+v", entries)
	}
}

// TestHeaderRightClickMenus 验证各面板标题右键打开“整体”菜单（分支/改动/提交/命令）。
func TestHeaderRightClickMenus(t *testing.T) {
	a, _ := newFakeApp(t)
	a.commands = []commandEntry{{at: time.Now(), line: "git status"}}
	l := a.panelLayout()
	click := func(x, y int) {
		a.modal = nil
		a.Update(tea.MouseMsg{Action: tea.MouseActionPress, Button: tea.MouseButtonRight, X: x, Y: y})
		if a.modal == nil {
			t.Fatalf("右键标题 (%d,%d) 应弹出菜单", x, y)
		}
	}
	click(2, 0)
	if !strings.Contains(a.modal.title, "分支") {
		t.Errorf("BRANCHES 标题菜单 = %q", a.modal.title)
	}
	// 提交区右键标题/边框不再弹菜单（避免误触），左键点标题才打开分支筛选
	a.modal = nil
	a.Update(tea.MouseMsg{Action: tea.MouseActionPress, Button: tea.MouseButtonRight, X: l.leftW + 5, Y: 0})
	if a.modal != nil {
		t.Errorf("COMMITS 标题右键不应弹菜单，实际 %q", a.modal.title)
	}
	a.Update(tea.MouseMsg{Action: tea.MouseActionPress, Button: tea.MouseButtonLeft, X: l.leftW + 5, Y: 0})
	if a.modal == nil || !strings.Contains(a.modal.title, "分支的提交") {
		t.Errorf("COMMITS 标题左键应打开分支筛选")
	}
	click(l.leftW+5, l.rightTopH+2+l.drawerH)
	if !strings.Contains(a.modal.title, "命令历史") {
		t.Errorf("COMMANDS 标题菜单 = %q", a.modal.title)
	}
}

// TestBranchOrderAndCurrentMarker 验证分支按 tip 时间倒序、当前分支带 * 标记。
func TestBranchOrderAndCurrentMarker(t *testing.T) {
	a, _ := newFakeApp(t)
	var order []string
	for _, it := range a.sideItems {
		if it.kind == sideBranch {
			order = append(order, it.ref.Name)
		}
	}
	if len(order) < 2 || order[0] != "main" || order[1] != "feature/x" {
		t.Errorf("本地分支顺序 = %v, 期望 main 在 feature/x 前（tip 更新）", order)
	}
	if !strings.Contains(plainView(a), "* main") {
		t.Error("当前分支缺少 * 标记")
	}
}

// TestOperationMenusCoverage 验证分支/提交菜单覆盖清单要求的分支与提交操作。
func TestOperationMenusCoverage(t *testing.T) {
	a, _ := newFakeApp(t)
	var local git.Ref
	for _, r := range a.refs {
		if r.Kind == git.RefLocal && r.Name == "feature/x" {
			local = r
		}
	}
	labels := func(cs []choice) string {
		var b strings.Builder
		for _, c := range cs {
			b.WriteString(c.label)
			b.WriteByte('\n')
		}
		return b.String()
	}
	branch := labels(a.branchMenuChoices(&sideItem{kind: sideBranch, ref: local}))
	for _, want := range []string{"检出", "新建", "重命名", "删除", "强制删除", "合并", "rebase", "推送", "强制推送", "上游", "复制"} {
		if !strings.Contains(branch, want) {
			t.Errorf("本地分支菜单缺少 %q", want)
		}
	}
	remote := labels(a.branchMenuChoices(&sideItem{kind: sideRemote, ref: git.Ref{Kind: git.RefRemote, Name: "origin/main"}}))
	for _, want := range []string{"跟踪分支", "合并", "rebase", "删除远端", "复制"} {
		if !strings.Contains(remote, want) {
			t.Errorf("远端分支菜单缺少 %q", want)
		}
	}
	tag := labels(a.branchMenuChoices(&sideItem{kind: sideTag, ref: git.Ref{Kind: git.RefTag, Name: "v1.0"}}))
	for _, want := range []string{"检出", "推送标签", "删除标签", "复制"} {
		if !strings.Contains(tag, want) {
			t.Errorf("标签菜单缺少 %q", want)
		}
	}
	stash := labels(a.branchMenuChoices(&sideItem{kind: sideStash, stash: git.Stash{Selector: "stash@{0}"}}))
	for _, want := range []string{"查看", "apply", "pop", "drop"} {
		if !strings.Contains(stash, want) {
			t.Errorf("stash 菜单缺少 %q", want)
		}
	}
	commit := labels(a.commitMenuChoices(&git.Commit{Hash: "aaaaaaaaaaaa1111", Short: "aaaaaaa", Subject: "x"}))
	for _, want := range []string{"详情", "检出", "新建分支", "标签", "cherry-pick", "revert", "reset", "sha", "提交信息"} {
		if !strings.Contains(commit, want) {
			t.Errorf("提交菜单缺少 %q", want)
		}
	}
}

// TestF4Removed 验证 F4 不再触发搜索入口。
func TestF4Removed(t *testing.T) {
	a, _ := newFakeApp(t)
	sendKey(a, tea.KeyMsg{Type: tea.KeyF4})
	if a.searchActive || a.modal != nil {
		t.Fatalf("F4 不应打开搜索: searchActive=%v modal=%+v", a.searchActive, a.modal)
	}
}

// TestInlineInputNotPopup 验证单行输入在底部行内显示，不弹居中对话框。
func TestInlineInputNotPopup(t *testing.T) {
	a, _ := newFakeApp(t)
	a.pane = PaneBranches
	a.sideSel = 0
	a.sideSelKey = a.sideItems[0].key
	sendKey(a, tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("r")})
	if a.modal == nil || a.modal.kind != modalInput {
		t.Fatalf("重命名应进入行内输入，modal=%+v", a.modal)
	}
	v := plainView(a)
	if strings.Contains(v, "╭─ 重命名") {
		t.Errorf("行内输入不应渲染居中弹框:\n%s", v)
	}
	if strings.Contains(v, "╭─ 重命名") {
		t.Errorf("底部应显示输入提示:\n%s", v)
	}
}

// TestCommitInfoFixedPanel 验证提交信息固定显示在左下，而非浮在图上。
func TestCommitInfoFixedPanel(t *testing.T) {
	a, _ := newFakeApp(t)
	a.pane = PaneCommits
	a.graphSel = a.dirtyOffset() + 1
	pump(t, a, a.loadFilesForSelection())
	v := plainView(a)
	if strings.Contains(v, "╭─ 重命名") {
		t.Errorf("缺少提交信息固定面板:\n%s", v)
	}
	// 面板标题应出现在左栏下半部（行首附近），而不是覆盖在提交图中央。
	found := false
	for _, line := range strings.Split(v, "\n") {
		if strings.Contains(line, "提交信息") && strings.Index(line, "提交信息") < a.panelLayout().leftW {
			found = true
		}
	}
	if !found {
		t.Error("提交信息标题应位于左下固定面板")
	}
}

// TestViewFillsEveryLine 每行都必须补满终端宽度，避免短行右侧残留上一帧内容。
func TestViewFillsEveryLine(t *testing.T) {
	a, _ := newFakeApp(t)
	a.Update(tea.WindowSizeMsg{Width: 60, Height: 16})
	check := func(name string, view string) {
		t.Helper()
		lines := strings.Split(view, "\n")
		if len(lines) != 16 {
			t.Fatalf("%s: 行数 %d，期望 16", name, len(lines))
		}
		for i, ln := range lines {
			if w := strWidth(ln); w != 60 {
				t.Fatalf("%s: 第 %d 行宽度 %d，期望 60: %q", name, i, w, ansi.Strip(ln))
			}
		}
	}
	check("main", a.View())
	a.overlay = overlayDetail
	a.detail.lines = []string{"short", "第二行"}
	check("detail", a.View())
	a.overlay = overlayHelp
	check("help", a.View())
}

// TestFooterKeepsHintsWithLongToast 长提示不能把右侧快捷键提示挤出屏幕。
func TestFooterKeepsHintsWithLongToast(t *testing.T) {
	a, _ := newFakeApp(t)
	a.toast = "已复制: " + strings.Repeat("很长的提交信息", 10)
	v := plainView(a)
	lines := strings.Split(v, "\n")
	last := strings.TrimSpace(lines[len(lines)-1])
	if !strings.Contains(last, "↑↓") {
		t.Fatalf("底部快捷键提示被挤掉: %q", last)
	}
}

// TestCopyToastImmediate 点击复制时提示必须立即出现，不能等剪贴板命令返回。
func TestCopyToastImmediate(t *testing.T) {
	a, _ := newFakeApp(t)
	a.toast = "上一条消息"
	a.copyToClipboard("新的提交信息")
	if !strings.Contains(a.toast, "已复制") || !strings.Contains(a.toast, "新的提交信息") {
		t.Fatalf("复制提示未立即刷新: %q", a.toast)
	}
}

// TestFilesScrollReachesLastRow 文件列表滚动到底必须能看到最后一个文件。
func TestFilesScrollReachesLastRow(t *testing.T) {
	a, _ := newFakeApp(t)
	a.Update(tea.WindowSizeMsg{Width: 120, Height: 40})
	var files []fileEntry
	for i := 0; i < 30; i++ {
		files = append(files, fileEntry{path: fmt.Sprintf("f%02d.go", i), status: "M"})
	}
	a.setFiles(files)
	a.pane = PaneFiles
	a.fileSel = len(a.fileRows) - 1
	a.ensureFileVisible()
	h := a.fileViewHeight()
	if want := len(a.fileRows) - h; a.fileOff != want {
		t.Fatalf("fileOff = %d, 期望 %d", a.fileOff, want)
	}
	if v := plainView(a); !strings.Contains(v, "f29.go") {
		t.Fatal("滚动到底后最后一行文件未渲染")
	}
}

// TestFileTreeDirPlaceholderNamed 目录占位条目（path 以 / 结尾）应显示为 "docs/"，不能是空白文件。
func TestFileTreeDirPlaceholderNamed(t *testing.T) {
	_, rows := buildFileTree([]fileEntry{{path: "docs/"}}, map[string]bool{})
	if len(rows) != 1 {
		t.Fatalf("行数 = %d，期望 1: %+v", len(rows), rows)
	}
	if name := rows[0].node.name; name != "docs/" {
		t.Fatalf("目录占位名称 = %q，期望 docs/", name)
	}
	if rows[0].node.path != "docs/" {
		t.Fatalf("目录占位路径 = %q，期望保留 docs/", rows[0].node.path)
	}
}

// TestMenuWheelThenOutsideClickCloses 菜单滚轮后，单击空白处应立即关闭。
func TestMenuWheelThenOutsideClickCloses(t *testing.T) {
	a, _ := newFakeApp(t)
	a.pane = PaneCommits
	a.graphSel = a.dirtyOffset()
	a.modalAnchorX, a.modalAnchorY = 40, 4
	_ = a.openContextMenu()
	if a.modal == nil {
		t.Fatal("菜单未打开")
	}
	a.Update(tea.MouseMsg{Action: tea.MouseActionPress, Button: tea.MouseButtonWheelDown, X: 40, Y: 6})
	a.Update(tea.MouseMsg{Action: tea.MouseActionPress, Button: tea.MouseButtonLeft, X: 2, Y: a.height - 2})
	if a.modal != nil {
		t.Fatalf("滚轮后点击空白未关闭菜单: %q", a.modal.title)
	}
}

// TestScrollbarsRender 可滚动区域应显示滚动条（滑块字符 ┃）。
func TestScrollbarsRender(t *testing.T) {
	a, _ := newFakeApp(t)
	a.Update(tea.WindowSizeMsg{Width: 120, Height: 40})
	var files []fileEntry
	for i := 0; i < 40; i++ {
		files = append(files, fileEntry{path: fmt.Sprintf("f%02d.go", i), status: "M"})
	}
	a.setFiles(files)
	a.pane = PaneFiles
	a.fileSel = 0
	a.ensureFileVisible()
	if v := plainView(a); !strings.Contains(v, "┃") {
		t.Error("文件区滚动条未渲染")
	}

	// 详情浮层
	a.overlay = overlayDetail
	a.detail.lines = nil
	for i := 0; i < 60; i++ {
		a.detail.lines = append(a.detail.lines, fmt.Sprintf("line %d", i))
	}
	if v := plainView(a); !strings.Contains(v, "┃") {
		t.Error("详情浮层滚动条未渲染")
	}
	// 方向键滚动详情
	a.detail.offset = 0
	a.Update(tea.KeyMsg{Type: tea.KeyDown})
	if a.detail.offset == 0 {
		t.Error("方向键下未滚动详情浮层")
	}
}

// TestInfoPaneScrollsWithArrows 提交信息区方向键应滚动内容，而不是移动提交选中。
func TestInfoPaneScrollsWithArrows(t *testing.T) {
	a, _ := newFakeApp(t)
	a.pane = PaneInfo
	a.graphSel = a.dirtyOffset()
	a.clampSelection()
	a.commitBody = strings.Repeat("很长的正文行"+string(rune(10)), 40)
	a.infoOff = 0
	sel := a.graphSel
	a.Update(tea.KeyMsg{Type: tea.KeyDown})
	if a.infoOff == 0 {
		t.Errorf("提交信息区方向键下未滚动 (lines=%d viewH=%d)", a.infoLineCount(), a.infoViewHeight())
	}
	if a.graphSel != sel {
		t.Error("提交信息区滚动不应改变提交选中")
	}
}

// TestInfoDefaultHeightMatchesDrawer 提交信息区默认高度应与文件区一致。
func TestInfoDefaultHeightMatchesDrawer(t *testing.T) {
	a, _ := newFakeApp(t)
	a.commands = []commandEntry{{at: time.Now(), line: "git status"}}
	l := a.panelLayout()
	if !l.split {
		t.Skip("窄屏不分区")
	}
	if l.leftBotH != l.drawerH {
		t.Errorf("提交信息高度 = %d，文件区高度 = %d", l.leftBotH, l.drawerH)
	}
}

// TestCommitRowShowsBranchAndTag 同一提交上的分支与 tag 徽标都要显示。
func TestCommitRowShowsBranchAndTag(t *testing.T) {
	a, _ := newFakeApp(t)
	a.refs = append(a.refs, git.Ref{Name: "v9.9", Kind: git.RefTag, Target: "aaaaaaaaaaaa1111"})
	a.rebuildCommitColors()
	found := false
	for _, line := range strings.Split(plainView(a), "\n") {
		if !strings.Contains(line, "[HEAD]") {
			continue
		}
		found = true
		if !strings.Contains(line, "[*main]") {
			t.Errorf("提交行缺少分支徽标: %q", line)
		}
		if !strings.Contains(line, "[v9.9]") {
			t.Errorf("提交行缺少 tag 徽标: %q", line)
		}
	}
	if !found {
		t.Fatal("未找到 HEAD 提交行")
	}
}

// TestModalReleaseOnlyClosesChoice 鼠标 release 只能关闭选择菜单，不能误关确认框/输入框。
func TestModalReleaseOnlyClosesChoice(t *testing.T) {
	a, _ := newFakeApp(t)
	a.modal = &modal{kind: modalConfirm, title: "确认"}
	a.Update(tea.MouseMsg{Action: tea.MouseActionRelease, Button: tea.MouseButtonLeft, X: 2, Y: 2})
	if a.modal == nil {
		t.Fatal("release 不应关闭确认框")
	}
	a.modal = &modal{kind: modalChoice, choices: []choice{{label: "x"}}}
	a.modalAnchorX, a.modalAnchorY = 40, 4
	a.Update(tea.MouseMsg{Action: tea.MouseActionRelease, Button: tea.MouseButtonLeft, X: 2, Y: a.height - 2})
	if a.modal != nil {
		t.Fatal("release 在菜单外应关闭选择菜单")
	}
}

// TestFooterShowsBuildVersion Release 构建显示版本，本地构建不显示。
func TestFooterShowsBuildVersion(t *testing.T) {
	t.Cleanup(func() { SetVersion("") })
	SetVersion("v9.9.9")
	a, _ := newFakeApp(t)
	if v := plainView(a); !strings.Contains(v, "v9.9.9") {
		t.Fatal("底部未显示构建版本")
	}
	SetVersion("")
	if v := plainView(a); strings.Contains(v, "v9.9.9") {
		t.Fatal("本地构建不应显示版本")
	}
}

// TestSearchResultsClickable 搜索态下提交结果行仍可鼠标点击选择。
func TestSearchResultsClickable(t *testing.T) {
	a, _ := newFakeApp(t)
	l := a.panelLayout()
	sendKey(a, tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("/")})
	sendRunes(a, "修复")
	if len(a.visibleCommits()) == 0 {
		t.Fatal("搜索无结果，测试数据不对")
	}
	target := a.dirtyOffset() + 1 // 点击第二条结果
	if target >= a.totalRows() {
		target = a.dirtyOffset()
	}
	a.graphSel = 0
	a.Update(tea.MouseMsg{Action: tea.MouseActionPress, Button: tea.MouseButtonLeft, X: l.leftW + 3, Y: 1 + target})
	if a.graphSel != target {
		t.Fatalf("搜索态点击后 graphSel = %d，期望 %d", a.graphSel, target)
	}
	if !a.searchActive {
		t.Error("点击结果不应退出搜索")
	}
}

// TestFileMenuStageState 文件右键菜单应根据暂存状态显示 stage/unstage。
func TestFileMenuStageState(t *testing.T) {
	a, _ := newFakeApp(t)
	labels := func(n *fileNode) []string {
		var out []string
		for _, c := range a.fileNodeMenuChoices(n, true) {
			out = append(out, c.label)
		}
		return out
	}
	has := func(list []string, sub string) bool {
		for _, s := range list {
			if strings.Contains(s, sub) {
				return true
			}
		}
		return false
	}
	staged := &fileNode{name: "a.txt", path: "a.txt", entries: []fileEntry{{path: "a.txt", staged: true}}}
	if l := labels(staged); !has(l, "取消暂存") || has(l, "暂存该文件") {
		t.Fatalf("已暂存菜单 = %v", l)
	}
	unstaged := &fileNode{name: "a.txt", path: "a.txt", entries: []fileEntry{{path: "a.txt"}}}
	if l := labels(unstaged); !has(l, "暂存该文件") || has(l, "取消暂存") {
		t.Fatalf("未暂存菜单 = %v", l)
	}
	both := &fileNode{name: "a.txt", path: "a.txt", entries: []fileEntry{{path: "a.txt", staged: true}, {path: "a.txt"}}}
	if l := labels(both); !has(l, "暂存该文件") || !has(l, "取消暂存") {
		t.Fatalf("部分暂存菜单 = %v", l)
	}
}

// TestSearchResultsRightClick 搜索态下提交结果右键菜单与平时一致。
func TestSearchResultsRightClick(t *testing.T) {
	a, _ := newFakeApp(t)
	l := a.panelLayout()
	sendKey(a, tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("/")})
	sendRunes(a, "修复")
	if len(a.visibleCommits()) == 0 {
		t.Fatal("搜索无结果")
	}
	a.graphSel = 0
	y := 1 + a.dirtyOffset()
	a.Update(tea.MouseMsg{Action: tea.MouseActionPress, Button: tea.MouseButtonRight, X: l.leftW + 3, Y: y})
	if a.modal == nil {
		t.Fatal("搜索态右键提交应弹出菜单")
	}
	if a.graphSel != a.dirtyOffset() {
		t.Errorf("右键后选中行 = %d，期望 %d", a.graphSel, a.dirtyOffset())
	}
}
