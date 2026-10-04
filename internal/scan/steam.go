package scan

import (
	"os"
	"path/filepath"
	"strings"

	"github.com/CNCoreSteb/weandmod/internal/game"
)

// steamSkipNames lists appmanifest entries that aren't games.
var steamSkipNames = []string{
	"steamworks common redistributables",
	"steam linux runtime",
	"proton ",
	"steamvr",
	"dedicated server",
	"sdk",
	"dedicated_server",
	"beta participation",
}

// scanSteam detects the Steam installation and enumerates installed games.
func scanSteam() []game.Game {
	root := steamRoot()
	if root == "" {
		return nil
	}
	var games []game.Game
	for _, lib := range steamLibraries(root) {
		games = append(games, steamLibraryGames(lib)...)
	}
	return games
}

// steamRoot resolves the Steam install directory; candidates are
// OS-specific (see steam_windows.go / steam_other.go).
func steamRoot() string {
	for _, p := range steamRoots() {
		if p != "" && dirExists(p) {
			return filepath.Clean(p)
		}
	}
	return ""
}

// steamLibraries parses steamapps/libraryfolders.vdf for every library path.
func steamLibraries(root string) []string {
	data, err := os.ReadFile(filepath.Join(root, "steamapps", "libraryfolders.vdf"))
	if err != nil {
		return []string{root}
	}
	parsed, err := parseVDF(data)
	if err != nil {
		return []string{root}
	}
	var libs []string
	lf, _ := parsed["libraryfolders"].(map[string]vdfValue)
	for _, v := range lf {
		sub, ok := v.(map[string]vdfValue)
		if !ok {
			if s, ok := v.(string); ok && dirExists(s) {
				libs = append(libs, s)
			}
			continue
		}
		if p, ok := sub["path"].(string); ok && dirExists(p) {
			libs = append(libs, p)
		}
	}
	if len(libs) == 0 {
		libs = []string{root}
	}
	return libs
}

// steamLibraryGames reads every appmanifest_*.acf under lib/steamapps.
func steamLibraryGames(lib string) []game.Game {
	matches, _ := filepath.Glob(filepath.Join(lib, "steamapps", "appmanifest_*.acf"))
	var games []game.Game
	for _, m := range matches {
		data, err := os.ReadFile(m)
		if err != nil {
			continue
		}
		parsed, err := parseVDF(data)
		if err != nil {
			continue
		}
		state, _ := parsed["AppState"].(map[string]vdfValue)
		if state == nil {
			continue
		}
		name, _ := state["name"].(string)
		appid, _ := state["appid"].(string)
		install, _ := state["installdir"].(string)
		if name == "" || install == "" || steamSkipped(name) {
			continue
		}
		dir := filepath.Join(lib, "steamapps", "common", install)
		if !dirExists(dir) {
			continue
		}
		games = append(games, game.Game{
			ID:         "steam:" + appid,
			Name:       name,
			Platform:   game.PlatformSteam,
			InstallDir: dir,
		})
	}
	return games
}

func steamSkipped(name string) bool {
	l := strings.ToLower(name)
	for _, s := range steamSkipNames {
		if strings.Contains(l, s) {
			return true
		}
	}
	return false
}
