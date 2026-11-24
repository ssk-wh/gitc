package ui

import (
	"strings"

	"github.com/charmbracelet/lipgloss"

	"github.com/ssk-wh/gitc/internal/git"
)

// 主题色板：深色/浅色终端自适应。
var (
	colorFg      = lipgloss.AdaptiveColor{Light: "#24292f", Dark: "#d0d7de"}
	colorDim     = lipgloss.AdaptiveColor{Light: "#6e7781", Dark: "#8b949e"}
	colorAccent  = lipgloss.AdaptiveColor{Light: "#0969da", Dark: "#58a6ff"}
	colorGood    = lipgloss.AdaptiveColor{Light: "#1a7f37", Dark: "#3fb950"}
	colorWarn    = lipgloss.AdaptiveColor{Light: "#9a6700", Dark: "#d29922"}
	colorBad     = lipgloss.AdaptiveColor{Light: "#cf222e", Dark: "#f85149"}
	colorMagenta = lipgloss.AdaptiveColor{Light: "#8250df", Dark: "#bc8cff"}
	colorCyan    = lipgloss.AdaptiveColor{Light: "#1b7c83", Dark: "#56d4dd"}
	// colorBorder / colorSelect 由主题的 border / selection token 驱动。
	colorBorder = lipgloss.AdaptiveColor{Light: "#0969da", Dark: "#58a6ff"}
	colorSelect = lipgloss.AdaptiveColor{Light: "#0969da", Dark: "#58a6ff"}
)

var lanePalette = defaultLanes

var (
	styleTitle   = lipgloss.NewStyle().Bold(true).Foreground(colorFg)
	styleDim     = lipgloss.NewStyle().Foreground(colorDim)
	styleAccent  = lipgloss.NewStyle().Foreground(colorAccent)
	styleGood    = lipgloss.NewStyle().Foreground(colorGood)
	styleWarn    = lipgloss.NewStyle().Foreground(colorWarn)
	styleBad     = lipgloss.NewStyle().Foreground(colorBad)
	styleMagenta = lipgloss.NewStyle().Foreground(colorMagenta)
	styleCyan    = lipgloss.NewStyle().Foreground(colorCyan)
	styleBold    = lipgloss.NewStyle().Bold(true)
	styleSel     = lipgloss.NewStyle().Bold(true).Foreground(colorSelect)
	styleHeader  = lipgloss.NewStyle().Bold(true).Foreground(colorDim)
	styleBox     = lipgloss.NewStyle().Border(lipgloss.RoundedBorder()).BorderForeground(colorBorder).Padding(1, 2)
	styleBoxBad  = lipgloss.NewStyle().Border(lipgloss.RoundedBorder()).BorderForeground(colorBad).Padding(1, 2)
	styleSep     = lipgloss.NewStyle().Foreground(colorDim)
	styleRowSel  = lipgloss.NewStyle().Background(colorSelect).Foreground(lipgloss.Color("#0b1020")).Bold(true)
)

func laneStyle(i int) lipgloss.Style {
	if i < 0 || i >= len(lanePalette) {
		return lipgloss.NewStyle().Foreground(colorDim)
	}
	return lipgloss.NewStyle().Foreground(lanePalette[i])
}

func refStyle(kind string) lipgloss.Style {
	switch kind {
	case "local":
		return lipgloss.NewStyle().Foreground(colorGood).Bold(true)
	case "remote":
		return lipgloss.NewStyle().Foreground(colorMagenta)
	case "tag":
		return lipgloss.NewStyle().Foreground(colorWarn)
	case "stash":
		return lipgloss.NewStyle().Foreground(colorCyan)
	}
	return lipgloss.NewStyle().Foreground(colorDim)
}

// colorIndexForRef 为引用名分配稳定调色板下标。
func colorIndexForRef(name string) int {
	var h uint32 = 2166136261
	for i := 0; i < len(name); i++ {
		h ^= uint32(name[i])
		h *= 16777619
	}
	return int(h) % len(lanePalette)
}

// colorForRef 为引用名分配稳定颜色（同名始终同色）。
func colorForRef(name string) lipgloss.AdaptiveColor {
	return lanePalette[colorIndexForRef(name)]
}

// normalizeRefName 将远端分支归并到本地同名分支（origin/feature -> feature）。
func normalizeRefName(kind, name string) string {
	if kind == git.RefRemote {
		if i := strings.IndexByte(name, '/'); i >= 0 && i+1 < len(name) {
			return name[i+1:]
		}
	}
	return name
}
