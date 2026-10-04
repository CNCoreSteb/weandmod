package scan

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"

	"github.com/CNCoreSteb/weandmod/internal/game"
)

// epicManifest is the subset of Epic .item manifests we care about.
type epicManifest struct {
	AppName         string `json:"AppName"`
	DisplayName     string `json:"DisplayName"`
	InstallLocation string `json:"InstallLocation"`
	AppCategories   []struct {
		Category string `json:"Category"`
	} `json:"AppCategories"`
}

// scanEpic reads Epic Games Launcher manifests from ProgramData.
func scanEpic() []game.Game {
	pd := os.Getenv("ProgramData")
	if pd == "" {
		pd = `C:\ProgramData`
	}
	matches, _ := filepath.Glob(filepath.Join(pd, `Epic\EpicGamesLauncher\Data\Manifests\*.item`))
	var games []game.Game
	for _, m := range matches {
		data, err := os.ReadFile(m)
		if err != nil {
			continue
		}
		var man epicManifest
		if json.Unmarshal(data, &man) != nil {
			continue
		}
		if man.DisplayName == "" || man.InstallLocation == "" || !epicIsGame(man) {
			continue
		}
		dir := filepath.Clean(man.InstallLocation)
		if !dirExists(dir) {
			continue
		}
		games = append(games, game.Game{
			ID:         "epic:" + man.AppName,
			Name:       man.DisplayName,
			Platform:   game.PlatformEpic,
			InstallDir: dir,
		})
	}
	return games
}

// epicIsGame keeps manifests categorized as games; entries with no
// categories at all are included (older manifests omit them).
func epicIsGame(m epicManifest) bool {
	if len(m.AppCategories) == 0 {
		return true
	}
	for _, c := range m.AppCategories {
		if strings.HasPrefix(strings.ToLower(c.Category), "games") {
			return true
		}
	}
	return false
}
