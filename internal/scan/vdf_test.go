package scan

import "testing"

const sampleLibrary = `"libraryfolders"
{
	"0"
	{
		"path"		"C:\\Program Files (x86)\\Steam"
		"label"		""
	}
	"1"
	{
		"path"		"D:\\SteamLibrary"
		"label"		""
	}
}`

const sampleManifest = `"AppState"
{
	"appid"		"1091500"
	"name"		"Cyberpunk 2077"
	"installdir"		"Cyberpunk 2077"
	"StateFlags"		"4"
}`

func TestParseLibraryFolders(t *testing.T) {
	m, err := parseVDF([]byte(sampleLibrary))
	if err != nil {
		t.Fatal(err)
	}
	lf, ok := m["libraryfolders"].(map[string]vdfValue)
	if !ok {
		t.Fatal("missing libraryfolders")
	}
	e1, ok := lf["1"].(map[string]vdfValue)
	if !ok {
		t.Fatal("missing entry 1")
	}
	if got := e1["path"]; got != `D:\SteamLibrary` {
		t.Fatalf("path = %v", got)
	}
}

func TestParseManifest(t *testing.T) {
	m, err := parseVDF([]byte(sampleManifest))
	if err != nil {
		t.Fatal(err)
	}
	st := m["AppState"].(map[string]vdfValue)
	if st["appid"] != "1091500" || st["name"] != "Cyberpunk 2077" {
		t.Fatalf("bad manifest: %v", st)
	}
}
