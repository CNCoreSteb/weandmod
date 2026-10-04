//go:build !windows

package proc

// RunningExes 非 Windows 平台暂不实现进程枚举。
func RunningExes() map[string]bool { return nil }
