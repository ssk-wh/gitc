package ui

import (
	"os"
	"runtime"
	"testing"
	"time"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/ssk-wh/gitc/internal/git"
)

// TestPerfRepos 通过环境变量指定大仓库，测量首帧与完整加载耗时。
// 用法：GITC_PERF_10K=<path> GITC_PERF_50K=<path> go test ./internal/ui -run TestPerfRepos -v
func TestPerfRepos(t *testing.T) {
	cases := []struct{ name, env string }{
		{"10k", "GITC_PERF_10K"},
		{"50k", "GITC_PERF_50K"},
	}
	for _, c := range cases {
		path := os.Getenv(c.env)
		if path == "" {
			continue
		}
		t.Run(c.name, func(t *testing.T) {
			repo, err := git.Open(path)
			if err != nil {
				t.Fatal(err)
			}
			a := New(repo)
			a.Update(tea.WindowSizeMsg{Width: 120, Height: 40})

			// 分别测量首屏两条路径
			startRefresh := time.Now()
			a.Update(a.refreshCmd(true)())
			refreshDur := time.Since(startRefresh)

			startLog := time.Now()
			a.Update(a.reloadLogCmd()())
			firstLog := time.Since(startLog)
			_ = a.View()
			first := refreshDur
			if firstLog < first {
				first = firstLog
			}
			t.Logf("首屏并行路径: refresh=%v log首批=%v", refreshDur, firstLog)

			// 完整加载
			start2 := time.Now()
			for a.loadingLog {
				a.Update(readBatchCmd(a.stream, a.logGen)())
			}
			full := time.Since(start2)

			// 渲染一帧（含视口 40 行）
			start3 := time.Now()
			for i := 0; i < 100; i++ {
				a.graphSel = 100 + i
				a.ensureGraphVisible()
				_ = a.View()
			}
			viewAvg := time.Since(start3) / 100

			var ms runtime.MemStats
			runtime.ReadMemStats(&ms)
			t.Logf("提交数=%d 首帧=%v 完整加载=%v 单帧渲染=%v heap=%.1fMB sys=%.1fMB",
				len(a.commits), first, full, viewAvg, float64(ms.HeapAlloc)/1e6, float64(ms.Sys)/1e6)
			if len(a.commits) == 0 {
				t.Fatal("未加载提交")
			}
		})
	}
}

func BenchmarkView40Rows(b *testing.B) {
	path := os.Getenv("GITC_PERF_10K")
	if path == "" {
		b.Skip("未设置 GITC_PERF_10K")
	}
	repo, err := git.Open(path)
	if err != nil {
		b.Fatal(err)
	}
	a := New(repo)
	a.Update(tea.WindowSizeMsg{Width: 120, Height: 40})
	a.Update(a.refreshCmd(true)())
	a.Update(a.reloadLogCmd()())
	for a.loadingLog {
		a.Update(readBatchCmd(a.stream, a.logGen)())
	}
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		a.graphSel = 200 + i%100
		a.ensureGraphVisible()
		_ = a.View()
	}
}
