// We&Mod — trainer fetcher. Scans installed games and finds trainers.
package main

import (
	_ "embed"
	"os"
	"strconv"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/app"
	"fyne.io/fyne/v2/theme"

	"github.com/CNCoreSteb/weandmod/internal/store"
	"github.com/CNCoreSteb/weandmod/internal/ui"

	// trainer provider adapters (blank imports register them)
	_ "github.com/CNCoreSteb/weandmod/internal/provider/fling"
)

//go:embed assets/icon.svg
var iconSVG []byte

// version 由构建期 -ldflags -X main.version= 注入,默认 dev。
var version = "dev"

func main() {
	db := store.New()
	// 先应用保存的界面缩放(FYNE_SCALE 在驱动初始化时读取)
	if s := db.Settings(); s.UIScale > 0 {
		_ = os.Setenv("FYNE_SCALE", strconv.FormatFloat(float64(s.UIScale), 'f', 2, 32))
	}

	a := app.NewWithID("io.github.cncoresteb.weandmod")
	a.Settings().SetTheme(theme.DarkTheme())
	a.SetIcon(fyne.NewStaticResource("icon.svg", iconSVG))

	w := a.NewWindow("We&Mod " + version)
	w.Resize(fyne.NewSize(1180, 760))
	w.SetMaster()

	ui.NewHome(a, w, db)

	w.ShowAndRun()
}
