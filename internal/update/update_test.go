package update

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

func TestIsNewer(t *testing.T) {
	cases := []struct {
		latest, current string
		want            bool
	}{
		{"v0.1.5", "v0.1.4", true},
		{"v0.1.5", "0.1.5", false},
		{"v0.2.0", "v0.1.9", true},
		{"v0.1.9", "v0.2.0", false},
		{"v0.1.5", "", true},
		{"v0.1.5-rc1", "v0.1.5", false},
	}
	for _, c := range cases {
		if got := isNewer(c.latest, c.current); got != c.want {
			t.Errorf("isNewer(%q,%q)=%v, want %v", c.latest, c.current, got, c.want)
		}
	}
}

func TestAssetName(t *testing.T) {
	if got := assetName("windows", "amd64"); got != "gitc-windows-amd64.exe" {
		t.Errorf("windows asset = %q", got)
	}
	if got := assetName("linux", "arm64"); got != "gitc-linux-arm64" {
		t.Errorf("linux asset = %q", got)
	}
	if got := assetName("plan9", "amd64"); got != "" {
		t.Errorf("unsupported os should be empty, got %q", got)
	}
	if got := assetName("linux", "386"); got != "" {
		t.Errorf("unsupported arch should be empty, got %q", got)
	}
}

func TestTagFromURL(t *testing.T) {
	if got := tagFromURL("https://github.com/ssk-wh/gitc/releases/tag/v1.2.3"); got != "v1.2.3" {
		t.Errorf("tag = %q", got)
	}
	if got := tagFromURL("https://github.com/x/y/releases/latest"); got != "" {
		t.Errorf("no tag expected, got %q", got)
	}
}

func TestRunUpdatesExecutable(t *testing.T) {
	content := []byte("new-binary-content")
	asset := assetName(runtime.GOOS, runtime.GOARCH)
	if asset == "" {
		t.Skip("unsupported test platform")
	}
	sum := sha256.Sum256(content)
	var downloads int
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.URL.Path == "/ssk-wh/gitc/releases/latest":
			http.Redirect(w, r, "/ssk-wh/gitc/releases/tag/v9.9.9", http.StatusFound)
		case strings.HasSuffix(r.URL.Path, "/"+asset):
			downloads++
			w.Write(content)
		case strings.HasSuffix(r.URL.Path, "/SHA256SUMS"):
			fmt.Fprintf(w, "%s  %s\n", hex.EncodeToString(sum[:]), asset)
		default:
			http.NotFound(w, r)
		}
	}))
	defer srv.Close()

	dir := t.TempDir()
	exe := filepath.Join(dir, "gitc")
	if runtime.GOOS == "windows" {
		exe += ".exe"
	}
	if err := os.WriteFile(exe, []byte("old-binary"), 0o755); err != nil {
		t.Fatal(err)
	}
	var out bytes.Buffer
	err := Run(context.Background(), Options{
		Repo:     "ssk-wh/gitc",
		Current:  "v0.1.0",
		ExePath:  exe,
		BaseURL:  srv.URL,
		APIBase:  srv.URL,
		Client:   srv.Client(),
		Out:      &out,
		NowStamp: "test",
	})
	if err != nil {
		t.Fatalf("Run: %v\n%s", err, out.String())
	}
	got, err := os.ReadFile(exe)
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != string(content) {
		t.Fatalf("可执行文件未替换: %q", got)
	}
	if downloads != 1 {
		t.Errorf("下载次数 = %d，期望 1", downloads)
	}
	if !strings.Contains(out.String(), "已更新到 v9.9.9") || !strings.Contains(out.String(), "SHA256 校验通过") {
		t.Errorf("输出缺少更新信息:\n%s", out.String())
	}
}

func TestRunAlreadyLatest(t *testing.T) {
	var hits int
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/ssk-wh/gitc/releases/latest" {
			http.Redirect(w, r, "/ssk-wh/gitc/releases/tag/v9.9.9", http.StatusFound)
			return
		}
		hits++
		http.NotFound(w, r)
	}))
	defer srv.Close()
	dir := t.TempDir()
	exe := filepath.Join(dir, "gitc")
	if err := os.WriteFile(exe, []byte("keep"), 0o755); err != nil {
		t.Fatal(err)
	}
	var out bytes.Buffer
	if err := Run(context.Background(), Options{
		Repo: "ssk-wh/gitc", Current: "v9.9.9", ExePath: exe,
		BaseURL: srv.URL, APIBase: srv.URL, Client: srv.Client(), Out: &out,
	}); err != nil {
		t.Fatal(err)
	}
	if hits != 0 {
		t.Errorf("已是最新时不应下载，实际请求 %d 次", hits)
	}
	if got, _ := os.ReadFile(exe); string(got) != "keep" {
		t.Error("已是最新时不应修改可执行文件")
	}
	if !strings.Contains(out.String(), "已是最新版本") {
		t.Errorf("输出: %s", out.String())
	}
}

func TestRunDevVersionUpdates(t *testing.T) {
	content := []byte("dev-update")
	asset := assetName(runtime.GOOS, runtime.GOARCH)
	sum := sha256.Sum256(content)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.URL.Path == "/ssk-wh/gitc/releases/latest":
			http.Redirect(w, r, "/ssk-wh/gitc/releases/tag/v2.0.0", http.StatusFound)
		case strings.HasSuffix(r.URL.Path, "/"+asset):
			w.Write(content)
		case strings.HasSuffix(r.URL.Path, "/SHA256SUMS"):
			fmt.Fprintf(w, "%s  %s\n", hex.EncodeToString(sum[:]), asset)
		default:
			http.NotFound(w, r)
		}
	}))
	defer srv.Close()
	exe := filepath.Join(t.TempDir(), "gitc")
	if runtime.GOOS == "windows" {
		exe += ".exe"
	}
	os.WriteFile(exe, []byte("dev"), 0o755)
	var out bytes.Buffer
	if err := Run(context.Background(), Options{
		Repo: "ssk-wh/gitc", Current: "", ExePath: exe,
		BaseURL: srv.URL, APIBase: srv.URL, Client: srv.Client(), Out: &out,
	}); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out.String(), "本地构建") {
		t.Errorf("输出: %s", out.String())
	}
	if got, _ := os.ReadFile(exe); string(got) != "dev-update" {
		t.Error("本地构建应可更新到最新版")
	}
}

func TestLatestTagFromHTML(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/ssk-wh/gitc/releases/latest" {
			w.Header().Set("Content-Type", "text/html")
			fmt.Fprint(w, `<html><link rel="canonical" href="https://github.com/ssk-wh/gitc/releases/tag/v3.1.4"></html>`)
			return
		}
		http.NotFound(w, r)
	}))
	defer srv.Close()
	tag, err := latestTag(context.Background(), Options{
		Repo: "ssk-wh/gitc", BaseURL: srv.URL, APIBase: srv.URL,
		Client: srv.Client(), Mirror: "",
	})
	if err != nil {
		t.Fatal(err)
	}
	if tag != "v3.1.4" {
		t.Fatalf("tag = %q, 期望 v3.1.4", tag)
	}
}
