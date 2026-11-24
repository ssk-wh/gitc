//go:build !windows

package i18n

// osUILanguageZh 非 Windows 平台依据环境变量判断，无需额外探测。
func osUILanguageZh() bool { return false }
