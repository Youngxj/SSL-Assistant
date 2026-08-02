//go:build windows

package main

import "github.com/inconshreveable/mousetrap"

// IsDoubleClick 检测程序是否由 Windows 资源管理器双击启动（父进程为 explorer.exe）。
// 双击启动时进入交互菜单模式，替代 cobra 默认的“请到 cmd 运行”提示。
func IsDoubleClick() bool {
	return mousetrap.StartedByExplorer()
}
