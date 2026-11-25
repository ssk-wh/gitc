// Package update 实现 gitc 的自我更新：检测最新 Release、下载并替换当前可执行文件。
package update

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io/fs"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"regexp"
	"runtime"
	"strings"
	"time"
)

const (
	defaultRepo   = "ssk-wh/gitc"
	defaultMirror = "https://gh-proxy.com/"
	timeout       = 5 * time.Minute
)

// Options 是更新参数。
type Options struct {
	Repo     string
	Current  string // 当前版本（Release 构建注入；空表示本地构建）
	GOOS     string
	GOARCH   string
	ExePath  string
	Mirror   string
	BaseURL  string // 默认 https://github.com，测试可覆盖
	APIBase  string // 默认 https://api.github.com
	Client   *http.Client
	Out      io.Writer
	NowStamp string // 旧文件后缀，测试可固定
}

func (o *Options) fill() {
	if o.Repo == "" {
		o.Repo = defaultRepo
	}
	if o.GOOS == "" {
		o.GOOS = runtime.GOOS
	}
	if o.GOARCH == "" {
		o.GOARCH = runtime.GOARCH
	}
	if o.Mirror == "" {
		o.Mirror = defaultMirror
	}
	if o.BaseURL == "" {
		o.BaseURL = "https://github.com"
	}
	if o.APIBase == "" {
		o.APIBase = "https://api.github.com"
	}
	if o.Client == nil {
		o.Client = &http.Client{Timeout: timeout}
	}
	if o.Out == nil {
		o.Out = os.Stdout
	}
	if o.NowStamp == "" {
		o.NowStamp = fmt.Sprintf("%d", time.Now().UnixNano())
	}
}

// Run 检测并安装最新版本。已是最新时直接返回。
func Run(ctx context.Context, opts Options) error {
	opts.fill()
	if opts.ExePath == "" {
		exe, err := os.Executable()
		if err != nil {
			return fmt.Errorf("无法定位当前可执行文件: %w", err)
		}
		opts.ExePath = exe
	}

	tag, err := latestTag(ctx, opts)
	if err != nil {
		return err
	}
	if tag == "" {
		return errors.New("无法获取最新版本号")
	}
	if opts.Current != "" {
		fmt.Fprintf(opts.Out, "当前版本 %s，最新版本 %s\n", normalize(opts.Current), tag)
		if !isNewer(tag, opts.Current) {
			fmt.Fprintf(opts.Out, "已是最新版本 %s\n", tag)
			return nil
		}
	} else {
		fmt.Fprintf(opts.Out, "当前为本地构建（未注入版本），最新版本 %s\n", tag)
	}

	asset := assetName(opts.GOOS, opts.GOARCH)
	if asset == "" {
		return fmt.Errorf("不支持的平台: %s/%s", opts.GOOS, opts.GOARCH)
	}
	url := fmt.Sprintf("%s/%s/releases/download/%s/%s", opts.BaseURL, opts.Repo, tag, asset)
	fmt.Fprintf(opts.Out, "下载 %s …\n", asset)

	dir := filepath.Dir(opts.ExePath)
	tmp, err := os.CreateTemp(dir, ".gitc-update-*")
	if err != nil {
		if errors.Is(err, fs.ErrPermission) {
			return fmt.Errorf("安装目录 %s 不可写：请使用 `sudo %s --update`，或重新运行安装脚本安装到 ~/.local/bin", dir, filepath.Base(opts.ExePath))
		}
		return fmt.Errorf("无法在安装目录写入临时文件: %w", err)
	}
	tmpName := tmp.Name()
	tmp.Close()
	defer os.Remove(tmpName)
	data, err := fetchBytes(ctx, opts, url)
	if err != nil {
		return fmt.Errorf("下载失败: %w", err)
	}
	if err := os.WriteFile(tmpName, data, 0o755); err != nil {
		return err
	}

	sumURL := fmt.Sprintf("%s/%s/releases/download/%s/SHA256SUMS", opts.BaseURL, opts.Repo, tag)
	if err := verifySHA(ctx, opts, sumURL, asset, tmpName); err != nil {
		return err
	}
	fmt.Fprintln(opts.Out, "SHA256 校验通过")

	if err := os.Chmod(tmpName, 0o755); err != nil && runtime.GOOS != "windows" {
		return err
	}
	if err := replaceExecutable(opts.ExePath, tmpName, opts.NowStamp); err != nil {
		return fmt.Errorf("替换可执行文件失败: %w", err)
	}
	fmt.Fprintf(opts.Out, "已更新到 %s：%s\n重新启动 gitc 后生效。\n", tag, opts.ExePath)
	return nil
}

// assetName 返回当前平台对应的 Release 资产名。
func assetName(goos, goarch string) string {
	switch goarch {
	case "amd64", "arm64":
	default:
		return ""
	}
	switch goos {
	case "windows":
		return "gitc-windows-" + goarch + ".exe"
	case "linux", "darwin":
		return "gitc-" + goos + "-" + goarch
	}
	return ""
}

func normalize(v string) string {
	v = strings.TrimSpace(v)
	if v != "" && !strings.HasPrefix(v, "v") {
		return "v" + v
	}
	return v
}

// isNewer 判断 latest 是否比 current 新；current 为空表示本地构建，视为可更新。
func isNewer(latest, current string) bool {
	if current == "" {
		return true
	}
	lv, cv := parseVersion(latest), parseVersion(current)
	for i := 0; i < len(lv) || i < len(cv); i++ {
		var a, b int
		if i < len(lv) {
			a = lv[i]
		}
		if i < len(cv) {
			b = cv[i]
		}
		if a != b {
			return a > b
		}
	}
	return false
}

func parseVersion(v string) []int {
	v = strings.TrimPrefix(strings.TrimSpace(v), "v")
	if i := strings.IndexAny(v, "-+"); i >= 0 {
		v = v[:i]
	}
	parts := strings.Split(v, ".")
	out := make([]int, 0, len(parts))
	for _, p := range parts {
		n := 0
		for _, r := range p {
			if r < '0' || r > '9' {
				break
			}
			n = n*10 + int(r-'0')
		}
		out = append(out, n)
	}
	return out
}

// 只接受 GitHub tag 常见字符，避免把页面 JS 片段（如 '/releases/tag/' + ...）当成 tag。
var tagRe = regexp.MustCompile(`/releases/tag/([A-Za-z0-9][A-Za-z0-9._-]*)`)

// latestTag 通过 releases/latest 的 302 或发布页 HTML 获取 tag，API 仅作兜底。
func latestTag(ctx context.Context, opts Options) (string, error) {
	u := fmt.Sprintf("%s/%s/releases/latest", opts.BaseURL, opts.Repo)
	noRedirect := *opts.Client
	noRedirect.CheckRedirect = func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }
	var last error
	for _, candidate := range withMirror(u, opts.Mirror) {
		reqCtx, cancel := context.WithTimeout(ctx, 15*time.Second)
		req, _ := http.NewRequestWithContext(reqCtx, http.MethodGet, candidate, nil)
		req.Header.Set("User-Agent", "gitc-updater")
		resp, err := noRedirect.Do(req)
		if err != nil {
			cancel()
			last = err
			continue
		}
		if tag := tagFromURL(resp.Header.Get("Location")); tag != "" {
			resp.Body.Close()
			cancel()
			return tag, nil
		}
		// 镜像会直接返回发布页 HTML（200），从页面里提取 tag
		body, _ := io.ReadAll(io.LimitReader(resp.Body, 4<<20))
		resp.Body.Close()
		cancel()
		if m := tagRe.FindSubmatch(body); len(m) == 2 {
			return string(m[1]), nil
		}
		last = fmt.Errorf("HTTP %d 且未找到版本号", resp.StatusCode)
	}
	// Atom feed 兜底（页面可能被挑战页替代）
	atomURL := fmt.Sprintf("%s/%s/releases.atom", opts.BaseURL, opts.Repo)
	for _, candidate := range withMirror(atomURL, opts.Mirror) {
		reqCtx, cancel := context.WithTimeout(ctx, 15*time.Second)
		data, err := fetchBytes(reqCtx, opts, candidate)
		cancel()
		if err != nil {
			last = err
			continue
		}
		if m := tagRe.FindSubmatch(data); len(m) == 2 {
			return string(m[1]), nil
		}
	}
	// API 兜底
	api := fmt.Sprintf("%s/repos/%s/releases/latest", opts.APIBase, opts.Repo)
	req, _ := http.NewRequestWithContext(ctx, http.MethodGet, api, nil)
	req.Header.Set("User-Agent", "gitc-updater")
	resp, err := opts.Client.Do(req)
	if err != nil {
		if last != nil {
			return "", fmt.Errorf("查询最新版本失败: %w", last)
		}
		return "", fmt.Errorf("查询最新版本失败: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		if last != nil {
			return "", fmt.Errorf("查询最新版本失败: %w（API HTTP %d）", last, resp.StatusCode)
		}
		return "", fmt.Errorf("查询最新版本失败: HTTP %d", resp.StatusCode)
	}
	var body struct {
		Tag string `json:"tag_name"`
	}
	if err := json.NewDecoder(io.LimitReader(resp.Body, 1<<20)).Decode(&body); err != nil {
		return "", fmt.Errorf("解析最新版本失败: %w", err)
	}
	return body.Tag, nil
}

func tagFromURL(loc string) string {
	i := strings.Index(loc, "/tag/")
	if i < 0 {
		return ""
	}
	tag := loc[i+len("/tag/"):]
	if j := strings.IndexAny(tag, "/?#"); j >= 0 {
		tag = tag[:j]
	}
	return tag
}

func withMirror(u, mirror string) []string {
	out := []string{u}
	if mirror != "" {
		out = append(out, mirror+u)
	}
	return out
}

func fetchBytes(ctx context.Context, opts Options, url string) ([]byte, error) {
	var last error
	for _, candidate := range withMirror(url, opts.Mirror) {
		req, _ := http.NewRequestWithContext(ctx, http.MethodGet, candidate, nil)
		req.Header.Set("User-Agent", "gitc-updater")
		resp, err := opts.Client.Do(req)
		if err != nil {
			last = err
			continue
		}
		if resp.StatusCode != http.StatusOK {
			resp.Body.Close()
			last = fmt.Errorf("HTTP %d", resp.StatusCode)
			continue
		}
		data, err := io.ReadAll(io.LimitReader(resp.Body, 200<<20))
		resp.Body.Close()
		if err == nil {
			return data, nil
		}
		last = err
	}
	if last == nil {
		last = errors.New("下载失败")
	}
	return nil, last
}

func verifySHA(ctx context.Context, opts Options, sumURL, asset, file string) error {
	data, err := fetchBytes(ctx, opts, sumURL)
	if err != nil {
		return fmt.Errorf("下载 SHA256SUMS 失败: %w", err)
	}
	want := ""
	for _, line := range strings.Split(string(data), "\n") {
		fields := strings.Fields(line)
		if len(fields) == 2 && fields[1] == asset {
			want = strings.ToLower(fields[0])
			break
		}
	}
	if want == "" {
		return fmt.Errorf("SHA256SUMS 中缺少 %s", asset)
	}
	f, err := os.Open(file)
	if err != nil {
		return err
	}
	defer f.Close()
	h := sha256.New()
	if _, err := io.Copy(h, f); err != nil {
		return err
	}
	got := hex.EncodeToString(h.Sum(nil))
	if got != want {
		return errors.New("SHA256 校验失败")
	}
	return nil
}

// replaceExecutable 用新文件替换当前可执行文件。Windows 允许重命名正在运行的 exe。
func replaceExecutable(exe, newFile, stamp string) error {
	old := exe + ".old-" + stamp
	_ = os.Remove(old)
	if err := os.Rename(exe, old); err != nil {
		return err
	}
	if err := os.Rename(newFile, exe); err != nil {
		_ = os.Rename(old, exe)
		return err
	}
	// 旧文件可能因仍被运行中的进程占用而无法立即删除，忽略即可。
	_ = os.Remove(old)
	return nil
}
