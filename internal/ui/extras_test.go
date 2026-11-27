package ui

import (
	"context"
	"errors"
	"fmt"
	"reflect"
	"strings"
	"testing"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/x/ansi"

	"github.com/ssk-wh/gitc/internal/git"
)

// —— 扩展的 fakeSvc 方法：覆盖新增的 Service 接口 ——

func (f *fakeSvc) CompareBranches(ctx context.Context, base, target string, max int) (git.CompareSummary, error) {
	f.calls = append(f.calls, "compare:"+base+".."+target)
	return git.CompareSummary{
		Base:   base,
		Target: target,
		Commits: []git.Commit{{
			Hash: "bbbbbbbbbbbb2222", Short: "bbbbbbb", Author: "李四",
			When: time.Now(), Subject: "特性提交",
		}},
		DiffStat: " b.txt | 1 +\n 1 file changed, 1 insertion(+)",
	}, nil
}

func (f *fakeSvc) UnsetUpstream(ctx context.Context, branch string) (string, error) {
	return f.record("unset-upstream:" + branch)
}

func (f *fakeSvc) LockWorktree(ctx context.Context, path, reason string) (string, error) {
	return f.record("worktree-lock:" + path)
}

func (f *fakeSvc) UnlockWorktree(ctx context.Context, path string) (string, error) {
	return f.record("worktree-unlock:" + path)
}

func (f *fakeSvc) PruneWorktrees(context.Context) (string, error) {
	return f.record("worktree-prune")
}

func (f *fakeSvc) Worktrees(context.Context) ([]git.Worktree, error) {
	return nil, nil
}

// choiceByLabel 在菜单中按标签片段查找条目。
func choiceByLabel(cs []choice, sub string) (choice, bool) {
	for _, c := range cs {
		if strings.Contains(c.label, sub) {
			return c, true
		}
	}
	return choice{}, false
}

func TestStashMenuUsesStableHash(t *testing.T) {
	a, f := newFakeApp(t)
	st := git.Stash{Index: 1, Selector: "stash@{1}", Hash: "aaaaaaaaaaaa1111", Subject: "WIP"}
	choice, ok := choiceByLabel(a.branchMenuChoices(&sideItem{kind: sideStash, stash: st}), "应用")
	if !ok {
		t.Fatal("stash 菜单缺少 apply")
	}
	pump(t, a, choice.action(a))
	if !hasCall(f.calls, "stash-apply:aaaaaaaaaaaa1111") {
		t.Fatalf("stash apply 未使用稳定哈希: %v", f.calls)
	}
}

func TestTagCheckoutUsesFullReference(t *testing.T) {
	a, f := newFakeApp(t)
	ref := git.Ref{Name: "release", FullName: "refs/tags/release", Kind: git.RefTag, Target: "aaaaaaaaaaaa1111"}
	choice, ok := choiceByLabel(a.tagMenuChoices(ref), "检出该标签")
	if !ok {
		t.Fatal("标签菜单缺少检出入口")
	}
	pump(t, a, choice.action(a))
	if !hasCall(f.calls, "checkout:refs/tags/release") {
		t.Fatalf("标签检出未使用完整引用: %v", f.calls)
	}
}

func TestRemoteDeletePassesFullReference(t *testing.T) {
	a, f := newFakeApp(t)
	ref := git.Ref{Name: "team/origin/main", FullName: "refs/remotes/team/origin/main", Kind: git.RefRemote}
	a.sidebarDelete(&sideItem{kind: sideRemote, ref: ref})
	if a.modal == nil || a.modal.onConfirm == nil {
		t.Fatal("远端删除应弹出确认框")
	}
	pump(t, a, a.modal.onConfirm(a))
	if !hasCall(f.calls, "delete-remote:refs/remotes/team/origin/main") {
		t.Fatalf("远端删除未传递完整引用: %v", f.calls)
	}
}

// TestBranchCompareMenuShowsSummary 分支菜单可对比当前分支，详情展示提交与差异统计。
func TestBranchCompareMenuShowsSummary(t *testing.T) {
	a, f := newFakeApp(t)
	var local git.Ref
	for _, r := range a.refs {
		if r.Kind == git.RefLocal && r.Name == "feature/x" {
			local = r
		}
	}
	c, ok := choiceByLabel(a.branchMenuChoices(&sideItem{kind: sideBranch, ref: local}), "对比与当前分支")
	if !ok {
		t.Fatal("本地分支菜单缺少对比入口")
	}
	cmd := c.action(a)
	if a.overlay != overlayDetail || a.detail.kind != "compare" {
		t.Fatalf("对比应打开详情浮层: overlay=%v kind=%q", a.overlay, a.detail.kind)
	}
	pump(t, a, cmd)
	if !hasCall(f.calls, "compare:refs/heads/main..refs/heads/feature/x") {
		t.Fatalf("未调用 CompareBranches: %v", f.calls)
	}
	text := strings.Join(a.detail.lines, "\n")
	for _, want := range []string{"特性提交", "b.txt", "1 insertion"} {
		if !strings.Contains(text, want) {
			t.Errorf("对比详情缺少 %q:\n%s", want, text)
		}
	}
	// 当前分支自身不应出现对比入口。
	main := a.refs[0]
	if _, ok := choiceByLabel(a.branchMenuChoices(&sideItem{kind: sideBranch, ref: main}), "对比与当前分支"); ok {
		t.Error("当前分支菜单不应出现对比入口")
	}
	// 直接对当前分支触发时给出提示且不打开浮层。
	a.overlay = overlayNone
	if cmd := a.compareBranchCmd(main); cmd != nil {
		t.Error("当前分支对比不应返回命令")
	}
	if !strings.Contains(a.toast, "当前分支") {
		t.Errorf("当前分支提示 = %q", a.toast)
	}
}

// TestBranchUnsetUpstreamMenu 本地分支有上游时提供取消入口。
func TestBranchUnsetUpstreamMenu(t *testing.T) {
	a, f := newFakeApp(t)
	main := a.refs[0]
	c, ok := choiceByLabel(a.branchMenuChoices(&sideItem{kind: sideBranch, ref: main}), "取消上游")
	if !ok {
		t.Fatalf("菜单缺少取消上游: %+v", a.branchMenuChoices(&sideItem{kind: sideBranch, ref: main}))
	}
	pump(t, a, c.action(a))
	if !hasCall(f.calls, "unset-upstream:main") {
		t.Errorf("未调用 UnsetUpstream: %v", f.calls)
	}
}

// TestRemoteBranchSetUpstreamMenu 远端分支菜单可把当前分支上游设为该远端分支。
func TestRemoteBranchSetUpstreamMenu(t *testing.T) {
	a, f := newFakeApp(t)
	remote := git.Ref{Kind: git.RefRemote, Name: "origin/main", Target: "aaaaaaaaaaaa1111"}
	c, ok := choiceByLabel(a.branchMenuChoices(&sideItem{kind: sideRemote, ref: remote}), "设为当前分支上游")
	if !ok {
		t.Fatal("远端分支菜单缺少设为上游")
	}
	pump(t, a, c.action(a))
	if !hasCall(f.calls, "set-upstream") {
		t.Errorf("未调用 SetUpstream: %v", f.calls)
	}
}

// TestCommitMenuCopyShortSHA 提交菜单支持复制短 sha。
func TestCommitMenuCopyShortSHA(t *testing.T) {
	a, _ := newFakeApp(t)
	c := &git.Commit{Hash: "aaaaaaaaaaaa1111", Short: "aaaaaaa", Subject: "提交"}
	ch, ok := choiceByLabel(a.commitMenuChoices(c), "复制短 sha")
	if !ok {
		t.Fatal("提交菜单缺少复制短 sha")
	}
	ch.action(a)
	if !strings.Contains(a.toast, "aaaaaaa") {
		t.Fatalf("复制短 sha 提示 = %q", a.toast)
	}
}

// TestCommitDetailAndInfoShowParents 提交详情与信息面板展示父提交。
func TestCommitDetailAndInfoShowParents(t *testing.T) {
	lines := buildCommitDetailLines(git.CommitDetail{
		Hash:    "aaaaaaaaaaaa1111",
		Parents: []string{"bbbbbbbbbbbb2222", "cccccccccccc3333"},
	}, "")
	text := strings.Join(lines, "\n")
	if !strings.Contains(text, "Parent:") || !strings.Contains(text, "bbbbbbb") || !strings.Contains(text, "ccccccc") {
		t.Fatalf("详情缺少父提交: %s", text)
	}

	a, _ := newFakeApp(t)
	a.pane = PaneCommits
	a.graphSel = a.dirtyOffset() + 1 // bbbb 提交，父提交为 cccc
	pump(t, a, a.loadFilesForSelection())
	info := strings.Join(a.infoLines(80), "\n")
	if !strings.Contains(info, "Parent") || !strings.Contains(info, "ccccccc") {
		t.Fatalf("信息面板缺少父提交:\n%s", info)
	}
}

// TestWorktreeLockUnlockMenu worktree 菜单根据锁定状态显示锁定/解锁，并调用对应服务。
func TestWorktreeLockUnlockMenu(t *testing.T) {
	a, f := newFakeApp(t)
	root, wt := t.TempDir(), t.TempDir()
	a.SetProjects([]string{root}, root)
	a.openRepo = func(p string) (git.Service, error) { return f, nil }
	rootPath, wtPath := cleanProjectPath(root), cleanProjectPath(wt)

	a.openWorktreeMenu(rootPath, git.Worktree{Path: wtPath, Branch: "wt"})
	c, ok := choiceByLabel(a.modal.choices, "锁定 worktree")
	if !ok {
		t.Fatalf("未锁定 worktree 菜单缺少锁定项: %+v", a.modal.choices)
	}
	pump(t, a, c.action(a))
	if !hasCall(f.calls, "worktree-lock:"+wtPath) {
		t.Errorf("未调用 LockWorktree: %v", f.calls)
	}

	a.openWorktreeMenu(rootPath, git.Worktree{Path: wtPath, Branch: "wt", Locked: true})
	c, ok = choiceByLabel(a.modal.choices, "解锁 worktree")
	if !ok {
		t.Fatalf("已锁定 worktree 菜单缺少解锁项: %+v", a.modal.choices)
	}
	pump(t, a, c.action(a))
	if !hasCall(f.calls, "worktree-unlock:"+wtPath) {
		t.Errorf("未调用 UnlockWorktree: %v", f.calls)
	}

	// 主 worktree 不提供锁定，避免无意义操作。
	a.openWorktreeMenu(rootPath, git.Worktree{Path: rootPath, Main: true})
	if _, ok := choiceByLabel(a.modal.choices, "锁定 worktree"); ok {
		t.Error("主 worktree 不应提供锁定入口")
	}
}

// TestOpenDirectoryMenus 项目与 worktree 菜单可在文件管理器中打开。
func TestOpenDirectoryMenus(t *testing.T) {
	a, _ := newFakeApp(t)
	root, wt := t.TempDir(), t.TempDir()
	a.SetProjects([]string{root}, root)
	opened := ""
	a.openDir = func(p string) error { opened = p; return nil }
	rootPath, wtPath := cleanProjectPath(root), cleanProjectPath(wt)

	a.openWorktreeMenu(rootPath, git.Worktree{Path: wtPath, Branch: "wt"})
	c, ok := choiceByLabel(a.modal.choices, "文件管理器")
	if !ok {
		t.Fatalf("worktree 菜单缺少打开文件管理器: %+v", a.modal.choices)
	}
	c.action(a)
	if opened != wtPath {
		t.Errorf("worktree 打开路径 = %q，期望 %q", opened, wtPath)
	}

	a.openProjectMenu(0)
	c, ok = choiceByLabel(a.modal.choices, "文件管理器")
	if !ok {
		t.Fatalf("项目菜单缺少打开文件管理器: %+v", a.modal.choices)
	}
	c.action(a)
	if opened != rootPath {
		t.Errorf("项目打开路径 = %q，期望 %q", opened, rootPath)
	}
}

// TestProjectMenuPruneWorktrees 项目菜单提供 prune 失效 worktree 记录。
func TestProjectMenuPruneWorktrees(t *testing.T) {
	a, f := newFakeApp(t)
	root := t.TempDir()
	a.SetProjects([]string{root}, root)
	a.openRepo = func(p string) (git.Service, error) { return f, nil }
	a.openProjectMenu(0)
	c, ok := choiceByLabel(a.modal.choices, "prune")
	if !ok {
		t.Fatalf("项目菜单缺少 prune: %+v", a.modal.choices)
	}
	pump(t, a, c.action(a))
	if !hasCall(f.calls, "worktree-prune") {
		t.Errorf("未调用 PruneWorktrees: %v", f.calls)
	}
}

// TestLockedWorktreeMarker 锁定的 worktree 在项目树中标记。
func TestLockedWorktreeMarker(t *testing.T) {
	a, _ := newFakeApp(t)
	root, wt := t.TempDir(), t.TempDir()
	a.SetProjects([]string{root}, root)
	rootPath, wtPath := cleanProjectPath(root), cleanProjectPath(wt)
	a.worktrees[rootPath] = []git.Worktree{
		{Path: rootPath, Branch: "main", Main: true},
		{Path: wtPath, Branch: "wt", Locked: true},
	}
	view := ansi.Strip(a.viewProjectsPanel(60, 5))
	if !strings.Contains(view, "（锁定）") {
		t.Fatalf("锁定标记未显示:\n%s", view)
	}
}

func TestDiscardFilePromptMatchesEntryScope(t *testing.T) {
	tests := []struct {
		name     string
		entry    fileEntry
		wantCall string
		wantDesc string
	}{
		{name: "staged", entry: fileEntry{path: "a.txt", staged: true}, wantCall: "discard-all:a.txt", wantDesc: "暂存、工作区"},
		{name: "unstaged", entry: fileEntry{path: "a.txt"}, wantCall: "discard:a.txt", wantDesc: "暂存改动将保留"},
		{name: "untracked", entry: fileEntry{path: "a.txt", untracked: true}, wantCall: "discard:a.txt", wantDesc: "永久删除"},
		{name: "rename", entry: fileEntry{path: "new.txt", origPath: "old.txt", staged: true}, wantCall: "discard-all:new.txt,old.txt", wantDesc: "暂存、工作区"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			a, f := newFakeApp(t)
			a.discardFilePrompt(tt.entry)
			if a.modal == nil || a.modal.onConfirm == nil {
				t.Fatal("丢弃操作缺少确认框")
			}
			if !strings.Contains(a.modal.body, tt.wantDesc) {
				t.Fatalf("确认文案未说明范围 %q: %q", tt.wantDesc, a.modal.body)
			}
			pump(t, a, a.modal.onConfirm(a))
			if !hasCall(f.calls, tt.wantCall) {
				t.Fatalf("丢弃调用范围错误: %v", f.calls)
			}
		})
	}
}

func TestDiscardDirectoryUsesCompleteChangeSet(t *testing.T) {
	a, f := newFakeApp(t)
	n := &fileNode{name: "dir", path: "dir", dir: true, children: []*fileNode{
		{name: "a.txt", path: "dir/a.txt", entries: []fileEntry{
			{path: "dir/a.txt", origPath: "old.txt", staged: true},
			{path: "dir/a.txt"},
		}},
		{name: "new.txt", path: "dir/new.txt", entries: []fileEntry{{path: "dir/new.txt", untracked: true}}},
	}}
	c, ok := choiceByLabel(a.dirMenuChoices(n, true), "丢弃该目录")
	if !ok {
		t.Fatal("目录菜单缺少丢弃入口")
	}
	c.action(a)
	if a.modal == nil || a.modal.onConfirm == nil {
		t.Fatal("目录丢弃缺少确认框")
	}
	pump(t, a, a.modal.onConfirm(a))
	if !hasCall(f.calls, "discard-all:dir/a.txt,old.txt,dir/new.txt") {
		t.Fatalf("目录丢弃未覆盖完整变更集: %v", f.calls)
	}
}

func TestConflictFileMenuOmitsDiscard(t *testing.T) {
	a, _ := newFakeApp(t)
	for _, c := range a.fileMenuChoices(fileEntry{path: "conflict.txt", unmerged: true}, true) {
		if strings.Contains(c.label, "丢弃") {
			t.Fatal("冲突文件不应提供普通丢弃入口")
		}
	}
}

type partialRefreshSvc struct {
	*fakeSvc
	statusErr error
}

func (s *partialRefreshSvc) Status(ctx context.Context, includeUntracked bool) (git.Status, error) {
	if s.statusErr != nil {
		return git.Status{}, s.statusErr
	}
	return s.fakeSvc.Status(ctx, includeUntracked)
}

func (s *partialRefreshSvc) Refs(context.Context) ([]git.Ref, error) {
	return nil, errors.New("refs unavailable")
}

func (s *partialRefreshSvc) State(context.Context) (git.RepoState, error) {
	return git.RepoState{}, errors.New("state unavailable")
}

func (s *partialRefreshSvc) Stashes(context.Context) ([]git.Stash, error) {
	return nil, errors.New("stashes unavailable")
}

func TestQuickRefreshRetainsUnreadSections(t *testing.T) {
	a, _ := newFakeApp(t)
	a.status.Untracked = []git.FileChange{{Path: "new.txt", Untracked: true}}
	a.stashes = []git.Stash{{Hash: "aaaaaaaaaaaa1111", Subject: "WIP"}}
	quick := a.status
	quick.Untracked = nil
	a.Update(refreshMsg{gen: a.refreshGen, quick: true, status: quick, refs: a.refs, state: a.state})
	if len(a.status.Untracked) != 1 || len(a.stashes) != 1 {
		t.Fatalf("quick 刷新清空了未读取数据: status=%+v stashes=%+v", a.status, a.stashes)
	}
}

func TestPartialRefreshFailureRetainsFailedSections(t *testing.T) {
	a, f := newFakeApp(t)
	oldRefs := append([]git.Ref(nil), a.refs...)
	oldState := git.RepoState{Kind: git.StateMerge}
	oldStashes := []git.Stash{{Hash: "bbbbbbbbbbbb2222", Subject: "old stash"}}
	a.refs, a.state, a.stashes = oldRefs, oldState, oldStashes
	f.status = git.Status{Branch: "new-branch", OID: "cccccccccccc3333"}
	a.svc = &partialRefreshSvc{fakeSvc: f}

	msg := a.refreshCmdWith(false, false)().(refreshMsg)
	if msg.statusErr != nil || msg.refsErr == nil || msg.stateErr == nil || msg.stashesErr == nil {
		t.Fatalf("局部刷新错误未分别记录: %+v", msg)
	}
	a.Update(msg)
	if a.status.Branch != "new-branch" {
		t.Fatalf("成功的状态刷新未应用: %+v", a.status)
	}
	if !reflect.DeepEqual(a.refs, oldRefs) || a.state != oldState || !reflect.DeepEqual(a.stashes, oldStashes) {
		t.Fatalf("失败section被清空: refs=%+v state=%+v stashes=%+v", a.refs, a.state, a.stashes)
	}
	if !strings.Contains(a.toast, "refs unavailable") {
		t.Fatalf("局部刷新失败未提示: %q", a.toast)
	}
}

func TestStatusRefreshFailureRetainsWorktree(t *testing.T) {
	a, f := newFakeApp(t)
	oldStatus := a.status
	a.svc = &partialRefreshSvc{fakeSvc: f, statusErr: errors.New("status unavailable")}
	msg := a.refreshCmdWith(false, false)().(refreshMsg)
	a.Update(msg)
	if !reflect.DeepEqual(a.status, oldStatus) {
		t.Fatalf("状态读取失败后工作区被清空: %+v", a.status)
	}
	if !strings.Contains(a.toast, "status unavailable") {
		t.Fatalf("状态读取失败未提示: %q", a.toast)
	}
}

func TestCommandHistoryAndDiagnosticsRedactSecrets(t *testing.T) {
	a, _ := newFakeApp(t)
	raw := "git push https://user:pass@example.com/repo.git -c http.extraHeader=Authorization: Bearer topsecret"
	a.Update(commandMsg{at: time.Now(), line: raw})
	if len(a.commands) != 1 {
		t.Fatal("命令未进入历史")
	}
	for _, secret := range []string{"user:pass", "topsecret"} {
		if strings.Contains(a.commands[0].line, secret) || strings.Contains(plainView(a), secret) {
			t.Fatalf("命令历史展示泄露凭据 %q", secret)
		}
	}

	a.commands = []commandEntry{{at: time.Now(), line: raw}}
	a.openCommandsHeaderMenu()
	c, ok := choiceByLabel(a.modal.choices, "复制全部")
	if !ok {
		t.Fatal("命令历史缺少复制入口")
	}
	c.action(a)
	if strings.Contains(a.toast, "user:pass") || strings.Contains(a.toast, "topsecret") {
		t.Fatalf("复制命令历史提示泄露凭据: %q", a.toast)
	}

	a.logLine("Authorization: Bearer topsecret")
	a.setToast("token=abc123", true)
	if strings.Contains(strings.Join(a.logs, "\n"), "topsecret") || strings.Contains(a.toast, "abc123") {
		t.Fatal("日志或提示泄露凭据")
	}
}

func firstOperationCmd(t *testing.T, cmd tea.Cmd) tea.Cmd {
	t.Helper()
	if cmd == nil {
		t.Fatal("操作未返回命令")
	}
	msg := cmd()
	batch, ok := msg.(tea.BatchMsg)
	if !ok || len(batch) == 0 {
		t.Fatalf("操作命令类型错误: %T", msg)
	}
	return batch[0]
}

func TestWriteOperationRejectsReentry(t *testing.T) {
	a, _ := newFakeApp(t)
	calls := 0
	first := firstOperationCmd(t, a.runOp("first", func(context.Context) (string, error) {
		calls++
		return "", nil
	}))
	if !a.busy || a.opCancel == nil {
		t.Fatal("操作启动前未同步注册忙碌与取消函数")
	}
	if cmd := a.runOp("second", func(context.Context) (string, error) {
		calls++
		return "", nil
	}); cmd != nil {
		t.Fatal("忙碌期间重入不应启动命令")
	}
	if calls != 0 || !strings.Contains(a.toast, "上一个操作") {
		t.Fatalf("忙碌重入保护失败: calls=%d toast=%q", calls, a.toast)
	}
	a.Update(first())
	if calls != 1 || a.busy {
		t.Fatalf("首个操作未正确完成: calls=%d busy=%v", calls, a.busy)
	}
}

func TestWriteOperationCanCancelBeforeCommandStarts(t *testing.T) {
	a, _ := newFakeApp(t)
	calls := 0
	op := firstOperationCmd(t, a.runOp("wait", func(ctx context.Context) (string, error) {
		calls++
		<-ctx.Done()
		return "", ctx.Err()
	}))
	a.Update(tea.KeyMsg{Type: tea.KeyEsc})
	done, ok := op().(opDoneMsg)
	if !ok || !errors.Is(done.err, context.Canceled) {
		t.Fatalf("立即取消失败: %#v", done)
	}
	if calls != 0 {
		t.Fatalf("已取消的操作仍被执行: calls=%d", calls)
	}
	a.Update(done)
	if a.busy || a.opCancel != nil || !strings.Contains(a.toast, "已取消") {
		t.Fatalf("取消状态未清理: busy=%v cancel=%v toast=%q", a.busy, a.opCancel != nil, a.toast)
	}
}

func TestProgressOperationRejectsReentryAndCancelsBeforeStart(t *testing.T) {
	a, _ := newFakeApp(t)
	a.msgs = make(chan tea.Msg, 4)
	calls := 0
	op := firstOperationCmd(t, a.runProgressOp("fetch", func(ctx context.Context, progress func(string)) (string, error) {
		calls++
		progress("fetching")
		<-ctx.Done()
		return "", ctx.Err()
	}))
	if cmd := a.runProgressOp("push", func(context.Context, func(string)) (string, error) {
		calls++
		return "", nil
	}); cmd != nil {
		t.Fatal("进度操作忙碌期间重入不应启动命令")
	}
	a.Update(tea.KeyMsg{Type: tea.KeyEsc})
	done := op().(opDoneMsg)
	if !errors.Is(done.err, context.Canceled) || calls != 0 {
		t.Fatalf("进度操作取消失败: calls=%d err=%v", calls, done.err)
	}
	a.Update(done)
	if a.busy {
		t.Fatal("进度操作取消后未清理忙碌状态")
	}
}

func TestWriteOperationIgnoresStaleCompletionAndProgress(t *testing.T) {
	a, _ := newFakeApp(t)
	first := firstOperationCmd(t, a.runOp("first", func(context.Context) (string, error) { return "", nil }))
	done1 := first().(opDoneMsg)
	a.Update(done1)

	second := firstOperationCmd(t, a.runOp("second", func(context.Context) (string, error) { return "", nil }))
	a.Update(done1)
	if !a.busy || a.taskName != "second" || a.opCancel == nil {
		t.Fatalf("旧完成消息破坏了新操作: busy=%v task=%q", a.busy, a.taskName)
	}
	a.Update(progressMsg{id: done1.id, line: "old progress"})
	if a.toast == "old progress" {
		t.Fatal("旧操作进度覆盖了新操作提示")
	}
	a.Update(progressMsg{id: a.opID, line: "new progress"})
	if a.toast != "new progress" {
		t.Fatalf("当前操作进度未显示: %q", a.toast)
	}
	a.Update(second())
	if a.busy {
		t.Fatal("当前操作完成后仍处于忙碌状态")
	}
}

func TestDetailIgnoresSupersededResponses(t *testing.T) {
	a, _ := newFakeApp(t)
	a.openCommitDetail("old")
	oldGen, oldKey := a.detailGen, a.detailKey
	a.openFileDiff("new.txt", "new.txt", false, true)

	a.Update(detailMsg{gen: oldGen, key: oldKey, kind: "commit", title: "旧详情", diff: "old"})
	if !a.detail.loading || a.detail.title != "new.txt" || len(a.detail.lines) != 0 {
		t.Fatalf("旧详情覆盖了新请求: %+v", a.detail)
	}
	a.Update(detailMsg{gen: oldGen, key: oldKey, kind: "commit", err: errors.New("旧错误")})
	if a.detail.err != "" {
		t.Fatalf("旧请求错误覆盖了新请求: %q", a.detail.err)
	}
	a.Update(detailMsg{gen: a.detailGen, key: "wrong", kind: "file", title: "错误请求", diff: "wrong"})
	if !a.detail.loading || a.detail.title != "new.txt" {
		t.Fatal("请求键不匹配的响应仍被接受")
	}
	a.Update(detailMsg{gen: a.detailGen, key: a.detailKey, kind: "file", title: "new.txt", diff: "+new"})
	if a.detail.loading || len(a.detail.lines) == 0 {
		t.Fatalf("当前详情响应未显示: %+v", a.detail)
	}
}

func TestDetailCloseRejectsLateResponse(t *testing.T) {
	a, _ := newFakeApp(t)
	a.openCommitDetail("aaaaaaaaaaaa1111")
	gen, key := a.detailGen, a.detailKey
	a.Update(tea.KeyMsg{Type: tea.KeyEsc})
	a.Update(detailMsg{gen: gen, key: key, kind: "commit", title: "迟到详情", diff: "late"})
	if a.overlay != overlayNone {
		t.Fatalf("迟到响应重新打开了详情: %v", a.overlay)
	}
}

func TestCompareIgnoresSupersededResponses(t *testing.T) {
	a, _ := newFakeApp(t)
	feature := git.Ref{Name: "feature/x", FullName: "refs/heads/feature/x", Kind: git.RefLocal}
	remote := git.Ref{Name: "origin/main", FullName: "refs/remotes/origin/main", Kind: git.RefRemote}
	a.compareBranchCmd(feature)
	oldGen, oldKey := a.detailGen, a.detailKey
	a.compareBranchCmd(remote)

	a.Update(compareMsg{gen: oldGen, key: oldKey, title: "旧对比", err: errors.New("旧错误")})
	if !a.detail.loading || a.detail.err != "" {
		t.Fatalf("旧对比破坏了新请求: %+v", a.detail)
	}
	a.Update(compareMsg{gen: a.detailGen, key: a.detailKey, title: "新对比"})
	if a.detail.loading || a.detail.title != "新对比" {
		t.Fatalf("当前对比响应未显示: %+v", a.detail)
	}
}

func TestSearchIgnoresSupersededAndClosedResponses(t *testing.T) {
	a, _ := newFakeApp(t)
	a.runSearch("old")
	oldGen := a.searchGen
	a.runSearch("new")
	a.Update(searchDoneMsg{gen: oldGen, query: "old", commits: []git.Commit{{Hash: "old"}}})
	if !a.searching || a.searchQuery != "new" || a.searchResults != nil {
		t.Fatalf("旧搜索破坏了新请求: query=%q searching=%v results=%v", a.searchQuery, a.searching, a.searchResults)
	}
	a.Update(searchDoneMsg{gen: a.searchGen, query: "other", err: errors.New("错误查询")})
	if !a.searching || a.toastErr {
		t.Fatal("查询键不匹配的搜索响应仍被接受")
	}
	current := git.Commit{Hash: "new"}
	a.Update(searchDoneMsg{gen: a.searchGen, query: "new", commits: []git.Commit{current}})
	if a.searching || len(a.searchResults) != 1 || a.searchResults[0].Hash != "new" {
		t.Fatalf("当前搜索结果未显示: %+v", a.searchResults)
	}

	a.runSearch("pending")
	pendingGen := a.searchGen
	a.Update(tea.KeyMsg{Type: tea.KeyEsc})
	a.Update(searchDoneMsg{gen: pendingGen, query: "pending", commits: []git.Commit{{Hash: "late"}}})
	if a.searching || a.searchResults != nil || a.searchQuery != "" {
		t.Fatalf("关闭后的搜索响应仍被接受: %+v", a)
	}
}

func TestFileRefreshPreservesSelectionAndScroll(t *testing.T) {
	a, _ := newFakeApp(t)
	files := make([]fileEntry, 30)
	for i := range files {
		files[i] = fileEntry{path: fmt.Sprintf("f%02d.go", i), status: "M"}
	}
	a.setFiles(files)
	a.fileSel = 15
	a.fileOff = 5
	if got := a.selectedFile(); got == nil || got.path != "f15.go" {
		t.Fatalf("测试选择未生效: %+v", got)
	}
	off := a.fileOff
	a.setFiles(append([]fileEntry{{path: "aaa.go", status: "M"}}, files...))
	if got := a.selectedFile(); got == nil || got.path != "f15.go" {
		t.Fatalf("刷新后丢失原文件选择: %+v", got)
	}
	if a.fileOff != off {
		t.Fatalf("刷新后文件滚动位置变化: %d -> %d", off, a.fileOff)
	}
}

func TestWorkingRefreshPreservesContentScroll(t *testing.T) {
	a, _ := newFakeApp(t)
	a.filesOwner = "work"
	a.setFiles([]fileEntry{{path: "a.txt", status: "M"}, {path: "b.txt", status: "M"}})
	a.fileSel = 1
	f := a.selectedFile()
	if f == nil {
		t.Fatal("测试文件未选中")
	}
	a.loadFileContent(*f, true)
	a.content.lines = make([]string, 100)
	a.content.offset = 10
	a.status = git.Status{Unstaged: []git.FileChange{{Path: "a.txt", Y: 'M'}, {Path: "b.txt", Y: 'M'}}}
	cmd := a.loadWorkingFiles()
	if got := a.selectedFile(); got == nil || got.path != "b.txt" {
		t.Fatalf("工作区刷新后丢失文件选择: %+v", got)
	}
	if a.content.offset != 10 {
		t.Fatalf("工作区刷新后丢失内容滚动位置: %d", a.content.offset)
	}
	if cmd == nil {
		t.Fatal("工作区刷新未重新读取当前文件")
	}
}

func TestContentFailureCanRetryAndEmptyIsDistinct(t *testing.T) {
	a, _ := newFakeApp(t)
	a.filesOwner = "work"
	a.setFiles([]fileEntry{{path: "a.txt", status: "M"}})
	f := a.selectedFile()
	if f == nil {
		t.Fatal("测试文件未选中")
	}
	a.loadFileContent(*f, true)
	gen := a.contentGen
	a.Update(contentMsg{gen: gen, title: "M a.txt", sha: "work", working: true, err: errors.New("读取失败")})
	if a.content.retry != "content" || a.content.err != "读取失败" {
		t.Fatalf("内容读取失败状态不明确: %+v", a.content)
	}
	if !strings.Contains(plainView(a), "r 重试") {
		t.Fatal("内容读取失败未提供重试提示")
	}
	a.pane = PaneContent
	cmd := sendKey(a, tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("r")})
	if cmd == nil {
		t.Fatal("重试未触发内容读取")
	}
	pump(t, a, cmd)
	if a.content.err != "" || a.content.retry != "" {
		t.Fatalf("重试成功后仍保留错误态: %+v", a.content)
	}

	a.loadFileContent(*f, true)
	gen = a.contentGen
	a.Update(contentMsg{gen: gen, title: "M a.txt", sha: "work", working: true, diff: ""})
	if a.content.err != "" || a.content.retry != "" || !strings.Contains(strings.Join(a.content.lines, "\n"), "无差异内容") {
		t.Fatalf("真实空状态被误判为失败: %+v", a.content)
	}
	if cmd := sendKey(a, tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("r")}); cmd != nil {
		t.Fatal("空状态不应提供失败重试")
	}
}

func TestFilesFailureIsNotEmptyAndCanRetry(t *testing.T) {
	a, _ := newFakeApp(t)
	a.filesOwner = "c:aaaaaaaaaaaa1111"
	a.filesGen = 1
	a.setFiles([]fileEntry{{path: "a.txt", status: "M"}})
	a.Update(filesMsg{gen: a.filesGen, owner: a.filesOwner, err: errors.New("读取文件列表失败")})
	if len(a.files) != 1 || a.content.retry != "files" || a.content.err != "读取文件列表失败" {
		t.Fatalf("文件列表失败未保留上下文或未进入错误态: files=%v content=%+v", a.files, a.content)
	}
	if strings.Contains(strings.Join(a.content.lines, "\n"), "没有文件变更") {
		t.Fatal("文件列表读取失败被误报为没有变更")
	}
	a.pane = PaneContent
	cmd := sendKey(a, tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("r")})
	if cmd == nil {
		t.Fatal("文件列表失败未提供重试")
	}

	a.filesGen++
	a.Update(filesMsg{gen: a.filesGen, owner: a.filesOwner})
	if a.content.retry != "" || !strings.Contains(strings.Join(a.content.lines, "\n"), "没有文件变更") {
		t.Fatalf("真实空文件列表状态不明确: %+v", a.content)
	}
}

func TestKeyboardOpensPaneMenus(t *testing.T) {
	cases := []struct {
		pane Pane
		want string
	}{
		{PaneContent, "内容（整体操作）"},
		{PaneLog, "命令历史（整体操作）"},
		{PaneInfo, "提交信息（整体操作）"},
	}
	for _, c := range cases {
		t.Run(fmt.Sprint(c.pane), func(t *testing.T) {
			a, _ := newFakeApp(t)
			a.pane = c.pane
			sendKey(a, tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("b")})
			if a.modal == nil || a.modal.kind != modalChoice || a.modal.title != c.want {
				t.Fatalf("键盘菜单错误: %+v", a.modal)
			}
		})
	}
}

func TestInfoEnterOpensCurrentCommitDetail(t *testing.T) {
	a, _ := newFakeApp(t)
	a.pane = PaneInfo
	a.graphSel = a.dirtyOffset() + 1
	a.clampSelection()
	commit := a.selectedCommit()
	if commit == nil {
		t.Fatal("测试提交未选中")
	}
	cmd := sendKey(a, tea.KeyMsg{Type: tea.KeyEnter})
	if cmd == nil || a.overlay != overlayDetail || a.detail.kind != "commit" || a.detail.sha != commit.Hash {
		t.Fatalf("信息区回车未打开当前提交详情: cmd=%v detail=%+v", cmd != nil, a.detail)
	}

	a.closeDetail()
	a.graphSel = 0
	cmd = sendKey(a, tea.KeyMsg{Type: tea.KeyEnter})
	if cmd != nil || a.overlay != overlayNone {
		t.Fatal("未提交伪行不应打开提交详情")
	}
}

// TestContentRetryDoesNotStealBranchRename 验证内容加载失败后的 r 只作用于内容面板。
func TestContentRetryDoesNotStealBranchRename(t *testing.T) {
	a, _ := newFakeApp(t)
	a.pane = PaneBranches
	a.sideSel = 0
	a.sideSelKey = a.sideItems[0].key
	a.content.retry = "content"
	a.content.err = "读取失败"
	sendKey(a, tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("r")})
	if a.modal == nil || a.modal.kind != modalInput {
		t.Fatalf("分支面板按 r 应打开重命名输入，modal=%+v", a.modal)
	}
}

// TestAutoRefreshErrorToastNotRepeated 验证持续失败不会每 3 秒覆盖成功提示。
func TestAutoRefreshErrorToastNotRepeated(t *testing.T) {
	a, _ := newFakeApp(t)
	msg := refreshMsg{gen: a.refreshGen, auto: true, status: a.status, refs: a.refs, state: a.state, statusErr: errors.New("读取失败")}
	a.Update(msg)
	if a.toast == "" {
		t.Fatal("首次自动刷新失败未提示")
	}
	a.setToast("已复制", false)
	msg.gen = a.refreshGen
	a.Update(msg)
	if a.toast != "已复制" {
		t.Fatalf("重复失败覆盖了新提示: %q", a.toast)
	}
}

// TestF5RefreshesGitInfo 验证 F5 与 R 一样刷新状态和提交历史。
func TestF5RefreshesGitInfo(t *testing.T) {
	a, _ := newFakeApp(t)
	refreshGen, logGen := a.refreshGen, a.logGen
	cmd := sendKey(a, tea.KeyMsg{Type: tea.KeyF5})
	if cmd == nil {
		t.Fatal("F5 未触发刷新")
	}
	if a.refreshGen != refreshGen+1 || a.logGen != logGen+1 {
		t.Fatalf("F5 未刷新状态/日志: refresh %d->%d log %d->%d", refreshGen, a.refreshGen, logGen, a.logGen)
	}
	if !strings.Contains(a.toast, "刷新") {
		t.Fatalf("F5 未提示刷新中: %q", a.toast)
	}
}

// TestCommitMenuPushOnlyForUnpushedLocal 验证只有当前分支未推送的本地提交显示推送项。
func TestCommitMenuPushOnlyForUnpushedLocal(t *testing.T) {
	a, _ := newFakeApp(t)
	for i := range a.refs {
		if a.refs[i].Kind == git.RefRemote {
			a.refs[i].Target = "cccccccccccc3333"
		}
	}
	unpushed := &a.commits[0]
	pushed := &a.commits[2]
	if _, ok := choiceByLabel(a.commitMenuChoices(unpushed), "推送当前分支"); !ok {
		t.Fatalf("本地未推送提交缺少推送项: %+v", a.commitMenuChoices(unpushed))
	}
	if _, ok := choiceByLabel(a.commitMenuChoices(pushed), "推送当前分支"); ok {
		t.Fatalf("已推送提交不应显示推送项: %+v", a.commitMenuChoices(pushed))
	}
	a.status.Detached = true
	a.status.Branch = ""
	if _, ok := choiceByLabel(a.commitMenuChoices(unpushed), "推送当前分支"); ok {
		t.Fatal("分离 HEAD 时不应显示推送项")
	}
}

// TestToastExpiresAndStaleTimerIgnored 验证提示 1 秒到期清空，旧定时器不影响新提示。
func TestToastExpiresAndStaleTimerIgnored(t *testing.T) {
	a, _ := newFakeApp(t)
	a.setToast("第一条", false)
	first := a.toastGen
	a.setToast("第二条", true)
	second := a.toastGen
	a.Update(toastExpireMsg{gen: first})
	if a.toast != "第二条" || !a.toastErr {
		t.Fatalf("旧定时器误清除了新提示: %q err=%v", a.toast, a.toastErr)
	}
	a.Update(toastExpireMsg{gen: second})
	if a.toast != "" || a.toastErr {
		t.Fatalf("提示到期未清空: %q err=%v", a.toast, a.toastErr)
	}
}
