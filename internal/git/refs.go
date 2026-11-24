package git

import (
	"context"
	"strings"
)

// Ref 种类。
const (
	RefLocal  = "local"
	RefRemote = "remote"
	RefTag    = "tag"
	RefStash  = "stash"
)

// Ref 是一个引用（分支/标签/stash）。
type Ref struct {
	Name     string // 展示名：main、origin/main、v1.0、stash
	FullName string // 完整引用名 refs/heads/main 等
	Kind     string
	Target   string // 指向的提交 sha（附注标签取 peel 后的提交）
	Upstream string
	Head     bool
}

// Refs 读取本地分支、远端分支、标签与 stash 引用。
func (r *Repo) Refs(ctx context.Context) ([]Ref, error) {
	const format = "%(refname)%00%(objectname)%00%(*objectname)%00%(upstream:short)%00%(HEAD)%00%(symref)%1e"
	out, err := r.run(ctx, true, "for-each-ref", "--format="+format,
		"refs/heads", "refs/remotes", "refs/tags", "refs/stash")
	if err != nil {
		return nil, err
	}
	var refs []Ref
	for _, rec := range strings.Split(out, "\x1e") {
		rec = strings.TrimSpace(rec)
		if rec == "" {
			continue
		}
		f := strings.Split(rec, "\x00")
		if len(f) < 6 {
			continue
		}
		full := strings.TrimSpace(f[0])
		if strings.TrimSpace(f[5]) != "" { // 符号引用（如 origin/HEAD）跳过
			continue
		}
		target := strings.TrimSpace(f[1])
		if peeled := strings.TrimSpace(f[2]); peeled != "" {
			target = peeled
		}
		ref := Ref{
			FullName: full,
			Target:   target,
			Upstream: strings.TrimSpace(f[3]),
			Head:     strings.TrimSpace(f[4]) == "*",
		}
		switch {
		case strings.HasPrefix(full, "refs/heads/"):
			ref.Kind = RefLocal
			ref.Name = strings.TrimPrefix(full, "refs/heads/")
		case strings.HasPrefix(full, "refs/remotes/"):
			ref.Kind = RefRemote
			ref.Name = strings.TrimPrefix(full, "refs/remotes/")
		case strings.HasPrefix(full, "refs/tags/"):
			ref.Kind = RefTag
			ref.Name = strings.TrimPrefix(full, "refs/tags/")
		case full == "refs/stash":
			ref.Kind = RefStash
			ref.Name = "stash"
		default:
			continue
		}
		refs = append(refs, ref)
	}
	return refs, nil
}

// Stash 是 stash 列表项。
type Stash struct {
	Index    int
	Selector string // stash@{0}
	Hash     string
	Subject  string
}

// Stashes 读取 stash 列表。
func (r *Repo) Stashes(ctx context.Context) ([]Stash, error) {
	out, err := r.run(ctx, true, "stash", "list", "--format=%gd%x00%H%x00%gs%x1e")
	if err != nil {
		return nil, err
	}
	var list []Stash
	for _, rec := range strings.Split(out, "\x1e") {
		rec = strings.TrimSpace(rec)
		if rec == "" {
			continue
		}
		f := strings.Split(rec, "\x00")
		if len(f) < 3 {
			continue
		}
		st := Stash{Selector: strings.TrimSpace(f[0]), Hash: strings.TrimSpace(f[1]), Subject: strings.TrimSpace(f[2])}
		// 从 stash@{n} 提取序号。
		if i := strings.IndexByte(st.Selector, '{'); i >= 0 {
			idx := 0
			for _, c := range st.Selector[i+1:] {
				if c >= '0' && c <= '9' {
					idx = idx*10 + int(c-'0')
				}
			}
			st.Index = idx
		}
		list = append(list, st)
	}
	return list, nil
}

// CurrentBranch 返回当前分支名；detached 时返回空串。
func (r *Repo) CurrentBranch(ctx context.Context) string {
	out, err := r.run(ctx, true, "symbolic-ref", "--short", "-q", "HEAD")
	if err != nil {
		return ""
	}
	return strings.TrimSpace(out)
}
