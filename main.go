package main

import (
	"fmt"
	"github.com/fatih/color"
	"os"

	"github.com/spf13/cobra"
)

var rootCmd = &cobra.Command{
	Use:   "certdssl",
	Short: "证书管理工具",
	Long:  `certdssl 是一个基于 Go 语言开发的跨平台工具，主要功能是主动获取、更新证书信息，并通过命令行执行。`,
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
		showCertificates()
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

func init() {
	// 添加子命令
	rootCmd.AddCommand(initCmd)
	rootCmd.AddCommand(addCmd)
	rootCmd.AddCommand(delCmd)
	rootCmd.AddCommand(showCmd)
	rootCmd.AddCommand(updateCmd)
}

func main() {
	// 初始化数据库（会自动选择SQLite或BadgerDB）
	err := initDatabase()
	if err != nil {
		fmt.Println("初始化数据库失败:", err)
		os.Exit(1)
	}

	// 确保程序退出时关闭数据库
	defer dbInterface.Close()

	// 执行命令
	if err := rootCmd.Execute(); err != nil {
		fmt.Println(err)
		os.Exit(1)
	}
}
