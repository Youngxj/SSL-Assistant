//go:build !windows

package utils

// setConsoleOutputCP 非 Windows 平台无控制台代码页概念，空实现
func setConsoleOutputCP(cp uint32) error {
	return nil
}

// setConsoleCP 非 Windows 平台无控制台代码页概念，空实现
func setConsoleCP(cp uint32) error {
	return nil
}
