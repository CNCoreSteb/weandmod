// We&Mod — trainer fetcher. Scans installed games and finds trainers.
package main

import (
	_ "embed"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/app"
	"fyne.io/fyne/v2/theme"

	"github.com/CNCoreSteb/weandmod/internal/store"
	"github.com/CNCoreSteb/weandmod/internal/ui"
)

//go:embed assets/icon.svg
var iconSVG []byte

func main() {
	a := app.NewWithID("io.github.cncoresteb.weandmod")
	a.Settings().SetTheme(theme.DarkTheme())
	a.SetIcon(fyne.NewStaticResource("icon.svg", iconSVG))

	w := a.NewWindow("We&Mod")
	w.Resize(fyne.NewSize(1180, 760))
	w.SetMaster()

	ui.NewHome(a, w, store.New())

	w.ShowAndRun()
}
