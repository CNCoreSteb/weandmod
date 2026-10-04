package ui

import (
	"context"
	"fmt"
	"net/url"
	"strings"
	"time"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/container"
	"fyne.io/fyne/v2/dialog"
	"fyne.io/fyne/v2/layout"
	"fyne.io/fyne/v2/theme"
	"fyne.io/fyne/v2/widget"

	"github.com/CNCoreSteb/weandmod/internal/game"
	"github.com/CNCoreSteb/weandmod/internal/scan"
	"github.com/CNCoreSteb/weandmod/internal/store"
	"github.com/CNCoreSteb/weandmod/internal/trainer"
)

type rowKind int

const (
	rowHeader rowKind = iota
	rowGame
	rowTrainer
)

type row struct {
	kind    rowKind
	header  string
	game    *game.Game
	trainer *trainer.Trainer
}

// Home is the main page: search, library results, trainer hits.
type Home struct {
	app fyne.App
	win fyne.Window
	db  *store.Store

	games    []game.Game
	trainers []trainer.Trainer
	scanning bool
	query    string
	rows     []row

	debounce *time.Timer

	list        *widget.List
	status      *widget.Label
	searchEntry *widget.Entry
	progress    *widget.ProgressBarInfinite
	empty       *widget.Label
	content     *fyne.Container
}

// NewHome builds the home page and kicks off the initial library scan.
func NewHome(a fyne.App, w fyne.Window, db *store.Store) *Home {
	h := &Home{app: a, win: w, db: db}
	h.games = db.CachedGames()
	h.build()
	h.rescan()
	return h
}

func (h *Home) build() {
	h.searchEntry = widget.NewEntry()
	h.searchEntry.SetPlaceHolder("搜索游戏或修改器…")
	h.searchEntry.OnChanged = h.onQueryChanged

	refresh := widget.NewButtonWithIcon("", theme.ViewRefreshIcon(), h.rescan)
	settings := widget.NewButtonWithIcon("", theme.SettingsIcon(), h.showSettings)

	top := container.NewBorder(nil, nil,
		widget.NewLabelWithStyle("We&Mod", fyne.TextAlignLeading, fyne.TextStyle{Bold: true}),
		container.NewHBox(refresh, settings),
		h.searchEntry,
	)

	h.status = widget.NewLabel("")
	h.progress = widget.NewProgressBarInfinite()
	h.progress.Hide()
	statusLine := container.NewBorder(nil, nil, h.status, h.progress, layout.NewSpacer())

	h.list = widget.NewList(
		func() int { return len(h.rows) },
		h.newRowObject,
		h.updateRow,
	)
	h.empty = widget.NewLabelWithStyle(
		"未检测到游戏\n点右上角齿轮添加游戏目录",
		fyne.TextAlignCenter, fyne.TextStyle{},
	)
	h.empty.Hide()
	h.content = container.NewStack(h.list, container.NewCenter(h.empty))

	h.win.SetContent(container.NewBorder(
		container.NewVBox(top, statusLine, widget.NewSeparator()), nil, nil, nil,
		h.content,
	))
	h.refresh()
}

// ---- data ----

func (h *Home) rescan() {
	if h.scanning {
		return
	}
	h.scanning = true
	h.progress.Show()
	h.progress.Start()
	h.status.SetText("正在扫描游戏库…")

	dirs := h.db.Settings().CustomDirs
	go func() {
		res := scan.Scan(dirs)
		fyne.Do(func() {
			h.scanning = false
			h.progress.Stop()
			h.progress.Hide()
			h.games = res.Games
			h.db.SaveGames(res.Games)
			h.status.SetText(fmt.Sprintf("共 %d 个游戏 · Steam %d · Epic %d · GOG %d · 本地 %d",
				len(res.Games),
				res.PerSource[game.PlatformSteam],
				res.PerSource[game.PlatformEpic],
				res.PerSource[game.PlatformGOG],
				res.PerSource[game.PlatformCustom]))
			h.refresh()
		})
	}()
}

func (h *Home) onQueryChanged(q string) {
	h.query = strings.TrimSpace(q)
	if h.debounce != nil {
		h.debounce.Stop()
	}
	if h.query == "" {
		h.trainers = nil
		h.refresh()
		return
	}
	h.debounce = time.AfterFunc(450*time.Millisecond, func() {
		h.searchTrainers(h.query)
	})
	h.refresh()
}

func (h *Home) searchTrainers(q string) {
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	var found []trainer.Trainer
	for _, src := range trainer.DefaultSources() {
		got, err := src.Search(ctx, q)
		if err == nil {
			found = append(found, got...)
		}
	}
	fyne.Do(func() {
		if h.query != q {
			return // stale
		}
		h.trainers = found
		h.refresh()
	})
}

// ---- rendering ----

func (h *Home) refresh() {
	h.rows = h.rows[:0]
	q := strings.ToLower(h.query)

	var matched []game.Game
	for _, g := range h.games {
		if q == "" || strings.Contains(strings.ToLower(g.Name), q) ||
			strings.Contains(strings.ToLower(g.InstallDir), q) {
			matched = append(matched, g)
		}
	}
	if len(matched) > 0 {
		h.rows = append(h.rows, row{kind: rowHeader, header: fmt.Sprintf("游戏库 · %d", len(matched))})
		for _, g := range matched {
			g := g
			h.rows = append(h.rows, row{kind: rowGame, game: &g})
		}
	}
	if q != "" {
		if len(h.trainers) > 0 {
			h.rows = append(h.rows, row{kind: rowHeader, header: fmt.Sprintf("修改器 · %d", len(h.trainers))})
			for _, t := range h.trainers {
				t := t
				h.rows = append(h.rows, row{kind: rowTrainer, trainer: &t})
			}
		}
	}
	if len(h.rows) == 0 && !h.scanning {
		h.empty.Show()
	} else {
		h.empty.Hide()
	}
	h.list.Refresh()
}

func (h *Home) newRowObject() fyne.CanvasObject {
	icon := widget.NewIcon(theme.ComputerIcon())
	title := widget.NewLabelWithStyle("", fyne.TextAlignLeading, fyne.TextStyle{Bold: true})
	sub := widget.NewLabel("")
	sub.Truncation = fyne.TextTruncateEllipsis
	action := widget.NewButton("", nil)
	// Border object order: [center, left, right] (top/bottom are nil).
	return container.NewBorder(nil, nil,
		container.NewCenter(icon),
		container.NewCenter(action),
		container.NewVBox(title, sub),
	)
}

func (h *Home) updateRow(i widget.ListItemID, o fyne.CanvasObject) {
	r := h.rows[i]
	box := o.(*fyne.Container)
	texts := box.Objects[0].(*fyne.Container)      // center
	iconBox := box.Objects[1].(*fyne.Container)   // left
	actionBox := box.Objects[2].(*fyne.Container) // right
	icon := iconBox.Objects[0].(*widget.Icon)
	action := actionBox.Objects[0].(*widget.Button)
	title := texts.Objects[0].(*widget.Label)
	sub := texts.Objects[1].(*widget.Label)

	switch r.kind {
	case rowHeader:
		iconBox.Hide()
		sub.Hide()
		action.Hide()
		title.TextStyle = fyne.TextStyle{Bold: true}
		title.SetText(r.header)
	case rowGame:
		iconBox.Show()
		icon.SetResource(theme.ComputerIcon())
		sub.Show()
		action.Show()
		action.SetText("找修改器")
		title.TextStyle = fyne.TextStyle{Bold: true}
		title.SetText(r.game.Name)
		sub.SetText(fmt.Sprintf("%s · %s", r.game.Platform, r.game.InstallDir))
		g := r.game
		action.OnTapped = func() {
			h.searchEntry.SetText(g.Name)
		}
	case rowTrainer:
		iconBox.Show()
		icon.SetResource(theme.DownloadIcon())
		sub.Show()
		action.Show()
		action.SetText("打开页面")
		title.TextStyle = fyne.TextStyle{Bold: false}
		title.SetText(r.trainer.Title)
		sub.SetText("来源: " + r.trainer.Source)
		u := r.trainer.URL
		action.OnTapped = func() {
			if parsed, err := url.Parse(u); err == nil {
				_ = h.app.OpenURL(parsed)
			}
		}
	}
	title.Refresh()
}

// ---- settings ----

func (h *Home) showSettings() {
	st := h.db.Settings()
	dirs := append([]string(nil), st.CustomDirs...)
	selected := -1

	list := widget.NewList(
		func() int { return len(dirs) },
		func() fyne.CanvasObject {
			return widget.NewLabel("")
		},
		func(i widget.ListItemID, o fyne.CanvasObject) {
			o.(*widget.Label).SetText(dirs[i])
		},
	)
	list.OnSelected = func(i widget.ListItemID) { selected = i }

	add := widget.NewButtonWithIcon("添加目录", theme.FolderOpenIcon(), func() {
		dialog.NewFolderOpen(func(uri fyne.ListableURI, err error) {
			if err != nil || uri == nil {
				return
			}
			dirs = append(dirs, uri.Path())
			list.Refresh()
		}, h.win).Show()
	})
	remove := widget.NewButtonWithIcon("移除选中", theme.DeleteIcon(), func() {
		if selected >= 0 && selected < len(dirs) {
			dirs = append(dirs[:selected], dirs[selected+1:]...)
			selected = -1
			list.UnselectAll()
			list.Refresh()
		}
	})

	body := container.NewBorder(
		widget.NewLabel("自定义游戏目录（扫描其中的 .exe）"), nil, nil, nil,
		container.NewBorder(nil,
			container.NewHBox(add, remove), nil, nil,
			container.NewGridWrap(fyne.NewSize(520, 260), list),
		),
	)

	dialog.ShowCustomConfirm("设置", "保存", "取消", body, func(ok bool) {
		if !ok {
			return
		}
		st.CustomDirs = dirs
		_ = h.db.SaveSettings(st)
		h.rescan()
	}, h.win)
}
