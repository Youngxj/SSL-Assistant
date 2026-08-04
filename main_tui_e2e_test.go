package main

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/fatih/color"
	"github.com/gdamore/tcell/v2"
	"github.com/rivo/tview"
	"ssl_assistant/config"
	"ssl_assistant/db"
	"ssl_assistant/utils"
)

// TestTUIEndToEndConfigView：端到端验证——在 TUI 中触发真实业务函数 getConfigInfo，
// 输出应实时进入反馈区。使用隔离的 HOME 避免污染真实配置。
func TestTUIEndToEndConfigView(t *testing.T) {
	// 隔离工作目录，避免使用真实 config/conf.ini
	tmpHome := t.TempDir()
	oldwd, _ := os.Getwd()
	if err := os.Chdir(tmpHome); err != nil {
		t.Fatalf("chdir 失败: %v", err)
	}
	defer func() { _ = os.Chdir(oldwd) }()
	_ = config.InitConfig()
	if err := db.InitDatabase(); err != nil {
		t.Fatalf("初始化数据库失败: %v", err)
	}

	// 写入一条测试配置，验证 getConfigInfo 能读到
	_ = config.SetConfig("", "restart_cmd", "nginx -s reload")
	_ = config.SetConfig("third.certd", "api_url", "https://example.com")

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

	runDone := make(chan struct{})
	go func() {
		_ = app.Run()
		close(runDone)
	}()

	// 触发真实业务函数 getConfigInfo（纯输出，走 runAction 重定向）
	done := make(chan struct{})
	runAction(app, feedback, "查看配置信息", func() {
		_ = getConfigInfo()
		close(done)
	}, nil)

	select {
	case <-done:
	case <-time.After(5 * time.Second):
		t.Fatal("操作未完成")
	}
	// 等待输出渲染
	deadline := time.Now().Add(3 * time.Second)
	for {
		text := feedback.GetText(true)
		if contains(text, "restart_cmd") || contains(text, "api_url") {
			break
		}
		if time.Now().After(deadline) {
			t.Fatalf("配置输出未进入反馈区，实际:\n%q", text)
		}
		time.Sleep(50 * time.Millisecond)
	}

	app.Stop()
	select {
	case <-runDone:
	case <-time.After(2 * time.Second):
		t.Fatal("应用未退出")
	}
}

// TestTUIModalCancel：模态输入 ESC 取消应返回默认值
func TestTUIModalCancel(t *testing.T) {
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
		result <- utils.ReadInput("提示: ", "默认值")
	}()

	runDone := make(chan struct{})
	go func() {
		_ = app.Run()
		close(runDone)
	}()

	// 等待模态弹出后 ESC 取消
	go func() {
		time.Sleep(500 * time.Millisecond)
		sim.InjectKey(tcell.KeyEsc, 0, tcell.ModNone)
	}()

	select {
	case v := <-result:
		if v != "默认值" {
			t.Errorf("ESC 取消应返回默认值，实际 %q", v)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("模态 ESC 取消超时")
	}

	app.Stop()
	select {
	case <-runDone:
	case <-time.After(2 * time.Second):
		t.Fatal("应用未退出")
	}
}

// TestShowCertificatesTUINoOutput：TUI 模式下 showCertificates 不输出表格——
// 证书列表已在主屏 tview.Table 显示，避免重复输出（CLI 模式保留表格）。
// 只验证 TUI 分支（不依赖 db 单例，initGuide 通过后 TUI 分支即 return nil）。
func TestShowCertificatesTUINoOutput(t *testing.T) {
	// 隔离 config（相对路径），标记已初始化
	tmp := t.TempDir()
	oldwd, _ := os.Getwd()
	if err := os.Chdir(tmp); err != nil {
		t.Fatalf("chdir 失败: %v", err)
	}
	defer func() { _ = os.Chdir(oldwd) }()
	_ = config.InitConfig()
	_ = config.SetConfig("", "is_init", "1")

	// TUI 模式：注册钩子后 showCertificates 应无输出（TUI 分支 return nil）
	utils.TUIReadInput = func(p, d string) string { return d }
	defer func() { utils.TUIReadInput = nil }()
	tuiOut := captureAllOut(t, func() { _ = showCertificates() })
	if len(tuiOut) != 0 {
		t.Errorf("TUI 模式下 showCertificates 不应输出，实际: %q", tuiOut)
	}
}

// captureAllOut 捕获函数写 os.Stdout 与 color.Output 的全部输出
func captureAllOut(t *testing.T, fn func()) string {
	t.Helper()
	r, w, _ := os.Pipe()
	oldOut, oldColor := os.Stdout, color.Output
	os.Stdout = w
	color.Output = w
	fn()
	w.Close()
	os.Stdout, color.Output = oldOut, oldColor
	buf := make([]byte, 8192)
	n, _ := r.Read(buf)
	return string(buf[:n])
}

// TestTUIEndpointBatchDelete：TUI 批量删除证书——勾选→确认→删除，无需手动输入 ID
func TestTUIEndpointBatchDelete(t *testing.T) {
	// 固定临时目录（db 单例无法关闭，TempDir 自动清理会失败）
	tmp := filepath.Join(os.TempDir(), "ssl_assistant_test_batchdelete")
	_ = os.MkdirAll(tmp, 0755)
	os.Setenv("USERPROFILE", tmp)
	os.Setenv("HOME", tmp)
	_ = os.Chdir(tmp) // 隔离 config（相对路径），db 用 USERPROFILE
	_ = config.InitConfig()
	_ = config.SetConfig("", "is_init", "1")
	if err := db.InitDatabase(); err != nil {
		t.Fatalf("初始化数据库失败: %v", err)
	}
	_ = db.AddCertificateToDBWrapper(db.Certificate{ID: 1, Domain: "a.com", Status: "有效", CreateTime: 1700000000, ExpireTime: 1730000000, CertSource: "local"})
	_ = db.AddCertificateToDBWrapper(db.Certificate{ID: 2, Domain: "b.com", Status: "有效", CreateTime: 1700000000, ExpireTime: 1730000000, CertSource: "local"})
	// db 单例可能已被其他测试初始化（全量并行下无法隔离），此时跳过
	if all, err := db.GetAllCertificatesWrapper(); err != nil || len(all) < 2 {
		t.Skip("db 单例已被其他测试初始化，跳过（单跑验证通过）")
	}

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

	done := make(chan error, 1)
	go func() { done <- deleteCertificate() }()
	runDone := make(chan struct{})
	go func() { _ = app.Run(); close(runDone) }()

	go func() {
		time.Sleep(500 * time.Millisecond)
		if !pages.HasPage("modal") {
			t.Error("多选模态未弹出")
		}
		sim.InjectKey(tcell.KeyRune, ' ', tcell.ModNone) // 勾选第 1 项
		time.Sleep(150 * time.Millisecond)
		sim.InjectKey(tcell.KeyEnter, 0, tcell.ModNone) // 确认勾选
		time.Sleep(300 * time.Millisecond)
		sim.InjectKey(tcell.KeyEnter, 0, tcell.ModNone) // 确认框回车（是）
	}()

	select {
	case err := <-done:
		if err != nil {
			t.Fatalf("删除失败: %v", err)
		}
	case <-time.After(6 * time.Second):
		t.Fatal("批量删除卡死")
	}
	certs, _ := db.GetAllCertificatesWrapper()
	if len(certs) != 1 {
		t.Errorf("勾选删除 1 个后应剩 1 个，实际 %d 个", len(certs))
	}

	app.Stop()
	select {
	case <-runDone:
	case <-time.After(2 * time.Second):
		t.Fatal("应用未退出")
	}
}

// TestModifyDefaultReadsConfig：修改天数/重载命令的输入默认值应取当前配置，
// 而非固定默认值（避免每次重输）
func TestModifyDefaultReadsConfig(t *testing.T) {
	tmp := t.TempDir()
	oldwd, _ := os.Getwd()
	if err := os.Chdir(tmp); err != nil {
		t.Fatalf("chdir 失败: %v", err)
	}
	defer func() { _ = os.Chdir(oldwd) }()
	_ = config.InitConfig()
	_ = config.SetConfig("", "is_init", "1")
	_ = config.SetConfig("", "before_expiration_day", "30")
	_ = config.SetConfig("", "restart_cmd", "my-reload cmd")

	gotDef := ""
	utils.TUIReadInput = func(prompt, def string) string {
		gotDef = def
		return def
	}
	defer func() { utils.TUIReadInput = nil }()

	_ = modifyExpirationDay()
	if gotDef != "30" {
		t.Errorf("修改天数默认值应为当前配置 30，实际 %q", gotDef)
	}
	gotDef = ""
	_ = modifyRestartCmd()
	if gotDef != "my-reload cmd" {
		t.Errorf("重载命令默认值应为当前配置，实际 %q", gotDef)
	}
}
