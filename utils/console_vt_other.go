//go:build !windows

package utils

// enableVTInput 非 Windows 平台（Linux/macOS）的原始终端天然发送 VT 方向键序列，空实现
func enableVTInput(fd int) error {
	return nil
}
