package git

import (
	"context"
	"os"
	"path/filepath"
)

// 中断态种类。
const (
	StateNone       = ""
	StateMerge      = "merge"
	StateRebase     = "rebase"
	StateCherryPick = "cherry-pick"
	StateRevert     = "revert"
)

// RepoState 是 merge/rebase/cherry-pick/revert 进行中的状态。
type RepoState struct {
	Kind string
}

// InProgress 表示存在中断的操作。
func (s RepoState) InProgress() bool { return s.Kind != StateNone }

// State 检测当前仓库是否处于 merge/rebase/cherry-pick/revert 中。
func (r *Repo) State(ctx context.Context) (RepoState, error) {
	gitDir := r.GitDir
	if gitDir == "" {
		return RepoState{}, nil
	}
	exists := func(name string) bool {
		_, err := os.Stat(filepath.Join(gitDir, name))
		return err == nil
	}
	switch {
	case exists("rebase-merge"), exists("rebase-apply"):
		return RepoState{Kind: StateRebase}, nil
	case exists("MERGE_HEAD"):
		return RepoState{Kind: StateMerge}, nil
	case exists("CHERRY_PICK_HEAD"):
		return RepoState{Kind: StateCherryPick}, nil
	case exists("REVERT_HEAD"):
		return RepoState{Kind: StateRevert}, nil
	}
	return RepoState{}, nil
}
