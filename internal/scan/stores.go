package scan

import (
	"os"
	"path/filepath"
	"strings"

	"github.com/CNCoreSteb/weandmod/internal/game"
)

// scanXbox 扫描各盘根下的 XboxGames 目录(Xbox app / Game Pass 安装位置)。
func scanXbox() []game.Game {
	return scanDriveRootDirs("XboxGames", func(name string) bool {
		l := strings.ToLower(name)
		// GameSave 是存档同步系统目录;*Digital Ownership 是授权占位包
		return l != "gamesave" && !strings.HasSuffix(l, "digital ownership")
	}, game.PlatformXbox)
}

// scanWeGame 扫描各盘根下的 WeGameApps 目录。
func scanWeGame() []game.Game {
	return scanDriveRootDirs("WeGameApps", func(name string) bool {
		l := strings.ToLower(name)
		return l != "wegame" && l != "tencent" // 跳过平台自身目录
	}, game.PlatformWeGame)
}

// scanDriveRootDirs 枚举 C~Z 盘根下的指定目录,每个子目录算一款游戏。
func scanDriveRootDirs(dirName string, keep func(string) bool, platform game.Platform) []game.Game {
	var games []game.Game
	for c := 'C'; c <= 'Z'; c++ {
		base := string(c) + `:\` + dirName
		entries, err := os.ReadDir(base)
		if err != nil {
			continue
		}
		for _, e := range entries {
			if !e.IsDir() || !keep(e.Name()) {
				continue
			}
			dir := filepath.Join(base, e.Name())
			// Xbox 布局: <Game>\Content;有 Content 则指到它
			if c := filepath.Join(dir, "Content"); dirExists(c) {
				dir = c
			}
			games = append(games, game.Game{
				ID:         strings.ToLower(string(platform)) + ":" + dir,
				Name:       e.Name(),
				Platform:   platform,
				InstallDir: dir,
			})
		}
	}
	return games
}

// regInstallScanner 描述"注册表子键 + 安装路径值"这一类平台。
type regInstallScanner struct {
	base      string // 注册表路径
	value     string // 安装路径值名
	platform  game.Platform
	nameField string // 非空时从该注册表值读名字,否则用目录名
}

// 各平台的注册表安装记录
var regInstallScanners = []regInstallScanner{
	// Ubisoft Connect 安装记录
	{`SOFTWARE\WOW6432Node\Ubisoft\Launcher\Installs`, "InstallDir", game.PlatformUbisoft, ""},
	// Rockstar Games
	{`SOFTWARE\WOW6432Node\Rockstar Games`, "InstallFolder", game.PlatformRockstar, ""},
	// Origin / EA App 旧版安装记录
	{`SOFTWARE\WOW6432Node\Origin Games`, "InstallDir", game.PlatformEA, ""},
}

// scanRegInstalls 按注册表安装记录枚举游戏。
func scanRegInstalls() []game.Game {
	var games []game.Game
	for _, spec := range regInstallScanners {
		keys, err := regSubKeys(regHKLM, spec.base)
		if err != nil {
			continue
		}
		for _, k := range keys {
			sub := spec.base + `\` + k
			dir, err := regGetString(regHKLM, sub, spec.value)
			if err != nil || dir == "" {
				continue
			}
			dir = filepath.Clean(dir)
			if !dirExists(dir) {
				continue
			}
			name := ""
			if spec.nameField != "" {
				name, _ = regGetString(regHKLM, sub, spec.nameField)
			}
			if name == "" {
				name = filepath.Base(dir)
			}
			games = append(games, game.Game{
				ID:         strings.ToLower(string(spec.platform)) + ":" + k,
				Name:       name,
				Platform:   spec.platform,
				InstallDir: dir,
			})
		}
	}
	return games
}

// uninstallPublisherMap 卸载表发行商 -> 平台。
var uninstallPublisherMap = map[string]game.Platform{
	"blizzard":         game.PlatformBattleNet,
	"electronic arts":  game.PlatformEA,
	"ea swiss":         game.PlatformEA,
	"ubisoft":          game.PlatformUbisoft,
	"rockstar":         game.PlatformRockstar,
	"riot games":       game.PlatformRiot,
	"valorant":         game.PlatformRiot,
}

// uninstallSkipNames 卸载表里明显不是游戏的条目。
var uninstallSkipNames = []string{
	"redistributable", "launcher", "driver", "runtime", "sdk",
	"service", "agent", "updater", "anticheat", "anti-cheat",
	"framework", "helper", "installer", "setup",
}

// scanUninstallGames 兜底:遍历卸载注册表,按发行商归类游戏平台。
// 覆盖 Battle.net(暴雪)等没有独立安装记录键的平台。
func scanUninstallGames() []game.Game {
	hives := []string{
		`SOFTWARE\Microsoft\Windows\CurrentVersion\Uninstall`,
		`SOFTWARE\WOW6432Node\Microsoft\Windows\CurrentVersion\Uninstall`,
	}
	var games []game.Game
	seen := map[string]bool{}
	for _, hive := range hives {
		keys, err := regSubKeys(regHKLM, hive)
		if err != nil {
			continue
		}
		for _, k := range keys {
			sub := hive + `\` + k
			pub, err := regGetString(regHKLM, sub, "Publisher")
			if err != nil || pub == "" {
				continue
			}
			platform, ok := matchPublisher(pub)
			if !ok {
				continue
			}
			name, _ := regGetString(regHKLM, sub, "DisplayName")
			dir, _ := regGetString(regHKLM, sub, "InstallLocation")
			if name == "" || dir == "" || uninstallSkipped(name) {
				continue
			}
			dir = filepath.Clean(strings.Trim(dir, `"`))
			if !dirExists(dir) || seen[dir] {
				continue
			}
			seen[dir] = true
			games = append(games, game.Game{
				ID:         strings.ToLower(string(platform)) + ":" + dir,
				Name:       name,
				Platform:   platform,
				InstallDir: dir,
			})
		}
	}
	return games
}

func matchPublisher(pub string) (game.Platform, bool) {
	l := strings.ToLower(pub)
	for k, p := range uninstallPublisherMap {
		if strings.Contains(l, k) {
			return p, true
		}
	}
	return "", false
}

func uninstallSkipped(name string) bool {
	l := strings.ToLower(name)
	for _, s := range uninstallSkipNames {
		if strings.Contains(l, s) {
			return true
		}
	}
	return false
}
