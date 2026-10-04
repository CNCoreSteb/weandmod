//go:build windows

// Package proc 枚举系统进程,用于检测已下载的修改器是否在运行。
package proc

import (
	"path/filepath"
	"strings"
	"unsafe"

	"golang.org/x/sys/windows"
)

// RunningExes 返回正在运行的可执行文件完整路径集合(小写规范化)。
// 无论手动双击还是从软件内启动都会命中。
func RunningExes() map[string]bool {
	out := map[string]bool{}
	snap, err := windows.CreateToolhelp32Snapshot(windows.TH32CS_SNAPPROCESS, 0)
	if err != nil {
		return out
	}
	defer windows.CloseHandle(snap)

	var e windows.ProcessEntry32
	e.Size = uint32(unsafe.Sizeof(e))
	if err := windows.Process32First(snap, &e); err != nil {
		return out
	}
	for {
		pid := e.ProcessID
		if pid != 0 {
			// 受限进程 OpenProcess 会失败,跳过即可
			if h, err := windows.OpenProcess(
				windows.PROCESS_QUERY_LIMITED_INFORMATION, false, pid); err == nil {
				var buf [windows.MAX_PATH]uint16
				sz := uint32(len(buf))
				if windows.QueryFullProcessImageName(h, 0, &buf[0], &sz) == nil {
					p := strings.ToLower(filepath.Clean(
						windows.UTF16ToString(buf[:sz])))
					out[p] = true
				}
				windows.CloseHandle(h)
			}
		}
		if err := windows.Process32Next(snap, &e); err != nil {
			break
		}
	}
	return out
}
