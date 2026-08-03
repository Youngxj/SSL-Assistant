package main

import "testing"

// TestComputeMenuCols：平铺列数按终端宽度自适应且不超项数
func TestComputeMenuCols(t *testing.T) {
	items := []string{"初始化程序", "添加证书", "删除证书", "查看证书", "更新证书", "退出"}
	// 宽终端列数 >= 窄终端
	if cols80, cols100 := computeMenuCols(items, 80), computeMenuCols(items, 100); cols100 < cols80 {
		t.Errorf("100列应 >= 80列: %d/%d", cols100, cols80)
	}
	if cols := computeMenuCols(items, 20); cols != 1 {
		t.Errorf("极窄终端应 1 列: %d", cols)
	}
	if cols := computeMenuCols(items, 10000); cols != len(items) {
		t.Errorf("极宽终端最多 len(items) 列: %d", cols)
	}
	// 空列表兜底
	if cols := computeMenuCols(nil, 80); cols != 1 {
		t.Errorf("空列表应 1 列: %d", cols)
	}
}

// TestDisplayWidthForMenu：CJK 全角按 2 列计算
func TestDisplayWidthForMenu(t *testing.T) {
	cases := []struct {
		in   string
		want int
	}{
		{"初始化程序", 10},
		{"abc", 3},
		{"a中文b", 6},
	}
	for _, c := range cases {
		if got := displayWidthForMenu(c.in); got != c.want {
			t.Errorf("displayWidthForMenu(%q)=%d, want %d", c.in, got, c.want)
		}
	}
}

// TestMenuExitIndex：两平台下"退出"恒为 index 13（Linux 末尾追加"查看任务"不影响）
func TestMenuExitIndex(t *testing.T) {
	items := []string{
		"初始化程序", "添加证书", "删除证书", "查看证书", "更新证书",
		"快速添加域名", "证书更新任务", "修改密钥", "修改重载命令",
		"修改提前更新天数", "查看配置信息", "显示版本信息", "检查更新", "退出",
	}
	// Windows：14 项，"退出"是最后一项
	if items[13] != "退出" {
		t.Fatalf("Windows 退出索引应为 13，实际 items[13]=%q", items[13])
	}
	// Linux：末尾追加"查看任务"，"退出"仍在 index 13
	linux := append(append([]string{}, items...), "查看任务")
	if linux[13] != "退出" {
		t.Fatalf("Linux 退出索引应为 13，实际 linux[13]=%q", linux[13])
	}
	if linux[14] != "查看任务" {
		t.Fatalf("Linux 查看任务索引应为 14，实际 linux[14]=%q", linux[14])
	}
	// 菜单项索引唯一（无重复）
	seen := map[string]int{}
	for i, it := range linux {
		if j, ok := seen[it]; ok {
			t.Fatalf("菜单项 %q 重复（index %d 与 %d）", it, j, i)
		}
		seen[it] = i
	}
}
