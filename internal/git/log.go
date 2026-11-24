package git

import (
	"bufio"
	"context"
	"errors"
	"fmt"
	"io"
	"strconv"
	"strings"
	"time"
)

// Commit 是一条提交的展示信息。
type Commit struct {
	Hash    string
	Short   string
	Parents []string
	Author  string
	When    time.Time
	Subject string
}

// logFormat 使用 NUL 分隔字段、RS(0x1e) 分隔记录；%at 避免 locale 影响。
const logFormat = "%H%x00%P%x00%an%x00%at%x00%s%x1e"

// logRevArgs 限定参与日志的引用范围（含 stash，排除 bisect 等杂项）。
// refs/stash 用 --glob 指定，仓库无 stash 时也不会报错。
var logRevArgs = []string{"--branches", "--tags", "--remotes", "HEAD"}

// logRevArgsNoHead 用于尚无提交的空仓库（HEAD 不存在时 git log 会报错）。
var logRevArgsNoHead = []string{"--branches", "--tags", "--remotes"}

// StreamLog 流式读取提交日志，按拓扑序（新→旧）逐条回调。
// max <= 0 表示不限；回调返回 false 时提前终止。
func (r *Repo) StreamLog(ctx context.Context, max int, cb func(Commit) bool) error {
	return r.streamLogRev(ctx, []string{"log", "--author-date-order", "--pretty=format:" + logFormat}, max, cb)
}

// streamLogRev 先带 HEAD 读取；空仓库 HEAD 不存在时去掉 HEAD 重试。
func (r *Repo) streamLogRev(ctx context.Context, base []string, max int, cb func(Commit) bool) error {
	args := append(append([]string{}, base...), logRevArgs...)
	err := r.streamLog(ctx, args, max, cb)
	if err != nil && strings.Contains(err.Error(), "ambiguous argument 'HEAD'") {
		args = append(append([]string{}, base...), logRevArgsNoHead...)
		return r.streamLog(ctx, args, max, cb)
	}
	return err
}

// LogOne 读取单个引用的最近一条提交（用于 sha 搜索）。
func (r *Repo) LogOne(ctx context.Context, rev string) (Commit, error) {
	out, err := r.run(ctx, true, "log", "-1", "--pretty=format:"+logFormat, rev)
	if err != nil {
		return Commit{}, err
	}
	rec := strings.TrimSpace(strings.TrimSuffix(strings.TrimSpace(out), "\x1e"))
	c, ok := parseCommitRecord(rec)
	if !ok {
		return Commit{}, fmt.Errorf("未找到提交: %s", rev)
	}
	return c, nil
}

// StreamLogRef 只读取指定引用可达的提交（供分支筛选）。
func (r *Repo) StreamLogRef(ctx context.Context, ref string, max int, cb func(Commit) bool) error {
	args := []string{"log", "--author-date-order", "--pretty=format:" + logFormat, ref}
	return r.streamLog(ctx, args, max, cb)
}

// StreamLogScope 按引用范围读取提交：local=仅本地分支，remote=仅远端分支，其余=全部引用。
func (r *Repo) StreamLogScope(ctx context.Context, scope string, max int, cb func(Commit) bool) error {
	var revs []string
	switch scope {
	case "local":
		revs = []string{"--branches", "HEAD"}
	case "remote":
		revs = []string{"--remotes"}
	default:
		revs = logRevArgs
	}
	base := []string{"log", "--author-date-order", "--pretty=format:" + logFormat}
	args := append(append([]string{}, base...), revs...)
	err := r.streamLog(ctx, args, max, cb)
	// 空仓库或分离 HEAD 不存在时去掉 HEAD 重试。
	if err != nil && strings.Contains(err.Error(), "ambiguous argument 'HEAD'") {
		var retry []string
		for _, v := range revs {
			if v != "HEAD" {
				retry = append(retry, v)
			}
		}
		args = append(append([]string{}, base...), retry...)
		return r.streamLog(ctx, args, max, cb)
	}
	return err
}

// SearchLog 在全部历史中按标题搜索（不区分大小写）。
func (r *Repo) SearchLog(ctx context.Context, query string, max int, cb func(Commit) bool) error {
	return r.streamLogRev(ctx, []string{
		"log", "--author-date-order", "-i", "--grep=" + query,
		"--pretty=format:" + logFormat,
	}, max, cb)
}

// SearchAuthorLog 在全部历史中按作者搜索。
func (r *Repo) SearchAuthorLog(ctx context.Context, query string, max int, cb func(Commit) bool) error {
	return r.streamLogRev(ctx, []string{
		"log", "--author-date-order", "-i", "--author=" + query,
		"--pretty=format:" + logFormat,
	}, max, cb)
}

func (r *Repo) streamLog(ctx context.Context, args []string, max int, cb func(Commit) bool) error {
	ctx, cancel := context.WithCancel(ctx)
	defer cancel()

	c := r.cmd(ctx, true, args...)
	stdout, err := c.StdoutPipe()
	if err != nil {
		return err
	}
	var errBuf strings.Builder
	c.Stderr = &errBuf
	if err := c.Start(); err != nil {
		return &GitError{Args: args, Err: err}
	}

	reader := bufio.NewReaderSize(stdout, 1<<20)
	count := 0
	for {
		rec, err := reader.ReadString('\x1e')
		rec = strings.TrimSuffix(rec, "\x1e")
		rec = strings.TrimLeft(rec, "\r\n")
		if rec != "" {
			cm, ok := parseCommitRecord(rec)
			if ok {
				count++
				if !cb(cm) {
					cancel()
					_ = c.Wait()
					return nil
				}
				if max > 0 && count >= max {
					cancel()
					_ = c.Wait()
					return nil
				}
			}
		}
		if err != nil {
			if errors.Is(err, io.EOF) {
				break
			}
			return err
		}
	}
	if err := c.Wait(); err != nil {
		if ctx.Err() != nil {
			return ctx.Err()
		}
		out := errBuf.String()
		// 空仓库（尚无提交）不是错误。
		if strings.Contains(out, "does not have any commits yet") || strings.Contains(out, "does not have any commits") {
			return nil
		}
		return &GitError{Args: args, Output: out, Err: err}
	}
	return nil
}

func parseCommitRecord(rec string) (Commit, bool) {
	fields := strings.Split(rec, "\x00")
	if len(fields) < 5 {
		return Commit{}, false
	}
	hash := strings.TrimSpace(fields[0])
	if hash == "" {
		return Commit{}, false
	}
	var parents []string
	if p := strings.TrimSpace(fields[1]); p != "" {
		parents = strings.Fields(p)
	}
	sec, _ := strconv.ParseInt(strings.TrimSpace(fields[3]), 10, 64)
	short := hash
	if len(short) > 7 {
		short = short[:7]
	}
	return Commit{
		Hash:    hash,
		Short:   short,
		Parents: parents,
		Author:  fields[2],
		When:    time.Unix(sec, 0),
		Subject: fields[4],
	}, true
}
