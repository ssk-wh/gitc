// Package git 封装对系统 git 的调用：仓库发现、日志/引用/状态读取与各类操作。
// 所有命令都通过参数数组传递（不经 shell），并对子进程做整组取消。
package git

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"
)

// MinVersion 是支持的最低 git 版本。
const MinVersion = "2.23"

// Repo 表示一个已发现的 git 仓库。
type Repo struct {
	Root      string // 工作区根目录
	GitDir    string // 当前 worktree 的 git 目录（状态文件所在）
	CommonDir string // 共享 git 目录（refs 所在）
	env       []string
	// OnCommand 在每次执行 git 子命令时回调（用于 UI 命令历史）。
	OnCommand func(string)
}

// SetCommandHook 设置命令执行回调。
func (r *Repo) SetCommandHook(fn func(string)) { r.OnCommand = fn }

func (r *Repo) notify(args []string) {
	if r.OnCommand == nil {
		return
	}
	line := "git " + strings.Join(args, " ")
	r.OnCommand(line)
}

// GitError 保留命令与输出，便于 UI 原样展示。
type GitError struct {
	Args   []string
	Output string
	Err    error
}

func (e *GitError) Error() string {
	out := strings.TrimSpace(e.Output)
	if out == "" {
		return fmt.Sprintf("git %s: %v", strings.Join(e.Args, " "), e.Err)
	}
	return fmt.Sprintf("git %s: %v\n%s", strings.Join(e.Args, " "), e.Err, out)
}

func (e *GitError) Unwrap() error { return e.Err }

// Open 在 dir 中查找仓库根目录并校验 git 版本。
func Open(dir string) (*Repo, error) {
	if dir == "" {
		dir = "."
	}
	abs, err := absPath(dir)
	if err != nil {
		return nil, err
	}
	// 先检查 git 是否可用及版本。
	verOut, err := exec.Command("git", "version").Output() //nolint:gosec // 固定命令
	if err != nil {
		return nil, fmt.Errorf("未找到可用的 git，请先安装 git >= %s: %w", MinVersion, err)
	}
	if ver := ParseVersion(string(verOut)); ver != "" && !VersionAtLeast(ver, MinVersion) {
		return nil, fmt.Errorf("git 版本过低（%s），需要 >= %s", ver, MinVersion)
	}

	r := &Repo{
		env: buildEnv(),
	}
	out, err := r.runRaw(context.Background(), abs, "rev-parse", "--show-toplevel", "--git-dir", "--git-common-dir")
	if err != nil {
		return nil, fmt.Errorf("当前目录不是 git 仓库: %w", err)
	}
	lines := strings.Split(strings.TrimSpace(out), "\n")
	if len(lines) < 3 {
		return nil, errors.New("无法解析仓库路径")
	}
	r.Root = strings.TrimSpace(lines[0])
	r.GitDir = resolvePath(r.Root, strings.TrimSpace(lines[1]))
	r.CommonDir = resolvePath(r.Root, strings.TrimSpace(lines[2]))
	return r, nil
}

// ParseVersion 从 "git version 2.43.0.windows.1" 提取 "2.43.0"。
func ParseVersion(s string) string {
	fields := strings.Fields(s)
	if len(fields) < 3 {
		return ""
	}
	v := fields[2]
	// 只保留数字与点
	var b strings.Builder
	for _, c := range v {
		if (c >= '0' && c <= '9') || c == '.' {
			b.WriteRune(c)
		} else {
			break
		}
	}
	return strings.Trim(b.String(), ".")
}

// VersionAtLeast 比较点分版本号。
func VersionAtLeast(v, min string) bool {
	pa := strings.Split(v, ".")
	pb := strings.Split(min, ".")
	for i := 0; i < len(pb); i++ {
		var a, b int
		if i < len(pa) {
			_, _ = fmt.Sscanf(pa[i], "%d", &a)
		}
		_, _ = fmt.Sscanf(pb[i], "%d", &b)
		if a != b {
			return a > b
		}
	}
	return true
}

func buildEnv() []string {
	env := os.Environ()
	// 去掉可能影响输出的变量后追加统一设置。
	drop := []string{"GIT_PAGER=", "GIT_TERMINAL_PROMPT=", "LC_ALL=", "LANG=", "GIT_OPTIONAL_LOCKS="}
	filtered := env[:0]
	for _, e := range env {
		skip := false
		for _, d := range drop {
			if strings.HasPrefix(e, d) {
				skip = true
				break
			}
		}
		if !skip {
			filtered = append(filtered, e)
		}
	}
	return append(filtered,
		"GIT_PAGER=cat",
		"GIT_TERMINAL_PROMPT=0",
		"GIT_OPTIONAL_LOCKS=0",
		"LC_ALL=C",
		"LANG=C",
	)
}

func absPath(p string) (string, error) {
	return filepath.Abs(p)
}

func resolvePath(base, p string) string {
	if p == "" {
		return ""
	}
	if filepath.IsAbs(p) {
		return filepath.Clean(p)
	}
	return filepath.Clean(filepath.Join(base, p))
}

// cmd 构造 git 命令。readOnly 为 true 时附加 --no-optional-locks。
func (r *Repo) cmd(ctx context.Context, readOnly bool, args ...string) *exec.Cmd {
	full := make([]string, 0, len(args)+4)
	full = append(full, "-C", r.Root, "--no-pager")
	if readOnly {
		full = append(full, "--no-optional-locks")
	}
	full = append(full, args...)
	if isUserCommand(args) {
		r.notify(args)
	}
	c := exec.CommandContext(ctx, "git", full...)
	c.Env = r.env
	prepareProcess(c)
	// 取消时整组终止，避免 ssh/git-remote-* 残留。
	c.Cancel = func() error {
		terminateTree(c)
		return nil
	}
	c.WaitDelay = 3 * time.Second
	return c
}

func (r *Repo) run(ctx context.Context, readOnly bool, args ...string) (string, error) {
	c := r.cmd(ctx, readOnly, args...)
	var outBuf, errBuf bytes.Buffer
	c.Stdout = &outBuf
	c.Stderr = &errBuf
	err := c.Run()
	if err != nil {
		return outBuf.String(), &GitError{Args: args, Output: errBuf.String() + outBuf.String(), Err: err}
	}
	return outBuf.String(), nil
}

func (r *Repo) runInput(ctx context.Context, input string, args ...string) (string, error) {
	c := r.cmd(ctx, false, args...)
	c.Stdin = strings.NewReader(input)
	var outBuf, errBuf bytes.Buffer
	c.Stdout = &outBuf
	c.Stderr = &errBuf
	err := c.Run()
	if err != nil {
		return outBuf.String(), &GitError{Args: args, Output: errBuf.String() + outBuf.String(), Err: err}
	}
	return outBuf.String(), nil
}

// runRaw 用于未建立 Repo 时的仓库发现。
func (r *Repo) runRaw(ctx context.Context, dir string, args ...string) (string, error) {
	full := append([]string{"-C", dir, "--no-pager"}, args...)
	c := exec.CommandContext(ctx, "git", full...)
	c.Env = r.env
	prepareProcess(c)
	c.Cancel = func() error {
		terminateTree(c)
		return nil
	}
	c.WaitDelay = 3 * time.Second
	out, err := c.CombinedOutput()
	if err != nil {
		return string(out), &GitError{Args: args, Output: string(out), Err: err}
	}
	return string(out), nil
}

// Name 返回仓库目录名。
func (r *Repo) Name() string {
	name := filepath.Base(filepath.Clean(r.Root))
	if name == "" || name == "." || name == string(filepath.Separator) {
		return "repo"
	}
	return name
}

// gitSubcommand 跳过 -c 等全局选项，返回真正的子命令。
func gitSubcommand(args []string) string {
	for i := 0; i < len(args); i++ {
		a := args[i]
		if a == "-c" {
			i++
			continue
		}
		if strings.HasPrefix(a, "-") {
			continue
		}
		return a
	}
	return ""
}

// isUserCommand 只记录用户主动触发的写操作，避免自动刷新命令刷屏。
func isUserCommand(args []string) bool {
	sub := gitSubcommand(args)
	switch sub {
	case "add", "rm", "mv", "commit", "merge", "rebase", "cherry-pick", "revert",
		"reset", "checkout", "switch", "branch", "tag", "clean", "restore",
		"push", "pull", "fetch", "worktree":
		return true
	case "stash":
		// 只记录 stash 的写操作
		for i := 0; i < len(args); i++ {
			switch args[i] {
			case "push", "apply", "pop", "drop", "clear", "save":
				return true
			}
		}
	}
	return false
}
