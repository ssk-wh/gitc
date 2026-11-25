package workspace

import (
	"os"
	"path/filepath"
	"testing"
)

func TestSaveLoadRoundTrip(t *testing.T) {
	root := t.TempDir()
	paths := []string{"/repo/a", "/repo/b"}
	if err := Save(root, paths); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(root, Name)); err != nil {
		t.Fatalf("workspace 文件未创建: %v", err)
	}
	f, err := Load(root)
	if err != nil {
		t.Fatal(err)
	}
	if f.Version != 1 || len(f.Projects) != 2 || f.Projects[1] != "/repo/b" {
		t.Fatalf("workspace = %+v", f)
	}
}

func TestLoadMissing(t *testing.T) {
	f, err := Load(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	if len(f.Projects) != 0 {
		t.Fatalf("缺失时应为空: %+v", f)
	}
}
