package main

import (
	"bytes"
	"fmt"
	"github.com/fatih/color"
	"github.com/gdamore/tcell/v2"
	"github.com/rivo/tview"
	"github.com/spf13/cobra"
	"os"
	"runtime"
	"ssl_assistant/config"
	"ssl_assistant/db"
	"ssl_assistant/third/github"
	"ssl_assistant/utils"
)

var Version string

var rootCmd = &cobra.Command{
	Use:   "ssl_assistant",
	Short: "SSL 证书部署管理助手",
	Long: `SSL Assistant ` + displayVersion() + ` 是一个基于 Go 语言开发的跨平台证书部署管理助手，用于 SSL 远程证书拉取、自动更新部署与重载生效。

支持自动寻找 Nginx / Apache 配置文件（宝塔面板、1Panel、小皮面板），可配置计划任务定期更新证书，内置检查更新（checkupdate），并配备持续集成自动化测试。

不带参数直接运行将进入交互操作菜单（类似 Windows 双击）；带子命令运行适合脚本与计划任务。`,
	// 错误由 main() 统一打印并以非零码退出（避免 RunE 双重打印与 usage 刷屏）
	SilenceErrors: true,
	SilenceUsage:  true,
	Run: func(cmd *cobra.Command, args []string) {
		// 无子命令时：交互终端直接进入操作菜单（类似 Windows 双击）；非交互环境显示帮助
		if utils.IsInteractive() {
			runInteractiveMenu()
		} else {
			_ = cmd.Help()
		}
	},
}

var initCmd = &cobra.Command{
	Use:   "init",
	Short: "初始化程序",
	Long:  `初始化程序，设置证书信息获取的凭证和证书更新后需要执行的命令。`,
	Run: func(cmd *cobra.Command, args []string) {
		initConfig()
	},
}

var addCmd = &cobra.Command{
	Use:   "add",
	Short: "添加证书",
	Long:  `添加证书，输入域名，程序自动根据域名获取证书信息，并将证书信息保存到数据库中。`,
	RunE: func(cmd *cobra.Command, args []string) error {
		return addCertificate()
	},
}

var delCmd = &cobra.Command{
	Use:   "del",
	Short: "删除证书",
	Long:  `删除证书，输入证书 ID，程序自动删除对应的证书信息。`,
	RunE: func(cmd *cobra.Command, args []string) error {
		return deleteCertificate()
	},
}

var showCmd = &cobra.Command{
	Use:   "show",
	Short: "查看证书",
	Long:  `查看证书，显示证书信息的表格，包括 ID、域名、状态、创建时间、过期时间、公钥、私钥等信息。`,
	RunE: func(cmd *cobra.Command, args []string) error {
		return showCertificates()
	},
}

var updateCmd = &cobra.Command{
	Use:   "update",
	Short: "更新证书",
	Long:  `更新证书，程序自动获取所有证书信息，并将证书信息保存到数据库中，更新证书对应域名的证书文件内容，并执行重载命令。`,
	RunE: func(cmd *cobra.Command, args []string) error {
		return updateCertificates()
	},
}

var findCmd = &cobra.Command{
	Use:   "find",
	Short: "快速添加域名（Nginx/Apache目录检索）",
	Long:  `检索Nginx/Apache配置目录，程序会自动检索其中的证书配置，并将证书文件路径保存到数据库中，用于快速添加站点`,
	RunE: func(cmd *cobra.Command, args []string) error {
		if err := initGuide(true); err != nil {
			return err
		}
		return findNginxPathCmd()
	},
}

var cronCmd = &cobra.Command{
	Use:   "cron",
	Short: "证书更新任务\n\t-f 强制添加任务，覆盖已存在的任务。",
	Long:  `证书更新自动化任务，每日凌晨4点自动检测证书更新，并执行证书更新操作。`,
	RunE: func(cmd *cobra.Command, args []string) error {
		if err := initGuide(true); err != nil {
			return err
		}
		// 获取强制标志
		force, _ := cmd.Flags().GetBool("force")
		cronTask(force) // 传递强制参数给 cronTask 函数
		return nil
	},
}

// displayVersion 返回版本号，本地构建未注入时显示 dev
func displayVersion() string {
	if Version == "" {
		return "dev"
	}
	return Version
}

var versionCmd = &cobra.Command{
	Use:   "version",
	Short: "显示版本信息",
	Long:  `显示版本信息，包括程序名称、版本号、数据库模式与数据路径等。`,
	RunE: func(cmd *cobra.Command, args []string) error {
		fmt.Printf("SSL Assistant %s\n", displayVersion())
		fmt.Printf("项目地址: %s\n", "https://github.com/Youngxj/SSL-Assistant")
		// 初始化数据库以确定当前模式（无数据时自动创建目录）
		if err := db.InitDatabase(); err != nil {
			color.Yellow("数据库模式: 未知（初始化失败: %v）\n", err)
		} else {
			fmt.Printf("数据库模式: %s\n", db.DBMode())
			fmt.Printf("数据库路径: %s\n", db.DBPath())
		}
		return nil
	},
}

var checkUpdateCmd = &cobra.Command{
	Use:   "checkupdate",
	Short: "检查更新",
	Long:  `检查 SSL Assistant 是否有新版本，并输出下载地址（不自动下载更新）。网络不可达时也会给出下载页面地址。`,
	RunE: func(cmd *cobra.Command, args []string) error {
		return checkUpdate()
	},
}

func init() {
	// 禁用 cobra 的 Windows 双击默认行为（打印“This is a command line tool”后退出），
	// 双击启动改由 runInteractiveMenu 处理，提供交互菜单。
	cobra.MousetrapHelpText = ""

	// 添加子命令
	rootCmd.AddCommand(initCmd)
	rootCmd.AddCommand(addCmd)
	rootCmd.AddCommand(delCmd)
	rootCmd.AddCommand(showCmd)
	rootCmd.AddCommand(updateCmd)
	rootCmd.AddCommand(versionCmd)
	rootCmd.AddCommand(findCmd)
	rootCmd.AddCommand(cronCmd)
	rootCmd.AddCommand(checkUpdateCmd)
	cronCmd.Flags().BoolP("force", "f", false, "强制添加任务，覆盖已存在的任务")
}

func main() {
	// 初始化跨平台控制台输出（Windows 下切换 UTF-8 代码页，避免中文/emoji 乱码）
	utils.InitConsole()

	// 初始化配置文件
	err := config.InitConfig()
	if err != nil {
		fmt.Printf("初始化配置文件失败: %v\n", err)
		return
	}

	// Windows 下双击 exe 启动：进入交互菜单；带参数从 cmd 运行时照常执行子命令。
	if IsDoubleClick() {
		runInteractiveMenu()
		return
	}

	// 执行命令
	if err := rootCmd.Execute(); err != nil {
		fmt.Println(err)
		os.Exit(1)
	}
}

// runInteractiveMenu 双击/无参数运行时进入的交互菜单（基于 tview 成熟 TUI 框架）。
// 布局：上方证书列表（主区）、下方菜单平铺（辅区）、底部提示区。
// 选择操作后通过 Suspend/Resume 以普通终端模式执行操作函数（输出/输入均原生可用），
// 执行完回到 TUI 并刷新列表——框架自动处理方向键导航、窗口 resize、重绘。
func runInteractiveMenu() {
	// 菜单项按平台动态生成：
	// Windows 下 cron 常驻/查任务不适用，隐藏"查看任务"
	items := []string{
		"初始化程序",
		"添加证书",
		"删除证书",
		"查看证书",
		"更新证书",
		"快速添加域名",
		"证书更新任务",
		"修改密钥",
		"修改重载命令",
		"修改提前更新天数",
		"查看配置信息",
		"显示版本信息",
		"检查更新",
		"退出",
	}
	viewTaskIdx := -1
	if runtime.GOOS != "windows" {
		viewTaskIdx = len(items)
		items = append(items, "查看任务")
	}

	app := tview.NewApplication()

	// 平铺菜单列数（按终端宽度动态调整，闭包共享）
	menuCols := 1

	// 标题
	title := tview.NewTextView().
		SetDynamicColors(true).
		SetTextAlign(tview.AlignCenter).
		SetText("[::b]SSL Assistant 操作菜单[::-]")

	// 主区：证书列表（含 ANSI 颜色）
	listView := tview.NewTextView().
		SetDynamicColors(true).
		SetScrollable(true)
	listView.SetBorder(true).SetTitle(" 证书列表 ")
	refreshList := func() {
		listView.Clear()
		fmt.Fprint(tview.ANSIWriter(listView), captureCertListText())
	}

	// 反馈/提示区
	status := tview.NewTextView().
		SetTextAlign(tview.AlignCenter).
		SetText("←/→/↑/↓ 移动  回车执行   ESC 退出")

	// 菜单：Table 单元格平铺（方向键导航由 tview 原生支持）
	menu := tview.NewTable().
		SetSelectable(true, true).
		SetSelectedFunc(func(row, col int) {
			idx := row*menuCols + col
			if idx < 0 || idx >= len(items) {
				return
			}
			if idx == viewTaskIdx {
				runActionSuspended(app, "查看任务", func() {
					if err := initGuide(true); err != nil {
						return
					}
					cPid := checkTask()
					if cPid == "" {
						color.Red("任务不存在，可以通过命令添加任务：./SSL-Assistant cron &")
					} else {
						color.Green("当前任务PID: %s", cPid)
					}
				})
				refreshList()
				return
			}
			switch idx {
			case 0:
				runActionSuspended(app, "初始化程序", initConfig)
			case 1:
				runActionSuspended(app, "添加证书", func() { _ = addCertificate() })
			case 2:
				runActionSuspended(app, "删除证书", func() { _ = deleteCertificate() })
			case 3:
				runActionSuspended(app, "查看证书", func() { _ = showCertificates() })
			case 4:
				runActionSuspended(app, "更新证书", func() { _ = updateCertificates() })
			case 5:
				runActionSuspended(app, "快速添加域名", func() {
					if err := initGuide(true); err != nil {
						return
					}
					_ = findNginxPathCmd()
				})
			case 6:
				// Windows 下 cron 常驻进程不适用，引导使用任务计划程序；
				// Linux 下与 CLI `cron` 一致
				if runtime.GOOS == "windows" {
					runActionSuspended(app, "证书更新任务", func() {
						color.Yellow("Windows 环境请使用任务计划程序定期执行 update（参见 README「计划任务设置」），无需本工具常驻进程\n")
					})
				} else {
					runActionSuspended(app, "证书更新任务", func() {
						if err := initGuide(true); err != nil {
							return
						}
						cronTask(false)
					})
				}
			case 7:
				runActionSuspended(app, "修改密钥", modifyKey)
			case 8:
				runActionSuspended(app, "修改重载命令", func() { _ = modifyRestartCmd() })
			case 9:
				runActionSuspended(app, "修改提前更新天数", func() { _ = modifyExpirationDay() })
			case 10:
				runActionSuspended(app, "查看配置信息", func() { _ = getConfigInfo() })
			case 11:
				runActionSuspended(app, "显示版本信息", func() {
					fmt.Printf("SSL Assistant %s\n项目地址: https://github.com/Youngxj/SSL-Assistant\n", displayVersion())
				})
			case 12:
				runActionSuspended(app, "检查更新", func() { _ = checkUpdate() })
			default:
				// "退出"（index 13，两平台固定）与 Linux 的"查看任务"（已在上方处理）
				if items[idx] == "退出" {
					app.Stop()
					return
				}
				color.Yellow("已取消\n")
			}
			refreshList()
		})
	// 平铺菜单：按终端宽度计算列数（初始 80 列，tview 启动后再按实际宽度重排）
	menuCols = computeMenuCols(items, 80)
	// 菜单平铺行数（用于 Flex 布局固定高度）
	menuRows := func() int {
		return (len(items) + menuCols - 1) / menuCols
	}

	// 布局：标题 + 列表（弹性）+ 菜单（固定高度=菜单行数）+ 状态
	layout := tview.NewFlex().SetDirection(tview.FlexRow).
		AddItem(title, 1, 0, false).
		AddItem(listView, 0, 1, false).
		AddItem(menu, menuRows(), 0, true).
		AddItem(status, 1, 0, false)

	rebuildMenu := func() {
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
					// 末行空位：不可选中，避免方向键停在高亮空单元格
					cell.SetSelectable(false)
				}
				menu.SetCell(r, c, cell)
			}
		}
		menu.SetSelectable(true, true)
		menu.Select(0, 0)                      // 重排后重置选中到首项，避免旧行列跳变
		layout.ResizeItem(menu, menuRows(), 0) // resize 后同步菜单高度
	}
	rebuildMenu()

	app.SetRoot(layout, true)

	// 终端 resize 时按实际宽度重排菜单列数。
	// 用 SetBeforeDrawFunc：回调在 root.Draw 前执行，直接改数据当帧生效，
	// 无需 Queue*（在 draw 回调里 QueueUpdateDraw 会自锁，见 tview 事件循环实现）。
	app.SetBeforeDrawFunc(func(screen tcell.Screen) bool {
		if w, _ := screen.Size(); w > 0 {
			if cols := computeMenuCols(items, w); cols != menuCols {
				menuCols = cols
				rebuildMenu()
			}
		}
		return false // 不中断绘制
	})

	// 菜单获得焦点后 Enter 已由 SetSelectedFunc 处理；ESC 退出
	app.SetInputCapture(func(event *tcell.EventKey) *tcell.EventKey {
		if event.Key() == tcell.KeyEsc {
			app.Stop()
			return nil
		}
		return event
	})

	refreshList()
	if err := app.Run(); err != nil {
		fmt.Printf("交互界面启动失败: %v\n", err)
	}
}

// computeMenuCols 按终端宽度与最长菜单项计算平铺列数（至少 1 列）
func computeMenuCols(items []string, termWidth int) int {
	if len(items) == 0 {
		return 1
	}
	maxW := 0
	for _, it := range items {
		if dw := displayWidthForMenu(fmt.Sprintf("%2d. %s", len(items), it)); dw > maxW {
			maxW = dw
		}
	}
	itemWidth := maxW + 1
	cols := (termWidth + 1) / (itemWidth + 1)
	if cols < 1 {
		cols = 1
	}
	if cols > len(items) {
		cols = len(items)
	}
	return cols
}

// displayWidthForMenu 计算字符串显示宽度（CJK 全角按 2 列），供菜单平铺对齐
func displayWidthForMenu(s string) int {
	w := 0
	for _, r := range s {
		if r >= 0x1100 && (r <= 0x115F || r == 0x2329 || r == 0x232A ||
			(r >= 0x2E80 && r <= 0xA4CF && r != 0x303F) ||
			(r >= 0xAC00 && r <= 0xD7A3) ||
			(r >= 0xF900 && r <= 0xFAFF) ||
			(r >= 0xFE30 && r <= 0xFE4F) ||
			(r >= 0xFF00 && r <= 0xFF60) ||
			(r >= 0xFFE0 && r <= 0xFFE6)) {
			w += 2
		} else {
			w++
		}
	}
	return w
}

// runActionSuspended 以普通终端模式执行菜单操作：
// tview Suspend(f) 恢复终端原始状态并在 f 内执行操作函数（输出/输入
// ReadInput/Confirm 等均原生可用），f 返回后自动 Resume 回到 TUI 重绘。
// 这是 tview 官方支持的"外部交互"模式，从根本上避免自绘画布的区域错乱/闪烁/残留问题。
func runActionSuspended(app *tview.Application, title string, fn func()) {
	app.Suspend(func() {
		fmt.Printf("\n========== %s ==========\n", title)
		fn()
		fmt.Println("\n按回车返回菜单...")
		utils.ReadInput("", "")
	})
}

// captureCertListText 执行 getCertificates 并捕获其输出文本（含 ANSI 颜色），
// 供 TUI 主区域证书列表渲染。
// 由于 os.Stdout 是 *os.File，使用 os.Pipe 捕获；defer 保证异常时也能恢复与关闭。
func captureCertListText() string {
	r, w, err := os.Pipe()
	if err != nil {
		return ""
	}
	oldOut, oldColor := os.Stdout, color.Output
	os.Stdout = w
	color.Output = w
	var buf bytes.Buffer
	done := make(chan struct{})
	go func() {
		defer close(done)
		_, _ = buf.ReadFrom(r)
	}()
	defer func() {
		w.Close()
		os.Stdout, color.Output = oldOut, oldColor
		<-done
		r.Close()
	}()
	getCertificates()
	return buf.String()
}

// checkUpdate 检查 GitHub 最新版本并输出下载地址（不自动下载更新）。
// 网络不可达/API 限流时提示失败并给出可手动访问的下载页面地址，同时返回错误（CLI 下退出码非零）。
func checkUpdate() error {
	color.Cyan("正在检查更新...\n")
	release, err := github.LatestRelease(github.DefaultRepo)
	if err != nil {
		// 错误文本由调用方打印一次（CLI: main；菜单: runMenuAction），这里仅补充手动下载地址提示
		color.Yellow("可手动访问下载页面获取最新版本:\n%s\n", github.DownloadPage(github.DefaultRepo))
		return err
	}

	color.Cyan("最新版本: %s\n", release.TagName)
	if release.Name != "" && release.Name != release.TagName {
		fmt.Printf("发布说明: %s\n", release.Name)
	}

	// 版本比较（Version 为空表示本地构建未注入版本号，跳过比较）
	if Version != "" {
		switch github.CompareVersions(Version, release.TagName) {
		case 0:
			color.Green("当前已是最新版本 (%s)\n", Version)
		case 1:
			color.Green("当前版本 (%s) 高于最新发布 (%s)\n", Version, release.TagName)
		default:
			color.Yellow("发现新版本 %s（当前 %s）\n", release.TagName, Version)
		}
	} else {
		color.Yellow("当前版本未知（本地构建未注入版本号）\n")
	}

	// 输出下载地址（仅提示，不自动下载）
	fmt.Println("\n下载地址（请手动下载，不自动更新）:")
	if len(release.Assets) > 0 {
		for _, asset := range release.Assets {
			fmt.Printf("  - %s\n    %s\n", asset.Name, asset.URL)
		}
	} else {
		fmt.Printf("  %s\n", release.HTMLURL)
	}
	fmt.Printf("\n或访问 Releases 下载页面:\n%s\n", github.DownloadPage(github.DefaultRepo))
	return nil
}
