package ui

import (
	"fmt"
	"strings"

	"github.com/ssk-wh/gitc/internal/git"
	"github.com/ssk-wh/gitc/internal/i18n"
	"github.com/ssk-wh/gitc/internal/sanitize"
)

// maxDetailLines 限制详情文档行数，避免超大 diff 占满内存。
const maxDetailLines = 6000

// buildCommitDetailLines 组装提交详情文档。
func buildCommitDetailLines(d git.CommitDetail, diff string) []string {
	lines := []string{
		"commit " + d.Hash,
		"Author: " + sanitize.Line(d.Author) + " <" + sanitize.Line(d.AuthorEmail) + ">",
	}
	if !d.When.IsZero() {
		lines = append(lines, "Date:   "+d.When.Format("2006-01-02 15:04:05 -0700"))
	}
	if len(d.Parents) > 0 {
		// 展示父提交，合并提交会有多个父。
		parents := make([]string, 0, len(d.Parents))
		for _, p := range d.Parents {
			parents = append(parents, shortSHA(p))
		}
		lines = append(lines, "Parent: "+strings.Join(parents, " "))
	}
	lines = append(lines, "", sanitize.Line(d.Subject))
	if d.Body != "" {
		lines = append(lines, "")
		lines = append(lines, strings.Split(sanitize.Text(d.Body), "\n")...)
	}
	if len(d.Files) > 0 {
		lines = append(lines, "", fmt.Sprintf(i18n.S("文件 (%d):", "Files (%d):"), len(d.Files)))
		for _, f := range d.Files {
			p := sanitize.Line(f.Path)
			if f.OldPath != "" {
				p = sanitize.Line(f.OldPath) + " → " + p
			}
			lines = append(lines, "  "+padRight(f.Status, 8)+" "+p)
		}
	}
	lines = append(lines, "", i18n.S("── diff ──", "── diff ──"))
	lines = append(lines, splitLinesCapped(sanitize.Text(diff), maxDetailLines)...)
	return lines
}

// buildCompareLines 组装分支对比文档：独有提交列表 + 差异统计。
func buildCompareLines(s git.CompareSummary) []string {
	lines := []string{
		fmt.Sprintf(i18n.S("对比范围: %s..%s", "compare: %s..%s"), sanitize.Line(s.Base), sanitize.Line(s.Target)),
		"",
		fmt.Sprintf(i18n.S("目标分支独有的提交 (%d):", "commits only in target (%d):"), len(s.Commits)),
	}
	if len(s.Commits) == 0 {
		lines = append(lines, i18n.S("  （无，两个分支提交相同）", "  (none, both branches point to the same history)"))
	}
	for _, c := range s.Commits {
		line := "  " + c.Short + " " + sanitize.Line(c.Subject)
		if c.Author != "" {
			line += "  — " + sanitize.Line(c.Author)
		}
		if !c.When.IsZero() {
			line += " " + c.When.Format("2006-01-02")
		}
		lines = append(lines, line)
	}
	if s.Truncated {
		lines = append(lines, i18n.S("  …（提交过多已截断）", "  … (truncated)"))
	}
	lines = append(lines, "", i18n.S("变化统计（分叉点 → 目标分支）:", "diff stat (merge-base → target):"))
	if strings.TrimSpace(s.DiffStat) == "" {
		lines = append(lines, i18n.S("  （无文件差异）", "  (no file changes)"))
	} else {
		lines = append(lines, splitLinesCapped(sanitize.Text(s.DiffStat), maxDetailLines)...)
	}
	return lines
}

// buildPlainDiffLines 组装纯 diff 文档。
func buildPlainDiffLines(diff string) []string {
	if strings.TrimSpace(diff) == "" {
		return []string{styleDim.Render(i18n.S("（无差异内容）", "(no changes)"))}
	}
	return splitLinesCapped(sanitize.Text(diff), maxDetailLines)
}

// splitLinesCapped 按行拆分并限制最大行数。
func splitLinesCapped(s string, max int) []string {
	s = strings.ReplaceAll(s, "\r\n", "\n")
	lines := strings.Split(strings.TrimRight(s, "\n"), "\n")
	if len(lines) > max {
		lines = append(lines[:max], fmt.Sprintf(i18n.S("…（已截断，共 %d 行）", "… (truncated, %d lines total)"), len(lines)))
	}
	return lines
}
