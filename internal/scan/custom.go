package scan

import (
	"os"
	"path/filepath"
	"regexp"
	"strings"

	"github.com/CNCoreSteb/weandmod/internal/game"
)

const (
	customMaxDepth  = 4
	customMaxResult = 500
)

// skipExeNames filters obvious non-game executables.
var skipExeNames = regexp.MustCompile(`(?i)(unins|uninstall|setup|install|update|patch|launcher|crash|report|helper|service|daemon|redist|vcredist|dxsetup|directx|dotnet|physx|eac|battleye|anticheat|config|tool|server|benchmark|test)`)

// skipDirNames prevents descending into support directories.
var skipDirNames = regexp.MustCompile(`(?i)^(__installer|_?commonredist|redist(rib)?|support|tools?|binaries?|installer|vc\d*|directx|dotnet|docs?|manual|crashdumps?|logs?|cache|temp|tmp)$`)

// scanCustom walks a user-supplied directory looking for game executables.
func scanCustom(root string) []game.Game {
	root = filepath.Clean(root)
	if !dirExists(root) {
		return nil
	}
	var games []game.Game
	rootDepth := depth(root)

	_ = filepath.WalkDir(root, func(path string, d os.DirEntry, err error) error {
		if err != nil || len(games) >= customMaxResult {
			return nil
		}
		rel := depth(path) - rootDepth
		if d.IsDir() {
			if rel > customMaxDepth {
				return filepath.SkipDir
			}
			if rel > 0 && skipDirNames.MatchString(d.Name()) {
				return filepath.SkipDir
			}
			return nil
		}
		if !strings.EqualFold(filepath.Ext(d.Name()), ".exe") {
			return nil
		}
		if skipExeNames.MatchString(d.Name()) {
			return nil
		}
		info, err := d.Info()
		if err != nil || info.Size() < 512*1024 { // skip tiny helpers
			return nil
		}
		games = append(games, game.Game{
			ID:         "custom:" + path,
			Name:       prettyExeName(d.Name()),
			Platform:   game.PlatformCustom,
			InstallDir: filepath.Dir(path),
			ExePath:    path,
		})
		return nil
	})
	return games
}

// prettyExeName turns "SomeGame-Win64_Shipping.exe" into "SomeGame Win64 Shipping".
func prettyExeName(file string) string {
	name := strings.TrimSuffix(file, filepath.Ext(file))
	name = strings.NewReplacer("_", " ", "-", " ").Replace(name)
	return strings.Join(strings.Fields(name), " ")
}

func depth(path string) int {
	return len(strings.Split(filepath.Clean(path), string(os.PathSeparator)))
}

func dirExists(p string) bool {
	st, err := os.Stat(p)
	return err == nil && st.IsDir()
}
