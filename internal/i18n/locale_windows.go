//go:build windows

package i18n

import "syscall"

// osUILanguageZh 通过 GetUserDefaultUILanguage 判断系统界面语言是否为中文。
func osUILanguageZh() bool {
	kernel32 := syscall.NewLazyDLL("kernel32.dll")
	proc := kernel32.NewProc("GetUserDefaultUILanguage")
	if err := proc.Find(); err != nil {
		return false
	}
	ret, _, _ := proc.Call()
	// LANGID 低 10 位为主语言 ID；0x04 为中文（含简繁）。
	primary := uint16(ret) & 0x3ff
	return primary == 0x04
}
