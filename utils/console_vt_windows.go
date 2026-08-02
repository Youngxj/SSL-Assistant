//go:build windows

package utils

import "golang.org/x/sys/windows"

// enableVTInput 启用 Windows 控制台的虚拟终端输入（ENABLE_VIRTUAL_TERMINAL_INPUT）。
// raw 模式下若不启用，方向键等无字符按键不会产生可读字节流（ReadConsoleW 返回空），
// 启用后方向键以 \x1b[A 等 VT 序列形式输入，程序才能读到。
func enableVTInput(fd int) error {
	handle := windows.Handle(fd)
	var mode uint32
	if err := windows.GetConsoleMode(handle, &mode); err != nil {
		return err
	}
	return windows.SetConsoleMode(handle, mode|windows.ENABLE_VIRTUAL_TERMINAL_INPUT)
}
