package ui

import (
	"context"
	"fmt"
	"strings"
	"time"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/ssk-wh/gitc/internal/git"
	"github.com/ssk-wh/gitc/internal/i18n"
	"github.com/ssk-wh/gitc/internal/sanitize"
)

// openContextMenu 为当前面板的选中项弹出上下文菜单（键盘 b / 鼠标右键）。
func (a *App) openContextMenu() tea.Cmd {
	switch a.pane {
	case PaneBranches:
		it := a.selectedSideItem()
		if it == nil {
			return nil
		}
		if it.kind == sideTag {
			a.tagMenuName = it.ref.Name
			a.choose(a.branchMenuTitle(it), a.tagMenuChoices(it.ref, a.remoteTagCache[it.ref.Name]))
			a.tagMenuModal = a.modal
			return a.checkRemoteTagCmd(it.ref.Name)
		}
		a.choose(a.branchMenuTitle(it), a.branchMenuChoices(it))
	case PaneCommits:
		if a.onDirtyRow() {
			a.choose(i18n.S("未提交的改动", "uncommitted changes"), a.uncommittedMenuChoices())
			return nil
		}
		c := a.selectedCommit()
		if c == nil {
			return nil
		}
		a.choose(fmt.Sprintf(i18n.S("提交 %s", "commit %s"), c.Short), a.commitMenuChoices(c))
	case PaneChanges:
		n := a.selectedChangeNode()
		if n == nil {
			a.choose(i18n.S("工作区改动（全部）", "working changes (all)"), a.uncommittedMenuChoices())
			return nil
		}
		if n.dir {
			a.choose(fmt.Sprintf(i18n.S("目录 %s", "directory %s"), sanitize.Line(n.path)), a.dirMenuChoices(n, true))
			return nil
		}
		if f := a.selectedChangeEntry(); f != nil {
			a.choose(fmt.Sprintf(i18n.S("文件 %s", "file %s"), sanitize.Line(f.path)), a.fileMenuChoices(*f, true))
		}
		return nil
	case PaneFiles:
		n := a.selectedFileNode()
		if n == nil {
			a.openFilesHeaderMenu()
			return nil
		}
		if n.dir {
			a.choose(fmt.Sprintf(i18n.S("目录 %s", "directory %s"), sanitize.Line(n.path)), a.dirMenuChoices(n, a.filesOwner == "work"))
			return nil
		}
		a.choose(fmt.Sprintf(i18n.S("文件 %s", "file %s"), sanitize.Line(n.path)), a.fileNodeMenuChoices(n, a.filesOwner == "work"))
	}
	return nil
}

// remoteRefForBranch 返回本地分支对应的远端跟踪引用。
func (a *App) remoteRefForBranch(ref git.Ref) (git.Ref, bool) {
	name := ref.Upstream
	if name == "" {
		name = "origin/" + ref.Name
	}
	for _, r := range a.refs {
		if r.Kind == git.RefRemote && r.Name == name {
			return r, true
		}
	}
	return git.Ref{}, false
}

// tagMenuChoices 生成标签菜单；pushed 表示 origin 已存在该标签。
func (a *App) tagMenuChoices(ref git.Ref, pushed bool) []choice {
	out := []choice{
		{label: i18n.S("检出该标签", "checkout tag"), action: func(a *App) tea.Cmd {
			return a.runOp("checkout "+ref.Name, func(ctx context.Context) (string, error) {
				return a.svc.Checkout(ctx, ref.Name)
			})
		}},
	}
	if !pushed {
		out = append(out, choice{label: i18n.S("推送标签到 origin", "push tag"), action: func(a *App) tea.Cmd {
			return a.pushTag(ref)
		}})
	}
	out = append(out, choice{label: i18n.S("删除标签", "delete tag"), action: func(a *App) tea.Cmd {
		return a.sidebarDelete(&sideItem{kind: sideTag, ref: ref})
	}})
	if pushed {
		name := ref.Name
		out = append(out, choice{label: i18n.S("删除远端标签（origin）", "delete remote tag (origin)"), action: func(a *App) tea.Cmd {
			a.confirm(i18n.S("删除远端标签 "+name, "delete remote tag "+name),
				i18n.S("会从 origin 删除该标签，不可恢复。", "Deletes the tag from origin; this cannot be undone."),
				"git push origin :refs/tags/"+name, true, func(a *App) tea.Cmd {
					return a.runOp("delete remote tag "+name, func(ctx context.Context) (string, error) {
						return a.svc.RunGitArgs(ctx, []string{"push", "origin", ":refs/tags/" + name})
					})
				})
			return nil
		}})
	}
	out = append(out, choice{label: i18n.S("复制名称", "copy name"), action: func(a *App) tea.Cmd {
		return a.copyToClipboard(ref.Name)
	}})
	return out
}

// remoteTagMsg 是远端标签存在性检查结果。
type remoteTagMsg struct {
	name   string
	exists bool
	err    error
}

// checkRemoteTagCmd 异步检查 origin 上是否已有该标签，菜单已立即展示。
func (a *App) checkRemoteTagCmd(name string) tea.Cmd {
	svc := a.svc
	return func() tea.Msg {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		out, err := svc.RunGitArgs(ctx, []string{"ls-remote", "--tags", "--refs", "origin", "refs/tags/" + name})
		return remoteTagMsg{name: name, exists: strings.TrimSpace(out) != "", err: err}
	}
}

func (a *App) branchMenuTitle(it *sideItem) string {
	if it.kind == sideStash {
		return i18n.S("Stash "+it.stash.Selector, "stash "+it.stash.Selector)
	}
	kind := map[sideKind]string{
		sideBranch: i18n.S("本地分支", "local branch"),
		sideRemote: i18n.S("远端分支", "remote branch"),
		sideTag:    i18n.S("标签", "tag"),
	}[it.kind]
	return kind + " " + it.ref.Name
}

func (a *App) branchMenuChoices(it *sideItem) []choice {
	ref := it.ref
	switch it.kind {
	case sideBranch:
		out := []choice{
			{label: i18n.S("检出该分支", "checkout"), action: func(a *App) tea.Cmd {
				if ref.Head {
					a.setToast(i18n.S("已在当前分支", "already on this branch"), false)
					return nil
				}
				return a.runOp("checkout "+ref.Name, func(ctx context.Context) (string, error) {
					return a.svc.Checkout(ctx, ref.Name)
				})
			}},
			{label: i18n.S("基于该分支新建并检出", "new branch here"), action: func(a *App) tea.Cmd {
				start := ref.Name
				a.newBranchPrompt(start)
				return nil
			}},
			{label: i18n.S("重命名", "rename"), action: func(a *App) tea.Cmd {
				a.renameBranchPrompt(ref)
				return nil
			}},
			{label: i18n.S("删除（安全）", "delete (safe)"), action: func(a *App) tea.Cmd {
				return a.sidebarDelete(&sideItem{kind: sideBranch, ref: ref})
			}},
			{label: i18n.S("强制删除", "force delete"), action: func(a *App) tea.Cmd {
				a.forceDeleteBranch(ref)
				return nil
			}},
			{label: i18n.S("合并到当前分支", "merge into current"), action: func(a *App) tea.Cmd {
				a.mergePrompt(ref)
				return nil
			}},
			{label: i18n.S("当前分支 rebase 到该分支", "rebase current onto this"), action: func(a *App) tea.Cmd {
				a.rebasePrompt(ref)
				return nil
			}},
			{label: i18n.S("只看该分支的提交", "show commits of this branch"), action: func(a *App) tea.Cmd {
				return a.setBranchFilter(ref.Name, "", ref.Name)
			}},
			{label: i18n.S("设为当前分支上游", "set as upstream"), action: func(a *App) tea.Cmd {
				cur := a.status.Branch
				if cur == "" {
					a.setToast(i18n.S("当前为分离 HEAD，无法设置上游", "detached HEAD; cannot set upstream"), true)
					return nil
				}
				def := "origin/" + ref.Name
				a.prompt(i18n.S("上游引用（默认 "+def+"）", "upstream (default "+def+")"), def, def, func(a *App, v string) tea.Cmd {
					v = strings.TrimSpace(v)
					if v == "" {
						return nil
					}
					return a.runOp("set-upstream "+v, func(ctx context.Context) (string, error) {
						return a.svc.SetUpstream(ctx, cur, v)
					})
				})
				return nil
			}},
			{label: i18n.S("复制分支名", "copy branch name"), action: func(a *App) tea.Cmd {
				return a.copyToClipboard(ref.Name)
			}},
		}
		remote, hasRemote := a.remoteRefForBranch(ref)
		if !hasRemote {
			out = append(out, choice{label: i18n.S("推送该分支到 origin", "push to origin"), action: func(a *App) tea.Cmd {
				name := ref.Name
				return a.runOp("push "+name, func(ctx context.Context) (string, error) {
					return a.svc.RunGitArgs(ctx, []string{"push", "origin", name})
				})
			}})
		} else if remote.Target != ref.Target {
			out = append(out,
				choice{label: i18n.S("推送该分支到 origin", "push to origin"), action: func(a *App) tea.Cmd {
					name := ref.Name
					return a.runOp("push "+name, func(ctx context.Context) (string, error) {
						return a.svc.RunGitArgs(ctx, []string{"push", "origin", name})
					})
				}},
				choice{label: i18n.S("强制推送（--force-with-lease）", "force push (with-lease)"), action: func(a *App) tea.Cmd {
					name := ref.Name
					a.confirm(i18n.S("强制推送 "+name, "force push "+name),
						i18n.S("将覆盖远端分支，使用 --force-with-lease 保护。", "Overwrites remote branch using --force-with-lease."),
						"git push --force-with-lease origin "+name, true, func(a *App) tea.Cmd {
							return a.runOp("push --force-with-lease "+name, func(ctx context.Context) (string, error) {
								return a.svc.RunGitArgs(ctx, []string{"push", "--force-with-lease", "origin", name})
							})
						})
					return nil
				}},
			)
		}
		return out
	case sideRemote:
		local := remoteLocalName(ref.Name)
		return []choice{
			{label: i18n.S("检出/创建跟踪分支 "+local, "checkout tracking "+local), action: func(a *App) tea.Cmd {
				return a.sidebarEnter(&sideItem{kind: sideRemote, ref: ref})
			}},
			{label: i18n.S("合并到当前分支", "merge into current"), action: func(a *App) tea.Cmd {
				a.mergePrompt(ref)
				return nil
			}},
			{label: i18n.S("当前分支 rebase 到该分支", "rebase current onto this"), action: func(a *App) tea.Cmd {
				a.rebasePrompt(ref)
				return nil
			}},
			{label: i18n.S("删除远端分支", "delete remote branch"), action: func(a *App) tea.Cmd {
				return a.sidebarDelete(&sideItem{kind: sideRemote, ref: ref})
			}},
			{label: i18n.S("复制名称", "copy name"), action: func(a *App) tea.Cmd {
				return a.copyToClipboard(ref.Name)
			}},
		}
	case sideTag:
		return a.tagMenuChoices(ref, false)
	case sideProject:
		var out []choice
		if it.project.Path != a.currentPath {
			out = append(out, choice{label: i18n.S("切换到该项目", "switch to project"), action: func(a *App) tea.Cmd {
				return a.switchProject(a.projectIndex(it.project.Path))
			}})
		}
		if it.project.Path != a.currentPath {
			out = append(out, choice{label: i18n.S("移除该项目", "remove project"), action: func(a *App) tea.Cmd {
				return a.removeProject(it.project)
			}})
		}
		out = append(out, choice{label: i18n.S("复制路径", "copy path"), action: func(a *App) tea.Cmd {
			return a.copyToClipboard(it.project.Path)
		}})
		return out
	case sideStash:
		st := it.stash
		return []choice{
			{label: i18n.S("查看内容", "view"), action: func(a *App) tea.Cmd {
				return a.openStashDiff(st)
			}},
			{label: i18n.S("应用（apply）", "apply"), action: func(a *App) tea.Cmd {
				return a.runOp("stash apply", func(ctx context.Context) (string, error) {
					return a.svc.StashApply(ctx, st.Index)
				})
			}},
			{label: i18n.S("弹出（pop）", "pop"), action: func(a *App) tea.Cmd {
				return a.runOp("stash pop", func(ctx context.Context) (string, error) {
					return a.svc.StashPop(ctx, st.Index)
				})
			}},
			{label: i18n.S("删除（drop）", "drop"), action: func(a *App) tea.Cmd {
				return a.stashAction(st, "drop")
			}},
		}
	}
	return nil
}

// commitMenuChoices 是提交上下文菜单。
func (a *App) commitMenuChoices(c *git.Commit) []choice {
	var out []choice
	if a.filter == "" && a.searchResults == nil {
		if n := a.commitSelectionCount(); n >= 2 {
			out = append(out, choice{label: fmt.Sprintf(i18n.S("合并 %d 个提交", "squash %d commits"), n), action: func(a *App) tea.Cmd {
				a.squashPrompt()
				return nil
			}})
		}
	}
	out = append(out, []choice{
		{label: i18n.S("查看提交详情", "view details"), action: func(a *App) tea.Cmd {
			return a.openCommitDetail(c.Hash)
		}},
		{label: i18n.S("编辑提交信息（改写历史）", "edit commit message"), action: func(a *App) tea.Cmd {
			return a.loadCommitMessageCmd(c.Hash)
		}},
		{label: i18n.S("检出此提交（分离 HEAD）", "checkout detached"), action: func(a *App) tea.Cmd {
			a.confirm(i18n.S("检出 "+c.Short, "checkout "+c.Short),
				i18n.S("将进入分离 HEAD 状态。", "Detached HEAD."), "git checkout "+c.Hash, false,
				func(a *App) tea.Cmd {
					return a.runOp("checkout "+c.Short, func(ctx context.Context) (string, error) {
						return a.svc.Checkout(ctx, c.Hash)
					})
				})
			return nil
		}},
		{label: i18n.S("在此提交新建分支并检出", "new branch here"), action: func(a *App) tea.Cmd {
			a.newBranchPrompt(c.Hash)
			return nil
		}},
		{label: i18n.S("打标签", "tag"), action: func(a *App) tea.Cmd {
			a.tagPrompt(c)
			return nil
		}},
		{label: "cherry-pick", action: func(a *App) tea.Cmd {
			a.cherryPickPrompt(c)
			return nil
		}},
		{label: "revert", action: func(a *App) tea.Cmd {
			a.revertPrompt(c)
			return nil
		}},
		{label: "reset", action: func(a *App) tea.Cmd {
			a.choose(i18n.S("reset 当前分支到 "+c.Short, "reset current to "+c.Short), []choice{
				{label: "soft", action: func(a *App) tea.Cmd { return a.doReset(c.Hash, git.ResetSoft) }},
				{label: "mixed", action: func(a *App) tea.Cmd { return a.doReset(c.Hash, git.ResetMixed) }},
				{label: "hard", action: func(a *App) tea.Cmd { return a.doReset(c.Hash, git.ResetHard) }},
			})
			return nil
		}},
		{label: i18n.S("复制完整 sha", "copy sha"), action: func(a *App) tea.Cmd {
			return a.copyToClipboard(c.Hash)
		}},
		{label: i18n.S("复制提交信息", "copy message"), action: func(a *App) tea.Cmd {
			return a.copyToClipboard(c.Subject)
		}},
	}...)
	return out
}

// fileNodeMenuChoices 根据节点下所有条目生成菜单：
// 同时存在已暂存/未暂存条目时，两个操作都提供，避免状态显示错误。
func (a *App) fileNodeMenuChoices(n *fileNode, working bool) []choice {
	if n == nil || !working {
		return nil
	}
	var hasStaged, hasUnstaged bool
	var pick fileEntry
	for _, e := range n.entries {
		if e.staged {
			hasStaged = true
		} else if !hasUnstaged {
			hasUnstaged = true
			pick = e
		}
	}
	if !hasUnstaged && len(n.entries) > 0 {
		pick = n.entries[0]
	}
	path := n.path
	var out []choice
	if hasUnstaged {
		out = append(out, choice{label: i18n.S("暂存该文件", "stage file"), action: func(a *App) tea.Cmd {
			return a.runOp("stage "+path, func(ctx context.Context) (string, error) {
				return a.svc.Stage(ctx, path)
			})
		}})
	}
	if hasStaged {
		out = append(out, choice{label: i18n.S("取消暂存", "unstage file"), action: func(a *App) tea.Cmd {
			return a.runOp("unstage "+path, func(ctx context.Context) (string, error) {
				return a.svc.Unstage(ctx, path)
			})
		}})
	}
	if len(n.entries) > 0 {
		out = append(out, choice{label: i18n.S("丢弃改动（危险）", "discard changes"), action: func(a *App) tea.Cmd {
			return a.discardFilePrompt(pick)
		}})
	}
	return out
}

func (a *App) fileMenuChoices(f fileEntry, working bool) []choice {
	var out []choice
	if working {
		label := i18n.S("暂存该文件", "stage file")
		if f.staged {
			label = i18n.S("取消暂存", "unstage file")
		}
		out = append(out, choice{label: label, action: func(a *App) tea.Cmd {
			path := f.path
			if f.staged {
				return a.runOp("unstage "+path, func(ctx context.Context) (string, error) {
					return a.svc.Unstage(ctx, path)
				})
			}
			return a.runOp("stage "+path, func(ctx context.Context) (string, error) {
				return a.svc.Stage(ctx, path)
			})
		}})
		out = append(out, choice{label: i18n.S("丢弃改动（危险）", "discard changes"), action: func(a *App) tea.Cmd {
			return a.discardFilePrompt(f)
		}})
	}
	return out
}

// forceDeleteBranch 强制删除分支（带确认）。
func (a *App) forceDeleteBranch(ref git.Ref) {
	if ref.Head {
		a.setToast(i18n.S("不能删除当前分支", "cannot delete current branch"), true)
		return
	}
	name := ref.Name
	a.confirm(i18n.S("强制删除分支 "+name+"（危险）", "force delete branch "+name+" (dangerous)"),
		i18n.S("即使未合并也会删除；可用 reflog 恢复。", "Deletes even if unmerged; recoverable via reflog."),
		"git branch -D "+name, true, func(a *App) tea.Cmd {
			return a.runOp("branch -D "+name, func(ctx context.Context) (string, error) {
				return a.svc.DeleteBranch(ctx, name, true)
			})
		})
}

// parseTagInput 解析两行输入：第一行标签名，第二行备注。
func parseTagInput(v string) (name, msg string) {
	nl := string(rune(10))
	v = strings.ReplaceAll(v, string(rune(13))+nl, nl)
	parts := strings.SplitN(v, nl, 2)
	name = strings.TrimSpace(parts[0])
	if len(parts) > 1 {
		msg = strings.TrimSpace(parts[1])
	}
	// 兼容旧的 name:message 写法
	if len(parts) == 1 && msg == "" {
		if i := indexByte(name, ':'); i > 0 {
			name, msg = strings.TrimSpace(name[:i]), strings.TrimSpace(name[i+1:])
		}
	}
	return name, msg
}

func (a *App) tagPrompt(c *git.Commit) {
	a.promptText(i18n.S("打标签：第一行标签名，第二行备注（Ctrl+S 创建）", "tag: name on line 1, message on line 2 (Ctrl+S creates)"), "v1.0", "", func(a *App, v string) tea.Cmd {
		name, msg := parseTagInput(v)
		if name == "" {
			a.setToast(i18n.S("标签名不能为空", "tag name required"), true)
			return nil
		}
		return a.runOp("tag "+name, func(ctx context.Context) (string, error) {
			return a.svc.CreateTag(ctx, name, c.Hash, msg)
		})
	})
	if a.modal != nil && a.modal.kind == modalText {
		a.modal.text.SetHeight(2)
	}
}

func (a *App) cherryPickPrompt(c *git.Commit) {
	a.confirm(i18n.S("cherry-pick 提交", "cherry-pick commit"),
		fmt.Sprintf("%s %s", c.Short, c.Subject), "git cherry-pick "+c.Hash, false,
		func(a *App) tea.Cmd {
			return a.runOp("cherry-pick", func(ctx context.Context) (string, error) {
				return a.svc.CherryPick(ctx, c.Hash)
			})
		})
}

func (a *App) revertPrompt(c *git.Commit) {
	a.confirm(i18n.S("revert 提交", "revert commit"),
		fmt.Sprintf("%s %s", c.Short, c.Subject), "git revert --no-edit "+c.Hash, false,
		func(a *App) tea.Cmd {
			return a.runOp("revert", func(ctx context.Context) (string, error) {
				return a.svc.Revert(ctx, c.Hash)
			})
		})
}

func (a *App) discardFilePrompt(f fileEntry) tea.Cmd {
	a.confirm(i18n.S("丢弃改动 "+f.path, "discard changes "+f.path),
		i18n.S("未提交改动将永久丢失。", "Uncommitted changes will be lost permanently."),
		discardCommand(f.path, f.untracked), true, func(a *App) tea.Cmd {
			return a.runOp("discard "+f.path, func(ctx context.Context) (string, error) {
				return a.svc.Discard(ctx, f.untracked, f.path)
			})
		})
	return nil
}

func indexByte(s string, b byte) int {
	for i := 0; i < len(s); i++ {
		if s[i] == b {
			return i
		}
	}
	return -1
}

// uncommittedMenuChoices 是「未提交变更」伪行的右键菜单。
func (a *App) uncommittedMenuChoices() []choice {
	return []choice{
		{label: i18n.S("全部暂存（git add -A）", "stage all (git add -A)"), action: func(a *App) tea.Cmd {
			return a.runOp("add -A", func(ctx context.Context) (string, error) {
				return a.svc.StageAll(ctx)
			})
		}},
		{label: i18n.S("全部取消暂存", "unstage all"), action: func(a *App) tea.Cmd {
			return a.runOp("reset HEAD", func(ctx context.Context) (string, error) {
				return a.svc.UnstageAll(ctx)
			})
		}},
		{label: i18n.S("全部丢弃（危险）", "discard everything (dangerous)"), action: func(a *App) tea.Cmd {
			a.discardAllPrompt()
			return nil
		}},
		{label: i18n.S("刷新状态", "refresh status"), action: func(a *App) tea.Cmd {
			return a.refreshCmd(false)
		}},
	}
}

// openFilesHeaderMenu 是文件区标题行的菜单（作用范围 = 全部）。
func (a *App) openFilesHeaderMenu() {
	if a.filesOwner == "work" {
		a.choose(i18n.S("工作区改动（全部）", "working changes (all)"), a.uncommittedMenuChoices())
		return
	}
	a.choose(i18n.S("文件列表", "files"), []choice{
		{label: i18n.S("全部展开", "expand all"), action: func(a *App) tea.Cmd {
			a.expandAllDirs(true)
			return nil
		}},
		{label: i18n.S("全部折叠", "collapse all"), action: func(a *App) tea.Cmd {
			a.expandAllDirs(false)
			return nil
		}},
	})
}

// expandAllDirs 展开/折叠所有目录。
func (a *App) expandAllDirs(expand bool) {
	var walk func(nodes []*fileNode)
	walk = func(nodes []*fileNode) {
		for _, n := range nodes {
			if n.dir {
				a.expanded[n.path] = expand
				walk(n.children)
			}
		}
	}
	walk(a.fileTree)
	a.rebuildFileRows()
	a.restoreFileSelection()
}

// entriesUnder 收集目录下的全部文件条目（按路径去重）。
func entriesUnder(n *fileNode) []fileEntry {
	var out []fileEntry
	seen := map[string]bool{}
	var walk func(node *fileNode)
	walk = func(node *fileNode) {
		if !node.dir {
			for _, e := range node.entries {
				if !seen[e.path] {
					seen[e.path] = true
					out = append(out, e)
				}
			}
			return
		}
		for _, c := range node.children {
			walk(c)
		}
	}
	walk(n)
	return out
}

// dirMenuChoices 是目录节点的右键菜单（作用范围 = 该目录）。
func (a *App) dirMenuChoices(n *fileNode, working bool) []choice {
	out := []choice{
		{label: i18n.S("展开/折叠", "expand/collapse"), action: func(a *App) tea.Cmd {
			a.toggleFileDir(n)
			return nil
		}},
		{label: i18n.S("复制目录路径", "copy path"), action: func(a *App) tea.Cmd {
			return a.copyToClipboard(n.path)
		}},
	}
	if !working {
		return out
	}
	files := entriesUnder(n)
	paths := make([]string, 0, len(files))
	for _, f := range files {
		paths = append(paths, f.path)
	}
	out = append(out,
		choice{label: i18n.S("暂存该目录全部文件", "stage all in directory"), action: func(a *App) tea.Cmd {
			if len(paths) == 0 {
				return nil
			}
			return a.runOp("add "+n.path, func(ctx context.Context) (string, error) {
				return a.svc.Stage(ctx, paths...)
			})
		}},
		choice{label: i18n.S("取消暂存该目录全部文件", "unstage directory"), action: func(a *App) tea.Cmd {
			if len(paths) == 0 {
				return nil
			}
			return a.runOp("unstage "+n.path, func(ctx context.Context) (string, error) {
				return a.svc.Unstage(ctx, paths...)
			})
		}},
		choice{label: i18n.S("丢弃该目录全部改动（危险）", "discard directory changes (dangerous)"), action: func(a *App) tea.Cmd {
			a.confirm(i18n.S("丢弃目录 "+n.path+" 的全部改动", "discard all changes under "+n.path),
				i18n.S("该目录下未提交改动将永久丢失。", "Uncommitted changes under this directory will be lost."),
				"git checkout/clean -- "+n.path, true, func(a *App) tea.Cmd {
					return a.runOp("discard "+n.path, func(ctx context.Context) (string, error) {
						var all string
						for _, f := range files {
							out, err := a.svc.Discard(ctx, f.untracked, f.path)
							all += out
							if err != nil {
								return all, err
							}
						}
						return all, nil
					})
				})
			return nil
		}},
	)
	return out
}

// discardAllPrompt 丢弃全部未提交改动（含未跟踪）。
func (a *App) discardAllPrompt() {
	a.confirm(i18n.S("丢弃全部未提交改动（危险）", "discard ALL uncommitted changes (dangerous)"),
		i18n.S("将重置已跟踪改动并删除未跟踪文件，无法恢复。",
			"Resets tracked changes and deletes untracked files. This cannot be undone."),
		"git reset --hard HEAD && git clean -f -d", true, func(a *App) tea.Cmd {
			return a.runOp("discard all", func(ctx context.Context) (string, error) {
				out, err := a.svc.RunGitArgs(ctx, []string{"reset", "--hard", "HEAD"})
				if err != nil {
					return out, err
				}
				out2, err := a.svc.RunGitArgs(ctx, []string{"clean", "-f", "-d"})
				return out + out2, err
			})
		})
}

// openBranchesHeaderMenu 是 BRANCHES 标题的整体菜单。
// openProjectMenu 是底部项目名右键菜单。
func (a *App) openProjectMenu() {
	choices := []choice{{label: i18n.S("添加项目…", "add project..."), action: func(a *App) tea.Cmd {
		a.openProjectPicker()
		return nil
	}}}
	for _, p := range a.projects {
		if p.Path == a.currentPath {
			continue
		}
		p := p
		choices = append(choices, choice{label: i18n.S("切换到 "+p.Name, "switch to "+p.Name), action: func(a *App) tea.Cmd {
			return a.switchProject(a.projectIndex(p.Path))
		}})
	}
	a.choose(i18n.S("项目 "+a.svc.Name(), "projects"), choices)
}

func (a *App) openBranchesHeaderMenu() {
	a.choose(i18n.S("分支（整体操作）", "branches (overall)"), []choice{
		{label: i18n.S("全部展开", "expand all"), action: func(a *App) tea.Cmd {
			for k := range a.collapsed {
				a.collapsed[k] = false
			}
			return nil
		}},
		{label: i18n.S("全部折叠", "collapse all"), action: func(a *App) tea.Cmd {
			for _, k := range []string{"projects", "branches", "remote", "tags", "stash"} {
				a.collapsed[k] = true
			}
			return nil
		}},
		{label: "fetch", action: func(a *App) tea.Cmd { return a.fetch(false) }},
		{label: "fetch --prune", action: func(a *App) tea.Cmd { return a.fetch(true) }},
		{label: i18n.S("刷新", "refresh"), action: func(a *App) tea.Cmd {
			return a.refreshCmd(false)
		}},
		{label: i18n.S("切换主题（当前: "+a.themeName+"）", "cycle theme (current: "+a.themeName+")"), action: func(a *App) tea.Cmd {
			a.cycleTheme()
			return nil
		}},
	})
}

// openContentHeaderMenu 是 CONTENT 标题的整体菜单。
func (a *App) openContentHeaderMenu() {
	a.choose(i18n.S("内容（整体操作）", "content (overall)"), []choice{
		{label: i18n.S("滚动到顶部", "scroll to top"), action: func(a *App) tea.Cmd {
			a.content.offset = 0
			return nil
		}},
		{label: i18n.S("滚动到底部", "scroll to bottom"), action: func(a *App) tea.Cmd {
			a.content.offset = len(a.content.lines)
			a.clampContentScroll()
			return nil
		}},
		{label: i18n.S("复制全部内容", "copy all content"), action: func(a *App) tea.Cmd {
			return a.copyToClipboard(strings.Join(a.content.lines, "\n"))
		}},
	})
}

// openSectionMenu 是分组标题的右键菜单（作用范围 = 该分组）。
func (a *App) openSectionMenu(section string) {
	name := strings.ToUpper(section)
	choices := []choice{
		{label: i18n.S("展开本组", "expand section"), action: func(a *App) tea.Cmd {
			a.collapsed[section] = false
			return nil
		}},
		{label: i18n.S("折叠本组", "collapse section"), action: func(a *App) tea.Cmd {
			a.collapsed[section] = true
			return nil
		}},
	}
	switch section {
	case "branches":
		choices = append(choices,
			choice{label: i18n.S("新建分支", "new branch"), action: func(a *App) tea.Cmd {
				a.newBranchPrompt("HEAD")
				return nil
			}},
			choice{label: i18n.S("刷新", "refresh"), action: func(a *App) tea.Cmd {
				return a.refreshCmd(false)
			}},
		)
	case "remote":
		choices = append(choices,
			choice{label: "fetch", action: func(a *App) tea.Cmd { return a.fetch(false) }},
			choice{label: "fetch --prune", action: func(a *App) tea.Cmd { return a.fetch(true) }},
		)
	case "tags":
		choices = append(choices,
			choice{label: i18n.S("在 HEAD 打标签", "tag HEAD"), action: func(a *App) tea.Cmd {
				a.promptText(i18n.S("打标签：第一行标签名，第二行备注（Ctrl+S 创建）", "tag: name on line 1, message on line 2 (Ctrl+S creates)"), "v1.0", "", func(a *App, v string) tea.Cmd {
					name, msg := parseTagInput(v)
					if name == "" {
						return nil
					}
					return a.runOp("tag "+name, func(ctx context.Context) (string, error) {
						return a.svc.CreateTag(ctx, name, "HEAD", msg)
					})
				})
				return nil
			}},
		)
	case "stash":
		choices = append(choices,
			choice{label: i18n.S("新建 stash", "new stash"), action: func(a *App) tea.Cmd {
				return a.stashPrompt()
			}},
		)
	}
	a.choose(i18n.S("分组 ", "section ")+name, choices)
}

// openCommandsHeaderMenu 是命令历史标题的菜单。
func (a *App) openCommandsHeaderMenu() {
	a.choose(i18n.S("命令历史（整体操作）", "command history (overall)"), []choice{
		{label: i18n.S("复制全部", "copy all"), action: func(a *App) tea.Cmd {
			lines := make([]string, 0, len(a.commands))
			for _, e := range a.commands {
				lines = append(lines, e.at.Format("15:04:05")+"  "+e.line)
			}
			return a.copyToClipboard(strings.Join(lines, string(rune(10))))
		}},
		{label: i18n.S("清空", "clear"), action: func(a *App) tea.Cmd {
			a.commands = nil
			a.cmdOffset = 0
			if a.pane == PaneLog {
				a.pane = PaneCommits
			}
			return nil
		}},
		{label: i18n.S("刷新状态", "refresh"), action: func(a *App) tea.Cmd {
			return a.refreshCmd(false)
		}},
	})
}

// openInfoHeaderMenu 是提交信息标题的菜单。
func (a *App) openInfoHeaderMenu() {
	choices := []choice{
		{label: i18n.S("滚动到顶部", "scroll to top"), action: func(a *App) tea.Cmd {
			a.infoOff = 0
			return nil
		}},
		{label: i18n.S("滚动到底部", "scroll to bottom"), action: func(a *App) tea.Cmd {
			a.infoOff = a.infoLineCount()
			a.clampInfoOffset()
			return nil
		}},
	}
	if c := a.selectedCommit(); c != nil && !a.onDirtyRow() {
		subject := c.Subject
		sha := c.Hash
		choices = append(choices,
			choice{label: i18n.S("复制提交信息", "copy message"), action: func(a *App) tea.Cmd {
				return a.copyToClipboard(subject)
			}},
			choice{label: i18n.S("复制完整 sha", "copy sha"), action: func(a *App) tea.Cmd {
				return a.copyToClipboard(sha)
			}},
			choice{label: i18n.S("查看完整详情", "view full details"), action: func(a *App) tea.Cmd {
				return a.openCommitDetail(sha)
			}},
		)
	}
	a.choose(i18n.S("提交信息（整体操作）", "commit info (overall)"), choices)
}
