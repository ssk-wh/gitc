package git

import (
	"bufio"
	"bytes"
	"context"
	"fmt"
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
func (r *Repo) CheckoutRemoteBranch(ctx context.Context, remoteRef, local string) (string, error) {
	return r.run(ctx, false, "checkout", "-b", local, "--track", remoteRef)
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

// RenameBranch 重命名分支。
func (r *Repo) RenameBranch(ctx context.Context, oldName, newName string) (string, error) {
	if oldName == "" {
		return r.run(ctx, false, "branch", "-m", newName)
	}
	return r.run(ctx, false, "branch", "-m", oldName, newName)
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
func (r *Repo) DeleteRemoteBranch(ctx context.Context, remote, branch string) (string, error) {
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

// Stage 暂存指定路径。
func (r *Repo) Stage(ctx context.Context, paths ...string) (string, error) {
	args := []string{"add", "--"}
	args = append(args, paths...)
	return r.run(ctx, false, args...)
}

// StageAll 暂存全部变更（含删除与未跟踪）。
func (r *Repo) StageAll(ctx context.Context) (string, error) {
	return r.run(ctx, false, "add", "-A")
}

// Unstage 取消暂存指定路径。
func (r *Repo) Unstage(ctx context.Context, paths ...string) (string, error) {
	args := []string{"reset", "-q", "HEAD", "--"}
	args = append(args, paths...)
	out, err := r.run(ctx, false, args...)
	if err == nil {
		return out, nil
	}
	// 尚无提交时 HEAD 不存在，改用 rm --cached。
	args = []string{"rm", "--cached", "-r", "-q", "--"}
	args = append(args, paths...)
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
		args = append(args, paths...)
		return r.run(ctx, false, args...)
	}
	args := []string{"checkout", "--"}
	args = append(args, paths...)
	return r.run(ctx, false, args...)
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

// StashApply 应用 stash 但保留记录。
func (r *Repo) StashApply(ctx context.Context, index int) (string, error) {
	return r.run(ctx, false, "stash", "apply", fmt.Sprintf("stash@{%d}", index))
}

// StashPop 应用并删除 stash 记录。
func (r *Repo) StashPop(ctx context.Context, index int) (string, error) {
	return r.run(ctx, false, "stash", "pop", fmt.Sprintf("stash@{%d}", index))
}

// StashDrop 删除 stash 记录。
func (r *Repo) StashDrop(ctx context.Context, index int) (string, error) {
	return r.run(ctx, false, "stash", "drop", fmt.Sprintf("stash@{%d}", index))
}

// StashDiff 查看 stash 内容。
func (r *Repo) StashDiff(ctx context.Context, index int) (string, error) {
	return r.run(ctx, true, "stash", "show", "-p", "--no-color", fmt.Sprintf("stash@{%d}", index))
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
