package main

import (
	"fmt"
	"github.com/fatih/color"
	"os"

	"github.com/spf13/cobra"
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

var versionCmd = &cobra.Command{
	Use:   "version",
	Short: "显示版本信息",
	Long:  `显示版本信息，包括程序名称、版本号、编译时间等。`,
	Run: func(cmd *cobra.Command, args []string) {
		fmt.Printf("SSL Assistant %s\n项目地址: %s\n", Version, "https://github.com/Youngxj/SSL-Assistant")
	},
}

func init() {
	// 添加子命令
	rootCmd.AddCommand(initCmd)
	rootCmd.AddCommand(addCmd)
	rootCmd.AddCommand(delCmd)
	rootCmd.AddCommand(showCmd)
	rootCmd.AddCommand(updateCmd)
	rootCmd.AddCommand(versionCmd)
	rootCmd.AddCommand(findCmd)
}

func main() {
	if len(os.Args) > 1 && os.Args[1] != "version" {
		// 初始化数据库（会自动选择SQLite或BadgerDB）
		err := initDatabase()
		if err != nil {
			fmt.Println("初始化数据库失败:", err)
			os.Exit(1)
		}

		// 确保程序退出时关闭数据库
		defer dbInterface.Close()
	}

	// 执行命令
	if err := rootCmd.Execute(); err != nil {
		fmt.Println(err)
		os.Exit(1)
	}
}
