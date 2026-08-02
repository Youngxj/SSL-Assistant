package main

import (
	"fmt"
	"github.com/fatih/color"
	"github.com/spf13/cobra"
	"os"
	"ssl_assistant/config"
	"ssl_assistant/utils"
)

var Version string

var rootCmd = &cobra.Command{
	Use:   "ssl_assistant",
	Short: "证书管理工具",
	Long:  `SSL Assistant` + Version + ` 是一个基于 Go 语言开发的跨平台工具，用于SSL远程证书拉取，并自动完成SSL证书更新及生效流程。`,
	Run: func(cmd *cobra.Command, args []string) {
		// 如果没有子命令，则显示帮助信息
		cmd.Help()
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
	Run: func(cmd *cobra.Command, args []string) {
		err := addCertificate()
		if err != nil {
			color.Red("%s", err)
			return
		}
	},
}

var delCmd = &cobra.Command{
	Use:   "del",
	Short: "删除证书",
	Long:  `删除证书，输入证书 ID，程序自动删除对应的证书信息。`,
	Run: func(cmd *cobra.Command, args []string) {
		err := deleteCertificate()
		if err != nil {
			color.Red("%s", err)
			return
		}
	},
}

var showCmd = &cobra.Command{
	Use:   "show",
	Short: "查看证书",
	Long:  `查看证书，显示证书信息的表格，包括 ID、域名、状态、创建时间、过期时间、公钥、私钥等信息。`,
	Run: func(cmd *cobra.Command, args []string) {
		err := showCertificates()
		if err != nil {
			color.Red("%s", err)
			return
		}
	},
}

var updateCmd = &cobra.Command{
	Use:   "update",
	Short: "更新证书",
	Long:  `更新证书，程序自动获取所有证书信息，并将证书信息保存到数据库中，更新证书对应域名的证书文件内容，并执行重载命令。`,
	Run: func(cmd *cobra.Command, args []string) {
		err := updateCertificates()
		if err != nil {
			color.Red("%s", err)
			return
		}
	},
}

var findCmd = &cobra.Command{
	Use:   "find",
	Short: "快速添加域名（Nginx目录检索）",
	Long:  `检索Nginx目录，程序会自动检索Nginx目录下的所有证书文件，并将证书文件路径保存到数据库中，用于快速添加站点`,
	Run: func(cmd *cobra.Command, args []string) {
		initGuide(true)
		err := findNginxPathCmd()
		if err != nil {
			color.Red("%s", err)
			return
		}
	},
}

var cronCmd = &cobra.Command{
	Use:   "cron",
	Short: "证书更新任务\n\t-f 强制添加任务，覆盖已存在的任务。",
	Long:  `证书更新自动化任务，每日凌晨4点自动检测证书更新，并执行证书更新操作。`,
	Run: func(cmd *cobra.Command, args []string) {
		initGuide(true)
		// 获取强制标志
		force, _ := cmd.Flags().GetBool("force")
		cronTask(force) // 传递强制参数给 cronTask 函数
	},
}

var versionCmd = &cobra.Command{
	Use:   "version",
	Short: "显示版本信息",
	Long:  `显示版本信息，包括程序名称、版本号、编译时间等。`,
	Run: func(cmd *cobra.Command, args []string) {
		fmt.Printf("SSL Assistant %s\n项目地址: %s\n", Version, "https://github.com/Youngxj/SSL-Assistant")
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
		fmt.Println("  1. 初始化程序      (init)")
		fmt.Println("  2. 添加证书        (add)")
		fmt.Println("  3. 删除证书        (del)")
		fmt.Println("  4. 查看证书        (show)")
		fmt.Println("  5. 更新证书        (update)")
		fmt.Println("  6. 快速添加域名    (find)")
		fmt.Println("  7. 证书更新任务    (cron)")
		fmt.Println("  8. 显示版本信息    (version)")
		fmt.Println("  0. 退出")
		fmt.Println("============================================")

		choice := utils.ReadInput("请选择: ", "")
		switch choice {
		case "1":
			runMenuAction("初始化程序", func() error { initConfig(); return nil })
		case "2":
			runMenuAction("添加证书", addCertificate)
		case "3":
			runMenuAction("删除证书", deleteCertificate)
		case "4":
			runMenuAction("查看证书", showCertificates)
		case "5":
			runMenuAction("更新证书", updateCertificates)
		case "6":
			runMenuAction("快速添加域名", func() error {
				initGuide(true)
				return findNginxPathCmd()
			})
		case "7":
			runMenuAction("证书更新任务", func() error {
				initGuide(true)
				cronTask(false)
				return nil
			})
		case "8":
			fmt.Printf("SSL Assistant %s\n项目地址: https://github.com/Youngxj/SSL-Assistant\n", Version)
		case "0", "q", "exit":
			fmt.Println("再见！")
			return
		default:
			color.Yellow("无效选择，请重新输入\n")
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
