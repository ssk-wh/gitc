package git

import (
	"bufio"
	"bytes"
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
)

// runProgress 执行命令并把 stderr 的进度行实时回调（fetch/push/pull 用）。
func (r *Repo) runProgress(ctx context.Context, args []string, progress func(string)) (string, error) {
	c := r.cmd(ctx, false, args...)
	var outBuf bytes.Buffer
	c.Stdout = &outBuf
	stderr, err := c.StderrPipe()
	if err != nil {
		return "", err
	}
	if err := c.Start(); err != nil {
		return "", &GitError{Args: args, Err: err}
	}
	var errBuf strings.Builder
	sc := bufio.NewScanner(stderr)
	sc.Buffer(make([]byte, 0, 64*1024), 1<<20)
	// git 进度用 CR 刷新同一行，这里同时按 CR 与 LF 分段。
	sc.Split(func(data []byte, atEOF bool) (int, []byte, error) {
		for i, b := range data {
			if b == '\n' || b == '\r' {
				return i + 1, data[:i], nil
			}
		}
		if atEOF && len(data) > 0 {
			return len(data), data, nil
		}
		return 0, nil, nil
	})
	for sc.Scan() {
		line := sc.Text()
		errBuf.WriteString(line)
		errBuf.WriteByte('\n')
		if progress != nil {
			progress(line)
		}
	}
	_ = sc.Err()
	runErr := c.Wait()
	out := outBuf.String()
	if runErr != nil {
		return out + errBuf.String(), &GitError{Args: args, Output: out + errBuf.String(), Err: runErr}
	}
	return out, nil
}

// Checkout 检出引用（分支、标签或提交）。
func (r *Repo) Checkout(ctx context.Context, ref string) (string, error) {
	return r.run(ctx, false, "checkout", ref)
}

// CheckoutRemoteBranch 以远端分支为跟踪目标创建并检出本地分支。
func (r *Repo) CheckoutRemoteBranch(ctx context.Context, remoteRef string) (string, error) {
	remote, branch, err := r.resolveRemoteBranch(ctx, remoteRef)
	if err != nil {
		return "", err
	}
	if _, err := r.run(ctx, true, "show-ref", "--verify", "--quiet", "refs/heads/"+branch); err == nil {
		return r.run(ctx, false, "checkout", branch)
	}
	fullRef := "refs/remotes/" + remote + "/" + branch
	out, err := r.run(ctx, false, "checkout", "-b", branch, "--no-track", fullRef)
	if err != nil {
		return out, err
	}
	if _, err := r.run(ctx, false, "config", "branch."+branch+".remote", remote); err != nil {
		return out, err
	}
	if _, err := r.run(ctx, false, "config", "branch."+branch+".merge", "refs/heads/"+branch); err != nil {
		return out, err
	}
	return out, nil
}

// CreateBranch 基于 startPoint 创建分支；checkout 为 true 时同时切换。
func (r *Repo) CreateBranch(ctx context.Context, name, startPoint string, checkout bool) (string, error) {
	if checkout {
		args := []string{"checkout", "-b", name}
		if startPoint != "" {
			args = append(args, startPoint)
		}
		return r.run(ctx, false, args...)
	}
	args := []string{"branch", name}
	if startPoint != "" {
		args = append(args, startPoint)
	}
	return r.run(ctx, false, args...)
}

// branchUpstream 返回分支的上游短名（如 origin/main）；没有上游时返回空串。
func (r *Repo) branchUpstream(ctx context.Context, branch string) string {
	out, err := r.run(ctx, true, "rev-parse", "--abbrev-ref", branch+"@{upstream}")
	if err != nil {
		return ""
	}
	return strings.TrimSpace(out)
}

func (r *Repo) resolveRemoteBranch(ctx context.Context, ref string) (remote, branch string, err error) {
	ref = strings.TrimPrefix(ref, "refs/remotes/")
	out, err := r.run(ctx, true, "remote")
	if err != nil {
		return "", "", err
	}
	remotes := strings.Fields(out)
	sort.Slice(remotes, func(i, j int) bool { return len(remotes[i]) > len(remotes[j]) })
	for _, name := range remotes {
		prefix := name + "/"
		if strings.HasPrefix(ref, prefix) && len(ref) > len(prefix) {
			return name, strings.TrimPrefix(ref, prefix), nil
		}
	}
	return "", "", fmt.Errorf("无法识别远端分支 %q，请检查 git remote 配置", ref)
}

// RenameBranch 重命名分支，并处理同名上游：
//   - 远端已存在 <remote>/<新名> 时改为跟踪它；
//   - 否则清除旧的同名上游，避免重命名后仍跟踪 <remote>/<旧名>；
//   - 上游本来就跟其他分支同名（跨分支跟踪）时保持不变。
func (r *Repo) RenameBranch(ctx context.Context, oldName, newName string) (string, error) {
	newName = strings.TrimSpace(newName)
	if newName == "" {
		return "", errors.New("分支名不能为空")
	}
	old := strings.TrimSpace(oldName)
	if old == "" {
		// 未指定旧名时按当前分支处理，便于同样修正上游。
		old = strings.TrimSpace(r.CurrentBranch(ctx))
	}
	var oldUpstream string
	if old != "" {
		oldUpstream = r.branchUpstream(ctx, old)
	}
	var out string
	var err error
	if old == "" {
		out, err = r.run(ctx, false, "branch", "-m", newName)
	} else {
		out, err = r.run(ctx, false, "branch", "-m", old, newName)
	}
	if err != nil || old == "" || oldUpstream == "" {
		return out, err
	}
	remote, upstreamBranch, err := r.resolveRemoteBranch(ctx, oldUpstream)
	if err != nil || upstreamBranch != old {
		// 上游跟踪的是别的分支名，保持原样。
		return out, nil
	}
	newUpstream := remote + "/" + newName
	if _, err := r.run(ctx, true, "rev-parse", "--verify", "--quiet", "refs/remotes/"+newUpstream); err == nil {
		note, serr := r.run(ctx, false, "branch", "--set-upstream-to="+newUpstream, newName)
		out += note
		if serr == nil {
			out += "上游已更新为 " + newUpstream + string(rune(10))
		}
		return out, nil
	}
	// 远端还没有新名字，先清掉指向旧名的上游，推送时会重新设置。
	note, uerr := r.run(ctx, false, "branch", "--unset-upstream", newName)
	out += note
	if uerr == nil {
		out += "旧上游 " + oldUpstream + " 已清除（推送时会重新设置）" + string(rune(10))
	}
	return out, nil
}

// UnsetUpstream 取消分支的上游跟踪；branch 为空时作用于当前分支。
func (r *Repo) UnsetUpstream(ctx context.Context, branch string) (string, error) {
	if strings.TrimSpace(branch) == "" {
		return r.run(ctx, false, "branch", "--unset-upstream")
	}
	return r.run(ctx, false, "branch", "--unset-upstream", branch)
}

// DeleteBranch 删除本地分支；force 对应 -D。
func (r *Repo) DeleteBranch(ctx context.Context, name string, force bool) (string, error) {
	flag := "-d"
	if force {
		flag = "-D"
	}
	return r.run(ctx, false, "branch", flag, name)
}

// DeleteRemoteBranch 删除远端分支。
func (r *Repo) DeleteRemoteBranch(ctx context.Context, remoteRef string) (string, error) {
	remote, branch, err := r.resolveRemoteBranch(ctx, remoteRef)
	if err != nil {
		return "", err
	}
	return r.run(ctx, false, "push", remote, "--delete", branch)
}

// Merge 合并 ref 到当前分支（不打开编辑器）。
func (r *Repo) Merge(ctx context.Context, ref string) (string, error) {
	return r.run(ctx, false, "merge", "--no-edit", ref)
}

// Rebase 把当前分支 rebase 到 ref。
func (r *Repo) Rebase(ctx context.Context, ref string) (string, error) {
	return r.run(ctx, false, "rebase", ref)
}

// CherryPick 摘取提交。
func (r *Repo) CherryPick(ctx context.Context, sha string) (string, error) {
	return r.run(ctx, false, "cherry-pick", sha)
}

// Revert 还原提交（不打开编辑器）。
func (r *Repo) Revert(ctx context.Context, sha string) (string, error) {
	return r.run(ctx, false, "revert", "--no-edit", sha)
}

// ResetMode 是 reset 模式。
type ResetMode string

const (
	ResetSoft  ResetMode = "--soft"
	ResetMixed ResetMode = "--mixed"
	ResetHard  ResetMode = "--hard"
)

// Reset 把当前分支重置到 sha。
func (r *Repo) Reset(ctx context.Context, sha string, mode ResetMode) (string, error) {
	return r.run(ctx, false, "reset", string(mode), sha)
}

// CreateTag 创建标签；message 非空时创建附注标签。
func (r *Repo) CreateTag(ctx context.Context, name, sha, message string) (string, error) {
	if message != "" {
		return r.run(ctx, false, "tag", "-a", name, "-m", message, sha)
	}
	return r.run(ctx, false, "tag", name, sha)
}

// DeleteTag 删除标签。
func (r *Repo) DeleteTag(ctx context.Context, name string) (string, error) {
	return r.run(ctx, false, "tag", "-d", name)
}

// PushTag 推送标签到远端。
func (r *Repo) PushTag(ctx context.Context, remote, name string, progress func(string)) (string, error) {
	return r.runProgress(ctx, []string{"push", "--progress", remote, "refs/tags/" + name}, progress)
}

func literalPathspec(path string) string {
	return ":(literal)" + path
}

func literalPathspecs(paths []string) []string {
	out := make([]string, len(paths))
	for i, path := range paths {
		out[i] = literalPathspec(path)
	}
	return out
}

// Stage 暂存指定路径。
func (r *Repo) Stage(ctx context.Context, paths ...string) (string, error) {
	args := []string{"add", "--"}
	args = append(args, literalPathspecs(paths)...)
	return r.run(ctx, false, args...)
}

// StageAll 暂存全部变更（含删除与未跟踪）。
func (r *Repo) StageAll(ctx context.Context) (string, error) {
	return r.run(ctx, false, "add", "-A")
}

// Unstage 取消暂存指定路径。
func (r *Repo) Unstage(ctx context.Context, paths ...string) (string, error) {
	args := []string{"reset", "-q", "HEAD", "--"}
	args = append(args, literalPathspecs(paths)...)
	out, err := r.run(ctx, false, args...)
	if err == nil {
		return out, nil
	}
	// 尚无提交时 HEAD 不存在，改用 rm --cached。
	args = []string{"rm", "--cached", "-r", "-q", "--"}
	args = append(args, literalPathspecs(paths)...)
	return r.run(ctx, false, args...)
}

// UnstageAll 取消全部暂存。
func (r *Repo) UnstageAll(ctx context.Context) (string, error) {
	out, err := r.run(ctx, false, "reset", "-q", "HEAD")
	if err == nil {
		return out, nil
	}
	return r.run(ctx, false, "rm", "--cached", "-r", "-q", ".")
}

// Discard 丢弃工作区改动；untracked 为 true 时删除未跟踪文件。
func (r *Repo) Discard(ctx context.Context, untracked bool, paths ...string) (string, error) {
	if untracked {
		args := []string{"clean", "-f", "-d", "--"}
		args = append(args, literalPathspecs(paths)...)
		return r.run(ctx, false, args...)
	}
	args := []string{"checkout", "--"}
	args = append(args, literalPathspecs(paths)...)
	return r.run(ctx, false, args...)
}

// DiscardAll 清除指定路径的暂存、工作区与未跟踪改动。
func (r *Repo) DiscardAll(ctx context.Context, paths ...string) (string, error) {
	if len(paths) == 0 {
		return "", errors.New("丢弃路径不能为空")
	}
	for _, path := range paths {
		if path == "" {
			return "", errors.New("丢弃路径不能为空")
		}
	}
	specs := literalPathspecs(paths)
	var out string
	var err error
	if _, headErr := r.run(ctx, true, "rev-parse", "--verify", "HEAD"); headErr != nil {
		// 尚无首个提交时 restore 没有 HEAD 可用，先取消暂存再清理。
		out, err = r.run(ctx, false, append([]string{"rm", "--cached", "-r", "-q", "--ignore-unmatch", "--"}, specs...)...)
		if err != nil {
			return out, err
		}
	} else {
		restoreSpecs, err := r.restorePathspecs(ctx, specs)
		if err != nil {
			return "", err
		}
		if len(restoreSpecs) > 0 {
			out, err = r.run(ctx, false, append([]string{"restore", "--source=HEAD", "--staged", "--worktree", "--"}, restoreSpecs...)...)
			if err != nil {
				return out, err
			}
		}
	}
	cleanOut, err := r.run(ctx, false, append([]string{"clean", "-f", "-d", "--"}, specs...)...)
	return out + cleanOut, err
}

// restorePathspecs 返回同时存在于 HEAD 或 index 的路径，避免仅未跟踪路径导致
// git restore 报 pathspec 不匹配而中断后续 clean。
func (r *Repo) restorePathspecs(ctx context.Context, specs []string) ([]string, error) {
	seen := map[string]bool{}
	add := func(out string) {
		for _, path := range strings.Split(out, string(rune(0))) {
			if path != "" {
				seen[path] = true
			}
		}
	}
	if out, err := r.run(ctx, true, append([]string{"ls-tree", "-r", "--name-only", "-z", "HEAD", "--"}, specs...)...); err == nil {
		add(out)
	}
	out, err := r.run(ctx, true, append([]string{"ls-files", "-z", "--"}, specs...)...)
	if err != nil {
		return nil, err
	}
	add(out)
	restore := make([]string, 0, len(seen))
	for path := range seen {
		restore = append(restore, literalPathspec(path))
	}
	sort.Strings(restore)
	return restore, nil
}

// Commit 提交暂存内容，message 通过 stdin 传递；amend 为 true 时修订最近提交。
// amend 且 message 为空时保留原提交信息（--no-edit）。
func (r *Repo) Commit(ctx context.Context, message string, amend bool) (string, error) {
	args := []string{"commit"}
	if amend {
		args = append(args, "--amend")
		if strings.TrimSpace(message) == "" {
			args = append(args, "--no-edit")
			return r.run(ctx, false, args...)
		}
	}
	args = append(args, "-F", "-")
	return r.runInput(ctx, message, args...)
}

// Ignore 把一个未跟踪路径加入仓库根目录的 .gitignore。
func (r *Repo) Ignore(ctx context.Context, path string) (string, error) {
	if err := ctx.Err(); err != nil {
		return "", err
	}
	rel := filepath.ToSlash(path)
	trailing := ""
	if strings.HasSuffix(rel, "/") {
		rel = strings.TrimSuffix(rel, "/")
		trailing = "/"
	}
	if rel == "" {
		return "", errors.New("路径不能为空")
	}
	escape := func(s string) string {
		var b strings.Builder
		for _, ch := range s {
			switch ch {
			case '\\', '*', '?', '[', ']', ' ', '#', '!':
				b.WriteByte('\\')
			}
			b.WriteRune(ch)
		}
		return b.String()
	}
	pattern := "/" + escape(rel) + trailing
	file := filepath.Join(r.Root, ".gitignore")
	info, err := os.Lstat(file)
	if err != nil && !os.IsNotExist(err) {
		return "", err
	}
	if err == nil && !info.Mode().IsRegular() {
		return "", errors.New(".gitignore 必须是普通文件，不能是符号链接或目录")
	}
	data, err := os.ReadFile(file)
	if err != nil && !os.IsNotExist(err) {
		return "", err
	}
	for _, line := range strings.Split(string(data), string(rune(10))) {
		if strings.TrimSuffix(line, "\r") == pattern {
			return "已在 .gitignore 中: " + pattern, nil
		}
	}
	content := string(data)
	if content != "" && !strings.HasSuffix(content, string(rune(10))) {
		content += string(rune(10))
	}
	content += pattern + string(rune(10))
	tmp, err := os.CreateTemp(r.Root, ".gitignore-*.tmp")
	if err != nil {
		return "", err
	}
	tmpPath := tmp.Name()
	defer os.Remove(tmpPath)
	mode := os.FileMode(0o644)
	if info != nil {
		mode = info.Mode().Perm()
	}
	if err := tmp.Chmod(mode); err != nil {
		_ = tmp.Close()
		return "", err
	}
	if _, err := tmp.WriteString(content); err != nil {
		_ = tmp.Close()
		return "", err
	}
	if err := tmp.Close(); err != nil {
		return "", err
	}
	if err := ctx.Err(); err != nil {
		return "", err
	}
	if err := os.Rename(tmpPath, file); err != nil {
		return "", err
	}
	return "已添加到 .gitignore: " + pattern, nil
}

// Worktree 是一个 git worktree。
type Worktree struct {
	Path     string
	Head     string
	Branch   string
	Bare     bool
	Detached bool
	Locked   bool
	Prunable bool
	Main     bool
}

// Worktrees 读取当前仓库的 worktree 列表（第一个为主 worktree）。
func (r *Repo) Worktrees(ctx context.Context) ([]Worktree, error) {
	out, err := r.run(ctx, true, "worktree", "list", "--porcelain")
	if err != nil {
		return nil, err
	}
	var list []Worktree
	var cur *Worktree
	flush := func() {
		if cur != nil && cur.Path != "" {
			list = append(list, *cur)
			cur = nil
		}
	}
	for _, line := range strings.Split(out, string(rune(10))) {
		line = strings.TrimSuffix(line, "\r")
		if line == "" {
			flush()
			continue
		}
		if strings.HasPrefix(line, "worktree ") {
			flush()
			cur = &Worktree{Path: strings.TrimPrefix(line, "worktree ")}
			continue
		}
		if cur == nil {
			continue
		}
		switch {
		case strings.HasPrefix(line, "HEAD "):
			cur.Head = strings.TrimPrefix(line, "HEAD ")
		case strings.HasPrefix(line, "branch "):
			cur.Branch = strings.TrimPrefix(strings.TrimPrefix(line, "branch "), "refs/heads/")
		case line == "bare":
			cur.Bare = true
		case line == "detached":
			cur.Detached = true
		case strings.HasPrefix(line, "locked"):
			cur.Locked = true
		case strings.HasPrefix(line, "prunable"):
			cur.Prunable = true
		}
	}
	flush()
	if len(list) > 0 {
		list[0].Main = true
	}
	return list, nil
}

// AddWorktree 添加 worktree：分支不存在时新建，已存在时直接检出该分支。
func (r *Repo) AddWorktree(ctx context.Context, path, branch string) (string, error) {
	if _, err := r.run(ctx, true, "show-ref", "--verify", "--quiet", "refs/heads/"+branch); err == nil {
		return r.run(ctx, false, "worktree", "add", path, branch)
	}
	return r.run(ctx, false, "worktree", "add", "-b", branch, path)
}

// RemoveWorktree 移除 worktree；force 会丢弃其中未提交改动。
func (r *Repo) RemoveWorktree(ctx context.Context, path string, force bool) (string, error) {
	args := []string{"worktree", "remove"}
	if force {
		args = append(args, "--force")
	}
	args = append(args, path)
	return r.run(ctx, false, args...)
}

// PruneWorktrees 清理已失效的 worktree 记录（不会删除目录内容）。
func (r *Repo) PruneWorktrees(ctx context.Context) (string, error) {
	return r.run(ctx, false, "worktree", "prune")
}

// LockWorktree 锁定 worktree，防止被 prune/remove；reason 可留空。
func (r *Repo) LockWorktree(ctx context.Context, path, reason string) (string, error) {
	args := []string{"worktree", "lock"}
	if strings.TrimSpace(reason) != "" {
		args = append(args, "--reason", reason)
	}
	args = append(args, path)
	return r.run(ctx, false, args...)
}

// UnlockWorktree 解除 worktree 锁定。
func (r *Repo) UnlockWorktree(ctx context.Context, path string) (string, error) {
	return r.run(ctx, false, "worktree", "unlock", path)
}

// CommitMessage 读取提交的完整原始信息（%B）。
func (r *Repo) CommitMessage(ctx context.Context, sha string) (string, error) {
	out, err := r.run(ctx, true, "log", "-1", "--format=%B", sha)
	if err != nil {
		return "", err
	}
	return strings.TrimRight(out, string(rune(10))), nil
}

// EditCommitMessage 改写某个本地提交的信息，并把其后的提交 rebase 到新提交上。
func (r *Repo) EditCommitMessage(ctx context.Context, sha, message string) (string, error) {
	if strings.TrimSpace(message) == "" {
		return "", errors.New("提交信息不能为空")
	}
	branch := r.CurrentBranch(ctx)
	if branch == "" {
		return "", errors.New("分离 HEAD 状态下不能编辑提交信息")
	}
	if err := r.requireCleanWorktree(ctx); err != nil {
		return "", err
	}
	if _, err := r.run(ctx, true, "merge-base", "--is-ancestor", sha, "HEAD"); err != nil {
		return "", errors.New("该提交不在当前分支上")
	}
	parent, err := r.run(ctx, true, "rev-parse", sha+"^")
	if err != nil {
		return "", errors.New("不能编辑根提交")
	}
	if _, err := r.run(ctx, true, "rev-parse", sha+"^2"); err == nil {
		return "", errors.New("暂不支持编辑合并提交")
	}
	tree, err := r.run(ctx, true, "rev-parse", sha+"^{tree}")
	if err != nil {
		return "", err
	}
	oldHead, err := r.run(ctx, true, "rev-parse", "HEAD")
	if err != nil {
		return "", err
	}
	// 保留原提交作者信息
	author, _ := r.run(ctx, true, "log", "-1", "--format=%an%x00%ae%x00%ad", "--date=raw", sha)
	parts := strings.Split(strings.TrimRight(author, string(rune(10))), string(rune(0)))
	var authorEnv []string
	if len(parts) == 3 {
		authorEnv = []string{
			"GIT_AUTHOR_NAME=" + parts[0],
			"GIT_AUTHOR_EMAIL=" + parts[1],
			"GIT_AUTHOR_DATE=" + parts[2],
		}
	}
	newSha, err := r.runInputEnv(ctx, message+string(rune(10)), authorEnv, "commit-tree", strings.TrimSpace(tree), "-p", strings.TrimSpace(parent))
	if err != nil {
		return "", err
	}
	newSha = strings.TrimSpace(newSha)
	if _, err := r.run(ctx, false, "rebase", "--onto", newSha, sha, branch); err != nil {
		_, _ = r.run(context.Background(), false, "rebase", "--abort")
		_, _ = r.run(context.Background(), false, "reset", "--soft", strings.TrimSpace(oldHead))
		return "", err
	}
	short := func(v string) string {
		if len(v) > 7 {
			return v[:7]
		}
		return v
	}
	return "已更新提交信息: " + short(sha) + " -> " + short(newSha), nil
}

// Squash 把 head 到 parent（不含）之间的一段提交合并为一个；head 必须是当前 HEAD。
func (r *Repo) Squash(ctx context.Context, head, parent, message string) (string, error) {
	if strings.TrimSpace(message) == "" {
		return "", errors.New("提交信息不能为空")
	}
	cur, err := r.run(ctx, true, "rev-parse", "HEAD")
	if err != nil {
		return "", err
	}
	if strings.TrimSpace(cur) != head {
		return "", errors.New("HEAD 已变化，请刷新后重试")
	}
	if out, err := r.run(ctx, true, "diff", "--cached", "--name-only"); err != nil {
		return "", err
	} else if strings.TrimSpace(out) != "" {
		return "", errors.New("存在已暂存改动，请先提交或取消暂存")
	}
	if _, err := r.run(ctx, true, "merge-base", "--is-ancestor", parent, head); err != nil {
		return "", errors.New("父提交不是 HEAD 的祖先")
	}
	if _, err := r.run(ctx, false, "reset", "--soft", parent); err != nil {
		return "", err
	}
	out, err := r.runInput(ctx, message+string(rune(10)), "commit", "-F", "-")
	if err != nil {
		_, _ = r.run(context.Background(), false, "reset", "--soft", head)
		return out, err
	}
	return out, nil
}

// runInputEnv 执行命令并附加环境变量（用于保留作者信息）。
func (r *Repo) runInputEnv(ctx context.Context, input string, extraEnv []string, args ...string) (string, error) {
	c := r.cmd(ctx, false, args...)
	c.Env = append(append([]string{}, c.Env...), extraEnv...)
	c.Stdin = strings.NewReader(input)
	var outBuf, errBuf bytes.Buffer
	c.Stdout = &outBuf
	c.Stderr = &errBuf
	if err := c.Run(); err != nil {
		return outBuf.String(), &GitError{Args: args, Output: errBuf.String() + outBuf.String(), Err: err}
	}
	return outBuf.String(), nil
}

func (r *Repo) requireCleanWorktree(ctx context.Context) error {
	for _, args := range [][]string{{"diff", "--name-only"}, {"diff", "--cached", "--name-only"}} {
		out, err := r.run(ctx, true, args...)
		if err != nil {
			return err
		}
		if strings.TrimSpace(out) != "" {
			return errors.New("工作区或暂存区有改动，请先提交或暂存后再改写历史")
		}
	}
	return nil
}

// Stash 保存工作区改动；includeUntracked 对应 -u。
func (r *Repo) Stash(ctx context.Context, message string, includeUntracked bool) (string, error) {
	args := []string{"stash", "push"}
	if includeUntracked {
		args = append(args, "-u")
	}
	if message != "" {
		args = append(args, "-m", message)
	}
	return r.run(ctx, false, args...)
}

func (r *Repo) stashSelector(ctx context.Context, hash string) (string, error) {
	stashes, err := r.Stashes(ctx)
	if err != nil {
		return "", err
	}
	for _, st := range stashes {
		if st.Hash == hash {
			return st.Selector, nil
		}
	}
	return "", errors.New("stash 已变化或不存在，请刷新后重试")
}

// StashApply 应用 stash 但保留记录。
func (r *Repo) StashApply(ctx context.Context, hash string) (string, error) {
	return r.run(ctx, false, "stash", "apply", hash)
}

// StashPop 应用并删除 stash 记录。
func (r *Repo) StashPop(ctx context.Context, hash string) (string, error) {
	out, err := r.run(ctx, false, "stash", "apply", hash)
	if err != nil {
		return out, err
	}
	selector, err := r.stashSelector(ctx, hash)
	if err != nil {
		return out, fmt.Errorf("stash 已应用，但记录已变化，未删除: %w", err)
	}
	if _, err := r.run(ctx, false, "stash", "drop", selector); err != nil {
		return out, fmt.Errorf("stash 已应用，但删除失败: %w", err)
	}
	return out, nil
}

// StashDrop 删除 stash 记录。
func (r *Repo) StashDrop(ctx context.Context, hash string) (string, error) {
	selector, err := r.stashSelector(ctx, hash)
	if err != nil {
		return "", err
	}
	return r.run(ctx, false, "stash", "drop", selector)
}

// StashDiff 查看 stash 内容。
func (r *Repo) StashDiff(ctx context.Context, hash string) (string, error) {
	return r.run(ctx, true, "stash", "show", "-p", "--no-color", hash)
}

// Fetch 抓取全部远端；prune 对应 --prune。
func (r *Repo) Fetch(ctx context.Context, prune bool, progress func(string)) (string, error) {
	args := []string{"fetch", "--all", "--progress"}
	if prune {
		args = append(args, "--prune")
	}
	return r.runProgress(ctx, args, progress)
}

// Pull 拉取当前分支；rebase 为 true 时使用 --rebase。
func (r *Repo) Pull(ctx context.Context, rebase bool, progress func(string)) (string, error) {
	args := []string{"pull", "--progress"}
	if rebase {
		args = append(args, "--rebase")
	}
	return r.runProgress(ctx, args, progress)
}

// PushOptions 控制 push 行为。
type PushOptions struct {
	SetUpstream bool
	Force       bool // 仅使用 --force-with-lease
}

// Push 推送当前分支。
func (r *Repo) Push(ctx context.Context, opts PushOptions, branch string, progress func(string)) (string, error) {
	args := []string{"push", "--progress"}
	if opts.Force {
		args = append(args, "--force-with-lease")
	}
	if opts.SetUpstream {
		args = append(args, "-u", "origin")
		if branch == "" {
			args = append(args, "HEAD")
		} else {
			args = append(args, branch)
		}
	}
	return r.runProgress(ctx, args, progress)
}

// SetUpstream 设置当前分支上游。
func (r *Repo) SetUpstream(ctx context.Context, branch, upstream string) (string, error) {
	return r.run(ctx, false, "branch", "--set-upstream-to="+upstream, branch)
}

// ContinueOperation 继续 merge/rebase/cherry-pick/revert。
func (r *Repo) ContinueOperation(ctx context.Context, kind string) (string, error) {
	switch kind {
	case StateMerge:
		return r.run(ctx, false, "commit", "--no-edit")
	case StateRebase:
		return r.run(ctx, false, "-c", "core.editor=true", "rebase", "--continue")
	case StateCherryPick:
		return r.run(ctx, false, "-c", "core.editor=true", "cherry-pick", "--continue")
	case StateRevert:
		return r.run(ctx, false, "-c", "core.editor=true", "revert", "--continue")
	}
	return "", fmt.Errorf("未知的中断操作: %s", kind)
}

// AbortOperation 终止 merge/rebase/cherry-pick/revert。
func (r *Repo) AbortOperation(ctx context.Context, kind string) (string, error) {
	switch kind {
	case StateMerge:
		return r.run(ctx, false, "merge", "--abort")
	case StateRebase:
		return r.run(ctx, false, "rebase", "--abort")
	case StateCherryPick:
		return r.run(ctx, false, "cherry-pick", "--abort")
	case StateRevert:
		return r.run(ctx, false, "revert", "--abort")
	}
	return "", fmt.Errorf("未知的中断操作: %s", kind)
}

// RunGitArgs 透传任意 git 参数（供 `:` 命令使用）。
func (r *Repo) RunGitArgs(ctx context.Context, args []string) (string, error) {
	return r.run(ctx, false, args...)
}

// SplitArgs 按 shell 风格拆分参数（支持单/双引号与反斜杠转义）。
func SplitArgs(s string) []string {
	var out []string
	var cur strings.Builder
	inSingle, inDouble, escaped := false, false, false
	has := false
	for _, c := range s {
		switch {
		case escaped:
			cur.WriteRune(c)
			escaped = false
			has = true
		case c == '\\' && !inSingle:
			escaped = true
		case c == '\'' && !inDouble:
			inSingle = !inSingle
			has = true
		case c == '"' && !inSingle:
			inDouble = !inDouble
			has = true
		case (c == ' ' || c == '\t') && !inSingle && !inDouble:
			if has {
				out = append(out, cur.String())
				cur.Reset()
				has = false
			}
		default:
			cur.WriteRune(c)
			has = true
		}
	}
	if escaped {
		cur.WriteRune('\\')
	}
	if has {
		out = append(out, cur.String())
	}
	return out
}
