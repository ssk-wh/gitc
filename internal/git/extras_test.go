package git

import (
	"context"
	"path/filepath"
	"strings"
	"testing"
)

// TestCompareBranches 验证分支对比的提交范围、差异统计与边界。
func TestCompareBranches(t *testing.T) {
	_, dir := newTestRepo(t)
	commitFile(t, dir, "a.txt", "1\n", "main-1")
	runGit(t, dir, "checkout", "-q", "-b", "feature")
	commitFile(t, dir, "b.txt", "2\n", "feature-1")
	r := mustRepo(t, dir)
	ctx := context.Background()

	sum, err := r.CompareBranches(ctx, "main", "feature", 10)
	if err != nil {
		t.Fatal(err)
	}
	if len(sum.Commits) != 1 || sum.Commits[0].Subject != "feature-1" {
		t.Fatalf("独有提交 = %+v", sum.Commits)
	}
	if !strings.Contains(sum.DiffStat, "b.txt") {
		t.Fatalf("差异统计缺少 b.txt: %q", sum.DiffStat)
	}
	if sum.Truncated {
		t.Error("提交未超上限时不应标记截断")
	}

	// 新增一条提交后，max=1 应截断。
	commitFile(t, dir, "b.txt", "3\n", "feature-2")
	sum, err = r.CompareBranches(ctx, "main", "feature", 1)
	if err != nil {
		t.Fatal(err)
	}
	if len(sum.Commits) != 1 || !sum.Truncated {
		t.Fatalf("截断结果: commits=%d truncated=%v", len(sum.Commits), sum.Truncated)
	}

	// 目标分支不存在、参数为空都应报错。
	if _, err := r.CompareBranches(ctx, "main", "missing", 10); err == nil {
		t.Error("不存在的分支应报错")
	}
	if _, err := r.CompareBranches(ctx, "", "feature", 10); err == nil {
		t.Error("空分支名应报错")
	}
}

// TestUnsetUpstream 验证取消上游后 Ref.Upstream 为空。
func TestUnsetUpstream(t *testing.T) {
	_, dir := newTestRepo(t)
	commitFile(t, dir, "a.txt", "1\n", "first")
	remote := filepath.Join(t.TempDir(), "remote.git")
	runGit(t, dir, "init", "--bare", remote)
	r := mustRepo(t, dir)
	ctx := context.Background()
	runGit(t, dir, "remote", "add", "origin", remote)
	if _, err := r.Push(ctx, PushOptions{SetUpstream: true}, "main", nil); err != nil {
		t.Fatalf("push: %v", err)
	}
	if up := mainRef(t, r).Upstream; up != "origin/main" {
		t.Fatalf("推送后上游 = %q", up)
	}
	if _, err := r.UnsetUpstream(ctx, "main"); err != nil {
		t.Fatalf("UnsetUpstream: %v", err)
	}
	if up := mainRef(t, r).Upstream; up != "" {
		t.Fatalf("取消上游后 = %q", up)
	}
}

// mainRef 返回 main 分支的引用。
func mainRef(t *testing.T, r *Repo) Ref {
	t.Helper()
	refs, err := r.Refs(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	for _, ref := range refs {
		if ref.Kind == RefLocal && ref.Name == "main" {
			return ref
		}
	}
	t.Fatal("未找到 main 分支")
	return Ref{}
}

// TestWorktreeLockUnlock 验证 worktree 锁定状态可写可读可解除。
func TestWorktreeLockUnlock(t *testing.T) {
	_, dir := newTestRepo(t)
	commitFile(t, dir, "a.txt", "1\n", "first")
	r := mustRepo(t, dir)
	ctx := context.Background()
	wtPath := filepath.Join(t.TempDir(), "wt")
	if _, err := r.AddWorktree(ctx, wtPath, "wt-branch"); err != nil {
		t.Fatalf("AddWorktree: %v", err)
	}
	find := func() Worktree {
		t.Helper()
		wts, err := r.Worktrees(ctx)
		if err != nil {
			t.Fatal(err)
		}
		for _, wt := range wts {
			if wt.Branch == "wt-branch" {
				return wt
			}
		}
		t.Fatalf("未找到 worktree: %+v", wts)
		return Worktree{}
	}
	if wt := find(); wt.Locked {
		t.Fatal("新建 worktree 不应处于锁定状态")
	}
	if _, err := r.LockWorktree(ctx, wtPath, "测试锁定"); err != nil {
		t.Fatalf("LockWorktree: %v", err)
	}
	if wt := find(); !wt.Locked {
		t.Fatal("锁定后 Locked 应为 true")
	}
	if _, err := r.UnlockWorktree(ctx, wtPath); err != nil {
		t.Fatalf("UnlockWorktree: %v", err)
	}
	if wt := find(); wt.Locked {
		t.Fatal("解锁后 Locked 应为 false")
	}
}

// TestAddWorktreeExistingBranch 已存在的分支可直接检出到新 worktree，而不是报“分支已存在”。
func TestAddWorktreeExistingBranch(t *testing.T) {
	_, dir := newTestRepo(t)
	commitFile(t, dir, "a.txt", "1\n", "first")
	runGit(t, dir, "branch", "existing")
	r := mustRepo(t, dir)
	ctx := context.Background()
	wtPath := filepath.Join(t.TempDir(), "wt-existing")
	if _, err := r.AddWorktree(ctx, wtPath, "existing"); err != nil {
		t.Fatalf("为已有分支添加 worktree: %v", err)
	}
	wts, err := r.Worktrees(ctx)
	if err != nil {
		t.Fatal(err)
	}
	found := false
	for _, wt := range wts {
		if wt.Branch == "existing" && wt.Path != "" {
			found = true
		}
	}
	if !found {
		t.Fatalf("未找到 existing 分支的 worktree: %+v", wts)
	}
}

// TestRenameBranchUpstream 验证重命名后的上游处理：
// 远端存在新名 → 改跟踪新名；远端没有新名 → 清除同名旧上游；跨名跟踪保持不变。
func TestRenameBranchUpstream(t *testing.T) {
	ctx := context.Background()
	setup := func(t *testing.T) (*Repo, string) {
		t.Helper()
		_, dir := newTestRepo(t)
		commitFile(t, dir, "a.txt", "1\n", "first")
		remote := filepath.Join(t.TempDir(), "remote.git")
		runGit(t, dir, "init", "--bare", remote)
		r := mustRepo(t, dir)
		runGit(t, dir, "remote", "add", "origin", remote)
		if _, err := r.Push(ctx, PushOptions{SetUpstream: true}, "main", nil); err != nil {
			t.Fatalf("push: %v", err)
		}
		return r, dir
	}
	upstreamOf := func(t *testing.T, r *Repo, branch string) string {
		t.Helper()
		refs, err := r.Refs(ctx)
		if err != nil {
			t.Fatal(err)
		}
		for _, ref := range refs {
			if ref.Kind == RefLocal && ref.Name == branch {
				return ref.Upstream
			}
		}
		t.Fatalf("未找到分支 %s", branch)
		return ""
	}

	t.Run("远端无新名时清除旧上游", func(t *testing.T) {
		r, _ := setup(t)
		out, err := r.RenameBranch(ctx, "main", "renamed")
		if err != nil {
			t.Fatalf("RenameBranch: %v", err)
		}
		if up := upstreamOf(t, r, "renamed"); up != "" {
			t.Fatalf("新分支上游 = %q，期望空", up)
		}
		if !strings.Contains(out, "旧上游") {
			t.Errorf("输出未说明上游处理: %q", out)
		}
	})

	t.Run("远端已有新名时改跟踪新名", func(t *testing.T) {
		r, dir := setup(t)
		runGit(t, dir, "branch", "old", "main")
		runGit(t, dir, "push", "-q", "-u", "origin", "old")
		// 预先创建远端 renamed，模拟已存在同名远端分支。
		runGit(t, dir, "push", "-q", "origin", "main:refs/heads/renamed")
		if _, err := r.RenameBranch(ctx, "old", "renamed"); err != nil {
			t.Fatalf("RenameBranch: %v", err)
		}
		if up := upstreamOf(t, r, "renamed"); up != "origin/renamed" {
			t.Fatalf("新分支上游 = %q，期望 origin/renamed", up)
		}
	})

	t.Run("跨名跟踪保持不变", func(t *testing.T) {
		r, dir := setup(t)
		runGit(t, dir, "branch", "tracker", "main")
		runGit(t, dir, "branch", "--set-upstream-to=origin/main", "tracker")
		if _, err := r.RenameBranch(ctx, "tracker", "tracker2"); err != nil {
			t.Fatalf("RenameBranch: %v", err)
		}
		if up := upstreamOf(t, r, "tracker2"); up != "origin/main" {
			t.Fatalf("跨名上游 = %q，期望 origin/main", up)
		}
	})
}
