package scan

import (
	"path/filepath"

	"github.com/CNCoreSteb/weandmod/internal/game"
)

// scanGOG enumerates GOG Galaxy installed games via the registry.
// HKLM\SOFTWARE\WOW6432Node\GOG.com\Games\<id> holds gameName / path / exe.
func scanGOG() []game.Game {
	const base = `SOFTWARE\WOW6432Node\GOG.com\Games`
	ids, err := regSubKeys(regHKLM, base)
	if err != nil || len(ids) == 0 {
		// 32-bit registry view fallback.
		ids, err = regSubKeys(regHKLM, `SOFTWARE\GOG.com\Games`)
		if err != nil {
			return nil
		}
		return gogGames(`SOFTWARE\GOG.com\Games`, ids)
	}
	return gogGames(base, ids)
}

func gogGames(base string, ids []string) []game.Game {
	var games []game.Game
	for _, id := range ids {
		name, err1 := regGetString(regHKLM, base+`\`+id, "gameName")
		dir, err2 := regGetString(regHKLM, base+`\`+id, "path")
		if err1 != nil || err2 != nil || name == "" || dir == "" {
			continue
		}
		dir = filepath.Clean(dir)
		if !dirExists(dir) {
			continue
		}
		exe, _ := regGetString(regHKLM, base+`\`+id, "exe")
		games = append(games, game.Game{
			ID:         "gog:" + id,
			Name:       name,
			Platform:   game.PlatformGOG,
			InstallDir: dir,
			ExePath:    exe,
		})
	}
	return games
}
