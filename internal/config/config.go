// Package config 读写 gitc 的用户配置。
package config

import (
	"encoding/json"
	"os"
	"path/filepath"
)

// File 是配置文件内容。
type File struct {
	Theme     string            `json:"theme,omitempty"`
	Colors    map[string]string `json:"colors,omitempty"`
	LeftWidth int               `json:"left_width,omitempty"`
	// DrawerLeft 是抽屉内文件列宽度（0 表示默认比例）
	DrawerLeft int `json:"drawer_left,omitempty"`
	// RightTop 是提交区高度（0 表示默认比例）
	RightTop int `json:"right_top,omitempty"`
	// ProjectH 是项目区高度（0 表示自适应）
	ProjectH int `json:"project_h,omitempty"`
	// LeftTop 是左侧分支区高度（0 表示默认比例）
	LeftTop int `json:"left_top,omitempty"`
	// LeftMid 是左侧改动区高度（0 表示默认比例）
	LeftMid int `json:"left_mid,omitempty"`
	// CommandH 是命令行历史区高度（0 表示默认）
	CommandH int `json:"command_h,omitempty"`
	// Projects 是用户配置的多个项目仓库路径
	Projects []string `json:"projects,omitempty"`
	// LastProject 是最后使用的项目路径
	LastProject string `json:"last_project,omitempty"`
}

// Path 返回配置文件路径（用户配置目录/gitc/config.json）。
func Path() (string, error) {
	dir, err := os.UserConfigDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(dir, "gitc", "config.json"), nil
}

// Load 读取配置；文件不存在或损坏时返回零值。
func Load() File {
	var f File
	p, err := Path()
	if err != nil {
		return f
	}
	b, err := os.ReadFile(p)
	if err != nil {
		return f
	}
	_ = json.Unmarshal(b, &f)
	return f
}

// Save 写入配置（自动创建目录）。
func Save(f File) error {
	p, err := Path()
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
		return err
	}
	b, err := json.MarshalIndent(f, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(p, b, 0o644)
}
