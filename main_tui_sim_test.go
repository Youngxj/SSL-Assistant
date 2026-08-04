package main

import (
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/gdamore/tcell/v2"
	"github.com/rivo/tview"
)

// TestTUIStartsAndExits：用模拟屏幕驱动完整 TUI——
// 启动后注入 ESC 应正常退出（不崩溃、不挂起）。
func TestTUIStartsAndExits(t *testing.T) {
	sim := tcell.NewSimulationScreen("UTF-8")
	if err := sim.Init(); err != nil {
		t.Fatalf("模拟屏幕初始化失败: %v", err)
	}
	sim.SetSize(100, 30)

	items := buildTestMenuItems()

	// 启动应用（后台 goroutine 注入 ESC 触发退出）
	app := tview.NewApplication()
	app.SetScreen(sim)
	_, _ = buildTestLayout(app, items)

	done := make(chan struct{})
	go func() {
		defer close(done)
		time.Sleep(300 * time.Millisecond) // 等待首帧绘制
		sim.InjectKey(tcell.KeyESC, ' ', tcell.ModNone)
	}()

	if err := app.Run(); err != nil {
		t.Fatalf("app.Run 返回错误: %v", err)
	}
	select {
	case <-done:
	case <-time.After(3 * time.Second):
		t.Fatal("注入 ESC 后应用未退出")
	}
	// 注意：tview app.Stop() 已内部 Fini 屏幕，此处不再调用 sim.Fini() 避免重复关闭
}

// TestTUIMenuNavigation：模拟屏幕驱动——方向键在平铺菜单间移动，
// 验证菜单单元格渲染与导航（选中高亮位置变化）。
func TestTUIMenuNavigation(t *testing.T) {
	sim := tcell.NewSimulationScreen("UTF-8")
	if err := sim.Init(); err != nil {
		t.Fatalf("模拟屏幕初始化失败: %v", err)
	}
	sim.SetSize(100, 30)

	items := buildTestMenuItems()

	app := tview.NewApplication()
	app.SetScreen(sim)
	menu, _ := buildTestLayout(app, items)

	done := make(chan struct{})
	go func() {
		defer close(done)
		time.Sleep(300 * time.Millisecond)

		// 初始选中 (0,0)（事件循环内查询，避免数据竞争）
		assertSel := func(wantR, wantC int, step string) {
			ch := make(chan struct{})
			app.QueueUpdateDraw(func() {
				r, c := menu.GetSelection()
				if r != wantR || c != wantC {
					t.Errorf("%s后应选中 (%d,%d)，实际 (%d,%d)", step, wantR, wantC, r, c)
				}
				close(ch)
			})
			<-ch
		}
		assertSel(0, 0, "初始")

		// → 移动到 (0,1)
		sim.InjectKey(tcell.KeyRight, ' ', tcell.ModNone)
		time.Sleep(100 * time.Millisecond)
		assertSel(0, 1, "按 →")

		// ↓ 移动到下一行 (1,1)
		sim.InjectKey(tcell.KeyDown, ' ', tcell.ModNone)
		time.Sleep(100 * time.Millisecond)
		assertSel(1, 1, "按 ↓")

		// 连续移动到底部，验证不越界
		for i := 0; i < 20; i++ {
			sim.InjectKey(tcell.KeyRight, ' ', tcell.ModNone)
			sim.InjectKey(tcell.KeyDown, ' ', tcell.ModNone)
		}
		time.Sleep(150 * time.Millisecond)
		ch := make(chan struct{})
		app.QueueUpdateDraw(func() {
			r, c := menu.GetSelection()
			if r < 0 || c < 0 {
				t.Errorf("选中越界: (%d,%d)", r, c)
			}
			close(ch)
		})
		<-ch

		sim.InjectKey(tcell.KeyESC, ' ', tcell.ModNone)
	}()

	if err := app.Run(); err != nil {
		t.Fatalf("app.Run 返回错误: %v", err)
	}
	select {
	case <-done:
	case <-time.After(3 * time.Second):
		t.Fatal("注入 ESC 后应用未退出")
	}
	// 注意：tview app.Stop() 已内部 Fini 屏幕，此处不再调用 sim.Fini() 避免重复关闭
}

// TestTUIEnterOnExit：模拟屏幕驱动——选中"退出"菜单项并回车应退出应用。
// 用模拟屏实际尺寸（sim.Size()）计算 cols 与退出项位置，回车触发 SelectedFunc → app.Stop()。
func TestTUIEnterOnExit(t *testing.T) {
	sim := tcell.NewSimulationScreen("UTF-8")
	if err := sim.Init(); err != nil {
		t.Fatalf("模拟屏幕初始化失败: %v", err)
	}
	sim.SetSize(100, 30)

	items := buildTestMenuItems()

	app := tview.NewApplication()
	app.SetScreen(sim)
	menu, _ := buildTestLayout(app, items)

	exited := make(chan struct{})
	go func() {
		defer close(exited)
		time.Sleep(300 * time.Millisecond)

		// 首帧已画完，取实际生效尺寸计算退出项位置
		width, _ := sim.Size()
		cols := computeMenuCols(items, width)
		exitRow := (len(items) - 1) / cols
		exitCol := (len(items) - 1) % cols

		// 通过事件循环执行 Select/断言，避免与 UI 线程数据竞争
		done := make(chan struct{})
		app.QueueUpdateDraw(func() {
			menu.Select(exitRow, exitCol)
			r, c := menu.GetSelection()
			if r != exitRow || c != exitCol {
				t.Errorf("应选中退出项 (%d,%d)，实际 (%d,%d)（宽度=%d cols=%d）", exitRow, exitCol, r, c, width, cols)
			}
			close(done)
		})
		<-done
		// 回车触发 SelectedFunc（注入按键走正常事件流）
		sim.InjectKey(tcell.KeyEnter, 0, tcell.ModNone)
		time.Sleep(300 * time.Millisecond)
	}()

	if err := app.Run(); err != nil {
		t.Fatalf("app.Run 返回错误: %v", err)
	}
	select {
	case <-exited:
		// 注入 goroutine 完成（含 Enter 触发退出）
	case <-time.After(2 * time.Second):
		t.Fatal("选中退出项回车后应用未退出")
	}
	// 注意：tview app.Stop() 已内部 Fini 屏幕，此处不再调用 sim.Fini() 避免重复关闭
}

// TestTUIMenuRendered：验证菜单单元格文本确实渲染到屏幕（非仅"不崩溃"）。
func TestTUIMenuRendered(t *testing.T) {
	sim := tcell.NewSimulationScreen("UTF-8")
	if err := sim.Init(); err != nil {
		t.Fatalf("模拟屏幕初始化失败: %v", err)
	}
	sim.SetSize(100, 30)

	items := buildTestMenuItems()
	app := tview.NewApplication()
	app.SetScreen(sim)
	_, _ = buildTestLayout(app, items)

	rendered := make(chan struct{})
	go func() {
		defer close(rendered)
		time.Sleep(300 * time.Millisecond)

		cells, _, _ := sim.GetContents()
		// 拼接所有非空 cell 的文本（SimCell 无坐标字段，扁平拼接即可验证关键文本）
		text := ""
		for _, c := range cells {
			if len(c.Bytes) > 0 {
				text += string(c.Bytes)
			}
		}

		// 标题与全部 12 个菜单项应渲染
		wants := []string{"SSL Assistant 操作菜单", "初始化程序", "添加证书", "删除证书",
			"更新证书", "快速添加域名", "证书更新任务", "修改密钥", "修改重载命令",
			"修改提前更新天数", "查看配置信息", "版本与更新", "退出", "←/→/↑/↓"}
		for _, want := range wants {
			if !contains(text, want) {
				t.Errorf("屏幕内容缺少 %q，实际:\n%s", want, text)
			}
		}
		// 确认"查看证书"菜单项已移除（主界面即证书列表）
		if contains(text, "查看证书") {
			t.Errorf("不应存在「查看证书」菜单项（已移除）:\n%s", text)
		}

		sim.InjectKey(tcell.KeyESC, ' ', tcell.ModNone)
	}()

	if err := app.Run(); err != nil {
		t.Fatalf("app.Run 返回错误: %v", err)
	}
	select {
	case <-rendered:
	case <-time.After(3 * time.Second):
		t.Fatal("注入 ESC 后应用未退出")
	}
}

// contains 判断字符串是否包含子串
func contains(s, sub string) bool {
	return strings.Contains(s, sub)
}

// buildTestMenuItems 返回与 runInteractiveMenu 一致的菜单项（不含平台差异分支）
func buildTestMenuItems() []string {
	return []string{
		"初始化程序", "添加证书", "删除证书", "更新证书", "快速添加域名",
		"证书更新任务", "修改密钥", "修改重载命令", "修改提前更新天数",
		"查看配置信息", "版本与更新", "退出",
	}
}

// buildTestLayout 复刻 runInteractiveMenu 的 UI 构建（菜单 Table + 平铺）。
// 返回 (菜单Table, 平铺列数)。注意：操作执行（SelectedFunc → runActionSuspended）
// 在模拟屏幕下 Suspend 会失败，这里仅验证布局渲染与方向键导航，不触发真实操作。
func buildTestLayout(app *tview.Application, items []string) (*tview.Table, int) {
	menuCols := computeMenuCols(items, 80)

	title := tview.NewTextView().
		SetDynamicColors(true).
		SetTextAlign(tview.AlignCenter).
		SetText("[::b]SSL Assistant 操作菜单[::-]")

	listView := tview.NewTextView().
		SetDynamicColors(true).
		SetScrollable(true)
	listView.SetBorder(true).SetTitle(" 证书列表 ")

	status := tview.NewTextView().
		SetTextAlign(tview.AlignCenter).
		SetText("←/→/↑/↓ 移动  回车执行   ESC 退出")

	menu := tview.NewTable().SetSelectable(true, true)
	menuRows := func() int {
		return (len(items) + menuCols - 1) / menuCols
	}
	layout := tview.NewFlex().SetDirection(tview.FlexRow).
		AddItem(title, 1, 0, false).
		AddItem(listView, 0, 1, false).
		AddItem(menu, menuRows(), 0, true).
		AddItem(status, 1, 0, false)
	rebuild := func() {
		menu.Clear()
		cols := menuCols
		rows := (len(items) + cols - 1) / cols
		for r := 0; r < rows; r++ {
			for c := 0; c < cols; c++ {
				i := r*cols + c
				cell := tview.NewTableCell("")
				if i < len(items) {
					cell.SetText(tview.Escape(fmt.Sprintf("%2d. %s", i+1, items[i]))).
						SetAlign(tview.AlignLeft)
				} else {
					cell.SetSelectable(false)
				}
				menu.SetCell(r, c, cell)
			}
		}
		menu.SetSelectable(true, true)
		menu.Select(0, 0)
		layout.ResizeItem(menu, menuRows(), 0) // 与生产一致：重排后同步菜单高度
	}
	rebuild()

	// 与 runInteractiveMenu 一致的退出逻辑：选中"退出"项回车 → app.Stop()
	menu.SetSelectedFunc(func(row, col int) {
		idx := row*menuCols + col
		if idx >= 0 && idx < len(items) && items[idx] == "退出" {
			app.Stop()
		}
	})

	app.SetRoot(layout, true)
	app.SetFocus(menu) // 确保菜单 Table 获得焦点，Enter 才能触发 SelectedFunc
	// 与 runInteractiveMenu 一致：BeforeDraw 按真实宽度重排菜单列数
	app.SetBeforeDrawFunc(func(screen tcell.Screen) bool {
		if w, _ := screen.Size(); w > 0 {
			if cols := computeMenuCols(items, w); cols != menuCols {
				menuCols = cols
				rebuild()
			}
		}
		return false
	})
	app.SetInputCapture(func(event *tcell.EventKey) *tcell.EventKey {
		if event.Key() == tcell.KeyEsc {
			app.Stop()
			return nil
		}
		return event
	})
	return menu, menuCols
}
