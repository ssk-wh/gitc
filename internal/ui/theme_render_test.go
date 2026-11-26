package ui

import (
	"strings"
	"testing"

	"github.com/charmbracelet/lipgloss"
	"github.com/muesli/termenv"
)

// TestThemeRenderSnapshot 在真彩配置下对比默认主题与浅色主题的渲染输出，
// 确认主题切换确实改变配色（而非仅修改配置字段）。
func TestThemeRenderSnapshot(t *testing.T) {
	lipgloss.SetColorProfile(termenv.TrueColor)
	t.Cleanup(func() {
		applyTheme(defaultTheme())
		lipgloss.SetColorProfile(termenv.Ascii)
	})

	a, _ := newFakeApp(t)
	applyTheme(defaultTheme())
	darkView := a.View()
	if !strings.Contains(darkView, "38;2;") {
		t.Errorf("默认主题未渲染真彩色:\n%q", darkView)
	}

	applyTheme(lightTheme())
	lightView := a.View()
	if !strings.Contains(lightView, "38;2;") {
		t.Errorf("浅色主题未渲染真彩色:\n%q", lightView)
	}
	if darkView == lightView {
		t.Error("切换主题后渲染输出应不同")
	}
}

// TestChoiceMenuHasOwnColors 菜单每一行都显式着色，不依赖底层行的 SGR。
func TestChoiceMenuHasOwnColors(t *testing.T) {
	lipgloss.SetColorProfile(termenv.TrueColor)
	t.Cleanup(func() { lipgloss.SetColorProfile(termenv.Ascii) })
	a, _ := newFakeApp(t)
	a.choose("菜单", []choice{{label: "第一项"}, {label: "第二项"}})
	box, _, _ := a.choiceMenuBox()
	if len(box) == 0 {
		t.Fatal("菜单框为空")
	}
	for i, line := range box {
		if !strings.Contains(line, "\x1b[") {
			t.Fatalf("菜单第 %d 行未显式着色: %q", i, line)
		}
	}
}
