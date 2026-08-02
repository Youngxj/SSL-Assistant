//go:build windows

package utils

import "golang.org/x/sys/windows"

// setConsoleOutputCP 切换 Windows 控制台输出代码页
func setConsoleOutputCP(cp uint32) error {
	return windows.SetConsoleOutputCP(cp)
}

// setConsoleCP 切换 Windows 控制台输入代码页（读取中文输入时同样需要 UTF-8）
func setConsoleCP(cp uint32) error {
	return windows.SetConsoleCP(cp)
}
