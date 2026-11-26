package config

import (
	"os"
	"path/filepath"
	"testing"
)

// TestSaveLoadRoundTrip 验证主题/颜色/布局配置可写入并原样读回。
func TestSaveLoadRoundTrip(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("AppData", dir)
	t.Setenv("XDG_CONFIG_HOME", dir)
	t.Setenv("HOME", dir)

	want := File{
		Theme:       "light",
		Colors:      map[string]string{"accent": "#ff0000", "lane0": "#00ff00"},
		LeftWidth:   30,
		ProjectH:    4,
		DrawerLeft:  24,
		RightTop:    12,
		LeftTop:     10,
		LeftMid:     9,
		CommandH:    5,
		Projects:    []string{"/repo/a", "/repo/b"},
		LastProject: "/repo/b",
	}
	if err := Save(want); err != nil {
		t.Fatalf("Save: %v", err)
	}
	got := Load()
	if got.Theme != want.Theme {
		t.Errorf("Theme = %q, 期望 %q", got.Theme, want.Theme)
	}
	if got.Colors["accent"] != "#ff0000" || got.Colors["lane0"] != "#00ff00" {
		t.Errorf("Colors = %+v", got.Colors)
	}
	if got.LeftWidth != 30 || got.ProjectH != 4 || got.DrawerLeft != 24 || got.RightTop != 12 ||
		got.LeftTop != 10 || got.LeftMid != 9 || got.CommandH != 5 {
		t.Errorf("布局字段 = %+v", got)
	}
	if len(got.Projects) != 2 || got.Projects[1] != "/repo/b" || got.LastProject != "/repo/b" {
		t.Errorf("项目字段 = %+v last=%q", got.Projects, got.LastProject)
	}
}

// TestLoadMissingOrCorrupt 验证缺失/损坏配置返回零值而不报错。
func TestLoadMissingOrCorrupt(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("AppData", dir)
	t.Setenv("XDG_CONFIG_HOME", dir)
	t.Setenv("HOME", dir)
	if got := Load(); got.Theme != "" || got.LeftWidth != 0 {
		t.Errorf("缺失时应返回零值: %+v", got)
	}
	p, err := Path()
	if err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(p, []byte("{ not json"), 0o644); err != nil {
		t.Fatal(err)
	}
	if got := Load(); got.Theme != "" || got.Colors != nil {
		t.Errorf("损坏配置应返回零值: %+v", got)
	}
}
