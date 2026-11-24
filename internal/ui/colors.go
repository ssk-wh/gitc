package ui

import (
	"sort"
	"time"

	"github.com/charmbracelet/lipgloss"

	"github.com/ssk-wh/gitc/internal/git"
)

// rebuildCommitColors 按"最近分支优先"的多源 BFS 为提交分配分支颜色，
// 使提交图与分支徽标使用同一套稳定颜色。
func (a *App) rebuildCommitColors() {
	byHash := make(map[string]git.Commit, len(a.commits))
	for _, c := range a.commits {
		byHash[c.Hash] = c
	}
	a.rebuildBranchColors(byHash)
	type seed struct {
		hash  string
		color int
		when  time.Time
	}
	var seeds []seed
	for _, r := range a.refs {
		if r.Kind != git.RefLocal && r.Kind != git.RefRemote && r.Kind != git.RefStash {
			continue
		}
		w := time.Time{}
		if c, ok := byHash[r.Target]; ok {
			w = c.When
		}
			seeds = append(seeds, seed{hash: r.Target, color: a.refColorIndex(r.Kind, r.Name), when: w})
	}
	sort.SliceStable(seeds, func(i, j int) bool { return seeds[i].when.After(seeds[j].when) })

	colors := make(map[string]int, len(a.commits))
	queue := make([]seed, 0, len(seeds))
	queue = append(queue, seeds...)
	for head := 0; head < len(queue); head++ {
		cur := queue[head]
		if cur.hash == "" {
			continue
		}
		if _, ok := colors[cur.hash]; ok {
			continue
		}
		c, ok := byHash[cur.hash]
		if !ok {
			continue
		}
		colors[cur.hash] = cur.color
		for _, p := range c.Parents {
			if _, ok := colors[p]; ok {
				continue
			}
			queue = append(queue, seed{hash: p, color: cur.color})
		}
	}
	a.commitColor = colors
}

// rebuildBranchColors 按分支最新提交时间倒序分配调色板，保证靠前的分支颜色互不相同。
func (a *App) rebuildBranchColors(byHash map[string]git.Commit) {
	latest := map[string]time.Time{}
	for _, r := range a.refs {
		if r.Kind != git.RefLocal && r.Kind != git.RefRemote {
			continue
		}
		name := normalizeRefName(r.Kind, r.Name)
		var w time.Time
		if c, ok := byHash[r.Target]; ok {
			w = c.When
		}
		if old, ok := latest[name]; !ok || w.After(old) {
			latest[name] = w
		}
	}
	names := make([]string, 0, len(latest))
	for n := range latest {
		names = append(names, n)
	}
	sort.SliceStable(names, func(i, j int) bool {
		if !latest[names[i]].Equal(latest[names[j]]) {
			return latest[names[i]].After(latest[names[j]])
		}
		return names[i] < names[j]
	})
	m := make(map[string]int, len(names))
	for i, n := range names {
		m[n] = i % len(lanePalette)
	}
	a.branchColor = m
}

// refColorIndex 返回引用的配色下标（远端分支与本地同名分支同色）。
func (a *App) refColorIndex(kind, name string) int {
	if idx, ok := a.branchColor[normalizeRefName(kind, name)]; ok {
		return idx
	}
	return colorIndexForRef(name)
}

// refColor 返回引用颜色。
func (a *App) refColor(kind, name string) lipgloss.AdaptiveColor {
	idx := a.refColorIndex(kind, name)
	if idx < 0 || idx >= len(lanePalette) {
		return colorDim
	}
	return lanePalette[idx]
}

// commitColorOf 返回提交的分支配色下标；未知返回 fallback。
func (a *App) commitColorOf(hash string, fallback int) int {
	if idx, ok := a.commitColor[hash]; ok {
		return idx
	}
	return fallback
}
