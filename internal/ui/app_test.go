package ui

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/x/ansi"

	"github.com/ssk-wh/gitc/internal/config"
	"github.com/ssk-wh/gitc/internal/git"
	"github.com/ssk-wh/gitc/internal/i18n"
	"github.com/ssk-wh/gitc/internal/platform"
	"github.com/ssk-wh/gitc/internal/workspace"
)

// isolateConfig 把配置目录指向临时目录，避免测试污染真实配置。
func isolateConfig(t *testing.T) {
	t.Helper()
	dir := t.TempDir()
	t.Setenv("AppData", dir)
	t.Setenv("XDG_CONFIG_HOME", dir)
	t.Setenv("HOME", dir)
}

// fakeSvc 是测试用假服务：读接口返回预置数据，写接口记录调用。
type fakeSvc struct {
	git.Service
	root    string
	commits []git.Commit
	refs    []git.Ref
	status  git.Status
	state   git.RepoState
	stashes []git.Stash
	calls   []string
}

func (f *fakeSvc) Name() string { return "demo" }
func (f *fakeSvc) RootPath() string {
	if f.root != "" {
		return f.root
	}
	return "/tmp/demo"
}

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

func (f *fakeSvc) StashDiff(ctx context.Context, hash string) (string, error) {
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
func (f *fakeSvc) DiscardAll(ctx context.Context, paths ...string) (string, error) {
	return f.record("discard-all:" + strings.Join(paths, ","))
}
func (f *fakeSvc) Commit(ctx context.Context, msg string, amend bool) (string, error) {
	if amend {
		return f.record("amend")
	}
	return f.record("commit:" + msg)
}
func (f *fakeSvc) EditCommitMessage(ctx context.Context, sha, message string) (string, error) {
	return f.record("edit-message:" + sha)
}

func (f *fakeSvc) Squash(ctx context.Context, head, parent, message string) (string, error) {
	return f.record("squash:" + head)
}

func (f *fakeSvc) Checkout(ctx context.Context, ref string) (string, error) {
	return f.record("checkout:" + ref)
}
func (f *fakeSvc) CheckoutRemoteBranch(ctx context.Context, remoteRef string) (string, error) {
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
func (f *fakeSvc) DeleteRemoteBranch(ctx context.Context, remoteRef string) (string, error) {
	return f.record("delete-remote:" + remoteRef)
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
func (f *fakeSvc) StashApply(ctx context.Context, hash string) (string, error) {
	return f.record("stash-apply:" + hash)
}
func (f *fakeSvc) StashPop(ctx context.Context, hash string) (string, error) {
	return f.record("stash-pop:" + hash)
}
func (f *fakeSvc) StashDrop(ctx context.Context, hash string) (string, error) {
	return f.record("stash-drop:" + hash)
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
func (f *fakeSvc) CommitMessage(ctx context.Context, sha string) (string, error) {
	for _, c := range f.commits {
		if c.Hash == sha {
			return c.Subject + string(rune(10)) + string(rune(10)) + "body-" + c.Short, nil
		}
	}
	return "", nil
}

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

// sideY 返回左侧分支栏某可见行的屏幕 Y 坐标（扣掉项目区）。
func sideY(a *App, row int) int {
	return a.panelLayout().projH + 2 + row
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
		case spinnerTickMsg, autoRefreshTickMsg:
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
	a.Update(detailMsg{gen: a.detailGen, key: a.detailKey, sha: "aaaaaaaaaaaa1111", kind: "commit", title: "修复切换音频崩溃", detail: d, diff: "@@ -1 +1 @@\n-old\n+new"})
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
	click := tea.MouseMsg{Action: tea.MouseActionPress, Button: tea.MouseButtonLeft, X: 2, Y: sideY(a, idx)}
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
	a.Update(tea.MouseMsg{Action: tea.MouseActionPress, Button: tea.MouseButtonRight, X: 2, Y: sideY(a, idx)})
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
	a.Update(tea.MouseMsg{Action: tea.MouseActionPress, Button: tea.MouseButtonLeft, X: 2, Y: sideY(a, idx)})
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
	a.Update(tea.MouseMsg{Action: tea.MouseActionPress, Button: tea.MouseButtonLeft, X: 2, Y: sideY(a, idx)})
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
	if a.branchFilter != "refs/heads/feature/x" {
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
	a.Update(tea.MouseMsg{Action: tea.MouseActionPress, Button: tea.MouseButtonRight, X: 2, Y: sideY(a, idx)})
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
	order := []Pane{PaneInfo, PaneCommits, PaneFiles, PaneContent, PaneLog, PaneProjects, PaneBranches}
	for _, want := range order {
		sendKey(a, tea.KeyMsg{Type: tea.KeyTab})
		if a.pane != want {
			t.Fatalf("Tab 后焦点 = %v, 期望 %v", a.pane, want)
		}
	}
	sendKey(a, tea.KeyMsg{Type: tea.KeyShiftTab})
	if a.pane != PaneProjects {
		t.Fatalf("Shift+Tab 后焦点 = %v, 期望 PaneProjects", a.pane)
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
	sendKey(a, tea.KeyMsg{Type: tea.KeyRight, Alt: true})
	if a.pane != PaneContent {
		t.Fatalf("文件 alt+right = %v, 期望内容区", a.pane)
	}
	sendKey(a, tea.KeyMsg{Type: tea.KeyUp, Alt: true})
	if a.pane != PaneCommits {
		t.Fatalf("内容 alt+up = %v, 期望提交区（不是同排左边）", a.pane)
	}
	sendKey(a, tea.KeyMsg{Type: tea.KeyDown, Alt: true})
	sendKey(a, tea.KeyMsg{Type: tea.KeyRight, Alt: true})
	if a.pane != PaneContent {
		t.Fatalf("提交/文件 alt 序列 = %v", a.pane)
	}
	sendKey(a, tea.KeyMsg{Type: tea.KeyLeft, Alt: true})
	if a.pane != PaneFiles {
		t.Fatalf("内容 alt+left = %v, 期望文件区", a.pane)
	}
	sendKey(a, tea.KeyMsg{Type: tea.KeyLeft, Alt: true})
	if a.pane != PaneInfo {
		t.Fatalf("文件 alt+left = %v, 期望提交信息区", a.pane)
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
		if strings.Contains(c.label, "重置") {
			idx = i
		}
	}
	if idx < 0 {
		t.Fatal("提交菜单缺少重置")
	}
	_, x, y := a.choiceMenuBox()
	a.Update(tea.MouseMsg{Action: tea.MouseActionPress, Button: tea.MouseButtonLeft, X: x + 2, Y: y + 1 + idx})
	if a.modal == nil || a.modal.kind != modalChoice || !strings.Contains(a.modal.title, "重置") {
		t.Fatalf("点击重置应打开子菜单: %+v", a.modal)
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
	yTop := l.projH + 1 + l.leftTopH
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
	yTop = l.projH + 1 + l.leftTopH
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
	y := l.projH + 1 + l.leftTopH + 1
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
	a.projectH = 6
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
	if cfg.LeftWidth != 41 || cfg.ProjectH != 6 || cfg.DrawerLeft != 27 || cfg.RightTop != 9 ||
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
	click(2, l.projH+1)
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
	// 制造一个与远端有差异的分支，确保推送/强制推送项存在
	a.refs = append(a.refs, git.Ref{Kind: git.RefRemote, Name: "origin/feature/x", Target: "cccccccccccc3333"})
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
		if !strings.Contains(line, "[*main|origin]") {
			t.Errorf("提交行缺少分支徽标: %q", line)
		}
		if !strings.Contains(line, "[v9.9]") {
			t.Errorf("提交行缺少 tag 徽标: %q", line)
		}
		if strings.Index(line, "[*main|origin]") > strings.Index(line, "[v9.9]") {
			t.Errorf("分支徽标应排在 tag 前: %q", line)
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

// TestCommitCtrlRangeSelection Ctrl+点击可选择连续提交区间。
func TestCommitCtrlRangeSelection(t *testing.T) {
	a, _ := newFakeApp(t)
	a.pane = PaneCommits
	a.graphSel = a.dirtyOffset()
	l := a.panelLayout()
	click := func(row int, ctrl bool) {
		a.Update(tea.MouseMsg{Action: tea.MouseActionPress, Button: tea.MouseButtonLeft, Ctrl: ctrl, X: l.leftW + 3, Y: 1 + row})
	}
	click(a.dirtyOffset(), true)
	click(a.dirtyOffset()+1, true)
	if got := a.commitSelectionCount(); got != 2 {
		t.Fatalf("多选数量 = %d，期望 2", got)
	}
	click(a.dirtyOffset()+2, true)
	if got := a.commitSelectionCount(); got != 3 {
		t.Fatalf("扩展后多选数量 = %d，期望 3", got)
	}
	click(a.dirtyOffset()+1, false)
	if a.commitSelectionCount() != 0 || a.commitAnchor != -1 {
		t.Fatal("普通点击应清除多选")
	}
}

// TestCommitSquashMenuAndPlan 多选后菜单出现合并项，计划正确。
func TestCommitSquashMenuAndPlan(t *testing.T) {
	a, _ := newFakeApp(t)
	a.status.Staged = nil
	a.commitAnchor = a.dirtyOffset()
	a.graphSel = a.dirtyOffset() + 1
	c := a.selectedCommit()
	if c == nil {
		t.Fatal("没有选中提交")
	}
	labels := ""
	for _, ch := range a.commitMenuChoices(c) {
		labels += ch.label + "\n"
	}
	if !strings.Contains(labels, "合并 2 个提交") {
		t.Fatalf("菜单缺少合并项:\n%s", labels)
	}
	if !strings.Contains(labels, "编辑提交信息") {
		t.Fatalf("菜单缺少编辑提交信息:\n%s", labels)
	}
	hashes, head, parent, msg, err := a.squashPlan()
	if err != nil {
		t.Fatalf("squashPlan: %v", err)
	}
	if len(hashes) != 2 || head != "aaaaaaaaaaaa1111" || parent != "cccccccccccc3333" {
		t.Fatalf("计划错误: hashes=%v head=%s parent=%s", hashes, head, parent)
	}
	if !strings.Contains(msg, "修复切换音频崩溃") || !strings.Contains(msg, "支持摄像头分辨率") {
		t.Fatalf("默认提交信息 = %q", msg)
	}
}

// TestCommitEditMessagePrompt 读取提交信息后应打开可编辑弹窗。
func TestCommitEditMessagePrompt(t *testing.T) {
	a, _ := newFakeApp(t)
	a.Update(commitMessageMsg{gen: a.projectGen, svc: a.svc, sha: "aaaaaaaaaaaa1111", message: "旧提交信息"})
	if a.modal == nil || a.modal.kind != modalText {
		t.Fatalf("未打开提交信息编辑框: %+v", a.modal)
	}
	if !strings.Contains(a.modal.text.Value(), "旧提交信息") {
		t.Fatalf("编辑框内容 = %q", a.modal.text.Value())
	}
}

func TestHistoryPromptsIgnorePreviousProject(t *testing.T) {
	a, svc := newFakeApp(t)
	oldGen := a.projectGen
	a.resetForProject()
	a.Update(commitMessageMsg{gen: oldGen, svc: svc, sha: "aaaaaaaaaaaa1111", message: "旧项目", err: errors.New("旧项目错误")})
	if a.modal != nil || a.toastErr {
		t.Fatalf("旧项目的提交信息响应仍生效: modal=%+v toast=%q", a.modal, a.toast)
	}
	a.status.OID = "aaaaaaaaaaaa1111"
	a.Update(squashMessagesMsg{gen: oldGen, svc: svc, hashes: []string{"aaaaaaaaaaaa1111", "bbbbbbbbbbbb2222"}, head: "aaaaaaaaaaaa1111", parent: "cccccccccccc3333", msg: "旧项目"})
	if a.modal != nil {
		t.Fatalf("旧项目的合并信息响应仍打开弹窗: %+v", a.modal)
	}
}

func TestHistoryRewriteUsesRequestRepo(t *testing.T) {
	a, current := newFakeApp(t)
	original := &fakeSvc{}
	a.editCommitMessagePrompt(original, "aaaaaaaaaaaa1111", "old")
	_ = a.modal.onSubmit(a, "new")
	op := firstOperationCmd(t, a.modal.onConfirm(a))
	a.Update(op())
	if len(current.calls) != 0 || len(original.calls) != 1 || original.calls[0] != "edit-message:aaaaaaaaaaaa1111" {
		t.Fatalf("提交信息改写使用了错误仓库: current=%v original=%v", current.calls, original.calls)
	}

	a.openSquashPrompt(original, []string{"aaaaaaaaaaaa1111", "bbbbbbbbbbbb2222"}, "aaaaaaaaaaaa1111", "cccccccccccc3333", "old")
	_ = a.modal.onSubmit(a, "new")
	op = firstOperationCmd(t, a.modal.onConfirm(a))
	a.Update(op())
	if len(current.calls) != 0 || len(original.calls) != 2 || original.calls[1] != "squash:aaaaaaaaaaaa1111" {
		t.Fatalf("合并提交使用了错误仓库: current=%v original=%v", current.calls, original.calls)
	}
}

// TestCommitFileMenu 查看提交文件时右键应提供打开本地文件与复制路径。
func TestCommitFileMenu(t *testing.T) {
	a, _ := newFakeApp(t)
	a.pane = PaneCommits
	a.graphSel = a.dirtyOffset()
	pump(t, a, a.loadFilesForSelection())
	a.pane = PaneFiles
	a.filesOwner = "c:aaaaaaaaaaaa1111"
	a.fileSel = 0
	a.Update(tea.MouseMsg{Action: tea.MouseActionPress, Button: tea.MouseButtonRight, X: a.panelLayout().leftW + 2, Y: a.panelLayout().rightTopH + 2})
	if a.modal == nil {
		t.Fatal("提交文件右键应弹菜单")
	}
	for _, want := range []string{"文件管理器中打开", "复制路径"} {
		found := false
		for _, c := range a.modal.choices {
			if strings.Contains(c.label, want) {
				found = true
			}
		}
		if !found {
			t.Fatalf("提交文件菜单缺少 %q: %+v", want, a.modal.choices)
		}
	}
}

// TestDetailPageUpDown 详情浮层支持 PageUp/PageDown 整页翻页。
func TestDetailPageUpDown(t *testing.T) {
	a, _ := newFakeApp(t)
	a.overlay = overlayDetail
	a.detail.lines = nil
	for i := 0; i < 200; i++ {
		a.detail.lines = append(a.detail.lines, fmt.Sprintf("line %d", i))
	}
	a.detail.offset = 0
	a.Update(tea.KeyMsg{Type: tea.KeyPgDown})
	if a.detail.offset != a.pageSize() {
		t.Fatalf("PageDown 后 offset = %d，期望 %d", a.detail.offset, a.pageSize())
	}
	a.Update(tea.KeyMsg{Type: tea.KeyPgUp})
	if a.detail.offset != 0 {
		t.Fatalf("PageUp 后 offset = %d，期望 0", a.detail.offset)
	}
}

// TestUpdateBadgeHover 新版本徽标应可见，悬停提示 --update。
func TestUpdateBadgeHover(t *testing.T) {
	a, _ := newFakeApp(t)
	a.updateAvailable = true
	a.latestVersion = "v9.9.9"
	a.Update(tea.WindowSizeMsg{Width: 120, Height: 40})
	_ = a.View()
	if a.updateBadgeW == 0 {
		t.Fatal("新版本徽标未渲染")
	}
	a.Update(tea.MouseMsg{Action: tea.MouseActionMotion, X: a.updateBadgeX + 1, Y: a.height - 1})
	if !strings.Contains(a.toast, "gitc --update") {
		t.Fatalf("悬停提示 = %q", a.toast)
	}
}

// TestHelpContainsGitHubLinks 帮助页应包含源码/Release 地址。
func TestHelpContainsGitHubLinks(t *testing.T) {
	all := strings.Join(helpLines(), "\n")
	for _, want := range []string{"https://github.com/ssk-wh/gitc", "/releases", "/issues"} {
		if !strings.Contains(all, want) {
			t.Errorf("帮助页缺少 %q", want)
		}
	}
}

// TestUpdateCheckSkippedForLocalBuild 本地构建不做自动更新检查。
func TestUpdateCheckSkippedForLocalBuild(t *testing.T) {
	t.Cleanup(func() { SetVersion("") })
	SetVersion("")
	a, _ := newFakeApp(t)
	if a.checkUpdateCmd() != nil {
		t.Error("本地构建不应发起更新检查")
	}
}

// TestHelpProjectInfoFirst GitHub 信息应位于顶部独立区域，排在快捷键之前。
func TestHelpProjectInfoFirst(t *testing.T) {
	all := strings.Join(helpLines(), string(rune(10)))
	github := strings.Index(all, "GitHub")
	keys := strings.Index(all, "↑↓")
	if github < 0 || keys < 0 {
		t.Fatalf("帮助页缺少 GitHub 信息或快捷键: %q", all)
	}
	if github > keys {
		t.Fatalf("GitHub 信息应位于快捷键之前: github=%d keys=%d", github, keys)
	}
}

// TestTagMenuFixedActions 标签菜单固定展示推送与删除入口，不再异步查询远端状态。
func TestTagMenuFixedActions(t *testing.T) {
	a, _ := newFakeApp(t)
	a.pane = PaneBranches
	for i, it := range a.sideItems {
		if it.kind == sideTag && it.ref.Name == "v1.0" {
			a.sideSel = i
			a.sideSelKey = it.key
			break
		}
	}
	cmd := a.openContextMenu()
	if a.modal == nil {
		t.Fatal("标签菜单应立即打开")
	}
	if cmd != nil {
		t.Fatal("标签菜单不应再异步查询远端状态")
	}
	has := func(list []choice, sub string) bool {
		for _, c := range list {
			if strings.Contains(c.label, sub) {
				return true
			}
		}
		return false
	}
	for _, want := range []string{"推送标签", "删除标签", "删除远端标签"} {
		if !has(a.modal.choices, want) {
			t.Fatalf("标签菜单缺少固定项 %q: %+v", want, a.modal.choices)
		}
	}
}

// TestParseTagInput 两行输入解析：第一行名称，第二行备注。
func TestParseTagInput(t *testing.T) {
	cases := []struct{ in, name, msg string }{
		{"v1.0\n备注", "v1.0", "备注"},
		{"v1.0", "v1.0", ""},
		{"v1.0:备注", "v1.0", "备注"},
		{"\n备注", "", "备注"},
	}
	for _, c := range cases {
		name, msg := parseTagInput(c.in)
		if name != c.name || msg != c.msg {
			t.Errorf("parseTagInput(%q) = (%q,%q), 期望 (%q,%q)", c.in, name, msg, c.name, c.msg)
		}
	}
}

// TestTagPromptTwoLines 打标签输入框高度为两行。
func TestTagPromptTwoLines(t *testing.T) {
	a, _ := newFakeApp(t)
	a.tagPrompt(&git.Commit{Hash: "aaaaaaaaaaaa1111"})
	if a.modal == nil || a.modal.kind != modalText {
		t.Fatalf("未打开多行输入框: %+v", a.modal)
	}
	if h := a.modal.text.Height(); h != 2 {
		t.Fatalf("输入框高度 = %d，期望 2", h)
	}
}

// TestBranchMenuPushVisibility 分支已推送时不再显示推送/强制推送。
func TestBranchMenuPushVisibility(t *testing.T) {
	labels := func(a *App, ref git.Ref) string {
		var b strings.Builder
		for _, c := range a.branchMenuChoices(&sideItem{kind: sideBranch, ref: ref}) {
			b.WriteString(c.label + "\n")
		}
		return b.String()
	}
	a, _ := newFakeApp(t)
	main := git.Ref{Name: "main", Kind: git.RefLocal, Target: "aaaaaaaaaaaa1111", Upstream: "origin/main"}
	l := labels(a, main)
	if strings.Contains(l, "推送") {
		t.Fatalf("已推送分支仍显示推送:\n%s", l)
	}

	feat := git.Ref{Name: "feature/x", Kind: git.RefLocal, Target: "bbbbbbbbbbbb2222"}
	l = labels(a, feat)
	if !strings.Contains(l, "推送该分支") || strings.Contains(l, "强制推送") {
		t.Fatalf("无远端分支菜单错误:\n%s", l)
	}

	a.refs = append(a.refs, git.Ref{Name: "origin/feature/x", Kind: git.RefRemote, Target: "bbbbbbbbbbbb2222"})
	if l = labels(a, feat); strings.Contains(l, "推送") {
		t.Fatalf("远端同进度仍显示推送:\n%s", l)
	}
	a.refs[len(a.refs)-1].Target = "cccccccccccc3333"
	l = labels(a, feat)
	if !strings.Contains(l, "推送该分支") || !strings.Contains(l, "强制推送") {
		t.Fatalf("远端有差异应显示推送/强制推送:\n%s", l)
	}
}

// TestProjectPaneSwitch 项目区展示所有项目，点击行可切换。
func TestProjectPaneSwitch(t *testing.T) {
	a, _ := newFakeApp(t)
	dirA, dirB := t.TempDir(), t.TempDir()
	a.SetProjects([]string{dirA, dirB}, dirA)
	if len(a.projects) != 2 {
		t.Fatalf("项目数 = %d", len(a.projects))
	}
	if a.panelLayout().projH < 3 {
		t.Fatalf("项目区高度 = %d", a.panelLayout().projH)
	}
	next := &fakeSvc{root: dirB, status: git.Status{Branch: "dev"}}
	a.openRepo = func(path string) (git.Service, error) { return next, nil }
	idx := a.projectIndex(dirB)
	a.Update(tea.MouseMsg{Action: tea.MouseActionPress, Button: tea.MouseButtonLeft, X: 2, Y: 1 + idx})
	if a.svc != next || a.currentPath != cleanProjectPath(dirB) {
		t.Fatalf("未切换到目标项目: svc=%v path=%q", a.svc, a.currentPath)
	}
	if len(a.commits) != 0 || len(a.refs) != 0 {
		t.Error("切换后旧仓库状态未清空")
	}
	if a.projectSel != idx {
		t.Errorf("projectSel = %d, 期望 %d", a.projectSel, idx)
	}
}

// TestRemoveProject 非当前项目可移除，移除后恢复单项目布局。
func TestRemoveProject(t *testing.T) {
	a, _ := newFakeApp(t)
	dirA, dirB := t.TempDir(), t.TempDir()
	a.SetProjects([]string{dirA, dirB}, dirA)
	if err := a.removeProject(projectEntry{Name: "b", Path: cleanProjectPath(dirB)}); err != nil {
		t.Fatal(err)
	}
	if len(a.projects) != 1 {
		t.Fatalf("移除后项目数 = %d", len(a.projects))
	}
	if a.projectIndex(cleanProjectPath(dirB)) >= 0 {
		t.Error("已移除项目仍在列表中")
	}
}

// TestProjectPaneMenu 项目区标题右键提供添加项目；添加走系统选择框命令。
func TestProjectPaneMenu(t *testing.T) {
	a, _ := newFakeApp(t)
	dirA := t.TempDir()
	a.SetProjects([]string{dirA}, dirA)
	_ = a.View()
	a.Update(tea.MouseMsg{Action: tea.MouseActionPress, Button: tea.MouseButtonRight, X: 2, Y: 0})
	if a.modal == nil || a.modal.kind != modalChoice {
		t.Fatalf("项目区标题右键未弹菜单: %+v", a.modal)
	}
	found := false
	for _, c := range a.modal.choices {
		if strings.Contains(c.label, "添加项目") {
			found = true
			if cmd := c.action(a); cmd == nil {
				t.Fatal("添加项目应返回系统选择框命令")
			}
		}
	}
	if !found {
		t.Fatalf("菜单缺少添加项目: %+v", a.modal.choices)
	}
}

// TestProjectDirPickerChoose 目录选择框可直接选择当前目录添加项目。
func TestProjectDirPickerChoose(t *testing.T) {
	a, _ := newFakeApp(t)
	dir := t.TempDir()
	if err := os.Mkdir(filepath.Join(dir, "sub"), 0o755); err != nil {
		t.Fatal(err)
	}
	a.openRepo = func(p string) (git.Service, error) { return &fakeSvc{root: p}, nil }
	a.openProjectPicker()
	a.loadProjectDir(dir)
	if a.modal == nil || a.modal.kind != modalDirPicker {
		t.Fatalf("目录选择框未打开: %+v", a.modal)
	}
	if !strings.Contains(strings.Join(a.modal.dirs, ","), "sub") {
		t.Fatalf("目录列表缺少 sub: %v", a.modal.dirs)
	}
	a.modal.sel = 0
	a.Update(tea.KeyMsg{Type: tea.KeyEnter})
	if a.modal != nil {
		t.Fatalf("选择目录后模态框未关闭: %+v", a.modal)
	}
	if a.projectIndex(cleanProjectPath(dir)) < 0 {
		t.Fatalf("项目未添加: %+v", a.projects)
	}
}

// TestSystemDirPickAddsProject 系统目录选择结果直接添加项目。
func TestSystemDirPickAddsProject(t *testing.T) {
	a, _ := newFakeApp(t)
	dir := t.TempDir()
	a.openRepo = func(p string) (git.Service, error) { return &fakeSvc{root: p}, nil }
	a.pickDir = func() (string, error) { return dir, nil }
	cmd := a.pickProjectDirCmd()
	if cmd == nil {
		t.Fatal("未返回系统选择框命令")
	}
	a.Update(cmd())
	if a.projectIndex(cleanProjectPath(dir)) < 0 {
		t.Fatalf("项目未添加: %+v", a.projects)
	}
}

func TestSystemDirPickIgnoresPreviousProject(t *testing.T) {
	a, _ := newFakeApp(t)
	dir := t.TempDir()
	a.pickDir = func() (string, error) { return dir, nil }
	cmd := a.pickProjectDirCmd()
	a.resetForProject()
	a.Update(cmd())
	if a.projectIndex(cleanProjectPath(dir)) >= 0 {
		t.Fatal("旧项目的目录选择结果仍被添加")
	}
}

// TestSystemDirPickFallback 系统选择框不可用时回退内置选择器。
func TestSystemDirPickFallback(t *testing.T) {
	a, _ := newFakeApp(t)
	a.pickDir = func() (string, error) { return "", platform.ErrUnavailable }
	a.Update(a.pickProjectDirCmd()())
	if a.modal == nil || a.modal.kind != modalDirPicker {
		t.Fatalf("未回退到内置选择器: %+v", a.modal)
	}
}

// TestProjectPaneKeyboard 项目区上下移动不切换，Enter 才切换。
func TestProjectPaneKeyboard(t *testing.T) {
	a, _ := newFakeApp(t)
	dirA, dirB := t.TempDir(), t.TempDir()
	a.SetProjects([]string{dirA, dirB}, dirA)
	next := &fakeSvc{status: git.Status{Branch: "dev"}}
	a.openRepo = func(path string) (git.Service, error) { return next, nil }
	a.pane = PaneProjects
	a.projectSel = a.projectIndex(cleanProjectPath(dirA))
	old := a.currentPath
	a.Update(tea.KeyMsg{Type: tea.KeyDown})
	if a.projectSel != 1 || a.currentPath != old {
		t.Fatalf("下移后 sel=%d path=%q", a.projectSel, a.currentPath)
	}
	a.Update(tea.KeyMsg{Type: tea.KeyEnter})
	if a.svc != next {
		t.Fatal("Enter 未切换项目")
	}
}

// TestProjectPaneMenuRemove 非当前项目菜单可移除。
func TestProjectPaneMenuRemove(t *testing.T) {
	a, _ := newFakeApp(t)
	dirA, dirB := t.TempDir(), t.TempDir()
	a.SetProjects([]string{dirA, dirB}, dirA)
	a.pane = PaneProjects
	a.projectSel = a.projectIndex(cleanProjectPath(dirB))
	a.openProjectMenu(a.projectSel)
	if a.modal == nil {
		t.Fatal("项目菜单未打开")
	}
	removed := false
	for _, c := range a.modal.choices {
		if strings.Contains(c.label, "移除项目") {
			removed = true
			_ = c.action(a)
		}
	}
	if !removed {
		t.Fatalf("菜单缺少移除项: %+v", a.modal.choices)
	}
	if a.projectIndex(cleanProjectPath(dirB)) >= 0 {
		t.Error("项目未移除")
	}
}

// TestProjectPaneScroll 项目多时滚动可视区跟随选择。
func TestProjectPaneScroll(t *testing.T) {
	a, _ := newFakeApp(t)
	var dirs []string
	for i := 0; i < 10; i++ {
		dirs = append(dirs, t.TempDir())
	}
	a.SetProjects(dirs, dirs[0])
	a.pane = PaneProjects
	a.projectSel = len(a.projects) - 1
	a.ensureProjectVisible()
	if a.projectOff == 0 {
		t.Fatal("项目区未滚动")
	}
	if h := a.projectViewHeight(); a.projectSel >= a.projectOff+h {
		t.Fatalf("最后一项不可见: sel=%d off=%d h=%d", a.projectSel, a.projectOff, h)
	}
}

// TestProjectBranchesTwoColumns 项目行显示名称与分支两列，分支起始列对齐。
func TestProjectBranchesTwoColumns(t *testing.T) {
	a, _ := newFakeApp(t)
	dirA, dirB := t.TempDir(), t.TempDir()
	a.SetProjects([]string{dirA, dirB}, dirA)
	a.status.Branch = "main"
	a.projectBranch = map[string]string{cleanProjectPath(dirA): "main", cleanProjectPath(dirB): "dev"}
	view := ansi.Strip(a.viewProjectsPanel(40, 4))
	lines := strings.Split(view, string(rune(10)))
	mainCol, devCol := -1, -1
	for _, ln := range lines {
		if i := strings.Index(ln, "main"); i >= 0 {
			mainCol = i
		}
		if i := strings.Index(ln, "dev"); i >= 0 {
			devCol = i
		}
	}
	if mainCol < 0 || devCol < 0 {
		t.Fatalf("项目行缺少分支列: %s", view)
	}
	if mainCol != devCol {
		t.Fatalf("分支列未对齐: main=%d dev=%d: %s", mainCol, devCol, view)
	}
}

// TestLoadProjectBranches 异步命令加载所有项目当前分支。
func TestLoadProjectBranches(t *testing.T) {
	a, _ := newFakeApp(t)
	dirA, dirB := t.TempDir(), t.TempDir()
	a.SetProjects([]string{dirA, dirB}, dirA)
	a.openRepo = func(p string) (git.Service, error) {
		branch := "a"
		if strings.Contains(p, filepath.Base(dirB)) {
			branch = "b"
		}
		return &fakeSvc{root: p, status: git.Status{Branch: branch}}, nil
	}
	cmd := a.loadProjectBranchesCmd()
	if cmd == nil {
		t.Fatal("未返回分支加载命令")
	}
	a.Update(cmd())
	if a.projectBranch[cleanProjectPath(dirA)] == "" || a.projectBranch[cleanProjectPath(dirB)] == "" {
		t.Fatalf("分支未加载: %+v", a.projectBranch)
	}
}

func TestProjectMetadataIgnoresSupersededResponses(t *testing.T) {
	a, _ := newFakeApp(t)
	root := t.TempDir()
	a.SetProjects([]string{root}, root)
	stale := []git.Worktree{{Path: cleanProjectPath(root), Branch: "stale", Main: true}}
	a.Update(worktreesMsg{gen: a.worktreesGen - 1, list: map[string][]git.Worktree{cleanProjectPath(root): stale}})
	if len(a.worktrees[cleanProjectPath(root)]) != 0 {
		t.Fatal("过期的 worktree 响应仍被应用")
	}
	current := []git.Worktree{{Path: cleanProjectPath(root), Branch: "main", Main: true}}
	a.Update(worktreesMsg{gen: a.worktreesGen, list: map[string][]git.Worktree{cleanProjectPath(root): current}})
	if len(a.worktrees[cleanProjectPath(root)]) != 1 {
		t.Fatal("当前 worktree 响应未应用")
	}

	a.Update(projectBranchesMsg{gen: a.projectBranchesGen - 1, branches: map[string]string{cleanProjectPath(root): "stale"}})
	if a.projectBranch[cleanProjectPath(root)] == "stale" {
		t.Fatal("过期的分支响应仍被应用")
	}
	a.Update(projectBranchesMsg{gen: a.projectBranchesGen, branches: map[string]string{cleanProjectPath(root): "main"}})
	if a.projectBranch[cleanProjectPath(root)] != "main" {
		t.Fatal("当前分支响应未应用")
	}
}

func TestWorktreeLoadCoalescesUntilResponse(t *testing.T) {
	a, _ := newFakeApp(t)
	if a.loadWorktreesCmd() == nil {
		t.Fatal("首次 worktree 加载未启动")
	}
	if a.loadWorktreesCmd() != nil {
		t.Fatal("worktree 加载中不应重复启动")
	}
	a.worktreesGen++
	a.Update(worktreesMsg{gen: a.worktreesGen - 1})
	if a.worktreesLoading {
		t.Fatal("过期响应未解除加载状态")
	}
	if a.loadWorktreesCmd() == nil {
		t.Fatal("响应结束后应允许再次加载")
	}
}

// TestWorkspaceStartupAndAdd 启动读取 .gitc-workspace，添加项目时写回。
func TestWorkspaceStartupAndAdd(t *testing.T) {
	isolateConfig(t)
	root := t.TempDir()
	other := t.TempDir()
	if err := workspace.Save(root, []string{root, other}); err != nil {
		t.Fatal(err)
	}
	a, _ := newFakeApp(t)
	a.svc = &fakeSvc{root: root}
	a.currentPath = cleanProjectPath(root)
	a.currentProjectPath = cleanProjectPath(root)
	a.SetConfig(config.File{})
	if a.projectIndex(cleanProjectPath(other)) < 0 {
		t.Fatalf("未读取 workspace 项目: %+v", a.projects)
	}
	extra := t.TempDir()
	a.openRepo = func(p string) (git.Service, error) { return &fakeSvc{root: p}, nil }
	a.chooseProjectDir(extra)
	f, err := workspace.Load(root)
	if err != nil {
		t.Fatal(err)
	}
	found := false
	for _, p := range f.Projects {
		if cleanProjectPath(p) == cleanProjectPath(extra) {
			found = true
		}
	}
	if !found {
		t.Fatalf("workspace 未写入新项目: %+v", f.Projects)
	}
}

// TestProjectMenuUsesClickedProject 右键菜单以被点击项目为基准。
func TestProjectMenuUsesClickedProject(t *testing.T) {
	a, _ := newFakeApp(t)
	dirs := []string{t.TempDir(), t.TempDir(), t.TempDir()}
	a.SetProjects(dirs, dirs[0])
	a.openProjectMenu(1)
	if a.modal == nil {
		t.Fatal("菜单未打开")
	}
	text := ""
	for _, c := range a.modal.choices {
		text += c.label + string(rune(10))
	}
	b := filepath.Base(cleanProjectPath(dirs[1]))
	if n := strings.Count(text, "切换到 "+b); n != 1 {
		t.Fatalf("被点击项目应只出现一次切换入口，实际 %d 次: %s", n, text)
	}
	if !strings.Contains(text, "移除项目 "+b) {
		t.Fatalf("应可移除被点击项目: %s", text)
	}
	name := filepath.Base(cleanProjectPath(dirs[2]))
	if !strings.Contains(text, "切换到 "+name) {
		t.Fatalf("缺少切换到 %s: %s", name, text)
	}
}

// TestFooterHintsRightAligned 按键说明应靠右，左侧提示过长时省略。
func TestFooterHintsRightAligned(t *testing.T) {
	a, _ := newFakeApp(t)
	a.Update(tea.WindowSizeMsg{Width: 120, Height: 40})
	a.toast = strings.Repeat("已复制一段很长的提交信息用于测试底部截断", 3)
	v := ansi.Strip(a.viewFooter())
	plain := strings.TrimRight(v, " ")
	hint := strings.TrimRight(ansi.Strip(a.contextHints()), " ")
	if !strings.HasSuffix(plain, hint) {
		t.Fatalf("按键说明未靠右: %q", plain)
	}
	if !strings.Contains(plain, "…") {
		t.Fatalf("左侧过长时应省略: %q", plain)
	}
	if strWidth(v) > a.width {
		t.Fatalf("footer 超宽: %d", strWidth(v))
	}
}

// TestAltFocusLayoutOrder 固定 Alt+方向键顺序：左半区自上而下，右半区自上而下、同排从左到右。
func TestAltFocusLayoutOrder(t *testing.T) {
	a, _ := newFakeApp(t)
	a.Update(tea.WindowSizeMsg{Width: 120, Height: 40})
	a.commands = []commandEntry{{at: time.Now(), line: "git status"}}
	cases := []struct {
		from Pane
		dir  string
		want Pane
	}{
		{PaneProjects, "down", PaneBranches},
		{PaneBranches, "down", PaneInfo},
		{PaneInfo, "down", PaneInfo},
		{PaneInfo, "up", PaneBranches},
		{PaneBranches, "up", PaneProjects},
		{PaneProjects, "up", PaneProjects},

		{PaneCommits, "down", PaneFiles},
		{PaneFiles, "down", PaneLog},
		{PaneContent, "down", PaneLog},
		{PaneLog, "down", PaneLog},
		{PaneLog, "up", PaneFiles},
		{PaneContent, "up", PaneCommits},
		{PaneFiles, "up", PaneCommits},
		{PaneCommits, "up", PaneCommits},

		{PaneProjects, "right", PaneCommits},
		{PaneBranches, "right", PaneCommits},
		{PaneInfo, "right", PaneFiles},
		{PaneFiles, "right", PaneContent},
		{PaneContent, "right", PaneContent},
		{PaneCommits, "left", PaneBranches},
		{PaneFiles, "left", PaneInfo},
		{PaneContent, "left", PaneFiles},
		{PaneLog, "left", PaneInfo},
	}
	for _, c := range cases {
		a.pane = c.from
		a.altFocus(c.dir)
		if a.pane != c.want {
			t.Errorf("%v Alt+%s = %v，期望 %v", c.from, c.dir, a.pane, c.want)
		}
	}
	// 无命令历史时不进入隐藏的命令区
	a.commands = nil
	for _, p := range []Pane{PaneFiles, PaneContent} {
		a.pane = p
		a.altFocus("down")
		if a.pane != p {
			t.Errorf("命令区隐藏时 %v Alt+Down = %v", p, a.pane)
		}
	}
}

// TestFilesHintFollowsStagedState 文件区按键提示随暂存状态变化。
func TestFilesHintFollowsStagedState(t *testing.T) {
	a, _ := newFakeApp(t)
	a.pane = PaneFiles
	a.filesOwner = "work"
	a.setFiles([]fileEntry{{path: "a.txt", status: "M", staged: true}})
	a.fileSel = 0
	if h := a.contextHints(); !strings.Contains(h, "取消暂存") {
		t.Fatalf("已暂存提示 = %q", h)
	}
	a.setFiles([]fileEntry{{path: "a.txt", status: "M"}})
	a.fileSel = 0
	if h := a.contextHints(); !strings.Contains(h, "space 暂存") || strings.Contains(h, "取消暂存") {
		t.Fatalf("未暂存提示 = %q", h)
	}
	a.filesOwner = "c:aaaaaaaaaaaa1111"
	a.setFiles([]fileEntry{{path: "a.txt", status: "M"}})
	a.fileSel = 0
	if h := a.contextHints(); strings.Contains(h, "space") {
		t.Fatalf("提交文件不应提示暂存 = %q", h)
	}
}

// TestModalFloatsOverContent 弹窗必须浮在主界面之上，背景内容仍可见。
func TestModalFloatsOverContent(t *testing.T) {
	a, _ := newFakeApp(t)
	a.modal = &modal{kind: modalConfirm, title: "确认操作"}
	v := plainView(a)
	if !strings.Contains(v, "确认操作") {
		t.Fatal("弹窗标题未显示")
	}
	if !strings.Contains(v, "修复切换音频崩溃") {
		t.Fatal("弹窗遮住了主界面内容（应保留背景）")
	}
	a.modal = &modal{kind: modalInput, title: "输入内容", input: newTextInput("")}
	v = plainView(a)
	if !strings.Contains(v, "输入内容") || !strings.Contains(v, "修复切换音频崩溃") {
		t.Fatal("输入框未浮在主界面上")
	}
}

// TestLongBranchBadgeVisible 长分支名要截断显示，不能整条徽标消失。
func TestLongBranchBadgeVisible(t *testing.T) {
	a, _ := newFakeApp(t)
	long := "fix/device-space-text-baseline-20260904"
	a.refs = append(a.refs, git.Ref{Name: long, Kind: git.RefLocal, Target: "aaaaaaaaaaaa1111"})
	a.rebuildCommitColors()
	for _, line := range strings.Split(plainView(a), string(rune(10))) {
		if strings.Contains(line, "[HEAD]") {
			if !strings.Contains(line, "fix/device-space") {
				t.Fatalf("长分支名徽标未显示: %q", line)
			}
			return
		}
	}
	t.Fatal("未找到 HEAD 行")
}

// TestCheckoutWorktreeHint 分支被其他工作树占用时给出明确提示。
func TestCheckoutWorktreeHint(t *testing.T) {
	a, _ := newFakeApp(t)
	a.busy = true
	a.opID = 1
	a.Update(opDoneMsg{id: 1, name: "checkout fix/x", err: errors.New("fatal: 'fix/x' is already checked out at '/tmp/wt'")})
	if !strings.Contains(a.toast, "工作树") && !strings.Contains(a.toast, "worktree") {
		t.Fatalf("缺少工作树提示: %q", a.toast)
	}
}

// TestProjectCacheRestore 切回已打开过的项目应先恢复缓存再后台刷新。
func TestProjectCacheRestore(t *testing.T) {
	a, _ := newFakeApp(t)
	dirA, dirB := t.TempDir(), t.TempDir()
	a.SetProjects([]string{dirA, dirB}, dirA)
	a.currentPath = cleanProjectPath(dirA)
	a.svc = &fakeSvc{root: dirA, status: git.Status{Branch: "main"}}
	a.commits = sampleCommits()
	a.refs = []git.Ref{{Name: "main", Kind: git.RefLocal, Target: "aaaaaaaaaaaa1111", Head: true}}
	a.status = git.Status{Branch: "main", OID: "aaaaaaaaaaaa1111"}
	a.openRepo = func(p string) (git.Service, error) {
		return &fakeSvc{root: p, status: git.Status{Branch: "dev"}}, nil
	}
	cmd := a.switchProject(a.projectIndex(cleanProjectPath(dirB)))
	if cmd == nil {
		t.Fatal("切换项目未返回命令")
	}
	if len(a.commits) != 0 {
		t.Fatal("无缓存项目应重新加载")
	}
	cmd = a.switchProject(a.projectIndex(cleanProjectPath(dirA)))
	if cmd == nil {
		t.Fatal("切回项目未返回命令")
	}
	if len(a.commits) != len(sampleCommits()) || a.commits[0].Hash != "aaaaaaaaaaaa1111" {
		t.Fatalf("切回未恢复缓存: %+v", a.commits)
	}
	if a.projectCache[cleanProjectPath(dirA)] == nil {
		t.Fatal("项目缓存未保存")
	}
	// 缓存一致时，校验刷新不应重载提交历史
	a.verifyAfterRestore = true
	a.Update(refreshMsg{gen: a.refreshGen, quick: true, status: a.status, refs: a.refs})
	if a.logPreserve {
		t.Fatal("缓存未变化时不应重载提交历史")
	}
	if a.verifyAfterRestore {
		t.Fatal("校验标记未清除")
	}
	// 缓存过期时应后台重载
	a.verifyAfterRestore = true
	changed := append([]git.Ref(nil), a.refs...)
	changed[0].Target = "cccccccccccc3333"
	a.Update(refreshMsg{gen: a.refreshGen, quick: true, status: a.status, refs: changed})
	if !a.logPreserve {
		t.Fatal("缓存过期时应重载提交历史")
	}
}

func TestProjectCacheTracksLogCompleteness(t *testing.T) {
	a, _ := newFakeApp(t)
	root := cleanProjectPath(t.TempDir())
	a.currentPath = root
	a.commits = make([]git.Commit, maxCachedCommits+1)
	a.saveProjectCache()
	if a.projectCache[root].logComplete {
		t.Fatal("被截断的提交缓存不应标记为完整")
	}
	a.commits = a.commits[:maxCachedCommits]
	a.saveProjectCache()
	if !a.projectCache[root].logComplete {
		t.Fatal("完整的小型提交缓存应标记为完整")
	}
	a.loadingLog = true
	a.saveProjectCache()
	if a.projectCache[root].logComplete {
		t.Fatal("仍在加载的提交缓存不应标记为完整")
	}
	a.Update(logBatchMsg{gen: a.logGen, done: true, err: errors.New("读取日志失败")})
	a.saveProjectCache()
	if a.projectCache[root].logComplete {
		t.Fatal("加载失败的提交缓存不应标记为完整")
	}
}

func TestProjectCacheRestoreReloadsIncompleteLog(t *testing.T) {
	a, _ := newFakeApp(t)
	root := cleanProjectPath(t.TempDir())
	a.currentPath = root
	a.commits = sampleCommits()
	a.refs = []git.Ref{{Name: "main", Kind: git.RefLocal, Target: "aaaaaaaaaaaa1111", Head: true}}
	a.loadingLog = true
	a.saveProjectCache()
	a.resetForProject()
	_, restored, reloadLog := a.restoreProjectCache(root)
	if !restored || !reloadLog {
		t.Fatalf("不完整缓存恢复后应重新加载日志: restored=%v reloadLog=%v", restored, reloadLog)
	}
}

// TestFileMenuIgnore 未跟踪文件菜单提供添加到 .gitignore。
func TestFileMenuIgnore(t *testing.T) {
	a, _ := newFakeApp(t)
	n := &fileNode{name: "new.txt", path: "new.txt", entries: []fileEntry{{path: "new.txt", untracked: true}}}
	found := false
	for _, c := range a.fileNodeMenuChoices(n, true) {
		if strings.Contains(c.label, ".gitignore") {
			found = true
		}
	}
	if !found {
		t.Fatalf("未跟踪文件缺少忽略菜单: %+v", a.fileNodeMenuChoices(n, true))
	}
	tracked := &fileNode{name: "a.txt", path: "a.txt", entries: []fileEntry{{path: "a.txt"}}}
	for _, c := range a.fileNodeMenuChoices(tracked, true) {
		if strings.Contains(c.label, ".gitignore") {
			t.Fatal("已跟踪文件不应显示忽略菜单")
		}
	}
}

// TestProjectRowsWorktrees 存在多个 worktree 时默认展开，可折叠。
func TestProjectRowsWorktrees(t *testing.T) {
	a, _ := newFakeApp(t)
	root := t.TempDir()
	a.SetProjects([]string{root}, root)
	wt := t.TempDir()
	a.worktrees[cleanProjectPath(root)] = []git.Worktree{
		{Path: cleanProjectPath(root), Branch: "main", Main: true},
		{Path: cleanProjectPath(wt), Branch: "wt-branch"},
	}
	rows := a.projectRows()
	if len(rows) != 3 {
		t.Fatalf("树行数 = %d，期望 3: %+v", len(rows), rows)
	}
	if rows[1].worktree == nil || rows[2].worktree == nil {
		t.Fatal("worktree 子行缺失")
	}
	a.projectCollapsed[cleanProjectPath(root)] = true
	if rows := a.projectRows(); len(rows) != 1 {
		t.Fatalf("折叠后行数 = %d", len(rows))
	}
}

// TestSwitchToWorktree 选中 worktree 后切换到其目录并保留主项目。
func TestSwitchToWorktree(t *testing.T) {
	a, _ := newFakeApp(t)
	root, wt := t.TempDir(), t.TempDir()
	a.SetProjects([]string{root}, root)
	a.currentPath = cleanProjectPath(root)
	a.currentProjectPath = cleanProjectPath(root)
	a.worktrees[cleanProjectPath(root)] = []git.Worktree{
		{Path: cleanProjectPath(root), Branch: "main", Main: true},
		{Path: cleanProjectPath(wt), Branch: "wt-branch"},
	}
	next := &fakeSvc{root: wt, status: git.Status{Branch: "wt-branch"}}
	a.openRepo = func(p string) (git.Service, error) { return next, nil }
	cmd := a.switchToWorktree(cleanProjectPath(root), cleanProjectPath(wt))
	if cmd == nil {
		t.Fatal("切换 worktree 未返回命令")
	}
	if a.svc != next || a.currentPath != cleanProjectPath(wt) {
		t.Fatalf("未切换到 worktree: path=%q", a.currentPath)
	}
	if a.currentProjectPath != cleanProjectPath(root) {
		t.Fatalf("主项目路径 = %q", a.currentProjectPath)
	}
	rows := a.projectRows()
	if a.projectSel >= len(rows) || rows[a.projectSel].worktree == nil || rows[a.projectSel].worktree.Path != cleanProjectPath(wt) {
		t.Fatalf("项目区未选中 worktree: sel=%d rows=%+v", a.projectSel, rows)
	}
}

// TestWorktreeMenu 主 worktree 不能移除，子 worktree 提供切换/移除。
func TestWorktreeMenu(t *testing.T) {
	a, _ := newFakeApp(t)
	root, wt := t.TempDir(), t.TempDir()
	a.SetProjects([]string{root}, root)
	a.openWorktreeMenu(cleanProjectPath(root), git.Worktree{Path: cleanProjectPath(root), Main: true})
	if a.modal == nil {
		t.Fatal("worktree 菜单未打开")
	}
	for _, c := range a.modal.choices {
		if strings.Contains(c.label, "移除 worktree") {
			t.Fatal("主 worktree 不应提供移除")
		}
	}
	a.openWorktreeMenu(cleanProjectPath(root), git.Worktree{Path: cleanProjectPath(wt)})
	found := false
	for _, c := range a.modal.choices {
		if strings.Contains(c.label, "移除 worktree") {
			found = true
		}
	}
	if !found {
		t.Fatalf("子 worktree 缺少移除项: %+v", a.modal.choices)
	}
}

// TestWorktreesMsgExpands 加载 worktree 后默认展开，并保留项目选中。
func TestWorktreesMsgExpands(t *testing.T) {
	a, _ := newFakeApp(t)
	root, wt := t.TempDir(), t.TempDir()
	a.SetProjects([]string{root}, root)
	a.currentPath = cleanProjectPath(root)
	a.currentProjectPath = cleanProjectPath(root)
	a.Update(worktreesMsg{gen: a.worktreesGen, list: map[string][]git.Worktree{
		cleanProjectPath(root): {
			{Path: cleanProjectPath(root), Branch: "main", Main: true},
			{Path: cleanProjectPath(wt), Branch: "wt-branch"},
		},
	}})
	if rows := a.projectRows(); len(rows) != 3 {
		t.Fatalf("worktree 未默认展开: %+v", rows)
	}
	if a.projectSel != 0 {
		t.Fatalf("项目选中行 = %d", a.projectSel)
	}
	v := plainView(a)
	if !strings.Contains(v, "wt-branch") {
		t.Fatalf("视图缺少 worktree 分支: %s", v)
	}
}

// TestNewWorktreePrompt 新建 worktree 输入框为两行并带默认值。
func TestNewWorktreePrompt(t *testing.T) {
	a, _ := newFakeApp(t)
	p := projectEntry{Name: "demo", Path: cleanProjectPath(t.TempDir())}
	a.newWorktreePrompt(p)
	if a.modal == nil || a.modal.kind != modalText {
		t.Fatalf("未打开 worktree 输入框: %+v", a.modal)
	}
	if h := a.modal.text.Height(); h != 2 {
		t.Fatalf("输入框高度 = %d", h)
	}
	if v := a.modal.text.Value(); !strings.Contains(v, "demo-wt") || !strings.Contains(v, "wt/demo") {
		t.Fatalf("默认值 = %q", v)
	}
}

// TestProjectPaneWheelScroll 项目区滚轮应能滚动（含 worktree 子行）。
func TestProjectPaneWheelScroll(t *testing.T) {
	a, _ := newFakeApp(t)
	root := t.TempDir()
	a.SetProjects([]string{root}, root)
	a.currentPath = cleanProjectPath(root)
	a.currentProjectPath = cleanProjectPath(root)
	wts := []git.Worktree{{Path: cleanProjectPath(root), Branch: "main", Main: true}}
	for i := 0; i < 12; i++ {
		wts = append(wts, git.Worktree{Path: filepath.Join(t.TempDir(), "wt"), Branch: fmt.Sprintf("wt-%d", i)})
	}
	a.worktrees[cleanProjectPath(root)] = wts
	if rows := a.projectRows(); len(rows) <= a.projectViewHeight() {
		t.Fatalf("测试数据不足以滚动: rows=%d view=%d", len(rows), a.projectViewHeight())
	}
	a.pane = PaneProjects
	a.projectOff = 0
	a.Update(tea.MouseMsg{Action: tea.MouseActionPress, Button: tea.MouseButtonWheelDown, X: 2, Y: 2})
	if a.projectOff == 0 {
		t.Fatal("项目区滚轮未滚动")
	}
	max := len(a.projectRows()) - a.projectViewHeight()
	if a.projectOff > max {
		t.Fatalf("projectOff = %d 超过上限 %d", a.projectOff, max)
	}
}

// TestProjectDividerDrag 项目区与分支区之间的分隔条可拖动并双击复位。
func TestProjectDividerDrag(t *testing.T) {
	a, _ := newFakeApp(t)
	a.Update(tea.WindowSizeMsg{Width: 120, Height: 50})
	l := a.panelLayout()
	y := l.projH
	a.Update(tea.MouseMsg{Action: tea.MouseActionPress, Button: tea.MouseButtonLeft, X: 2, Y: y})
	if a.dragMode != 7 {
		t.Fatalf("项目分隔线拖动模式 = %d, 期望 7", a.dragMode)
	}
	a.Update(tea.MouseMsg{Action: tea.MouseActionMotion, X: 2, Y: y + 2})
	if a.projectH != l.projH+2 {
		t.Fatalf("projectH = %d, 期望 %d", a.projectH, l.projH+2)
	}
	a.Update(tea.MouseMsg{Action: tea.MouseActionRelease, Button: tea.MouseButtonLeft, X: 2, Y: y + 2})
	// 双击复位为自适应
	a.lastClick = time.Time{}
	l = a.panelLayout()
	y = l.projH
	a.Update(tea.MouseMsg{Action: tea.MouseActionPress, Button: tea.MouseButtonLeft, X: 2, Y: y})
	a.Update(tea.MouseMsg{Action: tea.MouseActionRelease, Button: tea.MouseButtonLeft, X: 2, Y: y})
	a.Update(tea.MouseMsg{Action: tea.MouseActionPress, Button: tea.MouseButtonLeft, X: 2, Y: y})
	if a.projectH != 0 || a.dragMode != 0 {
		t.Fatalf("双击复位后 projectH=%d dragMode=%d", a.projectH, a.dragMode)
	}
}

// TestProjectRightClickMenus 项目行与 worktree 行右键菜单要对应正确层级。
func TestProjectRightClickMenus(t *testing.T) {
	a, _ := newFakeApp(t)
	root, wt := t.TempDir(), t.TempDir()
	a.SetProjects([]string{root}, root)
	a.currentPath = cleanProjectPath(root)
	a.currentProjectPath = cleanProjectPath(root)
	a.worktrees[cleanProjectPath(root)] = []git.Worktree{
		{Path: cleanProjectPath(root), Branch: "main", Main: true},
		{Path: cleanProjectPath(wt), Branch: "wt-branch"},
	}
	a.pane = PaneProjects
	a.Update(tea.MouseMsg{Action: tea.MouseActionPress, Button: tea.MouseButtonRight, X: 2, Y: 1})
	if a.modal == nil || strings.Contains(strings.ToLower(a.modal.title), "worktree") {
		t.Fatalf("项目行右键菜单错误: %+v", a.modal)
	}
	foundAdd := false
	for _, c := range a.modal.choices {
		if strings.Contains(c.label, "添加项目") {
			foundAdd = true
		}
	}
	if !foundAdd {
		t.Fatalf("项目行菜单缺少添加项目: %+v", a.modal.choices)
	}
	a.modal = nil
	a.Update(tea.MouseMsg{Action: tea.MouseActionPress, Button: tea.MouseButtonRight, X: 2, Y: 2})
	if a.modal == nil || !strings.Contains(strings.ToLower(a.modal.title), "worktree") {
		t.Fatalf("worktree 行右键菜单错误: %+v", a.modal)
	}
}

// TestProjectTreeSpacing 项目行 * 与名称只留一格，worktree 子行不用额外符号。
func TestProjectTreeSpacing(t *testing.T) {
	a, _ := newFakeApp(t)
	root := t.TempDir()
	a.SetProjects([]string{root}, root)
	a.currentPath = cleanProjectPath(root)
	a.currentProjectPath = cleanProjectPath(root)
	view := ansi.Strip(a.viewProjectsPanel(40, 4))
	if !strings.Contains(view, "* "+filepath.Base(cleanProjectPath(root))) {
		t.Fatalf("当前项目标记间距错误: %q", view)
	}
	a.worktrees[cleanProjectPath(root)] = []git.Worktree{
		{Path: cleanProjectPath(root), Branch: "main", Main: true},
		{Path: filepath.Join(t.TempDir(), "wt"), Branch: "wt-branch"},
	}
	view = ansi.Strip(a.viewProjectsPanel(40, 5))
	if strings.Contains(view, "└") {
		t.Fatalf("worktree 子行不应有额外符号: %q", view)
	}
	if !strings.Contains(view, "wt-branch") {
		t.Fatalf("worktree 子行缺少分支: %q", view)
	}
}

// TestWorktreeCurrentMarker Git 输出的正斜杠路径归一化后，当前 worktree 要有 * 标记。
func TestWorktreeCurrentMarker(t *testing.T) {
	a, _ := newFakeApp(t)
	root, wt := t.TempDir(), t.TempDir()
	a.SetProjects([]string{root}, root)
	a.currentPath = cleanProjectPath(root)
	a.currentProjectPath = cleanProjectPath(root)
	// 模拟 git 输出：正斜杠路径
	wtRaw := strings.ReplaceAll(cleanProjectPath(wt), string(rune(92)), "/")
	a.Update(worktreesMsg{gen: a.worktreesGen, list: map[string][]git.Worktree{
		cleanProjectPath(root): {
			{Path: cleanProjectPath(root), Branch: "main", Main: true},
			{Path: wtRaw, Branch: "wt-branch"},
		},
	}})
	if got := a.worktrees[cleanProjectPath(root)][1].Path; got != cleanProjectPath(wt) {
		t.Fatalf("worktree 路径未归一化: %q", got)
	}
	// 切换到该 worktree 后再渲染，应有 * 标识
	a.currentPath = cleanProjectPath(wt)
	view := ansi.Strip(a.viewProjectsPanel(50, 5))
	found := false
	for _, line := range strings.Split(view, string(rune(10))) {
		if strings.Contains(line, "wt-branch") && strings.Contains(line, "* ") {
			found = true
		}
	}
	if !found {
		t.Fatalf("当前 worktree 缺少 * 标识:\n%s", view)
	}
}

// TestRemoveCurrentProject 当前项目也可移除，并自动切换到剩余项目。
func TestRemoveCurrentProject(t *testing.T) {
	a, _ := newFakeApp(t)
	dirA, dirB := t.TempDir(), t.TempDir()
	a.SetProjects([]string{dirA, dirB}, dirA)
	a.currentPath = cleanProjectPath(dirA)
	a.currentProjectPath = cleanProjectPath(dirA)
	next := &fakeSvc{root: dirB, status: git.Status{Branch: "dev"}}
	a.openRepo = func(p string) (git.Service, error) { return next, nil }
	// 菜单里必须有移除当前项目
	a.openProjectMenu(a.projectIndex(cleanProjectPath(dirA)))
	found := false
	for _, c := range a.modal.choices {
		if strings.Contains(c.label, "移除项目") {
			found = true
		}
	}
	if !found {
		t.Fatalf("当前项目菜单缺少移除项: %+v", a.modal.choices)
	}
	a.removeProject(a.projects[0])
	if len(a.projects) != 1 || a.projects[0].Path != cleanProjectPath(dirB) {
		t.Fatalf("移除后项目 = %+v", a.projects)
	}
	if a.currentPath != cleanProjectPath(dirB) {
		t.Fatalf("未自动切换项目: %q", a.currentPath)
	}
	// 最后一个项目不可移除，菜单也不显示移除项
	a.removeProject(a.projects[0])
	if len(a.projects) != 1 {
		t.Fatal("最后一个项目不应被移除")
	}
	a.openProjectMenu(0)
	for _, c := range a.modal.choices {
		if strings.Contains(c.label, "移除项目") {
			t.Fatalf("单项目菜单不应显示移除项: %+v", a.modal.choices)
		}
	}
}

// TestAutoRefreshDetectsExternalRefs 外部引用变化应触发后台重载提交历史。
func TestAutoRefreshDetectsExternalRefs(t *testing.T) {
	a, _ := newFakeApp(t)
	a.refs = []git.Ref{{Name: "main", FullName: "refs/heads/main", Kind: git.RefLocal, Target: "aaaaaaaaaaaa1111", Head: true}}
	a.status = git.Status{Branch: "main", OID: "aaaaaaaaaaaa1111"}
	refs := append([]git.Ref(nil), a.refs...)
	refs = append(refs, git.Ref{Name: "v9.9", FullName: "refs/tags/v9.9", Kind: git.RefTag, Target: "aaaaaaaaaaaa1111"})
	_, cmd := a.Update(refreshMsg{
		gen: a.refreshGen, quick: true, auto: true,
		status: a.status, refs: refs,
	})
	if cmd == nil {
		t.Fatal("外部引用变化未触发刷新")
	}
	if !a.logPreserve {
		t.Fatal("引用变化后应后台重载提交历史")
	}
}

// TestAutoRefreshNoChange 无变化时不重载提交历史。
func TestAutoRefreshNoChange(t *testing.T) {
	a, _ := newFakeApp(t)
	a.refs = []git.Ref{{Name: "main", FullName: "refs/heads/main", Kind: git.RefLocal, Target: "aaaaaaaaaaaa1111", Head: true}}
	a.status = git.Status{Branch: "main", OID: "aaaaaaaaaaaa1111"}
	a.logPreserve = false
	a.Update(refreshMsg{gen: a.refreshGen, quick: true, auto: true, status: a.status, refs: a.refs})
	if a.logPreserve {
		t.Fatal("无变化时不应重载提交历史")
	}
}

// TestSameRefs 引用比较对顺序不敏感、对目标敏感。
func TestSameRefs(t *testing.T) {
	a := []git.Ref{{FullName: "refs/heads/main", Target: "1"}, {FullName: "refs/tags/v1", Target: "2"}}
	b := []git.Ref{{FullName: "refs/tags/v1", Target: "2"}, {FullName: "refs/heads/main", Target: "1"}}
	if !sameRefs(a, b) {
		t.Fatal("同内容不同顺序应相等")
	}
	b[0].Target = "3"
	if sameRefs(a, b) {
		t.Fatal("目标变化应检测到")
	}
}

// TestOverlayKeepsBackgroundColors 浮层所在行要保留左右内容的原始颜色。
func TestOverlayKeepsBackgroundColors(t *testing.T) {
	base := "\x1b[31mLEFT\x1b[0m     \x1b[32mRIGHT\x1b[0m"
	out := overlayBox(base, []string{"[BOX]"}, 4, 0, 20)
	if !strings.Contains(out, "\x1b[31m") {
		t.Fatalf("左侧颜色丢失: %q", out)
	}
	if !strings.Contains(out, "\x1b[32m") {
		t.Fatalf("右侧颜色丢失: %q", out)
	}
	plain := ansi.Strip(out)
	for _, want := range []string{"LEFT", "[BOX]", "RIGHT"} {
		if !strings.Contains(plain, want) {
			t.Fatalf("内容缺失 %q: %q", want, plain)
		}
	}
}

// TestOverlayResetsUnderlyingStyle 浮层内容前要复位底层 SGR，避免继承行颜色。
func TestOverlayResetsUnderlyingStyle(t *testing.T) {
	base := "[31mRED[0m"
	out := overlayBox(base, []string{"[BOX]"}, 0, 0, 20)
	if !strings.Contains(out, "[0m[BOX]") {
		t.Fatalf("浮层未复位底层颜色: %q", out)
	}
}

// TestOverlayAlignsBorderWithWideChars 背景含中文/宽字符时浮层边框仍按列对齐。
func TestOverlayAlignsBorderWithWideChars(t *testing.T) {
	_, _ = newFakeApp(t)
	line := "中文中文中文中文中文中文中文中文"
	base := line + string(rune(10)) + line + string(rune(10)) + line
	box := []string{"╭───╮", "│abc│", "╰───╯"}
	out := ansi.Strip(overlayBox(base, box, 5, 0, 30))
	lines := strings.Split(out, string(rune(10)))
	if len(lines) < 3 {
		t.Fatal("浮层行数不足")
	}
	for i, want := range []string{"╭", "│", "╰"} {
		idx := strings.Index(lines[i], want)
		if idx < 0 {
			t.Fatalf("第 %d 行缺少边框: %q", i, lines[i])
		}
		col := strWidth(lines[i][:idx])
		if col != 5 {
			t.Fatalf("第 %d 行边框列 = %d，期望 5: %q", i, col, lines[i])
		}
	}
}

// TestChoiceMenuBorderAligned 真实菜单浮在含中文的主界面上时，左右边框逐行对齐。
func TestChoiceMenuBorderAligned(t *testing.T) {
	a, _ := newFakeApp(t)
	a.pane = PaneCommits
	a.graphSel = a.dirtyOffset()
	a.modalAnchorX, a.modalAnchorY = 60, 6
	_ = a.openContextMenu()
	if a.modal == nil {
		t.Fatal("菜单未打开")
	}
	box, x, y := a.choiceMenuBox()
	if len(box) == 0 {
		t.Fatal("菜单框为空")
	}
	lines := strings.Split(ansi.Strip(a.View()), string(rune(10)))
	for i, bl := range box {
		yy := y + i
		if yy < 0 || yy >= len(lines) {
			t.Fatalf("菜单行越界: %d", yy)
		}
		line := lines[yy]
		plainBox := ansi.Strip(bl)
		idx := strings.Index(line, plainBox)
		if idx < 0 {
			t.Fatalf("菜单第 %d 行未找到完整边框: %q", i, line)
		}
		col := strWidth(line[:idx])
		if col != x {
			t.Fatalf("菜单第 %d 行左边框列 = %d，期望 %d: %q", i, col, x, line)
		}
		if w := strWidth(plainBox); w != strWidth(bl) {
			t.Fatalf("菜单第 %d 行宽度异常: %d/%d", i, w, strWidth(bl))
		}
	}
}

// TestWorktreesMsgFiltersPrunable 失效 worktree 不应继续出现在项目树中。
func TestWorktreesMsgFiltersPrunable(t *testing.T) {
	a, _ := newFakeApp(t)
	root := t.TempDir()
	a.SetProjects([]string{root}, root)
	a.currentPath = cleanProjectPath(root)
	a.currentProjectPath = cleanProjectPath(root)
	missing := filepath.Join(t.TempDir(), "gone")
	a.Update(worktreesMsg{gen: a.worktreesGen, list: map[string][]git.Worktree{
		cleanProjectPath(root): {
			{Path: cleanProjectPath(root), Branch: "main", Main: true},
			{Path: missing, Branch: "gone", Prunable: true},
		},
	}})
	if rows := a.projectRows(); len(rows) != 1 {
		t.Fatalf("失效 worktree 仍在树中: %+v", rows)
	}
}

// TestInvalidWorktreeMenu 失效 worktree 菜单只提供清理/复制，不提示新建。
func TestInvalidWorktreeMenu(t *testing.T) {
	a, _ := newFakeApp(t)
	root := t.TempDir()
	a.SetProjects([]string{root}, root)
	a.openWorktreeMenu(cleanProjectPath(root), git.Worktree{Path: filepath.Join(t.TempDir(), "gone"), Prunable: true})
	if a.modal == nil {
		t.Fatal("菜单未打开")
	}
	hasPrune, hasNew := false, false
	for _, c := range a.modal.choices {
		if strings.Contains(c.label, "清理") || strings.Contains(c.label, "prune") {
			hasPrune = true
		}
		if strings.Contains(c.label, "新建 worktree") {
			hasNew = true
		}
	}
	if !hasPrune || hasNew {
		t.Fatalf("失效 worktree 菜单错误: %+v", a.modal.choices)
	}
}

// TestProjectBlankClickNoReload 点击项目区空白行不应触发提交列表刷新。
func TestProjectBlankClickNoReload(t *testing.T) {
	a, _ := newFakeApp(t)
	root, wt := t.TempDir(), t.TempDir()
	a.SetProjects([]string{root}, root)
	a.currentPath = cleanProjectPath(wt)
	a.currentProjectPath = cleanProjectPath(root)
	a.worktrees[cleanProjectPath(root)] = []git.Worktree{
		{Path: cleanProjectPath(root), Branch: "main", Main: true},
		{Path: cleanProjectPath(wt), Branch: "wt-branch"},
	}
	a.projectH = 7 // 强制留出空白行
	a.pane = PaneProjects
	rows := len(a.projectRows())
	if a.projectViewHeight() <= rows {
		t.Fatalf("没有空白行: view=%d rows=%d", a.projectViewHeight(), rows)
	}
	blankY := 1 + rows // 内容区第 rows 行（0-based）即空白
	gen := a.logGen
	_, cmd := a.Update(tea.MouseMsg{Action: tea.MouseActionPress, Button: tea.MouseButtonLeft, X: 2, Y: blankY})
	if cmd != nil || a.logGen != gen {
		t.Fatalf("空白点击触发了刷新: cmd=%v logGen=%d->%d", cmd, gen, a.logGen)
	}
}

// TestAutoRefreshUnchangedSkipsUpdate 数据无变化时不更新模型、不触发重绘。
func TestAutoRefreshUnchangedSkipsUpdate(t *testing.T) {
	a, _ := newFakeApp(t)
	a.refs = []git.Ref{{Name: "main", FullName: "refs/heads/main", Kind: git.RefLocal, Target: "aaaaaaaaaaaa1111", Head: true}}
	a.status = git.Status{Branch: "main", OID: "aaaaaaaaaaaa1111"}
	a.stashes = nil
	_, cmd := a.Update(refreshMsg{
		gen: a.refreshGen, auto: true,
		status: a.status, refs: a.refs, state: a.state, stashes: a.stashes,
	})
	if cmd != nil {
		t.Fatal("无变化时不应返回刷新命令")
	}
}

// TestWorktreesUnchangedSkipsUpdate worktree 未变化时不清空/重设项目选择。
func TestWorktreesUnchangedSkipsUpdate(t *testing.T) {
	a, _ := newFakeApp(t)
	root := t.TempDir()
	a.SetProjects([]string{root}, root)
	a.currentPath = cleanProjectPath(root)
	a.currentProjectPath = cleanProjectPath(root)
	list := []git.Worktree{{Path: cleanProjectPath(root), Branch: "main", Main: true}}
	a.worktrees[cleanProjectPath(root)] = list
	a.projectSel = 0
	a.projectOff = 0
	_, cmd := a.Update(worktreesMsg{gen: a.worktreesGen, list: map[string][]git.Worktree{cleanProjectPath(root): list}})
	if cmd != nil {
		t.Fatal("worktree 无变化时不应返回命令")
	}
}

// TestLogoBadge 底部左侧使用品牌化 wordmark。
func TestLogoBadge(t *testing.T) {
	a, _ := newFakeApp(t)
	if !strings.Contains(plainView(a), "▐ gitc ▌") {
		t.Fatalf("底部缺少品牌标识: %q", plainView(a))
	}
}

// TestHelpTwoColumnLayout 宽终端帮助页分左右两栏：左侧品牌/GitHub，右侧快捷键。
func TestHelpTwoColumnLayout(t *testing.T) {
	a, _ := newFakeApp(t)
	a.Update(tea.WindowSizeMsg{Width: 120, Height: 40})
	a.overlay = overlayHelp
	a.detail.offset = 0
	v := plainView(a)
	for _, want := range []string{"██████ ██ ██████ ██████", "终端里的 Git 工作台", "https://github.com/ssk-wh/gitc", "https://github.com/ssk-wh/gitc/releases", "https://github.com/ssk-wh/gitc/issues", "↑/↓ 滚动快捷键"} {
		if !strings.Contains(v, want) {
			t.Fatalf("帮助页缺少 %q:\n%s", want, v)
		}
	}
	// 品牌图标和快捷键区之间有分隔线，且左侧品牌区整体垂直居中。
	lines := strings.Split(v, string(rune(10)))
	logoLine := -1
	for i, line := range lines {
		if strings.Contains(line, "██████ ██ ██████ ██████") {
			logoLine = i
			if !strings.Contains(line, "│") {
				t.Fatalf("帮助页未形成左右分栏:\n%s", v)
			}
			break
		}
	}
	if logoLine < 0 {
		t.Fatalf("帮助页缺少 gitc 字标:\n%s", v)
	}
	if logoLine < 6 {
		t.Fatalf("左侧品牌区未垂直居中，字标行 = %d:\n%s", logoLine, v)
	}
}

// TestHelpNarrowFallsBack 窄终端退回单栏，GitHub 与快捷键仍可见。
func TestHelpNarrowFallsBack(t *testing.T) {
	a, _ := newFakeApp(t)
	a.Update(tea.WindowSizeMsg{Width: 80, Height: 24})
	a.overlay = overlayHelp
	v := plainView(a)
	for _, want := range []string{"██████ ██ ██████ ██████", "项目信息", "https://github.com/ssk-wh/gitc", "快捷键", "↑↓"} {
		if !strings.Contains(v, want) {
			t.Fatalf("窄屏帮助页缺少 %q:\n%s", want, v)
		}
	}
}

// TestHelpKeysVerticallyCentered verifies the key list is vertically centered in two-column help.
func TestHelpKeysVerticallyCentered(t *testing.T) {
	a, _ := newFakeApp(t)
	a.Update(tea.WindowSizeMsg{Width: 120, Height: 40})
	a.overlay = overlayHelp
	lines := strings.Split(plainView(a), string(rune(10)))
	for _, line := range lines {
		if strings.Contains(line, "│ 快捷键") {
			t.Fatal("help key list should not have a header")
		}
	}
	for i, line := range lines {
		if strings.Contains(line, "│") && strings.Contains(line, "↑↓") {
			if i < 2 {
				t.Fatalf("key list not vertically centered: line=%d\n%s", i, strings.Join(lines, string(rune(10))))
			}
			return
		}
	}
	t.Fatal("key list not found")
}

// TestSquashPromptIncludesAllCommitMessages 合并编辑框应包含所有选中提交的标题和正文。
func TestSquashPromptIncludesAllCommitMessages(t *testing.T) {
	a, _ := newFakeApp(t)
	a.status.Staged = nil
	a.commitAnchor = a.dirtyOffset()
	a.graphSel = a.dirtyOffset() + 1
	cmd := a.squashPrompt()
	if cmd == nil {
		t.Fatal("squashPrompt 未返回加载命令")
	}
	pump(t, a, cmd)
	if a.modal == nil || a.modal.kind != modalText {
		t.Fatalf("未打开提交信息编辑框: %+v", a.modal)
	}
	v := a.modal.text.Value()
	for _, want := range []string{"支持摄像头分辨率", "body-bbbbbbb", "修复切换音频崩溃", "body-aaaaaaa"} {
		if !strings.Contains(v, want) {
			t.Fatalf("合并编辑框缺少 %q: %q", want, v)
		}
	}
	if strings.Index(v, "支持摄像头分辨率") > strings.Index(v, "修复切换音频崩溃") {
		t.Fatalf("提交信息应按从旧到新排列: %q", v)
	}
}

// TestKeyboardMenuAnchorUsesSelectedRow b 打开的菜单应与右键点击选中行同一锚点。
func TestKeyboardMenuAnchorUsesSelectedRow(t *testing.T) {
	a, _ := newFakeApp(t)
	a.pane = PaneCommits
	a.graphSel = a.dirtyOffset() + 1
	a.graphOff = 0
	sendKey(a, tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("b")})
	if a.modal == nil {
		t.Fatal("b 未打开提交菜单")
	}
	b, ok := a.paneBoxOf(PaneCommits)
	if !ok {
		t.Fatal("找不到提交区")
	}
	wantY := b.y + 1 + a.graphSel - a.graphOff
	if a.modalAnchorY != wantY {
		t.Fatalf("b 菜单锚点 Y = %d，期望选中行 %d", a.modalAnchorY, wantY)
	}
	if a.modalAnchorX != b.x+2 {
		t.Fatalf("b 菜单锚点 X = %d，期望 %d", a.modalAnchorX, b.x+2)
	}
}

// TestHelpNoTitleAndNarrowTopPadding 帮助页不显示标题，单栏模式顶部留一个空行。
func TestHelpNoTitleAndNarrowTopPadding(t *testing.T) {
	a, _ := newFakeApp(t)
	a.Update(tea.WindowSizeMsg{Width: 80, Height: 24})
	a.overlay = overlayHelp
	v := plainView(a)
	if strings.Contains(v, "gitc 帮助") {
		t.Fatalf("帮助页不应显示标题:\n%s", v)
	}
	lines := strings.Split(v, string(rune(10)))
	if len(lines) < 2 {
		t.Fatalf("帮助页行数不足:\n%s", v)
	}
	if strings.TrimSpace(lines[0]) != "" {
		t.Fatalf("单栏帮助页顶部应留空行，首行 = %q", lines[0])
	}
	if !strings.Contains(lines[1], "██████ ██ ██████ ██████") {
		t.Fatalf("空行后应显示 Gitc 字标，第二行 = %q", lines[1])
	}
}

// TestAddProjectRejectsRepoSubdir 选择父仓库内的子目录时不能添加父仓库。
func TestAddProjectRejectsRepoSubdir(t *testing.T) {
	a, _ := newFakeApp(t)
	parent := t.TempDir()
	sub := filepath.Join(parent, "aipick")
	if err := os.MkdirAll(sub, 0o755); err != nil {
		t.Fatal(err)
	}
	a.openRepo = func(string) (git.Service, error) { return &fakeSvc{root: parent}, nil }
	before := len(a.projects)
	a.chooseProjectDir(sub)
	if len(a.projects) != before {
		t.Fatalf("子目录不应新增项目: %+v", a.projects)
	}
	if !strings.Contains(a.toast, "根目录") {
		t.Fatalf("缺少仓库根目录提示: %q", a.toast)
	}
	if a.projectIndex(cleanProjectPath(parent)) >= 0 && before > 0 {
		// 父仓库即使已存在也不能改变项目列表。
		if len(a.projects) != before {
			t.Fatal("项目列表被修改")
		}
	}
}

// TestAddProjectPickerShowsRootError 内置选择器在框内提示仓库根目录。
func TestAddProjectPickerShowsRootError(t *testing.T) {
	a, _ := newFakeApp(t)
	parent := t.TempDir()
	sub := filepath.Join(parent, "aipick")
	if err := os.MkdirAll(sub, 0o755); err != nil {
		t.Fatal(err)
	}
	a.openRepo = func(string) (git.Service, error) { return &fakeSvc{root: parent}, nil }
	a.modal = &modal{kind: modalDirPicker, title: "选择项目目录"}
	a.chooseProjectDir(sub)
	if a.modal == nil || !strings.Contains(a.modal.err, "根目录") {
		t.Fatalf("选择器未提示仓库根目录: %+v", a.modal)
	}
}

// TestSplitStyledEmojiAlignment 按 grapheme 切分，避免组合 emoji 导致右侧列错位。
func TestSplitStyledEmojiAlignment(t *testing.T) {
	line := strings.Repeat("a", 8) + "👨‍👩‍👧‍👦" + strings.Repeat("b", 20) + "DATE"
	_, right, _ := splitStyled(line, 10, 20)
	want := ansi.Cut(line, 20, ansi.StringWidth(line))
	if ansi.Strip(right) != ansi.Strip(want) {
		t.Fatalf("emoji 右侧切分错位: got=%q want=%q", ansi.Strip(right), ansi.Strip(want))
	}
}

// TestOverlayPreservesRightContentWithEmoji 菜单覆盖含 emoji 的行时，右侧内容不能丢。
func TestOverlayPreservesRightContentWithEmoji(t *testing.T) {
	line := strings.Repeat("👨‍👩‍👧‍👦", 3) + strings.Repeat("x", 20) + "RIGHT"
	out := ansi.Strip(overlayBox(line, []string{"[BOX]"}, 2, 0, 100))
	if !strings.Contains(out, "RIGHT") {
		t.Fatalf("emoji 行右侧内容丢失: %q", out)
	}
}

// TestMouseWheelScrollsFilesWithoutSnapping 文件多时滚轮应自由滚动，不被选中行拉回。
func TestMouseWheelScrollsFilesWithoutSnapping(t *testing.T) {
	a, _ := newFakeApp(t)
	a.Update(tea.WindowSizeMsg{Width: 120, Height: 40})
	files := make([]fileEntry, 40)
	for i := range files {
		files[i] = fileEntry{path: fmt.Sprintf("file-%02d.txt", i), status: "M"}
	}
	a.filesOwner = "work"
	a.setFiles(files)
	a.pane = PaneFiles
	a.fileSel = 0
	a.fileOff = 0
	a.scrollPane(PaneFiles, 3)
	if a.fileOff != 3 {
		t.Fatalf("滚轮未滚动文件区: off=%d", a.fileOff)
	}
	_ = a.View()
	if a.fileOff != 3 {
		t.Fatalf("渲染时文件区被选中行拉回: off=%d", a.fileOff)
	}
}

// TestCommitFileMenuOpenLocal 提交文件右键菜单可打开本地文件/目录，找不到时提示。
func TestCommitFileMenuOpenLocal(t *testing.T) {
	a, _ := newFakeApp(t)
	root := t.TempDir()
	a.svc = &fakeSvc{root: root}
	file := filepath.Join(root, "sub", "a.txt")
	if err := os.MkdirAll(filepath.Dir(file), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(file, []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	opened := ""
	a.openDir = func(p string) error { opened = p; return nil }
	menu := a.fileMenuChoices(fileEntry{path: "sub/a.txt"}, false)
	ch, ok := choiceByLabel(menu, "在文件管理器中打开")
	if !ok {
		t.Fatalf("提交文件菜单缺少打开入口: %+v", menu)
	}
	pump(t, a, ch.action(a))
	if opened != filepath.Dir(file) {
		t.Fatalf("打开目录 = %q，期望 %q", opened, filepath.Dir(file))
	}
	if _, ok := choiceByLabel(menu, "复制路径"); !ok {
		t.Fatalf("提交文件菜单缺少复制路径: %+v", menu)
	}
	a.revealLocalFile("missing.txt")
	if !strings.Contains(a.toast, "找不到") {
		t.Fatalf("缺失文件未提示: %q", a.toast)
	}
	nodeMenu := a.fileNodeMenuChoices(&fileNode{path: "sub", dir: true}, false)
	if len(nodeMenu) < 2 {
		t.Fatalf("提交目录菜单不完整: %+v", nodeMenu)
	}
}
