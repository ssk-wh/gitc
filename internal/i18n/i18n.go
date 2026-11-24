// Package i18n 提供中英文文案。默认根据环境变量语言选择。
package i18n

import (
	"os"
	"strings"
)

const (
	Zh = "zh"
	En = "en"
)

var current = En

// Detect 根据 LANG/LC_ALL 与系统界面语言推断语言。
func Detect() string {
	for _, k := range []string{"LC_ALL", "LC_MESSAGES", "LANG"} {
		v := strings.ToLower(os.Getenv(k))
		if strings.HasPrefix(v, "zh") || strings.Contains(v, "chinese") {
			return Zh
		}
	}
	if osUILanguageZh() {
		return Zh
	}
	return En
}

// Set 设置当前语言。
func Set(lang string) {
	if strings.HasPrefix(strings.ToLower(lang), "zh") {
		current = Zh
	} else if strings.HasPrefix(strings.ToLower(lang), "en") {
		current = En
	}
}

// Current 返回当前语言。
func Current() string { return current }

// S 按当前语言返回文案。
func S(zh, en string) string {
	if current == Zh {
		return zh
	}
	return en
}
