package git

import (
	"context"
	"strconv"
	"strings"
	"time"
)

// CommitFile 是提交中的一个文件变更。
type CommitFile struct {
	Status  string // A/M/D/R100 等
	Path    string
	OldPath string
}

// CommitDetail 是提交详情。
type CommitDetail struct {
	Hash        string
	Parents     []string
	Author      string
	AuthorEmail string
	When        time.Time
	Subject     string
	Body        string
	Files       []CommitFile
}

// CommitDetail 读取提交的元信息与文件列表。
func (r *Repo) CommitDetail(ctx context.Context, sha string) (CommitDetail, error) {
	out, err := r.run(ctx, true, "show", "-s", "--no-color",
		"--format=%H%x00%an%x00%ae%x00%at%x00%s%x00%b%x1e", sha)
	if err != nil {
		return CommitDetail{}, err
	}
	rec := strings.TrimSpace(strings.TrimSuffix(strings.TrimSpace(out), "\x1e"))
	f := strings.Split(rec, "\x00")
	if len(f) < 6 {
		return CommitDetail{}, nil
	}
	sec, _ := strconv.ParseInt(strings.TrimSpace(f[3]), 10, 64)
	d := CommitDetail{
		Hash:        strings.TrimSpace(f[0]),
		Author:      f[1],
		AuthorEmail: f[2],
		When:        time.Unix(sec, 0),
		Subject:     f[4],
		Body:        strings.TrimSpace(f[5]),
	}
	files, err := r.CommitFiles(ctx, sha)
	if err != nil {
		return d, err
	}
	d.Files = files
	return d, nil
}

// commitBaseArgs 返回读取某提交改动的命令参数：
// 普通提交用 git show；合并提交按"相对第一父提交"的差异展示。
func (r *Repo) commitBaseArgs(ctx context.Context, sha string) ([]string, error) {
	out, err := r.run(ctx, true, "rev-list", "--parents", "-n", "1", sha)
	if err != nil {
		return nil, err
	}
	fields := strings.Fields(strings.TrimSpace(out))
	if len(fields) > 2 {
		return []string{"diff", strings.TrimSpace(fields[1]), sha}, nil
	}
	return []string{"show"}, nil
}

// CommitFiles 读取提交改动的文件列表。
func (r *Repo) CommitFiles(ctx context.Context, sha string) ([]CommitFile, error) {
	base, err := r.commitBaseArgs(ctx, sha)
	if err != nil {
		return nil, err
	}
	args := append(base, "--no-color", "--name-status", "-z")
	if base[0] == "show" {
		args = append(args, "--format=", sha)
	}
	out, err := r.run(ctx, true, args...)
	if err != nil {
		return nil, err
	}
	var files []CommitFile
	toks := strings.Split(out, "\x00")
	for i := 0; i < len(toks); i++ {
		status := strings.TrimSpace(toks[i])
		if status == "" {
			continue
		}
		if strings.HasPrefix(status, "R") || strings.HasPrefix(status, "C") {
			if i+2 >= len(toks) {
				break
			}
			files = append(files, CommitFile{Status: status, OldPath: toks[i+1], Path: toks[i+2]})
			i += 2
			continue
		}
		if i+1 >= len(toks) {
			break
		}
		files = append(files, CommitFile{Status: status, Path: toks[i+1]})
		i++
	}
	return files, nil
}

// CommitDiff 读取提交的补丁；path 非空时只取该文件。
func (r *Repo) CommitDiff(ctx context.Context, sha, path string) (string, error) {
	base, err := r.commitBaseArgs(ctx, sha)
	if err != nil {
		return "", err
	}
	args := append(base, "--no-color", "--patch")
	if base[0] == "show" {
		args = append(args, "--format=", sha)
	}
	if path != "" {
		args = append(args, "--", literalPathspec(path))
	}
	return r.run(ctx, true, args...)
}

// WorkingDiff 读取工作区 diff；staged 为 true 时读取索引区差异，untracked 用 --no-index 兜底。
func (r *Repo) WorkingDiff(ctx context.Context, path string, staged, untracked bool) (string, error) {
	if untracked {
		// git diff 不跟踪未跟踪文件，用 --no-index 生成 /dev/null 对比。
		args := []string{"diff", "--no-color", "--no-index", "--", "/dev/null", path}
		out, err := r.run(ctx, true, args...)
		// --no-index 有差异时退出码为 1，视为正常。
		if err != nil && out != "" {
			return out, nil
		}
		return out, err
	}
	args := []string{"diff", "--no-color"}
	if staged {
		args = append(args, "--cached")
	}
	if path != "" {
		args = append(args, "--", literalPathspec(path))
	}
	return r.run(ctx, true, args...)
}

// RevParse 解析引用的提交 sha。
func (r *Repo) RevParse(ctx context.Context, ref string) (string, error) {
	out, err := r.run(ctx, true, "rev-parse", "--verify", ref+"^{commit}")
	if err != nil {
		return "", err
	}
	return strings.TrimSpace(out), nil
}
