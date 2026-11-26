package git

import "context"

// Service 是 UI 依赖的 git 能力集合，*Repo 实现该接口；测试可用假实现。
type Service interface {
	RootPath() string
	Name() string
	StreamLog(ctx context.Context, max int, cb func(Commit) bool) error
	StreamLogRef(ctx context.Context, ref string, max int, cb func(Commit) bool) error
	// StreamLogScope 按范围读取：local=仅本地分支，remote=仅远端分支，其余=全部引用。
	StreamLogScope(ctx context.Context, scope string, max int, cb func(Commit) bool) error
	SearchLog(ctx context.Context, query string, max int, cb func(Commit) bool) error
	SearchAuthorLog(ctx context.Context, query string, max int, cb func(Commit) bool) error
	LogOne(ctx context.Context, rev string) (Commit, error)
	Refs(ctx context.Context) ([]Ref, error)
	Status(ctx context.Context, includeUntracked bool) (Status, error)
	State(ctx context.Context) (RepoState, error)
	Stashes(ctx context.Context) ([]Stash, error)
	CurrentBranch(ctx context.Context) string
	RevParse(ctx context.Context, ref string) (string, error)
	CommitDetail(ctx context.Context, sha string) (CommitDetail, error)
	CommitFiles(ctx context.Context, sha string) ([]CommitFile, error)
	CommitDiff(ctx context.Context, sha, path string) (string, error)
	WorkingDiff(ctx context.Context, path string, staged, untracked bool) (string, error)

	Checkout(ctx context.Context, ref string) (string, error)
	CheckoutRemoteBranch(ctx context.Context, remoteRef, local string) (string, error)
	CreateBranch(ctx context.Context, name, startPoint string, checkout bool) (string, error)
	RenameBranch(ctx context.Context, oldName, newName string) (string, error)
	DeleteBranch(ctx context.Context, name string, force bool) (string, error)
	DeleteRemoteBranch(ctx context.Context, remote, branch string) (string, error)
	CompareBranches(ctx context.Context, base, target string, max int) (CompareSummary, error)
	UnsetUpstream(ctx context.Context, branch string) (string, error)
	Merge(ctx context.Context, ref string) (string, error)
	Rebase(ctx context.Context, ref string) (string, error)
	CherryPick(ctx context.Context, sha string) (string, error)
	Revert(ctx context.Context, sha string) (string, error)
	Reset(ctx context.Context, sha string, mode ResetMode) (string, error)
	CreateTag(ctx context.Context, name, sha, message string) (string, error)
	DeleteTag(ctx context.Context, name string) (string, error)
	PushTag(ctx context.Context, remote, name string, progress func(string)) (string, error)
	Stage(ctx context.Context, paths ...string) (string, error)
	StageAll(ctx context.Context) (string, error)
	Unstage(ctx context.Context, paths ...string) (string, error)
	UnstageAll(ctx context.Context) (string, error)
	Discard(ctx context.Context, untracked bool, paths ...string) (string, error)
	Ignore(ctx context.Context, path string) (string, error)
	Commit(ctx context.Context, message string, amend bool) (string, error)
	Worktrees(ctx context.Context) ([]Worktree, error)
	AddWorktree(ctx context.Context, path, branch string) (string, error)
	RemoveWorktree(ctx context.Context, path string, force bool) (string, error)
	PruneWorktrees(ctx context.Context) (string, error)
	LockWorktree(ctx context.Context, path, reason string) (string, error)
	UnlockWorktree(ctx context.Context, path string) (string, error)
	CommitMessage(ctx context.Context, sha string) (string, error)
	EditCommitMessage(ctx context.Context, sha, message string) (string, error)
	Squash(ctx context.Context, head, parent, message string) (string, error)
	Stash(ctx context.Context, message string, includeUntracked bool) (string, error)
	StashApply(ctx context.Context, index int) (string, error)
	StashPop(ctx context.Context, index int) (string, error)
	StashDrop(ctx context.Context, index int) (string, error)
	StashDiff(ctx context.Context, index int) (string, error)
	Fetch(ctx context.Context, prune bool, progress func(string)) (string, error)
	Pull(ctx context.Context, rebase bool, progress func(string)) (string, error)
	Push(ctx context.Context, opts PushOptions, branch string, progress func(string)) (string, error)
	SetUpstream(ctx context.Context, branch, upstream string) (string, error)
	ContinueOperation(ctx context.Context, kind string) (string, error)
	AbortOperation(ctx context.Context, kind string) (string, error)
	RunGitArgs(ctx context.Context, args []string) (string, error)
}

// RootPath 返回仓库根目录。
func (r *Repo) RootPath() string { return r.Root }
