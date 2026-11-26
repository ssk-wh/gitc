package git

import (
	"context"
	"errors"
	"strings"
)

// compareCommitLimit 是分支对比时最多列出的提交数。
const compareCommitLimit = 300

// CompareSummary 是 base..target 的对比结果。
type CompareSummary struct {
	Base      string
	Target    string
	Commits   []Commit // target 独有、base 没有的提交（新→旧）
	DiffStat  string   // base...target 的 --stat 输出
	Truncated bool     // 提交是否被上限截断
}

// CompareBranches 对比两个分支：列出 target 相对 base 独有的提交，
// 并给出三点差异统计（base...target，即分叉后 target 引入的改动）。
// max <= 0 时使用默认上限。
func (r *Repo) CompareBranches(ctx context.Context, base, target string, max int) (CompareSummary, error) {
	base = strings.TrimSpace(base)
	target = strings.TrimSpace(target)
	if base == "" || target == "" {
		return CompareSummary{}, errors.New("对比分支不能为空")
	}
	if max <= 0 {
		max = compareCommitLimit
	}
	sum := CompareSummary{Base: base, Target: target}
	// 流式读取并限制条数，避免超大分支一次性占用内存。
	args := []string{"log", "--author-date-order", "--pretty=format:" + logFormat, base + ".." + target}
	err := r.streamLog(ctx, args, max+1, func(c Commit) bool {
		if len(sum.Commits) >= max {
			sum.Truncated = true
			return false
		}
		sum.Commits = append(sum.Commits, c)
		return true
	})
	if err != nil {
		return sum, err
	}
	stat, serr := r.run(ctx, true, "diff", "--no-color", "--stat", base+"..."+target)
	if serr != nil {
		return sum, serr
	}
	sum.DiffStat = stat
	return sum, nil
}
