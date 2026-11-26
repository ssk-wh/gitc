package git

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestLiteralPathspecOperations(t *testing.T) {
	_, dir := newTestRepo(t)
	commitFile(t, dir, "a.txt", "base\n", "base a")
	commitFile(t, dir, "[a].txt", "base\n", "base bracket")
	r := mustRepo(t, dir)
	ctx := context.Background()

	writeFile(t, dir, "a.txt", "plain changed\n")
	writeFile(t, dir, "[a].txt", "bracket changed\n")
	if _, err := r.Stage(ctx, "[a].txt"); err != nil {
		t.Fatal(err)
	}
	st, err := r.Status(ctx, true)
	if err != nil {
		t.Fatal(err)
	}
	if len(st.Staged) != 1 || st.Staged[0].Path != "[a].txt" {
		t.Fatalf("暂存范围错误: %+v", st.Staged)
	}
	if len(st.Unstaged) != 1 || st.Unstaged[0].Path != "a.txt" {
		t.Fatalf("普通文件被误暂存: %+v", st.Unstaged)
	}
	if _, err := r.Unstage(ctx, "[a].txt"); err != nil {
		t.Fatal(err)
	}
	st, _ = r.Status(ctx, true)
	if len(st.Staged) != 0 || len(st.Unstaged) != 2 {
		t.Fatalf("取消暂存范围错误: %+v", st)
	}
	diff, err := r.WorkingDiff(ctx, "[a].txt", false, false)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(diff, "bracket changed") || strings.Contains(diff, "plain changed") {
		t.Fatalf("diff 范围错误: %q", diff)
	}

	writeFile(t, dir, "[u].txt", "bracket untracked\n")
	writeFile(t, dir, "u.txt", "plain untracked\n")
	if _, err := r.Discard(ctx, true, "[u].txt"); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(dir, "[u].txt")); !os.IsNotExist(err) {
		t.Fatalf("选中的未跟踪文件未删除: %v", err)
	}
	if _, err := os.Stat(filepath.Join(dir, "u.txt")); err != nil {
		t.Fatalf("未选中的未跟踪文件被删除: %v", err)
	}
}

func TestCommandLogAndErrorsAreSanitized(t *testing.T) {
	r := &Repo{}
	var logged string
	r.SetCommandHook(func(line string) { logged = line })
	r.notify([]string{"push", "https://user:pass@example.com/repo.git", "-c", "http.extraHeader=Authorization: Bearer topsecret"})
	if strings.Contains(logged, "user:pass") || strings.Contains(logged, "topsecret") {
		t.Fatalf("命令历史泄露凭据: %q", logged)
	}
	if !strings.Contains(logged, "https://***@example.com/repo.git") || !strings.Contains(logged, "http.extraHeader=***") {
		t.Fatalf("命令历史未保留可读结构: %q", logged)
	}

	err := (&GitError{
		Args:   []string{"-c", "credential.helper=!echo password=hidden", "fetch", "https://user:pass@example.com/repo.git"},
		Output: "fatal: Authorization: Bearer topsecret\n",
		Err:    errors.New("exit status 128"),
	}).Error()
	for _, secret := range []string{"hidden", "user:pass", "topsecret"} {
		if strings.Contains(err, secret) {
			t.Fatalf("错误信息泄露凭据 %q: %q", secret, err)
		}
	}
	if !strings.Contains(err, "fatal: Authorization: ***") {
		t.Fatalf("错误信息未保留诊断上下文: %q", err)
	}
}

func TestDiscardWorkingTreePreservesStagedChanges(t *testing.T) {
	_, dir := newTestRepo(t)
	commitFile(t, dir, "a.txt", "base\n", "base")
	r := mustRepo(t, dir)
	ctx := context.Background()
	writeFile(t, dir, "a.txt", "staged\n")
	if _, err := r.Stage(ctx, "a.txt"); err != nil {
		t.Fatal(err)
	}
	writeFile(t, dir, "a.txt", "working\n")
	if _, err := r.Discard(ctx, false, "a.txt"); err != nil {
		t.Fatal(err)
	}
	got, err := os.ReadFile(filepath.Join(dir, "a.txt"))
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != "staged\n" {
		t.Fatalf("工作区未恢复到暂存内容: %q", got)
	}
	st, err := r.Status(ctx, true)
	if err != nil {
		t.Fatal(err)
	}
	if len(st.Staged) != 1 || len(st.Unstaged) != 0 {
		t.Fatalf("暂存改动被误清除: %+v", st)
	}
}

func TestDiscardAllClearsMixedDirectoryChanges(t *testing.T) {
	_, dir := newTestRepo(t)
	commitFile(t, dir, "dir/tracked.txt", "base\n", "base")
	commitFile(t, dir, "outside.txt", "outside base\n", "outside")
	r := mustRepo(t, dir)
	ctx := context.Background()

	writeFile(t, dir, "dir/tracked.txt", "staged\n")
	if _, err := r.Stage(ctx, "dir/tracked.txt"); err != nil {
		t.Fatal(err)
	}
	writeFile(t, dir, "dir/tracked.txt", "working\n")
	writeFile(t, dir, "dir/new.txt", "new staged\n")
	if _, err := r.Stage(ctx, "dir/new.txt"); err != nil {
		t.Fatal(err)
	}
	writeFile(t, dir, "dir/nested/untracked.txt", "untracked\n")
	writeFile(t, dir, "outside.txt", "outside changed\n")

	if _, err := r.DiscardAll(ctx, "dir"); err != nil {
		t.Fatal(err)
	}
	got, err := os.ReadFile(filepath.Join(dir, "dir/tracked.txt"))
	if err != nil || string(got) != "base\n" {
		t.Fatalf("目录内跟踪文件未恢复: %q %v", got, err)
	}
	for _, path := range []string{"dir/new.txt", "dir/nested/untracked.txt"} {
		if _, err := os.Stat(filepath.Join(dir, path)); !os.IsNotExist(err) {
			t.Fatalf("目录内新增文件未删除 %s: %v", path, err)
		}
	}
	outside, err := os.ReadFile(filepath.Join(dir, "outside.txt"))
	if err != nil || string(outside) != "outside changed\n" {
		t.Fatalf("目录外文件被误修改: %q %v", outside, err)
	}
	st, err := r.Status(ctx, true)
	if err != nil {
		t.Fatal(err)
	}
	for _, changes := range [][]FileChange{st.Staged, st.Unstaged, st.Untracked} {
		for _, change := range changes {
			if strings.HasPrefix(change.Path, "dir/") {
				t.Fatalf("目录内仍有未清除改动: %+v", change)
			}
		}
	}
}

func TestDiscardAllHandlesStagedRename(t *testing.T) {
	_, dir := newTestRepo(t)
	commitFile(t, dir, "old.txt", "base\n", "base")
	r := mustRepo(t, dir)
	ctx := context.Background()
	if _, err := r.RunGitArgs(ctx, []string{"mv", "old.txt", "new.txt"}); err != nil {
		t.Fatal(err)
	}
	writeFile(t, dir, "new.txt", "working\n")
	if _, err := r.DiscardAll(ctx, "new.txt", "old.txt"); err != nil {
		t.Fatal(err)
	}
	got, err := os.ReadFile(filepath.Join(dir, "old.txt"))
	if err != nil || string(got) != "base\n" {
		t.Fatalf("重命名源文件未恢复: %q %v", got, err)
	}
	if _, err := os.Stat(filepath.Join(dir, "new.txt")); !os.IsNotExist(err) {
		t.Fatalf("重命名目标文件未删除: %v", err)
	}
	st, err := r.Status(ctx, true)
	if err != nil {
		t.Fatal(err)
	}
	if len(st.Staged) != 0 || len(st.Unstaged) != 0 || len(st.Untracked) != 0 {
		t.Fatalf("重命名相关改动未清除: %+v", st)
	}
}

func TestDiscardAllRejectsEmptyPaths(t *testing.T) {
	_, dir := newTestRepo(t)
	commitFile(t, dir, "a.txt", "base\n", "base")
	r := mustRepo(t, dir)
	writeFile(t, dir, "a.txt", "changed\n")
	if _, err := r.DiscardAll(context.Background()); err == nil {
		t.Fatal("空路径不应触发全局丢弃")
	}
	got, err := os.ReadFile(filepath.Join(dir, "a.txt"))
	if err != nil || string(got) != "changed\n" {
		t.Fatalf("空路径调用修改了文件: %q %v", got, err)
	}
}

func TestIgnorePreservesPathWhitespaceAndRejectsSymlink(t *testing.T) {
	_, dir := newTestRepo(t)
	r := mustRepo(t, dir)
	ctx := context.Background()
	writeFile(t, dir, " leading.txt", "ignored\n")
	if _, err := r.Ignore(ctx, " leading.txt"); err != nil {
		t.Fatal(err)
	}
	st, err := r.Status(ctx, true)
	if err != nil {
		t.Fatal(err)
	}
	for _, change := range st.Untracked {
		if change.Path == " leading.txt" {
			t.Fatalf("前导空白路径未被忽略: %+v", st.Untracked)
		}
	}

	outside := filepath.Join(t.TempDir(), "victim.txt")
	if err := os.WriteFile(outside, []byte("do not change\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(outside, filepath.Join(dir, ".gitignore")); err != nil {
		t.Skipf("当前平台无法创建符号链接: %v", err)
	}
	if _, err := r.Ignore(ctx, "other.txt"); err == nil {
		t.Fatal("不应通过 .gitignore 符号链接写入仓库外文件")
	}
	got, err := os.ReadFile(outside)
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != "do not change\n" {
		t.Fatalf("仓库外文件被修改: %q", got)
	}
}

func TestStashOperationsFollowHashAfterStackShift(t *testing.T) {
	setup := func(t *testing.T) (*Repo, string, Stash, Stash) {
		t.Helper()
		_, dir := newTestRepo(t)
		commitFile(t, dir, "a.txt", "base\n", "base")
		r := mustRepo(t, dir)
		ctx := context.Background()
		writeFile(t, dir, "a.txt", "A\n")
		if _, err := r.Stash(ctx, "A", false); err != nil {
			t.Fatal(err)
		}
		writeFile(t, dir, "a.txt", "B\n")
		if _, err := r.Stash(ctx, "B", false); err != nil {
			t.Fatal(err)
		}
		list, err := r.Stashes(ctx)
		if err != nil {
			t.Fatal(err)
		}
		if len(list) != 2 || list[0].Hash == list[1].Hash {
			t.Fatalf("stash 列表错误: %+v", list)
		}
		return r, dir, list[1], list[0]
	}

	t.Run("apply and diff", func(t *testing.T) {
		r, dir, a, _ := setup(t)
		ctx := context.Background()
		diff, err := r.StashDiff(ctx, a.Hash)
		if err != nil {
			t.Fatal(err)
		}
		if !strings.Contains(diff, "+A") {
			t.Fatalf("stash diff 对象错误: %q", diff)
		}
		if _, err := r.StashApply(ctx, a.Hash); err != nil {
			t.Fatal(err)
		}
		got, err := os.ReadFile(filepath.Join(dir, "a.txt"))
		if err != nil {
			t.Fatal(err)
		}
		if string(got) != "A\n" {
			t.Fatalf("应用了错误 stash: %q", got)
		}
		list, _ := r.Stashes(ctx)
		if len(list) != 2 {
			t.Fatalf("apply 不应删除 stash: %+v", list)
		}
	})

	t.Run("pop", func(t *testing.T) {
		r, dir, a, b := setup(t)
		ctx := context.Background()
		if _, err := r.StashPop(ctx, a.Hash); err != nil {
			t.Fatal(err)
		}
		got, err := os.ReadFile(filepath.Join(dir, "a.txt"))
		if err != nil {
			t.Fatal(err)
		}
		if string(got) != "A\n" {
			t.Fatalf("pop 应用了错误 stash: %q", got)
		}
		list, _ := r.Stashes(ctx)
		if len(list) != 1 || list[0].Hash != b.Hash {
			t.Fatalf("pop 删除了错误 stash: %+v", list)
		}
	})

	t.Run("drop and missing", func(t *testing.T) {
		r, _, a, b := setup(t)
		ctx := context.Background()
		if _, err := r.StashDrop(ctx, a.Hash); err != nil {
			t.Fatal(err)
		}
		list, _ := r.Stashes(ctx)
		if len(list) != 1 || list[0].Hash != b.Hash {
			t.Fatalf("drop 删除了错误 stash: %+v", list)
		}
		if _, err := r.StashDrop(ctx, strings.Repeat("0", 40)); err == nil {
			t.Fatal("不存在的 stash 哈希应拒绝操作")
		}
	})
}

func TestRemoteBranchIdentityUsesLongestRemoteName(t *testing.T) {
	_, dir := newTestRepo(t)
	commitFile(t, dir, "a.txt", "base\n", "base")
	remoteA := filepath.Join(t.TempDir(), "team.git")
	remoteB := filepath.Join(t.TempDir(), "team-origin.git")
	runGit(t, dir, "init", "--bare", remoteA)
	runGit(t, dir, "init", "--bare", remoteB)
	runGit(t, dir, "remote", "add", "team", remoteA)
	runGit(t, dir, "remote", "add", "team/origin", remoteB)
	runGit(t, dir, "push", "team", "main:refs/heads/origin/main")
	runGit(t, dir, "push", "team/origin", "main:refs/heads/main")
	runGit(t, dir, "push", "team/origin", "main:refs/heads/feature")
	runGit(t, dir, "fetch", "team")
	runGit(t, dir, "fetch", "team/origin")

	r := mustRepo(t, dir)
	ctx := context.Background()
	if _, err := r.DeleteRemoteBranch(ctx, "refs/remotes/team/origin/main"); err != nil {
		t.Fatal(err)
	}
	out, err := r.RunGitArgs(ctx, []string{"ls-remote", "team/origin", "refs/heads/main"})
	if err != nil {
		t.Fatal(err)
	}
	if strings.TrimSpace(out) != "" {
		t.Fatalf("目标远端分支未删除: %q", out)
	}
	out, err = r.RunGitArgs(ctx, []string{"ls-remote", "team", "refs/heads/origin/main"})
	if err != nil {
		t.Fatal(err)
	}
	if strings.TrimSpace(out) == "" {
		t.Fatal("误删了另一个远端的同名分支")
	}
	if _, err := r.CheckoutRemoteBranch(ctx, "team/origin/feature"); err != nil {
		t.Fatal(err)
	}
	if got := r.CurrentBranch(ctx); got != "feature" {
		t.Fatalf("检出分支 = %q，期望 feature", got)
	}
	refs, err := r.Refs(ctx)
	if err != nil {
		t.Fatal(err)
	}
	for _, ref := range refs {
		if ref.Kind == RefLocal && ref.Name == "feature" {
			if ref.Upstream != "team/origin/feature" {
				t.Fatalf("跟踪上游 = %q", ref.Upstream)
			}
			return
		}
	}
	t.Fatal("未找到新检出的本地分支")
}

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

func TestDiscardAllOnlyUntrackedPath(t *testing.T) {
	_, dir := newTestRepo(t)
	commitFile(t, dir, "base.txt", "base\n", "base")
	r := mustRepo(t, dir)
	ctx := context.Background()
	writeFile(t, dir, "only/untracked.txt", "u\n")
	if _, err := r.DiscardAll(ctx, "only/untracked.txt"); err != nil {
		t.Fatalf("DiscardAll only-untracked: %v", err)
	}
	if _, err := os.Stat(filepath.Join(dir, "only/untracked.txt")); !os.IsNotExist(err) {
		t.Fatalf("未跟踪文件未删除: %v", err)
	}
	st, err := r.Status(ctx, true)
	if err != nil {
		t.Fatal(err)
	}
	if len(st.Staged)+len(st.Unstaged)+len(st.Untracked) != 0 {
		t.Fatalf("仍有改动: %+v", st)
	}
}

func TestDiscardAllBeforeFirstCommit(t *testing.T) {
	_, dir := newTestRepo(t)
	r := mustRepo(t, dir)
	ctx := context.Background()
	writeFile(t, dir, "first.txt", "new\n")
	if _, err := r.Stage(ctx, "first.txt"); err != nil {
		t.Fatal(err)
	}
	if _, err := r.DiscardAll(ctx, "first.txt"); err != nil {
		t.Fatalf("DiscardAll 无 HEAD: %v", err)
	}
	if _, err := os.Stat(filepath.Join(dir, "first.txt")); !os.IsNotExist(err) {
		t.Fatalf("首个提交前文件未删除: %v", err)
	}
	st, err := r.Status(ctx, true)
	if err != nil {
		t.Fatal(err)
	}
	if len(st.Staged)+len(st.Unstaged)+len(st.Untracked) != 0 {
		t.Fatalf("仍有改动: %+v", st)
	}
}
