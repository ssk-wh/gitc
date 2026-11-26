package ui

import (
	"context"
	"strings"
	"testing"
	"time"

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
	if !hasCall(f.calls, "compare:main..feature/x") {
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
