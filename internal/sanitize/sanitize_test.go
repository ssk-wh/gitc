package sanitize

import (
	"strings"
	"testing"
)

const (
	esc = "\x1b"
	bel = "\x07"
	st  = esc + `\`
)

func TestLineStripsEscapes(t *testing.T) {
	cases := []struct {
		in, want string
	}{
		{"正常提交标题", "正常提交标题"},
		{esc + "[31m红色" + esc + "[0m标题", "红色标题"},
		{"OSC52" + esc + "]52;c;Zm9v" + bel + "注入", "OSC52注入"},
		{"ESC" + esc + "]0;title" + st + "结束", "ESC结束"},
		{"控制" + "\x01" + "字符" + "\x7f", "控制字符"},
		{"多行\n标题", "多行 标题"},
	}
	for _, c := range cases {
		if got := Line(c.in); got != c.want {
			t.Errorf("Line(%q) = %q, 期望 %q", c.in, got, c.want)
		}
	}
}

func TestTextKeepsNewlines(t *testing.T) {
	in := "diff --git a/x b/x\n+新行\t制表\n" + esc + "[32m+绿" + esc + "[0m"
	got := Text(in)
	if !strings.Contains(got, "\n+新行    制表\n") {
		t.Errorf("应保留换行并将制表展开为 4 空格: %q", got)
	}
	if strings.Contains(got, esc) {
		t.Errorf("应去除 ESC: %q", got)
	}
}

func TestBidiAndZeroWidth(t *testing.T) {
	in := "a‮b​c"
	got := Line(in)
	if got != "abc" {
		t.Errorf("应移除方向与零宽字符: %q", got)
	}
}

func TestUnclosedEscape(t *testing.T) {
	if got := Line("abc" + esc + "[31"); got != "abc" {
		t.Errorf("未闭合序列应被移除: %q", got)
	}
}

func TestTextExpandsTabs(t *testing.T) {
	got := Text("a\tb")
	if got != "a    b" {
		t.Errorf("tab 应展开为 4 空格: %q", got)
	}
}
