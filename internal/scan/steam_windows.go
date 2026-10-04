//go:build windows

package scan

// steamRoots Windows 候选路径:注册表优先,常见安装路径兜底。
func steamRoots() []string {
	var roots []string
	if p, err := regGetString(regHKCU, `Software\Valve\Steam`, "SteamPath"); err == nil && p != "" {
		roots = append(roots, p)
	}
	return append(roots,
		`C:\Program Files (x86)\Steam`,
		`C:\Program Files\Steam`,
		`D:\Steam`,
	)
}
