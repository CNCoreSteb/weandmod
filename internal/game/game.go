// Package game defines the core game model shared across scanners and UI.
package game

import "strings"

// Platform identifies where a game installation was discovered.
type Platform string

const (
	PlatformSteam  Platform = "Steam"
	PlatformEpic   Platform = "Epic"
	PlatformGOG    Platform = "GOG"
	PlatformCustom Platform = "本地目录"
)

// Game is a detected game installation.
type Game struct {
	ID         string   `json:"id"`
	Name       string   `json:"name"`
	Platform   Platform `json:"platform"`
	InstallDir string   `json:"install_dir"`
	ExePath    string   `json:"exe_path,omitempty"`
}

// Key returns a normalized identity used to deduplicate scan results.
func (g Game) Key() string {
	return strings.ToLower(strings.Join(strings.FieldsFunc(g.Name, func(r rune) bool {
		return r == ' ' || r == '-' || r == '_' || r == ':' || r == '™' || r == '®'
	}), ""))
}
