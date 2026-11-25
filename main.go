package main

import (
	"context"
	"flag"
	"fmt"
	"os"
	"strings"
	"time"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/ssk-wh/gitc/internal/config"
	"github.com/ssk-wh/gitc/internal/git"
	"github.com/ssk-wh/gitc/internal/i18n"
	"github.com/ssk-wh/gitc/internal/ui"
)

// version 由 release 构建通过 -ldflags "-X main.version=vX.Y.Z" 注入；
// 本地构建为空，界面不显示版本号。
var version = ""

func displayVersion() string {
	if version == "" {
		return "dev"
	}
	return version
}

func main() {
	dir := flag.String("C", ".", "仓库目录")
	lang := flag.String("lang", "", "界面语言（zh/en）")
	theme := flag.String("theme", "", "主题：default/light/high-contrast")
	showVersion := flag.Bool("version", false, "显示版本")
	check := flag.Bool("check", false, "输出仓库摘要后退出（headless）")
	flag.Usage = usage
	flag.Parse()

	if *showVersion {
		fmt.Println("gitc", displayVersion())
		return
	}
	i18n.Set(i18n.Detect())
	if *lang != "" {
		i18n.Set(*lang)
	}

	repo, err := git.Open(*dir)
	if err != nil {
		fmt.Fprintln(os.Stderr, "gitc:", err)
		os.Exit(2)
	}
	if *check {
		runCheck(repo)
		return
	}
	if fi, err := os.Stdout.Stat(); err != nil || fi.Mode()&os.ModeCharDevice == 0 {
		fmt.Fprintln(os.Stderr, i18n.S(
			"gitc 需要在交互式终端中运行；无终端环境可用 gitc --check",
			"gitc needs an interactive terminal; use gitc --check in non-TTY environments"))
		os.Exit(2)
	}

	ui.SetVersion(version)
	app := ui.New(repo)
	repo.SetCommandHook(app.LogCommand)
	cfg := config.Load()
	if *theme != "" {
		cfg.Theme = *theme
	}
	app.SetConfig(cfg)
	p := tea.NewProgram(app, tea.WithAltScreen(), tea.WithMouseCellMotion())
	go func() {
		for m := range app.Msgs() {
			p.Send(m)
		}
	}()
	defer func() {
		if r := recover(); r != nil {
			p.Kill()
			fmt.Fprintf(os.Stderr, "gitc 崩溃: %v\n", r)
			os.Exit(1)
		}
	}()
	if _, err := p.Run(); err != nil {
		fmt.Fprintln(os.Stderr, "gitc:", err)
		os.Exit(1)
	}
}

func usage() {
	fmt.Fprintf(os.Stderr, `gitc %s — 终端交互式 Git 客户端

用法: gitc [选项]

选项:
  -C <dir>       指定仓库目录（默认当前目录）
  --lang <l>     界面语言：zh / en（默认跟随 LANG）
  --theme <名>   主题：default / light / ocean / forest / fire / rose / high-contrast（覆盖配置）
  --check        输出仓库摘要后退出（供 CI/脚本使用）
  --version      显示版本
`, displayVersion())
}

// runCheck 输出仓库摘要，便于无终端环境验证。
func runCheck(repo *git.Repo) {
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	st, err := repo.Status(ctx, true)
	if err != nil {
		fmt.Fprintln(os.Stderr, "gitc:", err)
		os.Exit(2)
	}
	refs, _ := repo.Refs(ctx)
	count := "?"
	if out, err := repo.RunGitArgs(ctx, []string{"rev-list", "--count", "--all"}); err == nil {
		count = strings.TrimSpace(out)
	}
	branch := st.Branch
	if st.Detached {
		branch = "(detached)"
	}
	fmt.Printf("repo:     %s (%s)\n", repo.Name(), repo.Root)
	fmt.Printf("branch:   %s", branch)
	if st.Upstream != "" {
		fmt.Printf(" -> %s", st.Upstream)
	}
	if st.Ahead > 0 || st.Behind > 0 {
		fmt.Printf(" (ahead %d, behind %d)", st.Ahead, st.Behind)
	}
	fmt.Println()
	fmt.Printf("refs:     %d\n", len(refs))
	fmt.Printf("commits:  %s\n", count)
	fmt.Printf("changes:  %d (staged %d, unstaged %d, untracked %d)\n", st.Total(), len(st.Staged), len(st.Unstaged), len(st.Untracked))
}
