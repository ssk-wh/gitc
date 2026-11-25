// Package workspace 读写项目根目录下的 .gitc-workspace 文件。
package workspace

import (
	"encoding/json"
	"os"
	"path/filepath"
)

// File 是 workspace 文件内容。
type File struct {
	Version  int      `json:"version"`
	Projects []string `json:"projects"`
}

// Name 是 workspace 文件名。
const Name = ".gitc-workspace"

// Path 返回仓库根目录下的 workspace 文件路径。
func Path(root string) string { return filepath.Join(root, Name) }

// Load 读取 workspace；文件不存在时返回零值。
func Load(root string) (File, error) {
	var f File
	b, err := os.ReadFile(Path(root))
	if err != nil {
		if os.IsNotExist(err) {
			return f, nil
		}
		return f, err
	}
	if err := json.Unmarshal(b, &f); err != nil {
		return File{}, err
	}
	return f, nil
}

// Save 写入 workspace 文件。
func Save(root string, projects []string) error {
	f := File{Version: 1, Projects: projects}
	b, err := json.MarshalIndent(f, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(Path(root), append(b, '\n'), 0o644)
}
