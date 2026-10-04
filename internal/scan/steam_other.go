//go:build !windows

package scan

import (
	"os"
	"path/filepath"
)

// steamRoots Linux/macOS 常见 Steam 安装位置(含 Flatpak)。
func steamRoots() []string {
	home, err := os.UserHomeDir()
	if err != nil {
		return nil
	}
	return []string{
		filepath.Join(home, ".steam", "steam"),
		filepath.Join(home, ".local", "share", "Steam"),
		filepath.Join(home, ".var", "app", "com.valvesoftware.Steam", ".local", "share", "Steam"),
		filepath.Join(home, "Library", "Application Support", "Steam"),
	}
}
