package main

import (
	"fmt"
	"testing"
	"time"

	"github.com/gdamore/tcell/v2"
	"github.com/rivo/tview"
	"ssl_assistant/utils"
)

// TestTUIModalInput：验证 TUI 模态输入钩子完整链路——
// ReadInput 弹出模态 → 注入文本 + 回车 → 钩子返回输入值。
// 这是全 TUI 重构的核心机制测试（业务函数输入走模态、不退出界面）。
func TestTUIModalInput(t *testing.T) {
	sim := tcell.NewSimulationScreen("UTF-8")
	if err := sim.Init(); err != nil {
		t.Fatalf("模拟屏幕初始化失败: %v", err)
	}
	sim.SetSize(100, 30)

	app := tview.NewApplication()
	app.SetScreen(sim)

	// 根布局（简单 TextView）
	root := tview.NewTextView().SetText("main page")
	pages := registerTUIInputHooks(app, root)
	app.SetRoot(pages, true)
	app.SetFocus(root)

	// 业务 goroutine：调用 utils.ReadInput（应走 TUI 模态）
	result := make(chan string, 1)
	go func() {
		result <- utils.ReadInput("请输入域名: ", "default.com")
	}()

	// 等待模态弹出，清空默认值后输入 "test" 并回车
	done := make(chan struct{})
	go func() {
		defer close(done)
		time.Sleep(500 * time.Millisecond)
		// 模态应已打开
		if !pages.HasPage("modal") {
			t.Error("模态未弹出")
		}
		// Ctrl+U 清空输入框（tview InputField 支持），再输入 "test"
		sim.InjectKey(tcell.KeyCtrlU, 0, tcell.ModCtrl)
		time.Sleep(50 * time.Millisecond)
		for _, r := range "test" {
			sim.InjectKey(tcell.KeyRune, r, tcell.ModNone)
			time.Sleep(30 * time.Millisecond)
		}
		time.Sleep(100 * time.Millisecond)
		sim.InjectKey(tcell.KeyEnter, 0, tcell.ModNone)
	}()

	runDone := make(chan struct{})
	go func() {
		if err := app.Run(); err != nil {
			t.Errorf("app.Run: %v", err)
		}
		close(runDone)
	}()

	// 等待 ReadInput 返回
	select {
	case v := <-result:
		if v != "test" {
			t.Errorf("ReadInput 返回 %q, want %q", v, "test")
		}
	case <-time.After(5 * time.Second):
		t.Fatal("ReadInput 模态输入超时（疑似死锁）")
	}

	// 关闭应用
	app.Stop()
	select {
	case <-runDone:
	case <-time.After(2 * time.Second):
		t.Fatal("应用未退出")
	}
	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("注入 goroutine 未完成")
	}
}

// TestTUIRunActionOutput：验证 runAction 的输出重定向——
// 操作函数打印的内容实时进入反馈区 TextView（不退出 TUI）。
func TestTUIRunActionOutput(t *testing.T) {
	sim := tcell.NewSimulationScreen("UTF-8")
	if err := sim.Init(); err != nil {
		t.Fatalf("模拟屏幕初始化失败: %v", err)
	}
	sim.SetSize(100, 30)

	app := tview.NewApplication()
	app.SetScreen(sim)
	feedback := tview.NewTextView().
		SetDynamicColors(true).
		SetScrollable(true)
	feedback.SetBorder(true).SetTitle(" 操作输出 ")

	root := tview.NewFlex().SetDirection(tview.FlexRow).
		AddItem(feedback, 0, 1, true)
	app.SetRoot(root, true)
	app.SetFocus(feedback)

	// 启动事件循环（QueueUpdateDraw 需要它执行）
	runDone := make(chan struct{})
	go func() {
		_ = app.Run()
		close(runDone)
	}()

	done := make(chan struct{})
	runAction(app, feedback, "测试操作", func() {
		fmt.Println("第一行输出")
		fmt.Println("第二行输出")
		close(done)
	}, nil)

	// 等待操作完成 + 输出渲染（pipe → goroutine → QueueUpdateDraw 需要时间）
	select {
	case <-done:
	case <-time.After(5 * time.Second):
		t.Fatal("操作未完成")
	}
	// 等待事件循环处理 QueueUpdateDraw（轮询最多 3 秒）
	deadline := time.Now().Add(3 * time.Second)
	for {
		if contains(feedback.GetText(true), "第二行输出") {
			break
		}
		if time.Now().After(deadline) {
			break
		}
		time.Sleep(50 * time.Millisecond)
	}

	// 检查反馈区内容
	text := feedback.GetText(true)
	if !contains(text, "测试操作") {
		t.Errorf("反馈区缺少标题: %q", text)
	}
	if !contains(text, "第一行输出") {
		t.Errorf("反馈区缺少操作输出: %q", text)
	}
	if !contains(text, "第二行输出") {
		t.Errorf("反馈区缺少第二行输出: %q", text)
	}

	app.Stop()
	select {
	case <-runDone:
	case <-time.After(2 * time.Second):
		t.Fatal("应用未退出")
	}
}

// TestTUIModalPassword：验证密码模态（掩码显示）返回输入值
func TestTUIModalPassword(t *testing.T) {
	sim := tcell.NewSimulationScreen("UTF-8")
	if err := sim.Init(); err != nil {
		t.Fatalf("模拟屏幕初始化失败: %v", err)
	}
	sim.SetSize(100, 30)

	app := tview.NewApplication()
	app.SetScreen(sim)
	root := tview.NewTextView().SetText("main")
	pages := registerTUIInputHooks(app, root)
	app.SetRoot(pages, true)
	app.SetFocus(root)

	result := make(chan string, 1)
	go func() {
		result <- utils.ReadPassword("请输入密钥: ")
	}()

	runDone := make(chan struct{})
	go func() {
		_ = app.Run()
		close(runDone)
	}()

	go func() {
		time.Sleep(500 * time.Millisecond)
		if !pages.HasPage("modal") {
			t.Error("密码模态未弹出")
		}
		for _, r := range "secret123" {
			sim.InjectKey(tcell.KeyRune, r, tcell.ModNone)
			time.Sleep(30 * time.Millisecond)
		}
		time.Sleep(100 * time.Millisecond)
		sim.InjectKey(tcell.KeyEnter, 0, tcell.ModNone)
	}()

	select {
	case v := <-result:
		if v != "secret123" {
			t.Errorf("密码模态返回 %q, want %q", v, "secret123")
		}
	case <-time.After(5 * time.Second):
		t.Fatal("密码模态超时")
	}

	app.Stop()
	select {
	case <-runDone:
	case <-time.After(2 * time.Second):
		t.Fatal("应用未退出")
	}
}

// TestTUIModalPasswordEmptyEnter：密码模态空回车返回空字符串（与 CLI ReadPassword 语义一致）
func TestTUIModalPasswordEmptyEnter(t *testing.T) {
	sim := tcell.NewSimulationScreen("UTF-8")
	if err := sim.Init(); err != nil {
		t.Fatalf("模拟屏幕初始化失败: %v", err)
	}
	sim.SetSize(100, 30)

	app := tview.NewApplication()
	app.SetScreen(sim)
	root := tview.NewTextView().SetText("main")
	pages := registerTUIInputHooks(app, root)
	app.SetRoot(pages, true)
	app.SetFocus(root)

	result := make(chan string, 1)
	go func() {
		result <- utils.ReadPassword("请输入密钥: ")
	}()

	runDone := make(chan struct{})
	go func() {
		_ = app.Run()
		close(runDone)
	}()

	go func() {
		time.Sleep(500 * time.Millisecond)
		if !pages.HasPage("modal") {
			t.Error("密码模态未弹出")
		}
		// 直接回车（空值）
		sim.InjectKey(tcell.KeyEnter, 0, tcell.ModNone)
	}()

	select {
	case v := <-result:
		if v != "" {
			t.Errorf("空回车应返回空字符串，实际 %q", v)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("密码模态超时")
	}

	app.Stop()
	select {
	case <-runDone:
	case <-time.After(2 * time.Second):
		t.Fatal("应用未退出")
	}
}

// TestTUIModalConfirm：确认框——默认焦点在"是"，Enter 返回 true（所见即所得）
func TestTUIModalConfirmYes(t *testing.T) {
	sim := tcell.NewSimulationScreen("UTF-8")
	if err := sim.Init(); err != nil {
		t.Fatalf("模拟屏幕初始化失败: %v", err)
	}
	sim.SetSize(100, 30)

	app := tview.NewApplication()
	app.SetScreen(sim)
	root := tview.NewTextView().SetText("main")
	pages := registerTUIInputHooks(app, root)
	app.SetRoot(pages, true)
	app.SetFocus(root)

	result := make(chan bool, 1)
	go func() {
		result <- utils.Confirm("确认删除证书?")
	}()

	runDone := make(chan struct{})
	go func() {
		_ = app.Run()
		close(runDone)
	}()

	go func() {
		time.Sleep(500 * time.Millisecond)
		if !pages.HasPage("modal") {
			t.Error("确认框未弹出")
		}
		// 默认焦点"是"，回车返回 true
		sim.InjectKey(tcell.KeyEnter, 0, tcell.ModNone)
	}()

	select {
	case v := <-result:
		if !v {
			t.Error("确认框 Enter 应返回 true（默认焦点是）")
		}
	case <-time.After(5 * time.Second):
		t.Fatal("确认框超时")
	}

	app.Stop()
	select {
	case <-runDone:
	case <-time.After(2 * time.Second):
		t.Fatal("应用未退出")
	}
}

// TestTUIModalConfirmNo：确认框——Tab 切到"否"后 Enter 返回 false（所见即所得）
func TestTUIModalConfirmNo(t *testing.T) {
	sim := tcell.NewSimulationScreen("UTF-8")
	if err := sim.Init(); err != nil {
		t.Fatalf("模拟屏幕初始化失败: %v", err)
	}
	sim.SetSize(100, 30)

	app := tview.NewApplication()
	app.SetScreen(sim)
	root := tview.NewTextView().SetText("main")
	pages := registerTUIInputHooks(app, root)
	app.SetRoot(pages, true)
	app.SetFocus(root)

	result := make(chan bool, 1)
	go func() {
		result <- utils.Confirm("确认删除证书?")
	}()

	runDone := make(chan struct{})
	go func() {
		_ = app.Run()
		close(runDone)
	}()

	go func() {
		time.Sleep(500 * time.Millisecond)
		if !pages.HasPage("modal") {
			t.Error("确认框未弹出")
		}
		// Tab 切到"否"按钮，回车返回 false
		sim.InjectKey(tcell.KeyTab, 0, tcell.ModNone)
		time.Sleep(100 * time.Millisecond)
		sim.InjectKey(tcell.KeyEnter, 0, tcell.ModNone)
	}()

	select {
	case v := <-result:
		if v {
			t.Error("Tab 到否后 Enter 应返回 false（所见即所得）")
		}
	case <-time.After(5 * time.Second):
		t.Fatal("确认框超时")
	}

	app.Stop()
	select {
	case <-runDone:
	case <-time.After(2 * time.Second):
		t.Fatal("应用未退出")
	}
}
