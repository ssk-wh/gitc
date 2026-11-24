package ui

import (
	"strings"

	"github.com/charmbracelet/lipgloss"
)

// Theme 是语义配色主题。
type Theme struct {
	Name      string
	Fg        lipgloss.AdaptiveColor
	Dim       lipgloss.AdaptiveColor
	Accent    lipgloss.AdaptiveColor
	Good      lipgloss.AdaptiveColor
	Warn      lipgloss.AdaptiveColor
	Bad       lipgloss.AdaptiveColor
	Magenta   lipgloss.AdaptiveColor
	Cyan      lipgloss.AdaptiveColor
	Border    lipgloss.AdaptiveColor
	Selection lipgloss.AdaptiveColor
	Lanes     []lipgloss.AdaptiveColor
}

// ThemeOrder 是切换主题的顺序。
var ThemeOrder = []string{"default", "light", "high-contrast"}

func defaultTheme() Theme {
	// 轻快配色：明亮蓝 + 薄荷绿 + 柠檬黄 + 珊瑚红 + 紫罗兰
	return Theme{
		Name:      "default",
		Fg:        lipgloss.AdaptiveColor{Light: "#272343", Dark: "#f2f7ff"},
		Dim:       lipgloss.AdaptiveColor{Light: "#5f6c7b", Dark: "#a3b5c7"},
		Accent:    lipgloss.AdaptiveColor{Light: "#0969da", Dark: "#3da9fc"},
		Good:      lipgloss.AdaptiveColor{Light: "#1a7f37", Dark: "#2cb67d"},
		Warn:      lipgloss.AdaptiveColor{Light: "#9a6700", Dark: "#ffd803"},
		Bad:       lipgloss.AdaptiveColor{Light: "#cf222e", Dark: "#ef4565"},
		Magenta:   lipgloss.AdaptiveColor{Light: "#6f42c1", Dark: "#a78bfa"},
		Cyan:      lipgloss.AdaptiveColor{Light: "#0f766e", Dark: "#38bec9"},
		Border:    lipgloss.AdaptiveColor{Light: "#0969da", Dark: "#3da9fc"},
		Selection: lipgloss.AdaptiveColor{Light: "#0969da", Dark: "#38bec9"},
		Lanes: []lipgloss.AdaptiveColor{
			{Light: "#0969da", Dark: "#3da9fc"}, // 清新蓝
			{Light: "#1a7f37", Dark: "#2cb67d"}, // 薄荷绿
			{Light: "#9a6700", Dark: "#ffd803"}, // 柠檬黄
			{Light: "#cf222e", Dark: "#ef4565"}, // 珊瑚红
			{Light: "#6f42c1", Dark: "#a78bfa"}, // 紫罗兰
			{Light: "#0f766e", Dark: "#38bec9"}, // 青碧
			{Light: "#bf3989", Dark: "#ff8ba7"}, // 蜜桃粉
			{Light: "#57606a", Dark: "#b8c7d9"}, // 雾蓝
		},
	}
}

func lightTheme() Theme {
	t := Theme{
		Name:      "light",
		Fg:        lipgloss.AdaptiveColor{Light: "#1f2328", Dark: "#1f2328"},
		Dim:       lipgloss.AdaptiveColor{Light: "#59636e", Dark: "#59636e"},
		Accent:    lipgloss.AdaptiveColor{Light: "#0550ae", Dark: "#0550ae"},
		Good:      lipgloss.AdaptiveColor{Light: "#116329", Dark: "#116329"},
		Warn:      lipgloss.AdaptiveColor{Light: "#7d4e00", Dark: "#7d4e00"},
		Bad:       lipgloss.AdaptiveColor{Light: "#a40e26", Dark: "#a40e26"},
		Magenta:   lipgloss.AdaptiveColor{Light: "#6639ba", Dark: "#6639ba"},
		Cyan:      lipgloss.AdaptiveColor{Light: "#0f6b6b", Dark: "#0f6b6b"},
		Border:    lipgloss.AdaptiveColor{Light: "#0550ae", Dark: "#0550ae"},
		Selection: lipgloss.AdaptiveColor{Light: "#0550ae", Dark: "#0550ae"},
	}
	t.Lanes = []lipgloss.AdaptiveColor{
		{Light: "#0550ae", Dark: "#0550ae"},
		{Light: "#116329", Dark: "#116329"},
		{Light: "#7d4e00", Dark: "#7d4e00"},
		{Light: "#a40e26", Dark: "#a40e26"},
		{Light: "#6639ba", Dark: "#6639ba"},
		{Light: "#0f6b6b", Dark: "#0f6b6b"},
		{Light: "#9a2d6e", Dark: "#9a2d6e"},
		{Light: "#424a53", Dark: "#424a53"},
	}
	return t
}

func highContrastTheme() Theme {
	t := Theme{
		Name:      "high-contrast",
		Fg:        lipgloss.AdaptiveColor{Light: "#000000", Dark: "#ffffff"},
		Dim:       lipgloss.AdaptiveColor{Light: "#333333", Dark: "#c0c0c0"},
		Accent:    lipgloss.AdaptiveColor{Light: "#0000ff", Dark: "#00afff"},
		Good:      lipgloss.AdaptiveColor{Light: "#008000", Dark: "#00ff5f"},
		Warn:      lipgloss.AdaptiveColor{Light: "#8a6d00", Dark: "#ffd700"},
		Bad:       lipgloss.AdaptiveColor{Light: "#b00020", Dark: "#ff5f5f"},
		Magenta:   lipgloss.AdaptiveColor{Light: "#800080", Dark: "#ff5fff"},
		Cyan:      lipgloss.AdaptiveColor{Light: "#006666", Dark: "#00ffff"},
		Border:    lipgloss.AdaptiveColor{Light: "#0000ff", Dark: "#00afff"},
		Selection: lipgloss.AdaptiveColor{Light: "#0000ff", Dark: "#00afff"},
	}
	t.Lanes = []lipgloss.AdaptiveColor{
		{Light: "#0000ff", Dark: "#00afff"},
		{Light: "#008000", Dark: "#00ff5f"},
		{Light: "#8a6d00", Dark: "#ffd700"},
		{Light: "#b00020", Dark: "#ff5f5f"},
		{Light: "#800080", Dark: "#ff5fff"},
		{Light: "#006666", Dark: "#00ffff"},
		{Light: "#a0006a", Dark: "#ff87d7"},
		{Light: "#444444", Dark: "#d0d0d0"},
	}
	return t
}

func builtinTheme(name string) (Theme, bool) {
	switch name {
	case "light":
		return lightTheme(), true
	case "high-contrast":
		return highContrastTheme(), true
	case "", "default":
		return defaultTheme(), true
	}
	return defaultTheme(), false
}

// resolveTheme 合并内置主题与自定义颜色；非法主题名/颜色被忽略并返回名称列表。
func resolveTheme(name string, overrides map[string]string) (Theme, []string) {
	t, known := builtinTheme(name)
	var bad []string
	if !known && name != "" {
		// 未知主题名回退默认主题，并让调用方提示用户。
		t = defaultTheme()
		bad = append(bad, "theme")
	}
	if t.Name == "" {
		t = defaultTheme()
	}
	if !known || t.Name == "" {
		t.Name = "default"
	}
	set := func(key string, dst *lipgloss.AdaptiveColor) {
		v, ok := overrides[key]
		if !ok {
			return
		}
		c, ok := parseHexColor(v)
		if !ok {
			bad = append(bad, key)
			return
		}
		*dst = c
	}
	set("fg", &t.Fg)
	set("dim", &t.Dim)
	set("accent", &t.Accent)
	set("good", &t.Good)
	set("warn", &t.Warn)
	set("bad", &t.Bad)
	set("magenta", &t.Magenta)
	set("cyan", &t.Cyan)
	set("border", &t.Border)
	set("selection", &t.Selection)
	for i := 0; i < 8; i++ {
		key := "lane" + string(rune('0'+i))
		if v, ok := overrides[key]; ok {
			if c, ok := parseHexColor(v); ok {
				if i < len(t.Lanes) {
					t.Lanes[i] = c
				}
			} else {
				bad = append(bad, key)
			}
		}
	}
	return t, bad
}

func parseHexColor(s string) (lipgloss.AdaptiveColor, bool) {
	s = strings.TrimSpace(s)
	if len(s) != 7 || s[0] != '#' {
		return lipgloss.AdaptiveColor{}, false
	}
	for _, r := range s[1:] {
		if !((r >= '0' && r <= '9') || (r >= 'a' && r <= 'f') || (r >= 'A' && r <= 'F')) {
			return lipgloss.AdaptiveColor{}, false
		}
	}
	return lipgloss.AdaptiveColor{Light: s, Dark: s}, true
}

func init() { applyTheme(defaultTheme()) }

// applyTheme 应用主题到全局样式。
func applyTheme(t Theme) {
	colorFg = t.Fg
	colorDim = t.Dim
	colorAccent = t.Accent
	colorGood = t.Good
	colorWarn = t.Warn
	colorBad = t.Bad
	colorMagenta = t.Magenta
	colorCyan = t.Cyan
	colorBorder = t.Border
	colorSelect = t.Selection
	lanePalette = t.Lanes

	styleTitle = lipgloss.NewStyle().Bold(true).Foreground(colorFg)
	styleDim = lipgloss.NewStyle().Foreground(colorDim)
	styleAccent = lipgloss.NewStyle().Foreground(colorAccent)
	styleGood = lipgloss.NewStyle().Foreground(colorGood)
	styleWarn = lipgloss.NewStyle().Foreground(colorWarn)
	styleBad = lipgloss.NewStyle().Foreground(colorBad)
	styleMagenta = lipgloss.NewStyle().Foreground(colorMagenta)
	styleCyan = lipgloss.NewStyle().Foreground(colorCyan)
	styleBold = lipgloss.NewStyle().Bold(true)
	styleSel = lipgloss.NewStyle().Bold(true).Foreground(colorSelect)
	styleHeader = lipgloss.NewStyle().Bold(true).Foreground(colorDim)
	styleBox = lipgloss.NewStyle().Border(lipgloss.RoundedBorder()).BorderForeground(colorBorder).Padding(1, 2)
	styleBoxBad = lipgloss.NewStyle().Border(lipgloss.RoundedBorder()).BorderForeground(colorBad).Padding(1, 2)
	styleSep = lipgloss.NewStyle().Foreground(colorDim)
	styleRowSel = lipgloss.NewStyle().Background(colorSelect).Foreground(lipgloss.Color("#0b1020")).Bold(true)
}
