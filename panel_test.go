package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// 小皮面板（phpstudy）目录探测：版本号可变、多版本共存、含 Apache、非 Web 组件忽略
func TestDiscoverPhpstudyFromRoot(t *testing.T) {
	root := t.TempDir()
	ext := filepath.Join(root, "Extensions")
	os.MkdirAll(filepath.Join(ext, "Nginx1.15.11", "conf", "vhosts"), 0755)
	os.MkdirAll(filepath.Join(ext, "Nginx1.18.0", "conf", "vhosts"), 0755)
	os.MkdirAll(filepath.Join(ext, "Apache2.4.39", "conf", "vhosts"), 0755)
	os.MkdirAll(filepath.Join(ext, "MySQL5.7"), 0755)

	paths := discoverPhpstudyFromRoot(root)
	if len(paths) != 3 {
		t.Fatalf("应探测到 3 个 vhosts 路径（Nginx×2 + Apache×1），实际 %d: %v", len(paths), paths)
	}
	for _, p := range paths {
		if !strings.Contains(p, "vhosts") || !strings.Contains(p, "*.conf") {
			t.Fatalf("路径格式错误: %s", p)
		}
	}
}

// phpstudy 根目录不存在时返回空（不影响原有扫描）
func TestDiscoverPhpstudyFromRootMissing(t *testing.T) {
	if paths := discoverPhpstudyFromRoot(filepath.Join(t.TempDir(), "nope")); len(paths) != 0 {
		t.Fatalf("不存在的根目录应返回空，实际 %v", paths)
	}
}
