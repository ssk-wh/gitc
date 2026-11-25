package ui

import (
	"strings"

	"github.com/charmbracelet/lipgloss"

	"github.com/ssk-wh/gitc/internal/config"
)

// SetConfig 应用用户配置（主题与布局）。
func (a *App) SetConfig(c config.File) {
	a.cfg = c
	a.persist = true
	a.leftW = c.LeftWidth
	a.drawerLeftW = c.DrawerLeft
	a.rightTopH = c.RightTop
	a.leftTopH = c.LeftTop
	a.leftMidH = c.LeftMid
	a.cmdH = c.CommandH
	a.applyThemeSettings(c.Theme, c.Colors)
	a.loadInitialProjects(c)
	if len(a.themeBad) > 0 {
		a.setToast("主题颜色非法并已忽略: "+strings.Join(a.themeBad, ", "), true)
	}
}

// saveConfig 持久化当前布局与主题。
func (a *App) saveConfig() {
	if !a.persist {
		return
	}
	a.cfg.LeftWidth = a.leftW
	a.cfg.DrawerLeft = a.drawerLeftW
	a.cfg.RightTop = a.rightTopH
	a.cfg.LeftTop = a.leftTopH
	a.cfg.LeftMid = a.leftMidH
	a.cfg.CommandH = a.cmdH
	a.cfg.Theme = a.themeName
	a.cfg.Colors = a.themeColors
	// 项目列表由各仓库的 .gitc-workspace 管理，不写入全局配置，避免跨仓库污染
	a.cfg.Projects = nil
	a.cfg.LastProject = ""
	_ = config.Save(a.cfg)
}

// applyThemeSettings 应用主题与自定义颜色。
func (a *App) applyThemeSettings(name string, colors map[string]string) {
	t, bad := resolveTheme(name, colors)
	// light 主题是给浅色终端用的；深色终端下自动回退默认配色，避免整体发暗
	if t.Name == "light" && lipgloss.HasDarkBackground() {
		t, bad = resolveTheme("default", colors)
		bad = append(bad, "light 主题不适用于深色终端，已回退 default")
	}
	applyTheme(t)
	a.themeName = name
	a.themeColors = colors
	a.themeBad = bad
}

// cycleTheme 循环切换内置主题并持久化。
func (a *App) cycleTheme() {
	idx := 0
	for i, n := range ThemeOrder {
		if n == a.themeName {
			idx = i
		}
	}
	next := ThemeOrder[(idx+1)%len(ThemeOrder)]
	a.applyThemeSettings(next, a.themeColors)
	a.cfg.Theme = next
	a.saveConfig()
	msg := "主题: " + next
	if len(a.themeBad) > 0 {
		msg += "（忽略: " + strings.Join(a.themeBad, ", ") + "）"
	}
	a.setToast(msg, false)
}
