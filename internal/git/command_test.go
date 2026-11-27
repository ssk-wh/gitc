package git

import (
	"context"
	"os"
	"os/exec"
	"testing"
	"time"
)

func TestIsUserCommand(t *testing.T) {
	cases := []struct {
		args []string
		want bool
	}{
		{[]string{"checkout", "feature/x"}, true},
		{[]string{"branch", "-d", "x"}, true},
		{[]string{"-c", "core.editor=true", "rebase", "--continue"}, true},
		{[]string{"log", "--topo-order"}, false},
		{[]string{"status", "--porcelain=v2"}, false},
		{[]string{"for-each-ref"}, false},
		{[]string{"stash", "push", "-m", "x"}, true},
		{[]string{"stash", "list"}, false},
		{[]string{"show", "-s"}, false},
		{[]string{"diff", "--no-color"}, false},
		{[]string{"push", "--progress"}, true},
	}
	for _, c := range cases {
		if got := isUserCommand(c.args); got != c.want {
			t.Errorf("isUserCommand(%v) = %v, 期望 %v", c.args, got, c.want)
		}
	}
}

func TestAuthorDateOrder(t *testing.T) {
	_, dir := newTestRepo(t)
	commit := func(name, content, msg, date string) {
		writeFile(t, dir, name, content)
		cmd := exec.Command("git", "-c", "core.fscache=false", "-C", dir, "add", "-A")
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("add: %v %s", err, out)
		}
		cmd = exec.Command("git", "-c", "core.fscache=false", "-C", dir, "commit", "-m", msg)
		cmd.Env = append(os.Environ(), "GIT_AUTHOR_DATE="+date, "GIT_COMMITTER_DATE="+date)
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("commit: %v %s", err, out)
		}
	}
	commit("a.txt", "1\n", "base", "2025-01-01T00:00:00Z")
	runGit(t, dir, "checkout", "-b", "side")
	commit("b.txt", "b\n", "side-newer", "2026-06-01T00:00:00Z")
	runGit(t, dir, "checkout", "main")
	commit("c.txt", "c\n", "main-mid", "2025-06-01T00:00:00Z")
	r := mustRepo(t, dir)
	var subjects []string
	if err := r.StreamLog(context.Background(), 0, func(c Commit) bool {
		subjects = append(subjects, c.Subject)
		return true
	}); err != nil {
		t.Fatal(err)
	}
	if len(subjects) < 3 {
		t.Fatalf("提交数 = %d", len(subjects))
	}
	// 作者日期倒序：2026-06 的分支提交应排在最前
	if subjects[0] != "side-newer" {
		t.Errorf("排序 = %v，期望 side-newer 在最前（作者日期序）", subjects)
	}
	var prev time.Time
	_ = r.StreamLog(context.Background(), 0, func(c Commit) bool {
		if !prev.IsZero() && c.When.After(prev) {
			t.Errorf("时间非倒序: %v -> %v", prev, c.When)
		}
		prev = c.When
		return true
	})
}
