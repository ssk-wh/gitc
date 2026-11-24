package git

import (
	"context"
	"strconv"
	"strings"
)

// FileChange 是一条工作区变更。
type FileChange struct {
	Path      string
	OrigPath  string // 重命名前的路径
	X, Y      byte   // 索引区/工作区状态字母
	Untracked bool
	Unmerged  bool
}

// Staged 表示该变更存在于索引区。
func (f FileChange) Staged() bool {
	return !f.Untracked && f.X != '.' && f.X != '?'
}

// Unstaged 表示该变更存在于工作区（含冲突）。
func (f FileChange) Unstaged() bool {
	return !f.Untracked && (f.Y != '.' || f.Unmerged)
}

// Label 返回展示用状态字母：index=true 取索引区状态，否则取工作区状态。
func (f FileChange) Label(index bool) string {
	if f.Untracked {
		return "?"
	}
	if f.Unmerged {
		return "U"
	}
	if index && f.X != '.' && f.X != '?' {
		return string(f.X)
	}
	if f.Y != '.' && f.Y != '?' {
		return string(f.Y)
	}
	if f.X != '.' && f.X != '?' {
		return string(f.X)
	}
	return " "
}

// Status 是仓库工作区状态。
type Status struct {
	Branch    string // 当前分支；detached 时为空
	Detached  bool
	OID       string
	Upstream  string
	Ahead     int
	Behind    int
	Staged    []FileChange
	Unstaged  []FileChange
	Untracked []FileChange
}

// Dirty 表示是否有任何未提交改动。
func (s Status) Dirty() bool {
	return len(s.Staged)+len(s.Unstaged)+len(s.Untracked) > 0
}

// Total 返回变更总数。
func (s Status) Total() int { return len(s.Staged) + len(s.Unstaged) + len(s.Untracked) }

// Status 读取工作区状态。includeUntracked=false 时不列出未跟踪文件（首帧更快）。
func (r *Repo) Status(ctx context.Context, includeUntracked bool) (Status, error) {
	args := []string{"status", "--porcelain=v2", "--branch", "-z"}
	if includeUntracked {
		args = append(args, "--untracked-files=all")
	} else {
		args = append(args, "--untracked-files=no")
	}
	out, err := r.run(ctx, false, args...)
	if err != nil {
		return Status{}, err
	}
	return ParseStatus(out)
}

// ParseStatus 解析 git status --porcelain=v2 --branch -z 的输出。
func ParseStatus(out string) (Status, error) {
	var st Status
	entries := strings.Split(out, "\x00")
	for i := 0; i < len(entries); i++ {
		line := entries[i]
		if line == "" {
			continue
		}
		switch {
		case strings.HasPrefix(line, "# branch.oid "):
			st.OID = strings.TrimPrefix(line, "# branch.oid ")
		case strings.HasPrefix(line, "# branch.head "):
			head := strings.TrimPrefix(line, "# branch.head ")
			if head == "(detached)" {
				st.Detached = true
			} else {
				st.Branch = head
			}
		case strings.HasPrefix(line, "# branch.upstream "):
			st.Upstream = strings.TrimPrefix(line, "# branch.upstream ")
		case strings.HasPrefix(line, "# branch.ab "):
			// 形如 "+2 -1"
			fields := strings.Fields(strings.TrimPrefix(line, "# branch.ab "))
			for _, f := range fields {
				if len(f) < 2 {
					continue
				}
				n, _ := strconv.Atoi(f[1:])
				switch f[0] {
				case '+':
					st.Ahead = n
				case '-':
					st.Behind = n
				}
			}
		case strings.HasPrefix(line, "1 "):
			if fc, ok := parseOrdinaryChange(line); ok {
				st.addChange(fc)
			}
		case strings.HasPrefix(line, "2 "):
			fc, ok := parseRenameChange(line)
			// -z 模式下原路径在下一个 NUL 条目中。
			if ok && i+1 < len(entries) {
				fc.OrigPath = entries[i+1]
				i++
			}
			if ok {
				st.addChange(fc)
			}
		case strings.HasPrefix(line, "u "):
			if fc, ok := parseUnmergedChange(line); ok {
				st.addChange(fc)
			}
		case strings.HasPrefix(line, "? "):
			path := strings.TrimPrefix(line, "? ")
			st.Untracked = append(st.Untracked, FileChange{Path: path, Untracked: true})
		case strings.HasPrefix(line, "! "):
			// 忽略文件，不展示
		}
	}
	return st, nil
}

// parseXY 从 XY 字段解析索引区/工作区状态。
func parseXY(s string) (byte, byte) {
	if len(s) < 2 {
		return '.', '.'
	}
	return s[0], s[1]
}

func parseOrdinaryChange(line string) (FileChange, bool) {
	// 1 XY sub mH mI mW hH hI path
	rest := strings.TrimPrefix(line, "1 ")
	fields := strings.SplitN(rest, " ", 8)
	if len(fields) < 8 {
		return FileChange{}, false
	}
	x, y := parseXY(fields[0])
	return FileChange{X: x, Y: y, Path: fields[7]}, true
}

func parseRenameChange(line string) (FileChange, bool) {
	// 2 XY sub mH mI mW hH hI Xscore path
	rest := strings.TrimPrefix(line, "2 ")
	fields := strings.SplitN(rest, " ", 9)
	if len(fields) < 9 {
		return FileChange{}, false
	}
	x, y := parseXY(fields[0])
	return FileChange{X: x, Y: y, Path: fields[8]}, true
}

func parseUnmergedChange(line string) (FileChange, bool) {
	// u XY sub m1 m2 m3 mW h1 h2 h3 path
	rest := strings.TrimPrefix(line, "u ")
	fields := strings.SplitN(rest, " ", 10)
	if len(fields) < 10 {
		return FileChange{}, false
	}
	x, y := parseXY(fields[0])
	return FileChange{X: x, Y: y, Path: fields[9], Unmerged: true}, true
}

func (s *Status) addChange(fc FileChange) {
	if fc.Staged() {
		s.Staged = append(s.Staged, fc)
	}
	if fc.Unstaged() {
		s.Unstaged = append(s.Unstaged, fc)
	}
}
