package utils

import (
	"fmt"
	"os"
	"testing"
)

// TestMenuGridSize：平铺网格行列数与列宽计算
func TestMenuGridSize(t *testing.T) {
	items := []string{"初始化程序", "添加证书", "删除证书", "查看证书"}
	cols, rows, itemWidth := menuGridSize(items, 80)
	if cols != 4 {
		t.Errorf("80列下 cols = %d, want 4", cols)
	}
	if rows != 1 {
		t.Errorf("80列下 rows = %d, want 1", rows)
	}
	if itemWidth <= 0 {
		t.Errorf("itemWidth = %d, want > 0", itemWidth)
	}
	// 空列表兜底
	if c, r, _ := menuGridSize(nil, 80); c != 1 || r != 0 {
		t.Errorf("空列表应返回 cols=1 rows=0，实际 %d/%d", c, r)
	}
}

// TestCanvasLayout：画布三区布局不重叠、行号递增
func TestCanvasLayout(t *testing.T) {
	items := []string{"初始化程序", "添加证书", "删除证书", "查看证书", "更新证书"}
	cv := &Canvas{width: 100, height: 24}
	cv.layout(items)
	if !(cv.listTop < cv.menuTop && cv.menuTop < cv.outTop) {
		t.Errorf("区域行号应递增: listTop=%d menuTop=%d outTop=%d", cv.listTop, cv.menuTop, cv.outTop)
	}
	if cv.listH < 1 || cv.menuH < 2 || cv.outH < 3 {
		t.Errorf("各区域高度过小: listH=%d menuH=%d outH=%d", cv.listH, cv.menuH, cv.outH)
	}
	if cv.outTop+cv.outH-1 > cv.height {
		t.Errorf("反馈区超出屏幕: outTop=%d outH=%d height=%d", cv.outTop, cv.outH, cv.height)
	}
}

// TestCanvasSmallHeight：小高度终端布局不越界
func TestCanvasSmallHeight(t *testing.T) {
	items := []string{"初始化程序", "添加证书", "删除证书", "查看证书", "更新证书", "退出"}
	cv := &Canvas{width: 80, height: 12}
	cv.layout(items)
	if cv.outTop+cv.outH-1 > cv.height {
		t.Errorf("小高度终端反馈区越界: outTop=%d outH=%d height=%d", cv.outTop, cv.outH, cv.height)
	}
	if cv.outH < 1 {
		t.Errorf("反馈区高度应至少 1 行，实际 %d", cv.outH)
	}
	if cv.menuTop < cv.listTop && cv.listH > 0 {
		t.Errorf("列表区与菜单区重叠: listTop=%d listH=%d menuTop=%d", cv.listTop, cv.listH, cv.menuTop)
	}
}

// TestCanvasTinyHeight：极小高度终端（如 8 行）不越界、不重叠
func TestCanvasTinyHeight(t *testing.T) {
	items := []string{"初始化程序", "添加证书", "删除证书", "查看证书", "更新证书", "退出"}
	for _, h := range []int{6, 8, 10} {
		cv := &Canvas{width: 80, height: h}
		cv.layout(items)
		if cv.menuTop < 1 || cv.outTop < cv.menuTop {
			t.Errorf("height=%d 布局错误: menuTop=%d outTop=%d", h, cv.menuTop, cv.outTop)
		}
		if cv.outTop+cv.outH-1 > cv.height {
			t.Errorf("height=%d 反馈区越界: outTop=%d outH=%d", h, cv.outTop, cv.outH)
		}
		// 列表区与菜单区不重叠（允许列表区高度为 0）
		if cv.listH > 0 && cv.listTop+cv.listH-1 >= cv.menuTop {
			t.Errorf("height=%d 列表区与菜单区重叠: listTop=%d listH=%d menuTop=%d", h, cv.listTop, cv.listH, cv.menuTop)
		}
	}
}

// TestCanvasBeginEndOutput：BeginOutput 重定向 os.Stdout，写入内容进入反馈区，
// EndOutput 恢复并完成管道同步
func TestCanvasBeginEndOutput(t *testing.T) {	cv := &Canvas{width: 80, height: 24, termOut: newFakeFile(t)}
	cv.layout([]string{"初始化程序", "退出"})
	cv.outH = 4
	cv.ResetFeedback()

	cv.BeginOutput()
	fmt.Fprint(os.Stdout, "hello\nworld\n")
	cv.EndOutput()

	if len(cv.outLines) != 2 {
		t.Fatalf("反馈区应有 2 行，实际 %d: %v", len(cv.outLines), cv.outLines)
	}
	if cv.outLines[0] != "hello" || cv.outLines[1] != "world" {
		t.Errorf("反馈区内容错误: %v", cv.outLines)
	}
	// EndOutput 后 os.Stdout 应恢复为原值
	if os.Stdout == cv.outPipeW {
		t.Error("EndOutput 后 os.Stdout 未恢复")
	}
}

// TestCanvasRunAction：RunAction 标题进入反馈区且不破坏主区/菜单区
func TestCanvasRunAction(t *testing.T) {
	cv := &Canvas{width: 80, height: 24, termOut: newFakeFile(t)}
	cv.layout([]string{"初始化程序", "退出"})
	cv.outH = 4
	cv.ResetFeedback()

	called := false
	cv.RunAction("测试操作", func() {
		called = true
		fmt.Println("输出内容")
	})
	if !called {
		t.Fatal("fn 未被执行")
	}
	// 标题行存在
	if len(cv.outLines) == 0 || cv.outLines[0] != "══════ 测试操作 ══════" {
		t.Errorf("反馈区首行应为标题，实际: %v", cv.outLines)
	}
	// 操作输出进入反馈区
	found := false
	for _, l := range cv.outLines {
		if l == "输出内容" {
			found = true
			break
		}
	}
	if !found {
		t.Errorf("操作输出未进入反馈区: %v", cv.outLines)
	}
	// EndOutput 已被 defer 调用：os.Stdout 恢复
	if os.Stdout == cv.outPipeW {
		t.Error("RunAction 结束后 os.Stdout 未恢复")
	}
}

// TestCanvasFeedbackWrite：反馈区写入按行缓冲并滚动（丢弃最旧行）
func TestCanvasFeedbackWrite(t *testing.T) {
	cv := &Canvas{width: 80, height: 24, termOut: newFakeFile(t)}
	cv.layout([]string{"初始化程序", "退出"})
	cv.outH = 3 // 强制小反馈区验证滚动
	cv.ResetFeedback()
	cv.Write([]byte("line1\nline2\nline3\nline4\n"))
	if len(cv.outLines) != 3 {
		t.Fatalf("滚动后应保留 3 行，实际 %d: %v", len(cv.outLines), cv.outLines)
	}
	if cv.outLines[0] != "line2" {
		t.Errorf("最旧行应被丢弃，首行 = %q, want line2", cv.outLines[0])
	}
	if cv.outLines[2] != "line4" {
		t.Errorf("末行 = %q, want line4", cv.outLines[2])
	}
}

// TestCanvasFeedbackCRLF：\r\n 兼容，\r 不产生空行
func TestCanvasFeedbackCRLF(t *testing.T) {
	cv := &Canvas{width: 80, height: 24, termOut: newFakeFile(t)}
	cv.layout([]string{"初始化程序", "退出"})
	cv.ResetFeedback()
	cv.Write([]byte("a\r\nb\r\n"))
	if len(cv.outLines) != 2 {
		t.Fatalf("CRLF 应产生 2 行，实际 %d: %v", len(cv.outLines), cv.outLines)
	}
	if cv.outLines[0] != "a" || cv.outLines[1] != "b" {
		t.Errorf("CRLF 解析错误: %v", cv.outLines)
	}
}

// TestTruncateWidth：按显示宽度截断（CJK 占 2 列）
func TestTruncateWidth(t *testing.T) {
	if got := truncateWidth("初始化程序 abc", 6); got != "初始化" {
		t.Errorf("truncateWidth 截断错误: %q", got)
	}
	if got := truncateWidth("初始化程序 abc", 8); got != "初始化程" {
		t.Errorf("truncateWidth 截断错误: %q", got)
	}
	if got := truncateWidth("abc", 10); got != "abc" {
		t.Errorf("未超宽不应截断: %q", got)
	}
}

// newFakeFile 返回一个可写但不落盘的 *os.File（os.Pipe 的写端），
// 避免单测向真实终端写 ANSI 序列。
func newFakeFile(t *testing.T) *os.File {
	t.Helper()
	_, w, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	return w
}

// TestMultiSelectWindowScroll：画布版多选窗口滚动——高亮移出窗口时窗口跟随。
// 直接测试窗口调整纯函数 adjustWindow（multiSelectKeyNavOnCanvas 的 render 使用同一函数）。
func TestMultiSelectWindowScroll(t *testing.T) {
	visible := 2
	items := 10

	cur, winStart := 0, 0
	// 向下移动到末尾：窗口跟随
	for i := 0; i < items-1; i++ {
		cur++
		winStart = adjustWindow(winStart, cur, visible)
	}
	if winStart != items-visible {
		t.Errorf("滚动到底后 winStart = %d, want %d", winStart, items-visible)
	}
	// 向上回到顶部：窗口回退
	for i := 0; i < items-1; i++ {
		cur--
		winStart = adjustWindow(winStart, cur, visible)
	}
	if winStart != 0 {
		t.Errorf("滚动到顶后 winStart = %d, want 0", winStart)
	}
	// 窗口内移动不滚动
	winStart = adjustWindow(3, 4, visible)
	if winStart != 3 {
		t.Errorf("窗口内右移不应滚动: winStart = %d, want 3", winStart)
	}
	// 环绕场景：顶部按 ↑ 环绕到末尾时窗口跳到底部
	winStart = adjustWindow(0, items-1, visible)
	if winStart != items-visible {
		t.Errorf("环绕到底后 winStart = %d, want %d", winStart, items-visible)
	}
	// 环绕回顶部
	winStart = adjustWindow(items-visible, 0, visible)
	if winStart != 0 {
		t.Errorf("环绕到顶后 winStart = %d, want 0", winStart)
	}
}
