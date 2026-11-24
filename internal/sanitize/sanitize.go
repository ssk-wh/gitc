package sanitize

import (
	"strings"
	"unicode"
)

const maxEscapeScan = 256

// Line 用于单行文本（提交标题、分支名、作者等）：去除所有控制字符与 ESC 序列。
func Line(s string) string {
	s = stripEscapes(s)
	var b strings.Builder
	b.Grow(len(s))
	for _, r := range s {
		if r == '\t' || r == '\n' || r == '\r' {
			b.WriteRune(' ')
			continue
		}
		if unsafeRune(r) {
			continue
		}
		b.WriteRune(r)
	}
	return strings.TrimSpace(b.String())
}

// Text 用于多行文本（diff、命令输出）：保留换行与制表符，去除其他控制字符与 ESC 序列。
func Text(s string) string {
	s = stripEscapes(s)
	var b strings.Builder
	b.Grow(len(s))
	for _, r := range s {
		if r == '\n' {
			b.WriteRune(r)
			continue
		}
		if r == '\t' {
			b.WriteString("    ")
			continue
		}
		if unsafeRune(r) {
			continue
		}
		b.WriteRune(r)
	}
	return strings.ReplaceAll(b.String(), "\r\n", "\n")
}

// unsafeRune 判断会干扰终端或伪造显示的字符。
func unsafeRune(r rune) bool {
	if r == 0x1b || r == 0x7f {
		return true
	}
	if r < 0x20 {
		return true
	}
	if r >= 0x80 && r <= 0x9f { // C1 控制字符
		return true
	}
	if unicode.Is(unicode.Cf, r) { // 零宽、方向控制等格式字符
		return true
	}
	return false
}

// stripEscapes 移除 ESC 序列（CSI/OSC/其他），含未闭合序列的保守处理。
func stripEscapes(s string) string {
	if !strings.ContainsRune(s, 0x1b) {
		return s
	}
	var b strings.Builder
	b.Grow(len(s))
	i := 0
	for i < len(s) {
		c := s[i]
		if c != 0x1b {
			b.WriteByte(c)
			i++
			continue
		}
		i++
		if i >= len(s) {
			break
		}
		switch s[i] {
		case '[': // CSI: ESC [ 参数 终止符(0x40-0x7e)
			i++
			n := 0
			for i < len(s) && n < maxEscapeScan {
				ch := s[i]
				i++
				n++
				if ch >= 0x40 && ch <= 0x7e {
					break
				}
			}
		case ']': // OSC: ESC ] ... BEL 或 ST(ESC \)
			i++
			n := 0
			for i < len(s) && n < maxEscapeScan {
				if s[i] == 0x07 {
					i++
					break
				}
				if s[i] == 0x1b && i+1 < len(s) && s[i+1] == '\\' {
					i += 2
					break
				}
				i++
				n++
			}
		default: // 其他两字节 ESC 序列
			i++
		}
	}
	return b.String()
}
