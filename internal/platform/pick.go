// Package platform 提供与操作系统交互的小工具。
package platform

import "errors"

var (
	// ErrCanceled 表示用户取消了选择。
	ErrCanceled = errors.New("canceled")
	// ErrUnavailable 表示当前环境没有可用的系统目录选择框。
	ErrUnavailable = errors.New("directory picker unavailable")
)

// PickDirectory 打开系统目录选择框，返回用户选择的目录。
func PickDirectory() (string, error) {
	return pickDirectory()
}
