package ui

import "testing"

func TestResolveThemeOverrides(t *testing.T) {
	th, bad := resolveTheme("default", map[string]string{
		"accent": "#ff0000",
		"lane0":  "#00ff00",
		"bad":    "not-a-color",
	})
	if len(bad) != 1 || bad[0] != "bad" {
		t.Fatalf("非法颜色列表 = %v", bad)
	}
	if th.Accent.Dark != "#ff0000" || th.Accent.Light != "#ff0000" {
		t.Errorf("accent 覆盖失败: %+v", th.Accent)
	}
	if th.Lanes[0].Dark != "#00ff00" {
		t.Errorf("lane0 覆盖失败: %+v", th.Lanes[0])
	}
}

func TestParseHexColor(t *testing.T) {
	if _, ok := parseHexColor("#a1B2c3"); !ok {
		t.Error("合法十六进制应通过")
	}
	for _, s := range []string{"a1b2c3", "#12345", "#gggggg", ""} {
		if _, ok := parseHexColor(s); ok {
			t.Errorf("%q 不应通过", s)
		}
	}
}

func TestCycleTheme(t *testing.T) {
	a, _ := newFakeApp(t)
	if a.themeName != "default" {
		t.Fatalf("初始主题 = %s", a.themeName)
	}
	for _, want := range ThemeOrder[1:] {
		a.cycleTheme()
		if a.themeName != want {
			t.Fatalf("切换到 %s, 期望 %s", a.themeName, want)
		}
	}
	a.cycleTheme()
	if a.themeName != ThemeOrder[0] {
		t.Errorf("回到 %s, 期望 %s", a.themeName, ThemeOrder[0])
	}
	if plainView(a) == "" {
		t.Error("切换主题后视图为空")
	}
}

// TestBuiltinThemes 每套内置主题都应可用且有 20 色分支调色板。
func TestBuiltinThemes(t *testing.T) {
	for _, name := range ThemeOrder {
		th, ok := builtinTheme(name)
		if !ok {
			t.Fatalf("内置主题 %q 未注册", name)
		}
		if th.Name != name {
			t.Errorf("%q 主题 Name = %q", name, th.Name)
		}
		if len(th.Lanes) != 20 {
			t.Errorf("%q 调色板 %d 色，期望 20", name, len(th.Lanes))
		}
	}
}

// TestThemeLaneOverrideIsolated 自定义 lane 颜色不能污染其他主题。
func TestThemeLaneOverrideIsolated(t *testing.T) {
	t.Cleanup(func() { applyTheme(defaultTheme()) })
	custom, bad := resolveTheme("default", map[string]string{"lane0": "#ff0000"})
	if len(bad) != 0 {
		t.Fatalf("合法颜色被拒: %v", bad)
	}
	if custom.Lanes[0].Dark != "#ff0000" {
		t.Fatalf("自定义 lane0 未生效: %+v", custom.Lanes[0])
	}
	ocean, _ := resolveTheme("ocean", nil)
	if ocean.Lanes[0].Dark == "#ff0000" {
		t.Fatal("lane 自定义色污染了其他主题")
	}
	if defaultLanes[0].Dark == "#ff0000" {
		t.Fatal("全局默认调色板被改写")
	}
}
