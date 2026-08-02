//go:build !windows

package main

// IsDoubleClick 非 Windows 平台不存在资源管理器双击场景，恒返回 false。
func IsDoubleClick() bool {
	return false
}
