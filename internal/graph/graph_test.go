package graph

import "testing"

// renderRow 将一行转成字符串，便于断言（不含颜色）。
func renderRow(r Row) string {
	out := make([]rune, 0, len(r.LaneChars)*2)
	for i, c := range r.LaneChars {
		out = append(out, c)
		if i < len(r.Links) {
			out = append(out, r.Links[i])
		}
	}
	return string(out)
}

func renderAll(l *Layout) []string {
	rows := make([]string, len(l.Rows))
	for i, r := range l.Rows {
		rows[i] = renderRow(r)
	}
	return rows
}

func TestLinear(t *testing.T) {
	l := Compute([]Commit{
		{Hash: "c", Parents: []string{"b"}},
		{Hash: "b", Parents: []string{"a"}},
		{Hash: "a"},
	})
	want := []string{"●", "●", "●"}
	got := renderAll(l)
	if len(got) != len(want) {
		t.Fatalf("行数 = %d, 期望 %d: %v", len(got), len(want), got)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Errorf("第 %d 行 = %q, 期望 %q", i, got[i], want[i])
		}
	}
	if l.MaxLanes != 1 {
		t.Errorf("MaxLanes = %d, 期望 1", l.MaxLanes)
	}
	if l.Index["a"] != 2 || l.Index["c"] != 0 {
		t.Errorf("Index 错误: %v", l.Index)
	}
}

func TestMergeTwoBranches(t *testing.T) {
	l := Compute([]Commit{
		{Hash: "m", Parents: []string{"a", "b"}},
		{Hash: "a", Parents: []string{"x"}},
		{Hash: "b", Parents: []string{"x"}},
		{Hash: "x"},
	})
	want := []string{
		"●─╮",
		"● │",
		"│ ●",
		"●─╯",
	}
	got := renderAll(l)
	if len(got) != len(want) {
		t.Fatalf("行数 = %d, 期望 %d", len(got), len(want))
	}
	for i := range want {
		if got[i] != want[i] {
			t.Errorf("第 %d 行 = %q, 期望 %q", i, got[i], want[i])
		}
	}
	if !l.Rows[0].IsMerge {
		t.Error("m 应为合并提交")
	}
	if l.Rows[0].NodeLane != 0 {
		t.Errorf("m 节点 lane = %d, 期望 0", l.Rows[0].NodeLane)
	}
}

func TestOctopus(t *testing.T) {
	l := Compute([]Commit{
		{Hash: "o", Parents: []string{"a", "b", "c"}},
		{Hash: "a"},
		{Hash: "b"},
		{Hash: "c"},
	})
	want := []string{
		"●─╮─╮",
		"● │ │",
		"  ● │",
		"    ●",
	}
	got := renderAll(l)
	for i := range want {
		if got[i] != want[i] {
			t.Errorf("第 %d 行 = %q, 期望 %q", i, got[i], want[i])
		}
	}
}

func TestTwoRoots(t *testing.T) {
	l := Compute([]Commit{
		{Hash: "a"},
		{Hash: "b"},
	})
	want := []string{"●", "●"}
	got := renderAll(l)
	for i := range want {
		if got[i] != want[i] {
			t.Errorf("第 %d 行 = %q, 期望 %q", i, got[i], want[i])
		}
	}
}

func TestCrissCross(t *testing.T) {
	// m1 合并 a b，m2 合并 b a，形成交叉汇合
	l := Compute([]Commit{
		{Hash: "m1", Parents: []string{"a", "b"}},
		{Hash: "m2", Parents: []string{"b", "a"}},
		{Hash: "a", Parents: []string{"r"}},
		{Hash: "b", Parents: []string{"r"}},
		{Hash: "r"},
	})
	// 只断言结构合法：节点行数正确、无 panic、每条父边都有 lane 归属
	if len(l.Rows) != 5 {
		t.Fatalf("行数 = %d", len(l.Rows))
	}
	for i, r := range l.Rows {
		if r.NodeLane < 0 || r.NodeLane >= len(r.LaneChars) {
			t.Errorf("第 %d 行 NodeLane 越界: %d (lanes=%d)", i, r.NodeLane, len(r.LaneChars))
		}
	}
	// 所有 lane 数不超过 4（两个独立 tip + 两条汇合 lane）
	if l.MaxLanes > 4 {
		t.Errorf("MaxLanes = %d, 期望 <= 4", l.MaxLanes)
	}
}

func TestLaneReuse(t *testing.T) {
	// a 与 b 合并后 lane1 释放，新的独立根 c 应复用 lane1
	l := Compute([]Commit{
		{Hash: "m", Parents: []string{"a", "b"}},
		{Hash: "a"},
		{Hash: "b"},
		{Hash: "c"},
	})
	got := renderAll(l)
	want := []string{
		"●─╮",
		"● │",
		"  ●",
		"●",
	}
	for i := range want {
		if got[i] != want[i] {
			t.Errorf("第 %d 行 = %q, 期望 %q", i, got[i], want[i])
		}
	}
}

func TestIncrementalEqualsBatch(t *testing.T) {
	commits := syntheticCommits(500)
	batch := Compute(commits)
	b := NewBuilder()
	for _, c := range commits {
		b.Add(c)
	}
	inc := b.Layout()
	if len(inc.Rows) != len(batch.Rows) || inc.MaxLanes != batch.MaxLanes {
		t.Fatalf("增量与批量布局规模不一致: rows %d/%d maxLanes %d/%d",
			len(inc.Rows), len(batch.Rows), inc.MaxLanes, batch.MaxLanes)
	}
	for i := range batch.Rows {
		if renderRow(inc.Rows[i]) != renderRow(batch.Rows[i]) {
			t.Fatalf("第 %d 行不一致: %q vs %q", i, renderRow(inc.Rows[i]), renderRow(batch.Rows[i]))
		}
	}
}

func TestCrossingUsesJunction(t *testing.T) {
	l := Compute([]Commit{
		{Hash: "m1", Parents: []string{"a", "b"}},
		{Hash: "m2", Parents: []string{"b", "a"}},
		{Hash: "a", Parents: []string{"r"}},
		{Hash: "b", Parents: []string{"r"}},
		{Hash: "r"},
	})
	if got := renderRow(l.Rows[2]); got != "●─┼─┼─╯" {
		t.Errorf("交叉行 = %q, 期望 %q", got, "●─┼─┼─╯")
	}
}

func TestEmpty(t *testing.T) {
	l := Compute(nil)
	if len(l.Rows) != 0 || l.MaxLanes != 0 {
		t.Errorf("空输入应得到空布局: rows=%d lanes=%d", len(l.Rows), l.MaxLanes)
	}
}

func TestColorsStable(t *testing.T) {
	l := Compute([]Commit{
		{Hash: "m", Parents: []string{"a", "b"}},
		{Hash: "a"},
		{Hash: "b"},
	})
	// 同一 lane 在不同行的颜色应一致
	if l.Rows[0].Colors[0] != l.Rows[1].Colors[0] {
		t.Errorf("lane0 颜色不稳定")
	}
	// 不同 lane 初始颜色应不同
	if l.Rows[0].Colors[0] == l.Rows[0].Colors[1] {
		t.Errorf("lane0/lane1 颜色相同")
	}
}

func TestIndexAllCommits(t *testing.T) {
	commits := []Commit{
		{Hash: "m", Parents: []string{"a", "b"}},
		{Hash: "a", Parents: []string{"x"}},
		{Hash: "b", Parents: []string{"x"}},
		{Hash: "x"},
	}
	l := Compute(commits)
	if len(l.Index) != len(commits) {
		t.Errorf("Index 大小 = %d, 期望 %d", len(l.Index), len(commits))
	}
	for i, c := range commits {
		if l.Index[c.Hash] != i {
			t.Errorf("%s 的索引 = %d, 期望 %d", c.Hash, l.Index[c.Hash], i)
		}
	}
}

func BenchmarkCompute10k(b *testing.B) {
	commits := syntheticCommits(10000)
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		Compute(commits)
	}
}

func BenchmarkCompute50k(b *testing.B) {
	commits := syntheticCommits(50000)
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		Compute(commits)
	}
}

// syntheticCommits 生成带周期合并的提交序列（新→旧）。
func syntheticCommits(n int) []Commit {
	commits := make([]Commit, n)
	for i := 0; i < n; i++ {
		c := Commit{Hash: itoa(i)}
		if i+1 < n {
			c.Parents = append(c.Parents, itoa(i+1))
		}
		// 每 50 个提交插入一条并行支线，10 行后汇合
		if i%50 == 0 && i+70 < n {
			c.Parents = append(c.Parents, itoa(i+70))
		}
		commits[i] = c
	}
	return commits
}

func itoa(i int) string {
	if i == 0 {
		return "0"
	}
	var b [12]byte
	p := len(b)
	for i > 0 {
		p--
		b[p] = byte('0' + i%10)
		i /= 10
	}
	return string(b[p:])
}

// TestMergeLinkColors 验证合并连接线带 lane 颜色，且与目标 lane 颜色一致。
func TestMergeLinkColors(t *testing.T) {
	l := Compute([]Commit{
		{Hash: "m", Parents: []string{"a", "b"}},
		{Hash: "a", Parents: []string{"x"}},
		{Hash: "b", Parents: []string{"x"}},
		{Hash: "x"},
	})
	row := l.Rows[0]
	if len(row.LinkColors) == 0 || row.LinkColors[0] < 0 {
		t.Fatalf("合并行连接线缺少颜色: %+v", row.LinkColors)
	}
	if row.LinkColors[0] != row.Colors[1] {
		t.Errorf("连接线颜色 = %d, 期望与 lane1 一致 = %d", row.LinkColors[0], row.Colors[1])
	}
}
