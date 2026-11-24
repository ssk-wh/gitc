package ui

import (
	"strconv"
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
var ThemeOrder = []string{"default", "light", "ocean", "forest", "fire", "rose", "high-contrast"}

// defaultLanes 是 20 色分支/提交图配色，深浅终端各一套。
var defaultLanes = []lipgloss.AdaptiveColor{
	{Light: "#0969da", Dark: "#58a6ff"}, // 蓝
	{Light: "#1a7f37", Dark: "#3fb950"}, // 绿
	{Light: "#9a6700", Dark: "#d29922"}, // 琥珀
	{Light: "#cf222e", Dark: "#f85149"}, // 红
	{Light: "#8250df", Dark: "#bc8cff"}, // 紫
	{Light: "#1b7c83", Dark: "#56d4dd"}, // 青
	{Light: "#bf3989", Dark: "#ff7bcf"}, // 粉
	{Light: "#57606a", Dark: "#a5b1bd"}, // 灰蓝
	{Light: "#0b7285", Dark: "#63e6be"}, // 薄荷
	{Light: "#b35c00", Dark: "#ffa94d"}, // 橙
	{Light: "#5f3dc4", Dark: "#b197fc"}, // 靛
	{Light: "#a61e4d", Dark: "#f06595"}, // 玫红
	{Light: "#2b8a3e", Dark: "#8ce99a"}, // 草绿
	{Light: "#1864ab", Dark: "#74c0fc"}, // 天蓝
	{Light: "#862e9c", Dark: "#da77f2"}, // 紫罗兰
	{Light: "#0c8599", Dark: "#66d9e8"}, // 湖蓝
	{Light: "#e67700", Dark: "#ffd43b"}, // 金黄
	{Light: "#c92a2a", Dark: "#ff8787"}, // 珊瑚
	{Light: "#364fc7", Dark: "#91a7ff"}, // 宝蓝
	{Light: "#5c940d", Dark: "#c0eb75"}, // 橄榄
}

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
		Lanes: defaultLanes,
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
	t.Lanes = defaultLanes
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
	t.Lanes = defaultLanes
	return t
}


// oceanTheme 深海蓝：冷静、专注。
func oceanTheme() Theme {
	return Theme{
		Name:      "ocean",
		Fg:        lipgloss.AdaptiveColor{Light: "#0b2545", Dark: "#eaf4ff"},
		Dim:       lipgloss.AdaptiveColor{Light: "#506a85", Dark: "#94a1b2"},
		Accent:    lipgloss.AdaptiveColor{Light: "#0550ae", Dark: "#58a6ff"},
		Good:      lipgloss.AdaptiveColor{Light: "#0b7285", Dark: "#2cb67d"},
		Warn:      lipgloss.AdaptiveColor{Light: "#9a6700", Dark: "#ffd43b"},
		Bad:       lipgloss.AdaptiveColor{Light: "#cf222e", Dark: "#ff6b6b"},
		Magenta:   lipgloss.AdaptiveColor{Light: "#5f3dc4", Dark: "#b197fc"},
		Cyan:      lipgloss.AdaptiveColor{Light: "#0c8599", Dark: "#66d9e8"},
		Border:    lipgloss.AdaptiveColor{Light: "#0550ae", Dark: "#4dabf7"},
		Selection: lipgloss.AdaptiveColor{Light: "#0550ae", Dark: "#4dabf7"},
		Lanes:     defaultLanes,
	}
}

// forestTheme 林间绿：自然、安静。
func forestTheme() Theme {
	return Theme{
		Name:      "forest",
		Fg:        lipgloss.AdaptiveColor{Light: "#00332c", Dark: "#eafff7"},
		Dim:       lipgloss.AdaptiveColor{Light: "#526d64", Dark: "#9bc4b8"},
		Accent:    lipgloss.AdaptiveColor{Light: "#b35c00", Dark: "#f9bc60"},
		Good:      lipgloss.AdaptiveColor{Light: "#1a7f37", Dark: "#2cb67d"},
		Warn:      lipgloss.AdaptiveColor{Light: "#7d4e00", Dark: "#ffd803"},
		Bad:       lipgloss.AdaptiveColor{Light: "#b00020", Dark: "#e16162"},
		Magenta:   lipgloss.AdaptiveColor{Light: "#6f42c1", Dark: "#b197fc"},
		Cyan:      lipgloss.AdaptiveColor{Light: "#0b7285", Dark: "#66d9e8"},
		Border:    lipgloss.AdaptiveColor{Light: "#2b8a3e", Dark: "#52b788"},
		Selection: lipgloss.AdaptiveColor{Light: "#2b8a3e", Dark: "#f9bc60"},
		Lanes:     defaultLanes,
	}
}

// fireTheme 焰火橙：热烈、醒目。
func fireTheme() Theme {
	return Theme{
		Name:      "fire",
		Fg:        lipgloss.AdaptiveColor{Light: "#2b1a0f", Dark: "#fff4e6"},
		Dim:       lipgloss.AdaptiveColor{Light: "#6d5a4e", Dark: "#a7a9be"},
		Accent:    lipgloss.AdaptiveColor{Light: "#b35c00", Dark: "#ff8906"},
		Good:      lipgloss.AdaptiveColor{Light: "#1a7f37", Dark: "#2cb67d"},
		Warn:      lipgloss.AdaptiveColor{Light: "#7d4e00", Dark: "#fde24f"},
		Bad:       lipgloss.AdaptiveColor{Light: "#b00020", Dark: "#f25f4c"},
		Magenta:   lipgloss.AdaptiveColor{Light: "#a61e4d", Dark: "#e53170"},
		Cyan:      lipgloss.AdaptiveColor{Light: "#0c8599", Dark: "#ffb86c"},
		Border:    lipgloss.AdaptiveColor{Light: "#b35c00", Dark: "#ff8906"},
		Selection: lipgloss.AdaptiveColor{Light: "#b35c00", Dark: "#ff8906"},
		Lanes:     defaultLanes,
	}
}

// roseTheme 暖玫瑰：柔和、温暖。
func roseTheme() Theme {
	return Theme{
		Name:      "rose",
		Fg:        lipgloss.AdaptiveColor{Light: "#33272a", Dark: "#fff1f5"},
		Dim:       lipgloss.AdaptiveColor{Light: "#77646a", Dark: "#c9ada7"},
		Accent:    lipgloss.AdaptiveColor{Light: "#a61e4d", Dark: "#ff8ba7"},
		Good:      lipgloss.AdaptiveColor{Light: "#1a7f37", Dark: "#8ce99a"},
		Warn:      lipgloss.AdaptiveColor{Light: "#9a6700", Dark: "#ffd6a5"},
		Bad:       lipgloss.AdaptiveColor{Light: "#b00020", Dark: "#ff6b6b"},
		Magenta:   lipgloss.AdaptiveColor{Light: "#5f3dc4", Dark: "#d0bfff"},
		Cyan:      lipgloss.AdaptiveColor{Light: "#0c8599", Dark: "#99e2ff"},
		Border:    lipgloss.AdaptiveColor{Light: "#a61e4d", Dark: "#ff8ba7"},
		Selection: lipgloss.AdaptiveColor{Light: "#a61e4d", Dark: "#ff8ba7"},
		Lanes:     defaultLanes,
	}
}

func builtinTheme(name string) (Theme, bool) {
	switch name {
	case "light":
		return lightTheme(), true
	case "ocean":
		return oceanTheme(), true
	case "forest":
		return forestTheme(), true
	case "fire":
		return fireTheme(), true
	case "rose":
		return roseTheme(), true
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
	// Lanes 是共享切片，覆盖自定义色前必须深拷贝，避免污染全局调色板
	t.Lanes = append([]lipgloss.AdaptiveColor(nil), t.Lanes...)
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
	for i := 0; i < 20; i++ {
		key := "lane" + strconv.Itoa(i)
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
