// Package store persists settings and the last library scan under
// the user config directory (%APPDATA%\WeAndMod on Windows).
package store

import (
	"encoding/json"
	"os"
	"path/filepath"

	"github.com/CNCoreSteb/weandmod/internal/game"
)

// Settings holds user preferences.
type Settings struct {
	CustomDirs []string `json:"custom_dirs"`
	// UIScale 界面缩放倍率,0 表示跟随系统(自动 DPI)。
	UIScale float32 `json:"ui_scale"`
	// PageSize 每页条数,0 表示按窗口高度自动。
	PageSize int `json:"page_size"`
}

type Store struct {
	dir string
}

// New opens the store, falling back to the working directory if the
// user config dir is unavailable.
func New() *Store {
	base, err := os.UserConfigDir()
	if err != nil {
		base = "."
	}
	dir := filepath.Join(base, "WeAndMod")
	return NewAt(dir)
}

// NewAt 打开指定目录的存储(测试与自定义场景用)。
func NewAt(dir string) *Store {
	if os.MkdirAll(dir, 0o755) != nil {
		dir = "."
	}
	return &Store{dir: dir}
}

func (s *Store) Settings() Settings {
	var st Settings
	data, err := os.ReadFile(filepath.Join(s.dir, "settings.json"))
	if err == nil {
		_ = json.Unmarshal(data, &st)
	}
	return st
}

func (s *Store) SaveSettings(st Settings) error {
	data, err := json.MarshalIndent(st, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(filepath.Join(s.dir, "settings.json"), data, 0o644)
}

// CoversDir 返回封面缓存目录(不存在则创建)。
func (s *Store) CoversDir() string {
	dir := filepath.Join(s.dir, "covers")
	_ = os.MkdirAll(dir, 0o755)
	return dir
}

// CachedGames returns games from the previous scan for instant startup.
func (s *Store) CachedGames() []game.Game {
	var games []game.Game
	data, err := os.ReadFile(filepath.Join(s.dir, "games_cache.json"))
	if err == nil {
		_ = json.Unmarshal(data, &games)
	}
	return games
}

func (s *Store) SaveGames(games []game.Game) {
	data, err := json.Marshal(games)
	if err == nil {
		_ = os.WriteFile(filepath.Join(s.dir, "games_cache.json"), data, 0o644)
	}
}
