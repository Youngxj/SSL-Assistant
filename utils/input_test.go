package utils

import (
	"os"
	"testing"
)

// 应用模式方向键 \x1bOA 应归一为 \x1b[A（↑），保证方向键统一处理
func TestReadRawKeyAppMode(t *testing.T) {
	r, w, _ := os.Pipe()
	os.Stdin = r
	w.Write([]byte("\x1bOA"))
	w.Close()
	key := readRawKey()
	if len(key) != 3 || key[1] != '[' || key[2] != 'A' {
		t.Fatalf("应用模式 \\x1bOA 应归一为 \\x1b[A，实际 %q", key)
	}
}

// VT 模式方向键 \x1b[B（↓）原样读取
func TestReadRawKeyVTMode(t *testing.T) {
	r, w, _ := os.Pipe()
	os.Stdin = r
	w.Write([]byte("\x1b[B"))
	w.Close()
	key := readRawKey()
	if len(key) != 3 || key[1] != '[' || key[2] != 'B' {
		t.Fatalf("VT 模式 \\x1b[B 读取错误，实际 %q", key)
	}
}

// 普通按键原样读取
func TestReadRawKeyNormal(t *testing.T) {
	r, w, _ := os.Pipe()
	os.Stdin = r
	w.Write([]byte("x"))
	w.Close()
	key := readRawKey()
	if len(key) != 1 || key[0] != 'x' {
		t.Fatalf("普通按键读取错误，实际 %q", key)
	}
}

// displayWidth：ASCII 按 1 列、CJK 全角按 2 列计算，用于平铺菜单对齐
func TestDisplayWidth(t *testing.T) {
	cases := []struct {
		in   string
		want int
	}{
		{"abc", 3},
		{"1. 初始化程序 (init)", 3 + 2*5 + 7}, // "1. " 3 半角 + 5 全角*2 + " (init)" 7 半角
		{"查看证书", 8},
		{"a中文b", 6},
	}
	for _, c := range cases {
		if got := displayWidth(c.in); got != c.want {
			t.Errorf("displayWidth(%q) = %d, want %d", c.in, got, c.want)
		}
	}
}

// menuGridCols：平铺列数不超过终端宽度（每项占 itemWidth+1 列含间距），至少 1 列、最多项数
func TestMenuGridCols(t *testing.T) {
	cases := []struct {
		termWidth, itemWidth, n int
		want                    int
	}{
		{80, 20, 15, 3}, // 3*21=63 <= 80，4*21=84 > 80
		{40, 20, 15, 1}, // 2*21=42 > 40
		{0, 20, 15, 1},  // 无终端宽度时兜底 1 列
		{80, 0, 15, 1},  // 无效项宽兜底 1 列
		{120, 15, 2, 2}, // 不超过项数
	}
	for _, c := range cases {
		got := menuGridCols(c.termWidth, c.itemWidth, c.n)
		if got != c.want {
			t.Errorf("menuGridCols(%d,%d,%d) = %d, want %d", c.termWidth, c.itemWidth, c.n, got, c.want)
		}
		if got < 1 || got > c.n {
			t.Errorf("menuGridCols(%d,%d,%d) = %d 越界 [1,%d]", c.termWidth, c.itemWidth, c.n, got, c.n)
		}
		// 整行宽度校验：cols*(itemWidth+1) <= termWidth（termWidth>0 且 itemWidth>0 时）
		if c.termWidth > 0 && c.itemWidth > 0 {
			if got*(c.itemWidth+1) > c.termWidth {
				t.Errorf("menuGridCols(%d,%d,%d) = %d 导致整行 %d 列超宽 %d", c.termWidth, c.itemWidth, c.n, got, got*(c.itemWidth+1), c.termWidth)
			}
		}
	}
}
