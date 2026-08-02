package main

import (
	"fmt"
	"github.com/fatih/color"
	"github.com/spf13/cobra"
	"os"
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
	Short: "快速添加域名（Nginx目录检索）",
	Long:  `检索Nginx目录，程序会自动检索Nginx目录下的所有证书文件，并将证书文件路径保存到数据库中，用于快速添加站点`,
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

// runInteractiveMenu 双击运行时进入的交互菜单：循环展示操作、读取选择并执行，
// 直至用户选择退出。替代 cobra 默认的“请在 cmd 中运行”提示，实现双击可用。
func runInteractiveMenu() {
	for {
		fmt.Println()
		fmt.Println("========== SSL Assistant 操作菜单 ==========")
		// 终端下支持方向键选择（↑/↓ + 回车），非终端回退序号输入
		items := []string{
			"初始化程序      (init)",
			"添加证书        (add)",
			"删除证书        (del)",
			"查看证书        (show)",
			"更新证书        (update)",
			"快速添加域名    (find)",
			"证书更新任务    (cron)",
			"显示版本信息    (version)",
			"检查更新        (checkupdate)",
			"退出",
		}
		idx := utils.SelectMenu(items, "请选择（↑/↓ 移动，回车确认）: ")
		fmt.Println("============================================")

		// 方向键菜单返回索引（0 起）；ESC 取消返回 -1
		switch idx {
		case 0:
			runMenuAction("初始化程序", func() error { initConfig(); return nil })
		case 1:
			runMenuAction("添加证书", addCertificate)
		case 2:
			runMenuAction("删除证书", deleteCertificate)
		case 3:
			runMenuAction("查看证书", showCertificates)
		case 4:
			runMenuAction("更新证书", updateCertificates)
		case 5:
			runMenuAction("快速添加域名", func() error {
				if err := initGuide(true); err != nil {
					return err
				}
				return findNginxPathCmd()
			})
		case 6:
			// Windows 下 cron 常驻进程不适用（会阻塞窗口），引导使用任务计划程序
			color.Yellow("Windows 环境请使用任务计划程序定期执行 update（参见 README「计划任务设置」），无需本工具常驻进程\n")
			utils.ReadInput("按回车返回菜单", "")
		case 7:
			fmt.Printf("SSL Assistant %s\n项目地址: https://github.com/Youngxj/SSL-Assistant\n", displayVersion())
		case 8:
			runMenuAction("检查更新", func() error {
				// 菜单场景：内部已打印完整提示（含手动下载地址），不重复输出错误
				checkUpdate()
				return nil
			})
		case 9:
			fmt.Println("再见！")
			return
		default:
			color.Yellow("已取消\n")
		}
	}
}

// runMenuAction 执行菜单项并统一处理错误，完成后返回菜单继续。
func runMenuAction(name string, fn func() error) {
	fmt.Printf("\n========== %s ==========\n", name)
	if err := fn(); err != nil {
		color.Red("%s", err)
	}
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
