// Package graph 负责将提交序列（新→旧）布局成终端字符图。
// 算法参考 gitk / `git log --graph` 的 lane 分配方式，纯函数、无外部依赖。
// 支持增量构建（Builder），便于流式读取 git log 时逐行追加。
package graph

// NumColors 是 lane 配色数量，调用方可按此取色板。
const NumColors = 20

// Commit 是布局所需的提交信息。
type Commit struct {
	Hash    string
	Parents []string
}

// Row 是字符图中的一行。
type Row struct {
	// LaneChars 每个 lane 的字符：● ◆ │ ╭ ╮ ╰ ╯ 或空格。
	LaneChars []rune
	// Links[i] 是 lane i 与 lane i+1 之间的连接符：─ 或空格。
	Links []rune
	// Colors 与 LaneChars 对应，值为调色板下标，-1 表示无色（空格）。
	Colors []int
	// NodeLane 是本行提交所在的 lane。
	NodeLane int
	// IsMerge 表示本提交有多个父提交。
	IsMerge bool
	// LinkColors[i] 是第 i 段横线的调色板下标。
	LinkColors []int
}

// Layout 是完整布局结果。
type Layout struct {
	Rows     []Row
	MaxLanes int
	Index    map[string]int // hash -> 行号
}

// Builder 增量构建布局，保持 lane 状态跨行延续。
type Builder struct {
	layout   *Layout
	lanes    []string // lanes[i] 表示 lane i 正在等待的父提交 hash，空串表示空闲
	colors   []int
	colorSeq int
	colorOf  map[string]int
}

// SetColorOf 设置提交 hash -> 调色板下标（用于按分支着色）。
func (b *Builder) SetColorOf(m map[string]int) { b.colorOf = m }

func (b *Builder) colorFor(hash string, fallback int) int {
	if b.colorOf != nil {
		if c, ok := b.colorOf[hash]; ok {
			return c
		}
	}
	return fallback
}

// NewBuilder 创建增量构建器。
func NewBuilder() *Builder {
	return &Builder{
		layout: &Layout{Index: make(map[string]int)},
	}
}

// Len 返回已添加的提交数。
func (b *Builder) Len() int { return len(b.layout.Rows) }

// Layout 返回当前布局快照（切片与 builder 共享底层数组，调用方只读）。
func (b *Builder) Layout() *Layout { return b.layout }

func (b *Builder) growTo(n int) {
	for len(b.lanes) < n {
		b.lanes = append(b.lanes, "")
		b.colors = append(b.colors, -1)
	}
}

func (b *Builder) newLane() int {
	for i, h := range b.lanes {
		if h == "" {
			return i
		}
	}
	b.lanes = append(b.lanes, "")
	b.colors = append(b.colors, -1)
	return len(b.lanes) - 1
}

func (b *Builder) allocRight(after int) int {
	for i := after + 1; i < len(b.lanes); i++ {
		if b.lanes[i] == "" {
			return i
		}
	}
	b.lanes = append(b.lanes, "")
	b.colors = append(b.colors, -1)
	return len(b.lanes) - 1
}

// Add 追加一个提交并返回该行布局。
func (b *Builder) Add(c Commit) Row {
	rowIdx := len(b.layout.Rows)
	b.layout.Index[c.Hash] = rowIdx

	// 找出所有正在等待该提交的 lane，取最左作为节点列。
	col := -1
	for i, h := range b.lanes {
		if h == c.Hash {
			col = i
			break
		}
	}
	if col < 0 {
		col = b.newLane()
		b.colors[col] = b.colorFor(c.Hash, b.colorSeq%NumColors)
		b.colorSeq++
	}
	b.growTo(col + 1)

	// 基线：有效 lane 画竖线，空闲画空格。
	chars := make([]rune, len(b.lanes))
	cols := make([]int, len(b.lanes))
	for i, h := range b.lanes {
		if h != "" {
			chars[i] = '│'
			cols[i] = b.colors[i]
		} else {
			chars[i] = ' '
			cols[i] = -1
		}
	}
	links := make([]rune, max(0, len(b.lanes)-1))
	for i := range links {
		links[i] = ' '
	}

	laneExpect := make([]string, len(b.lanes))
	copy(laneExpect, b.lanes)

	chars[col] = '●'
	cols[col] = b.colors[col]
	maxUsed := col + 1

	linkColors := make([]int, len(links))
	for i := range linkColors {
		linkColors[i] = -1
	}
	// drawLink 在 from<to 之间画横线，穿过竖线处用 ┼。
	drawLink := func(from, to int, color int) {
		for k := from; k < to; k++ {
			links[k] = '─'
			linkColors[k] = color
		}
		for k := from + 1; k < to; k++ {
			if chars[k] == '│' {
				chars[k] = '┼'
			}
		}
		maxUsed = max(maxUsed, to+1)
	}

	// 其他等待该提交的 lane 汇入 col（右侧来线用 ╯，左侧用 ╰）。
	for i, h := range b.lanes {
		if i == col || h != c.Hash {
			continue
		}
		if i > col {
			chars[i] = '╯'
			drawLink(col, i, b.colors[i])
		} else {
			chars[i] = '╰'
			drawLink(i, col, b.colors[i])
		}
		cols[i] = b.colors[i]
		b.lanes[i] = ""
	}

	// 父提交分配：首父延续当前 lane，其余父放右侧空闲 lane。
	if len(c.Parents) == 0 {
		b.lanes[col] = ""
	} else {
		b.lanes[col] = c.Parents[0]
		for _, p := range c.Parents[1:] {
			if p == c.Parents[0] {
				continue // 重复父提交（罕见）不重复连线
			}
			target := -1
			// 只复用 col 右侧且确实在等待该父提交的 lane。
			for i := col + 1; i < len(b.lanes); i++ {
				if b.lanes[i] == p {
					target = i
					break
				}
			}
			if target < 0 {
				target = b.allocRight(col)
				b.colors[target] = b.colorFor(p, b.colorSeq%NumColors)
				b.colorSeq++
				b.growTo(target + 1)
				for len(chars) < len(b.lanes) {
					chars = append(chars, ' ')
					cols = append(cols, -1)
				}
				for len(links) < len(b.lanes)-1 {
					links = append(links, ' ')
					linkColors = append(linkColors, -1)
				}
			}
			b.lanes[target] = p
			chars[target] = '╮'
			drawLink(col, target, b.colors[target])
			cols[target] = b.colors[target]
		}
	}

	// 清理尾部空闲 lane，但保留本行已绘制的最大列（节点/连线）。
	end := len(b.lanes)
	for end > 0 && b.lanes[end-1] == "" {
		end--
	}
	b.lanes = b.lanes[:end]
	b.colors = b.colors[:end]
	keep := max(end, maxUsed)
	if len(chars) > keep {
		chars = chars[:keep]
		cols = cols[:keep]
	}
	if len(links) > max(0, keep-1) {
		links = links[:max(0, keep-1)]
	}

	r := Row{
		LaneChars:  chars,
		Links:      links,
		Colors:     cols,
		NodeLane:   col,
		IsMerge:    len(c.Parents) > 1,
		LinkColors: linkColors,
	}
	b.layout.Rows = append(b.layout.Rows, r)
	if len(chars) > b.layout.MaxLanes {
		b.layout.MaxLanes = len(chars)
	}
	return r
}

// Compute 批量计算布局（等价于逐个 Add）。
func Compute(commits []Commit) *Layout {
	b := NewBuilder()
	for _, c := range commits {
		b.Add(c)
	}
	return b.Layout()
}

func max(a, b int) int {
	if a > b {
		return a
	}
	return b
}
