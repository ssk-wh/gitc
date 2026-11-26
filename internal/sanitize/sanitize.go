package sanitize

import (
	"regexp"
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

var (
	privateKeyPattern     = regexp.MustCompile(`(?s)-----BEGIN [^-]*PRIVATE KEY-----.*?-----END [^-]*PRIVATE KEY-----`)
	urlUserinfoPattern    = regexp.MustCompile(`(?i)([a-z][a-z0-9+.-]*://)[^/\s@]+@`)
	authorizationPattern  = regexp.MustCompile(`(?i)((?:proxy-)?authorization\s*[:=]\s*)(?:(?:bearer|basic|token)\s+)?[^\s,;]+`)
	sensitiveValuePattern = regexp.MustCompile(`(?i)\b(token|password|passwd|secret|access[_-]?token|api[_-]?key|credential\.(?:helper|username|password)|http\.(?:extraheader|cookie))\s*[:=]\s*[^\r\n,;]+`)
)

// Secrets 遮盖命令、错误与日志中的 URL 凭据、认证头和敏感配置值。
func Secrets(s string) string {
	s = privateKeyPattern.ReplaceAllString(s, "[REDACTED PRIVATE KEY]")
	s = urlUserinfoPattern.ReplaceAllString(s, "${1}***@")
	s = authorizationPattern.ReplaceAllString(s, "${1}***")
	s = sensitiveValuePattern.ReplaceAllString(s, "${1}=***")
	return s
}

// Command 将参数数组格式化为可阅读、可复制的命令，并脱敏敏感参数。
func Command(args []string) string {
	out := make([]string, len(args))
	for i, arg := range args {
		out[i] = quoteArg(redactArg(arg))
	}
	return strings.Join(out, " ")
}

func redactArg(arg string) string {
	lower := strings.ToLower(arg)
	for _, key := range []string{
		"credential.helper", "credential.username", "credential.password",
		"http.extraheader", "http.proxyauthorization", "http.cookie", "core.askpass",
	} {
		for _, sep := range []string{"=", ":"} {
			prefix := key + sep
			if i := strings.Index(lower, prefix); i >= 0 {
				return arg[:i+len(prefix)] + "***"
			}
		}
	}
	return Secrets(arg)
}

func quoteArg(arg string) string {
	arg = strings.ReplaceAll(Text(arg), "\n", " ")
	if arg != "" && isSafeCommandArg(arg) {
		return arg
	}
	return "'" + strings.ReplaceAll(arg, "'", "'\\''") + "'"
}

func isSafeCommandArg(s string) bool {
	for _, r := range s {
		if r >= 'a' && r <= 'z' || r >= 'A' && r <= 'Z' || r >= '0' && r <= '9' {
			continue
		}
		switch r {
		case '_', '-', '.', '/', ':', '@', '%', '+', '=', ',':
			continue
		}
		return false
	}
	return true
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
