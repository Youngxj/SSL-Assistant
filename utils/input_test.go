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
