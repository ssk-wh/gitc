package ui

import (
	"fmt"
	"strings"

	"gitc/internal/git"
	"gitc/internal/i18n"
	"gitc/internal/sanitize"
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
