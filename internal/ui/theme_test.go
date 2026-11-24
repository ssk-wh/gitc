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
	a.cycleTheme()
	if a.themeName != "light" {
		t.Errorf("切换到 %s, 期望 light", a.themeName)
	}
	a.cycleTheme()
	if a.themeName != "high-contrast" {
		t.Errorf("切换到 %s, 期望 high-contrast", a.themeName)
	}
	a.cycleTheme()
	if a.themeName != "default" {
		t.Errorf("回到 %s, 期望 default", a.themeName)
	}
	if plainView(a) == "" {
		t.Error("切换主题后视图为空")
	}
}
