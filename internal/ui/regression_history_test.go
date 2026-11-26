package ui

import (
	"context"
	"fmt"
	"path/filepath"
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/ssk-wh/gitc/internal/config"
	"github.com/ssk-wh/gitc/internal/git"
	"github.com/ssk-wh/gitc/internal/workspace"
)

// 本文件沉淀历史 fix 提交中此前缺少断言的行为，防止回归。
// 每个用例注释标注对应的修复语义。

// TestFileMenuHasNoViewDiffItem 回归 2ce915b：文件右键菜单不应再有冗余的
// “查看 diff（聚焦内容区）”，但暂存/丢弃等真实操作仍要保留。
func TestFileMenuHasNoViewDiffItem(t *testing.T) {
	a, _ := newFakeApp(t)
	choices := a.fileMenuChoices(fileEntry{path: "a.txt"}, true)
	if len(choices) == 0 {
		t.Fatal("工作区文件菜单不应为空")
	}
	var labels []string
	for _, c := range choices {
		labels = append(labels, c.label)
		if strings.Contains(c.label, "查看 diff") || strings.Contains(strings.ToLower(c.label), "view diff") {
			t.Fatalf("文件菜单仍含冗余的查看 diff 项: %+v", labels)
		}
	}
	joined := strings.Join(labels, "\n")
	if !strings.Contains(joined, "暂存") || !strings.Contains(joined, "丢弃") {
		t.Fatalf("文件菜单缺少暂存/丢弃操作: %+v", labels)
	}
}

// TestSearchWheelScrollsResults 回归 a3adf01：搜索框激活时，提交结果列表
// 仍应支持滚轮滚动视口，且不改变选中行、不退出搜索态。
func TestSearchWheelScrollsResults(t *testing.T) {
	a, _ := newFakeApp(t)
	a.Update(tea.WindowSizeMsg{Width: 120, Height: 40})

	// 构造足够多的搜索结果，保证视口可滚动。
	results := make([]git.Commit, 0, 40)
	for i := 0; i < 40; i++ {
		h := fmt.Sprintf("%016d", i)
		results = append(results, git.Commit{
			Hash: h, Short: h[:7], Author: "张三",
			Subject: fmt.Sprintf("修复第 %d 项", i),
		})
	}
	a.searchActive = true
	a.searchResults = results
	a.graphOff = 0
	a.graphSel = 0

	// 120x40 下 (60,5) 位于提交区内部，与 TestMouseWheelScroll 保持一致。
	a.Update(tea.MouseMsg{Action: tea.MouseActionPress, Button: tea.MouseButtonWheelDown, X: 60, Y: 5})
	if a.graphOff == 0 {
		t.Fatal("搜索态滚轮未滚动提交结果视口")
	}
	if a.graphSel != 0 {
		t.Errorf("搜索态滚轮不应改变选中行，实际 %d", a.graphSel)
	}
	if !a.searchActive {
		t.Error("搜索态滚轮不应退出搜索")
	}
}

// TestSaveConfigDoesNotPersistProjects 回归 0a75686：项目列表由各仓库的
// .gitc-workspace 管理，saveConfig 不能把项目路径写进全局配置，避免跨仓库污染。
func TestSaveConfigDoesNotPersistProjects(t *testing.T) {
	isolateConfig(t)
	a, _ := newFakeApp(t)
	a.persist = true
	// 预置一份“脏”配置，模拟旧版本写入过项目列表。
	a.cfg = config.File{
		Projects:    []string{"/old/repo-a", "/old/repo-b"},
		LastProject: "/old/repo-a",
	}
	a.saveConfig()

	got := config.Load()
	if len(got.Projects) != 0 {
		t.Fatalf("全局配置仍写入项目列表: %+v", got.Projects)
	}
	if got.LastProject != "" {
		t.Fatalf("全局配置仍写入 LastProject: %q", got.LastProject)
	}
}

// TestRemoveProjectSyncsWorkspace 回归 f7d0e14：移除项目时要把更新后的项目组
// 同步写回所有相关根目录的 .gitc-workspace（含被移除项目目录本身）。
func TestRemoveProjectSyncsWorkspace(t *testing.T) {
	dirA, dirB := t.TempDir(), t.TempDir()
	a, _ := newFakeApp(t)
	a.SetProjects([]string{dirA, dirB}, dirA)
	a.currentPath = cleanProjectPath(dirA)
	a.currentProjectPath = cleanProjectPath(dirA)

	// 移除非当前项目，避免触发 switchProject 的仓库打开逻辑。
	var target projectEntry
	for _, p := range a.projects {
		if p.Path == cleanProjectPath(dirB) {
			target = p
		}
	}
	if target.Path == "" {
		t.Fatal("未找到待移除项目")
	}
	a.removeProject(target)
	if len(a.projects) != 1 || a.projects[0].Path != cleanProjectPath(dirA) {
		t.Fatalf("移除后项目 = %+v", a.projects)
	}

	want := []string{cleanProjectPath(dirA)}
	for _, root := range []string{dirA, dirB} {
		ws, err := workspace.Load(cleanProjectPath(root))
		if err != nil {
			t.Fatalf("读取 %s 的 workspace 失败: %v", root, err)
		}
		if len(ws.Projects) != 1 || ws.Projects[0] != want[0] {
			t.Fatalf("%s 的 workspace 未同步: %+v", root, ws.Projects)
		}
	}
}

// pruneFakeSvc 只实现 loadWorktreesCmd 需要的两个方法。
type pruneFakeSvc struct {
	git.Service
	wts    []git.Worktree
	pruned bool
}

func (f *pruneFakeSvc) Worktrees(context.Context) ([]git.Worktree, error) { return f.wts, nil }

func (f *pruneFakeSvc) PruneWorktrees(context.Context) (string, error) {
	f.pruned = true
	return "", nil
}

// TestLoadWorktreesPrunesStale 回归 492b43b：加载 worktree 时既要过滤失效记录，
// 也要自动执行 worktree prune 清理元数据，避免下次继续列出。
func TestLoadWorktreesPrunesStale(t *testing.T) {
	root := t.TempDir()
	missing := filepath.Join(t.TempDir(), "gone")
	svc := &pruneFakeSvc{wts: []git.Worktree{
		{Path: root, Branch: "main", Main: true},
		{Path: missing, Branch: "gone", Prunable: true},
	}}

	a, _ := newFakeApp(t)
	a.SetProjects([]string{root}, root)
	a.currentPath = cleanProjectPath(root)
	a.currentProjectPath = cleanProjectPath(root)
	a.openRepo = func(string) (git.Service, error) { return svc, nil }

	msg := a.loadWorktreesCmd()()
	wm, ok := msg.(worktreesMsg)
	if !ok {
		t.Fatalf("loadWorktreesCmd 返回 %T，期望 worktreesMsg", msg)
	}
	list := wm.list[cleanProjectPath(root)]
	if len(list) != 1 || list[0].Branch != "main" {
		t.Fatalf("失效 worktree 未被过滤: %+v", list)
	}
	if !svc.pruned {
		t.Fatal("发现失效 worktree 后未自动执行 prune")
	}
}
