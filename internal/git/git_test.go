package git

import (
	"context"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func runGit(t *testing.T, dir string, args ...string) string {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, "git", append([]string{"-c", "core.fscache=false", "-C", dir}, args...)...)
	cmd.Env = append(os.Environ(), "GIT_AUTHOR_DATE=2026-01-01T00:00:00Z", "GIT_COMMITTER_DATE=2026-01-01T00:00:00Z")
	cmd.WaitDelay = 2 * time.Second
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("git %v 失败: %v\n%s", args, err, out)
	}
	return string(out)
}

func tempDir(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	t.Cleanup(func() {
		// Windows 上 Git 退出后文件句柄可能短暂保留。
		for i := 0; i < 20; i++ {
			if err := os.RemoveAll(dir); err == nil {
				return
			}
			time.Sleep(100 * time.Millisecond)
		}
	})
	return dir
}

func newTestRepo(t *testing.T) (*Repo, string) {
	t.Helper()
	dir := tempDir(t)
	runGit(t, dir, "init", "-b", "main")
	runGit(t, dir, "config", "user.name", "测试用户")
	runGit(t, dir, "config", "user.email", "test@example.com")
	runGit(t, dir, "config", "commit.gpgsign", "false")
	runGit(t, dir, "config", "core.autocrlf", "false")
	return nil, dir
}

func mustRepo(t *testing.T, dir string) *Repo {
	t.Helper()
	r, err := Open(dir)
	if err != nil {
		t.Fatalf("Open 失败: %v", err)
	}
	return r
}

func writeFile(t *testing.T, dir, name, content string) {
	t.Helper()
	p := filepath.Join(dir, name)
	if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(p, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
}

func commitFile(t *testing.T, dir, name, content, msg string) string {
	t.Helper()
	writeFile(t, dir, name, content)
	runGit(t, dir, "add", "-A")
	runGit(t, dir, "commit", "-m", msg)
	out := runGit(t, dir, "rev-parse", "HEAD")
	return strings.TrimSpace(out)
}

func TestOpenAndRefs(t *testing.T) {
	_, dir := newTestRepo(t)
	sha := commitFile(t, dir, "a.txt", "hello\n", "初始提交")
	r := mustRepo(t, dir)

	if filepath.Base(r.Root) != filepath.Base(dir) {
		t.Errorf("Root = %s, 期望 %s", r.Root, dir)
	}
	refs, err := r.Refs(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	var main *Ref
	for i := range refs {
		if refs[i].Kind == RefLocal && refs[i].Name == "main" {
			main = &refs[i]
		}
	}
	if main == nil {
		t.Fatalf("未找到 main 分支: %+v", refs)
	}
	if !main.Head {
		t.Error("main 应为当前分支")
	}
	if main.Target != sha {
		t.Errorf("main target = %s, 期望 %s", main.Target, sha)
	}
}

func TestStreamLog(t *testing.T) {
	_, dir := newTestRepo(t)
	commitFile(t, dir, "a.txt", "1\n", "第一条")
	commitFile(t, dir, "a.txt", "2\n", "第二条")
	commitFile(t, dir, "a.txt", "3\n", "第三条")
	r := mustRepo(t, dir)

	var commits []Commit
	err := r.StreamLog(context.Background(), 0, func(c Commit) bool {
		commits = append(commits, c)
		return true
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(commits) != 3 {
		t.Fatalf("提交数 = %d, 期望 3", len(commits))
	}
	if commits[0].Subject != "第三条" {
		t.Errorf("首条 = %q, 期望 第三条", commits[0].Subject)
	}
	if len(commits[1].Parents) != 1 || commits[1].Parents[0] != commits[2].Hash {
		t.Errorf("父子关系错误: %+v", commits)
	}
	if commits[0].When.IsZero() || commits[0].When.Year() != 2026 {
		t.Errorf("时间解析错误: %v", commits[0].When)
	}

	// max 限制
	var n int
	if err := r.StreamLog(context.Background(), 2, func(Commit) bool { n++; return true }); err != nil {
		t.Fatal(err)
	}
	if n != 2 {
		t.Errorf("max=2 时回调 %d 次", n)
	}
}

func TestStatusParse(t *testing.T) {
	_, dir := newTestRepo(t)
	commitFile(t, dir, "tracked.txt", "v1\n", "初始")
	r := mustRepo(t, dir)
	ctx := context.Background()

	writeFile(t, dir, "tracked.txt", "v2\n")
	writeFile(t, dir, "new.txt", "new\n")
	st, err := r.Status(ctx, true)
	if err != nil {
		t.Fatal(err)
	}
	if st.Branch != "main" || st.Detached {
		t.Errorf("分支解析错误: %+v", st)
	}
	if len(st.Unstaged) != 1 || st.Unstaged[0].Path != "tracked.txt" {
		t.Errorf("未暂存变更错误: %+v", st.Unstaged)
	}
	if len(st.Untracked) != 1 || st.Untracked[0].Path != "new.txt" {
		t.Errorf("未跟踪变更错误: %+v", st.Untracked)
	}

	runGit(t, dir, "add", "tracked.txt")
	st, _ = r.Status(ctx, true)
	if len(st.Staged) != 1 || st.Staged[0].Path != "tracked.txt" {
		t.Errorf("暂存变更错误: %+v", st.Staged)
	}
}

func TestParseStatusFixture(t *testing.T) {
	out := strings.Join([]string{
		"# branch.oid abc123",
		"# branch.head main",
		"# branch.upstream origin/main",
		"# branch.ab +2 -1",
		"1 M. N... 100644 100644 100644 aaa bbb staged file.txt",
		"1 .M N... 100644 100644 100644 aaa bbb work file.txt",
		"2 R. N... 100644 100644 100644 aaa bbb R100 new name.txt",
		"old name.txt",
		"? untracked with space.txt",
		"",
	}, "\x00")
	st, err := ParseStatus(out)
	if err != nil {
		t.Fatal(err)
	}
	if st.Branch != "main" || st.Upstream != "origin/main" || st.Ahead != 2 || st.Behind != 1 {
		t.Errorf("分支信息错误: %+v", st)
	}
	if len(st.Staged) != 2 {
		t.Errorf("暂存数 = %d: %+v", len(st.Staged), st.Staged)
	}
	if st.Staged[1].OrigPath != "old name.txt" || st.Staged[1].Path != "new name.txt" {
		t.Errorf("重命名解析错误: %+v", st.Staged[1])
	}
	if len(st.Unstaged) != 1 || st.Unstaged[0].Path != "work file.txt" {
		t.Errorf("工作区解析错误: %+v", st.Unstaged)
	}
	if len(st.Untracked) != 1 || st.Untracked[0].Path != "untracked with space.txt" {
		t.Errorf("未跟踪解析错误: %+v", st.Untracked)
	}
}

func TestOpsFlow(t *testing.T) {
	_, dir := newTestRepo(t)
	ctx := context.Background()
	commitFile(t, dir, "a.txt", "1\n", "c1")
	r := mustRepo(t, dir)

	// 创建/切换分支
	if _, err := r.CreateBranch(ctx, "feature", "main", true); err != nil {
		t.Fatal(err)
	}
	if b := r.CurrentBranch(ctx); b != "feature" {
		t.Fatalf("当前分支 = %s", b)
	}
	commitFile(t, dir, "b.txt", "b\n", "c2")
	if _, err := r.Checkout(ctx, "main"); err != nil {
		t.Fatal(err)
	}
	// 合并
	out, err := r.Merge(ctx, "feature")
	if err != nil {
		t.Fatalf("合并失败: %v\n%s", err, out)
	}
	// 标签
	if _, err := r.CreateTag(ctx, "v1.0", "HEAD", "版本 1.0"); err != nil {
		t.Fatal(err)
	}
	refs, _ := r.Refs(ctx)
	found := false
	for _, ref := range refs {
		if ref.Kind == RefTag && ref.Name == "v1.0" {
			found = true
		}
	}
	if !found {
		t.Error("标签未创建")
	}
	// stash
	writeFile(t, dir, "a.txt", "dirty\n")
	if _, err := r.Stash(ctx, "wip", false); err != nil {
		t.Fatal(err)
	}
	stashes, err := r.Stashes(ctx)
	if err != nil || len(stashes) != 1 {
		t.Fatalf("stash 列表错误: %v %+v", err, stashes)
	}
	if _, err := r.StashPop(ctx, stashes[0].Hash); err != nil {
		t.Fatal(err)
	}
	// reset
	head := strings.TrimSpace(runGit(t, dir, "rev-parse", "HEAD"))
	if _, err := r.Reset(ctx, head, ResetSoft); err != nil {
		t.Fatal(err)
	}
}

func TestMergeConflictState(t *testing.T) {
	_, dir := newTestRepo(t)
	ctx := context.Background()
	commitFile(t, dir, "f.txt", "base\n", "base")
	runGit(t, dir, "checkout", "-b", "side")

	commitFile(t, dir, "f.txt", "side\n", "side change")

	runGit(t, dir, "checkout", "main")
	commitFile(t, dir, "f.txt", "main\n", "main change")
	r := mustRepo(t, dir)

	if _, err := r.Merge(ctx, "side"); err == nil {
		t.Fatal("应当产生冲突")
	}
	state, err := r.State(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if state.Kind != StateMerge {
		t.Errorf("状态 = %s, 期望 merge", state.Kind)
	}
	st, _ := r.Status(ctx, true)
	if len(st.Unstaged) == 0 || !st.Unstaged[0].Unmerged {
		t.Errorf("冲突文件未标记: %+v", st.Unstaged)
	}
	if _, err := r.AbortOperation(ctx, StateMerge); err != nil {
		t.Fatalf("abort 失败: %v", err)
	}
	state, _ = r.State(ctx)
	if state.Kind != StateNone {
		t.Errorf("abort 后状态 = %s", state.Kind)
	}
}

func TestCommitDetailAndDiff(t *testing.T) {
	_, dir := newTestRepo(t)
	sha := commitFile(t, dir, "a.txt", "hello\n", "提交标题")
	writeFile(t, dir, "b.txt", "world\n")
	runGit(t, dir, "add", "-A")
	runGit(t, dir, "commit", "-m", "第二条\n\n正文内容")
	head := strings.TrimSpace(runGit(t, dir, "rev-parse", "HEAD"))
	r := mustRepo(t, dir)

	d, err := r.CommitDetail(context.Background(), head)
	if err != nil {
		t.Fatal(err)
	}
	if d.Subject != "第二条" || !strings.Contains(d.Body, "正文内容") {
		t.Errorf("标题/正文错误: %+v", d)
	}
	if len(d.Files) != 1 || d.Files[0].Path != "b.txt" {
		t.Errorf("文件列表错误: %+v", d.Files)
	}
	patch, err := r.CommitDiff(context.Background(), head, "")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(patch, "+world") {
		t.Errorf("diff 内容缺失: %s", patch)
	}
	// 第一条提交的 diff 不应包含 b.txt
	patch1, err := r.CommitDiff(context.Background(), sha, "")
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(patch1, "b.txt") {
		t.Errorf("第一条提交 diff 错误: %s", patch1)
	}
}

func TestPushToLocalBare(t *testing.T) {
	_, dir := newTestRepo(t)
	ctx := context.Background()
	commitFile(t, dir, "a.txt", "1\n", "c1")
	remote := filepath.Join(tempDir(t), "remote.git")
	runGit(t, dir, "init", "--bare", remote)
	r := mustRepo(t, dir)
	runGit(t, dir, "remote", "add", "origin", remote)

	if _, err := r.Push(ctx, PushOptions{SetUpstream: true}, "main", nil); err != nil {
		t.Fatalf("push 失败: %v", err)
	}
	out := runGit(t, remote, "log", "--oneline", "main")
	if !strings.Contains(out, "c1") {
		t.Errorf("远端未收到提交: %s", out)
	}
	st, _ := r.Status(ctx, true)
	if st.Upstream != "origin/main" {
		t.Errorf("上游 = %q", st.Upstream)
	}
}

func TestSplitArgs(t *testing.T) {
	cases := []struct {
		in   string
		want []string
	}{
		{"status", []string{"status"}},
		{"push -u origin main", []string{"push", "-u", "origin", "main"}},
		{`commit -m "hello world"`, []string{"commit", "-m", "hello world"}},
		{`commit -m 'a b'`, []string{"commit", "-m", "a b"}},
		{`log --grep=a\ b`, []string{"log", "--grep=a b"}},
	}
	for _, c := range cases {
		got := SplitArgs(c.in)
		if strings.Join(got, "|") != strings.Join(c.want, "|") {
			t.Errorf("SplitArgs(%q) = %v, 期望 %v", c.in, got, c.want)
		}
	}
}

func TestVersion(t *testing.T) {
	if v := ParseVersion("git version 2.36.1.windows.1"); v != "2.36.1" {
		t.Errorf("版本解析 = %q", v)
	}
	if VersionAtLeast("2.23.0", "2.23") != true {
		t.Error("2.23.0 应 >= 2.23")
	}
	if VersionAtLeast("2.20.0", "2.23") != false {
		t.Error("2.20.0 不应 >= 2.23")
	}
}

func TestOpenNonRepo(t *testing.T) {
	dir := tempDir(t)
	if _, err := Open(dir); err == nil {
		t.Fatal("非 git 目录应报错")
	}
}

func TestTimeParsingConsistency(t *testing.T) {
	_, dir := newTestRepo(t)
	commitFile(t, dir, "a.txt", "1\n", "c1")
	r := mustRepo(t, dir)
	var cm Commit
	_ = r.StreamLog(context.Background(), 1, func(c Commit) bool { cm = c; return false })
	if d := time.Since(cm.When); d < 0 || d > 365*24*time.Hour {
		t.Errorf("提交时间异常: %v", cm.When)
	}
}

func TestEmptyRepo(t *testing.T) {
	_, dir := newTestRepo(t)
	r := mustRepo(t, dir)
	var n int
	if err := r.StreamLog(context.Background(), 0, func(Commit) bool { n++; return true }); err != nil {
		t.Fatalf("空仓库读取日志应成功: %v", err)
	}
	if n != 0 {
		t.Errorf("空仓库提交数 = %d", n)
	}
	st, err := r.Status(context.Background(), true)
	if err != nil {
		t.Fatal(err)
	}
	if st.Total() != 0 {
		t.Errorf("空仓库变更数 = %d", st.Total())
	}
	refs, err := r.Refs(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if len(refs) != 0 {
		t.Errorf("空仓库引用数 = %d", len(refs))
	}
}

func TestDetachedHead(t *testing.T) {
	_, dir := newTestRepo(t)
	commitFile(t, dir, "a.txt", "1\n", "c1")
	runGit(t, dir, "checkout", "--detach", "HEAD")
	r := mustRepo(t, dir)
	st, err := r.Status(context.Background(), true)
	if err != nil {
		t.Fatal(err)
	}
	if !st.Detached || st.Branch != "" {
		t.Errorf("detached 解析错误: %+v", st)
	}
	if len(st.OID) < 7 {
		t.Errorf("detached OID 缺失: %+v", st)
	}
}

func TestWorktreeRefs(t *testing.T) {
	_, dir := newTestRepo(t)
	commitFile(t, dir, "a.txt", "1\n", "c1")
	wt := filepath.Join(tempDir(t), "wt")
	runGit(t, dir, "worktree", "add", "-b", "wt-branch", wt)
	r := mustRepo(t, wt)
	if b := r.CurrentBranch(context.Background()); b != "wt-branch" {
		t.Errorf("worktree 分支 = %q", b)
	}
	refs, err := r.Refs(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if len(refs) < 2 {
		t.Errorf("worktree 中引用数 = %d", len(refs))
	}
}

// TestStreamLogScope 验证本地/远端范围筛选（无远端时远端结果为空）。
func TestStreamLogScope(t *testing.T) {
	_, dir := newTestRepo(t)
	commitFile(t, dir, "a.txt", "1\n", "main-1")
	runGit(t, dir, "checkout", "-b", "feature")
	commitFile(t, dir, "b.txt", "2\n", "feat-1")
	runGit(t, dir, "checkout", "main")
	r := mustRepo(t, dir)

	collect := func(scope string) []string {
		var subjects []string
		err := r.StreamLogScope(context.Background(), scope, 100, func(c Commit) bool {
			subjects = append(subjects, c.Subject)
			return true
		})
		if err != nil {
			t.Fatalf("scope %q: %v", scope, err)
		}
		return subjects
	}

	local := collect("local")
	if len(local) != 2 || local[0] != "feat-1" || local[1] != "main-1" {
		t.Errorf("local 范围 = %v", local)
	}
	if remote := collect("remote"); len(remote) != 0 {
		t.Errorf("无远端时 remote 范围应为空: %v", remote)
	}
	if all := collect(""); len(all) != 2 {
		t.Errorf("all 范围 = %v", all)
	}
}

func TestSquashCommits(t *testing.T) {
	_, dir := newTestRepo(t)
	r := mustRepo(t, dir)
	h1 := commitFile(t, dir, "a.txt", "1", "first")
	h2 := commitFile(t, dir, "a.txt", "2", "second")
	h3 := commitFile(t, dir, "a.txt", "3", "third")
	ctx := context.Background()
	if _, err := r.Squash(ctx, h3, h1, "squashed two"); err != nil {
		t.Fatalf("Squash: %v", err)
	}
	head := strings.TrimSpace(runGit(t, dir, "rev-parse", "HEAD"))
	if n := strings.TrimSpace(runGit(t, dir, "rev-list", "--count", h1+"..HEAD")); n != "1" {
		t.Fatalf("合并后提交数 = %s，期望 1", n)
	}
	msg, err := r.CommitMessage(ctx, head)
	if err != nil {
		t.Fatal(err)
	}
	if msg != "squashed two" {
		t.Fatalf("合并信息 = %q", msg)
	}
	if diff := runGit(t, dir, "diff", "--stat", h3, head); strings.TrimSpace(diff) != "" {
		t.Fatalf("合并后内容不一致:\n%s", diff)
	}
	_ = h2
}

func TestEditCommitMessage(t *testing.T) {
	_, dir := newTestRepo(t)
	r := mustRepo(t, dir)
	commitFile(t, dir, "a.txt", "1", "first")
	h2 := commitFile(t, dir, "a.txt", "2", "second")
	commitFile(t, dir, "a.txt", "3", "third")
	authorBefore := strings.TrimSpace(runGit(t, dir, "log", "-1", "--format=%an <%ae>", h2))
	ctx := context.Background()
	if _, err := r.EditCommitMessage(ctx, h2, "second edited"); err != nil {
		t.Fatalf("EditCommitMessage: %v", err)
	}
	head := strings.TrimSpace(runGit(t, dir, "rev-parse", "HEAD"))
	msg, err := r.CommitMessage(ctx, head+"^")
	if err != nil {
		t.Fatal(err)
	}
	if msg != "second edited" {
		t.Fatalf("编辑后信息 = %q", msg)
	}
	if got := strings.TrimSpace(runGit(t, dir, "log", "-1", "--format=%an <%ae>", head+"^")); got != authorBefore {
		t.Fatalf("作者被改变: %q -> %q", authorBefore, got)
	}
	// 后续提交仍保留在 HEAD
	if got := strings.TrimSpace(runGit(t, dir, "log", "-1", "--format=%s")); got != "third" {
		t.Fatalf("HEAD 信息 = %q", got)
	}
}

func TestWorktrees(t *testing.T) {
	_, dir := newTestRepo(t)
	r := mustRepo(t, dir)
	commitFile(t, dir, "a.txt", "1", "first")
	ctx := context.Background()
	if _, err := r.AddWorktree(ctx, filepath.Join(tempDir(t), "wt1"), "wt-branch"); err != nil {
		t.Fatalf("AddWorktree: %v", err)
	}
	wts, err := r.Worktrees(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if len(wts) != 2 {
		t.Fatalf("worktree 数 = %d: %+v", len(wts), wts)
	}
	if !wts[0].Main {
		t.Errorf("第一个应为主 worktree: %+v", wts[0])
	}
	found := false
	for _, wt := range wts {
		if wt.Branch == "wt-branch" {
			found = true
			if wt.Path == "" || wt.Head == "" {
				t.Errorf("worktree 字段缺失: %+v", wt)
			}
		}
	}
	if !found {
		t.Fatalf("未找到新建 worktree: %+v", wts)
	}
	if _, err := r.RemoveWorktree(ctx, wts[1].Path, true); err != nil {
		t.Fatalf("RemoveWorktree: %v", err)
	}
	wts, err = r.Worktrees(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if len(wts) != 1 {
		t.Fatalf("移除后 worktree 数 = %d", len(wts))
	}
}

func TestIgnorePath(t *testing.T) {
	_, dir := newTestRepo(t)
	r := mustRepo(t, dir)
	writeFile(t, dir, "build/out.txt", "x")
	ctx := context.Background()
	out, err := r.Ignore(ctx, "build/out.txt")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out, "/build/out.txt") {
		t.Errorf("输出 = %q", out)
	}
	data, err := os.ReadFile(filepath.Join(dir, ".gitignore"))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(data), "/build/out.txt") {
		t.Errorf(".gitignore = %q", data)
	}
	// 重复添加不产生重复行
	if _, err := r.Ignore(ctx, "build/out.txt"); err != nil {
		t.Fatal(err)
	}
	data, _ = os.ReadFile(filepath.Join(dir, ".gitignore"))
	if strings.Count(string(data), "/build/out.txt") != 1 {
		t.Errorf("重复写入: %q", data)
	}
}

func TestIgnoreCanceledContextDoesNotWrite(t *testing.T) {
	_, dir := newTestRepo(t)
	r := mustRepo(t, dir)
	writeFile(t, dir, "build/out.txt", "x")
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := r.Ignore(ctx, "build/out.txt"); !errors.Is(err, context.Canceled) {
		t.Fatalf("取消的 Ignore 应返回 context.Canceled: %v", err)
	}
	if _, err := os.Stat(filepath.Join(dir, ".gitignore")); !os.IsNotExist(err) {
		t.Fatalf("取消后仍写入了 .gitignore: %v", err)
	}
}
