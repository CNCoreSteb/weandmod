package ui

import (
	"context"
	"fmt"
	"net/url"
	"os"
	"strconv"
	"strings"
	"time"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/canvas"
	"fyne.io/fyne/v2/container"
	"fyne.io/fyne/v2/dialog"
	"fyne.io/fyne/v2/layout"
	"fyne.io/fyne/v2/theme"
	"fyne.io/fyne/v2/widget"

	"github.com/CNCoreSteb/weandmod/internal/game"
	"github.com/CNCoreSteb/weandmod/internal/provider"
	"github.com/CNCoreSteb/weandmod/internal/scan"
	"github.com/CNCoreSteb/weandmod/internal/store"
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
	trainer *provider.Result
}

// navItem 侧栏导航项:platform 为空表示"全部"。
type navItem struct {
	platform game.Platform
	label    string
	count    int
}

// navRowObject 侧栏行对象。
type navRowObject struct {
	*fyne.Container
	name  *widget.Label
	count *widget.Label
}

func newNavRowObject() *navRowObject {
	name := widget.NewLabel("")
	count := widget.NewLabel("")
	count.TextStyle = fyne.TextStyle{}
	c := container.NewBorder(nil, nil, nil, count, container.NewPadded(name))
	return &navRowObject{Container: c, name: name, count: count}
}

// rowObject 列表行对象:持有全部子部件引用,updateRow 不用挖容器树。
type rowObject struct {
	*fyne.Container
	cover  *coverBox
	title  *widget.Label
	sub    *widget.Label
	action *widget.Button
}

func newRowObject() *rowObject {
	cover := newCoverBox()
	title := widget.NewLabelWithStyle("", fyne.TextAlignLeading, fyne.TextStyle{Bold: true})
	sub := widget.NewLabel("")
	sub.Truncation = fyne.TextTruncateEllipsis
	action := widget.NewButton("", nil)
	c := container.NewBorder(nil, nil,
		cover.container,
		container.NewCenter(action),
		container.NewVBox(title, sub),
	)
	return &rowObject{Container: c, cover: cover, title: title, sub: sub, action: action}
}

// sizedContainer 包装容器,把自身 Resize 事件透传给回调
//(用于窗口尺寸变化时重算自动分页大小)。
type sizedContainer struct {
	*fyne.Container
	onResize func(fyne.Size)
}

func (s *sizedContainer) Resize(sz fyne.Size) {
	s.Container.Resize(sz)
	if s.onResize != nil {
		s.onResize(sz)
	}
}

// Home is the main page: search, library results, trainer hits.
type Home struct {
	app fyne.App
	win fyne.Window
	db  *store.Store

	games    []game.Game
	trainers []provider.Result
	scanning bool
	query    string

	platform game.Platform // 侧栏选中的平台过滤,"" = 全部
	navItems []navItem
	navSync  bool // 程序化 Select 时抑制 OnSelected 递归

	allRows []row // 过滤后的全部行(分页前)
	rows    []row // 当前页行
	page    int

	debounce       *time.Timer
	resizeDebounce *time.Timer

	searchEntry *widget.Entry
	nav         *widget.List
	list        *widget.List
	status      *widget.Label
	pageLabel   *widget.Label
	prevBtn     *widget.Button
	nextBtn     *widget.Button
	progress    *widget.ProgressBarInfinite
	empty       *widget.Label

	rowMinH    float32 // 单行最小高度(量一次)
	listHeight float32 // 列表可视高度(resize 回调更新)
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

	// 底部分页栏
	h.pageLabel = widget.NewLabel("")
	h.prevBtn = widget.NewButtonWithIcon("上一页", theme.NavigateBackIcon(), func() { h.gotoPage(h.page - 1) })
	h.nextBtn = widget.NewButtonWithIcon("下一页", theme.NavigateNextIcon(), func() { h.gotoPage(h.page + 1) })
	pager := container.NewHBox(h.prevBtn, h.pageLabel, h.nextBtn)
	bottom := container.NewVBox(widget.NewSeparator(), container.NewCenter(pager))

	h.list = widget.NewList(
		func() int { return len(h.rows) },
		func() fyne.CanvasObject { return newRowObject() },
		h.updateRow,
	)
	h.rowMinH = newRowObject().MinSize().Height

	h.empty = widget.NewLabelWithStyle(
		"未检测到游戏\n点右上角齿轮添加游戏目录",
		fyne.TextAlignCenter, fyne.TextStyle{},
	)
	h.empty.Hide()

	// 监听列表容器尺寸 -> 自动分页大小
	sized := &sizedContainer{
		Container: container.NewStack(h.list, container.NewCenter(h.empty)),
	}
	sized.onResize = func(sz fyne.Size) {
		h.listHeight = sz.Height
		h.scheduleRelayout()
	}

	// 左侧游戏库导航(按平台筛选)
	h.nav = widget.NewList(
		func() int { return len(h.navItems) },
		func() fyne.CanvasObject { return newNavRowObject() },
		h.updateNavRow,
	)
	h.nav.OnSelected = func(i widget.ListItemID) {
		if h.navSync || i >= len(h.navItems) {
			return
		}
		h.platform = h.navItems[i].platform
		h.page = 0
		h.refresh()
	}
	sideBg := canvas.NewRectangle(theme.Color(theme.ColorNameMenuBackground))
	sideBg.SetMinSize(fyne.NewSize(150, 0))
	sidebar := container.NewStack(sideBg,
		container.NewBorder(
			widget.NewLabelWithStyle("游戏库", fyne.TextAlignLeading, fyne.TextStyle{Bold: true}),
			nil, nil, nil, h.nav))

	h.win.SetContent(container.NewBorder(
		container.NewVBox(top, statusLine, widget.NewSeparator()),
		bottom, sidebar, nil,
		sized,
	))
	h.refresh()
}

// ---- 数据 ----

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

			parts := []string{fmt.Sprintf("共 %d 个游戏", len(res.Games))}
			for _, p := range game.PlatformOrder {
				if n := res.PerSource[p]; n > 0 {
					parts = append(parts, fmt.Sprintf("%s %d", p, n))
				}
			}
			h.status.SetText(strings.Join(parts, " · "))
			h.refresh()
		})
	}()
}

func (h *Home) onQueryChanged(q string) {
	h.query = strings.TrimSpace(q)
	h.page = 0
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
	found, err := provider.SearchAll(ctx, q)
	if err != nil {
		found = nil
	}
	fyne.Do(func() {
		if h.query != q {
			return // 过期结果,丢弃
		}
		h.trainers = found
		h.refresh()
	})
}

// ---- 分页 ----

// pageSize 当前每页条数:设置>0 用固定值,否则按列表可视高度自动。
func (h *Home) pageSize() int {
	if ps := h.db.Settings().PageSize; ps > 0 {
		return ps
	}
	if h.rowMinH <= 0 || h.listHeight <= 0 {
		return 20
	}
	n := int(h.listHeight / h.rowMinH)
	if n < 6 {
		n = 6
	}
	return n
}

func (h *Home) totalPages() int {
	ps := h.pageSize()
	if ps <= 0 {
		return 1
	}
	tp := (len(h.allRows) + ps - 1) / ps
	if tp < 1 {
		tp = 1
	}
	return tp
}

func (h *Home) gotoPage(p int) {
	if p < 0 {
		p = 0
	}
	if tp := h.totalPages(); p >= tp {
		p = tp - 1
	}
	h.page = p
	h.applyPage()
	h.list.Refresh()
}

// scheduleRelayout 窗口 resize 后延迟重排(防抖)。
func (h *Home) scheduleRelayout() {
	if h.resizeDebounce != nil {
		h.resizeDebounce.Stop()
	}
	h.resizeDebounce = time.AfterFunc(150*time.Millisecond, func() {
		fyne.Do(h.refresh)
	})
}

// applyPage 把 allRows 按当前页切片到 rows 并刷新分页栏。
func (h *Home) applyPage() {
	ps := h.pageSize()
	tp := h.totalPages()
	if h.page >= tp {
		h.page = tp - 1
	}
	if h.page < 0 {
		h.page = 0
	}
	start := h.page * ps
	end := start + ps
	if end > len(h.allRows) {
		end = len(h.allRows)
	}
	if start > end {
		start = end
	}
	h.rows = h.allRows[start:end]

	h.pageLabel.SetText(fmt.Sprintf("第 %d / %d 页 · 共 %d 条 · 每页 %d",
		h.page+1, tp, len(h.allRows), ps))
	if h.page <= 0 {
		h.prevBtn.Disable()
	} else {
		h.prevBtn.Enable()
	}
	if h.page >= tp-1 {
		h.nextBtn.Disable()
	} else {
		h.nextBtn.Enable()
	}
}

// ---- 渲染 ----

// refresh 重建侧栏导航 + 过滤后的行集合,并应用分页。
func (h *Home) refresh() {
	h.rebuildNav()

	h.allRows = h.allRows[:0]
	q := strings.ToLower(h.query)

	var matched []game.Game
	for _, g := range h.games {
		if h.platform != "" && g.Platform != h.platform {
			continue
		}
		if q == "" || strings.Contains(strings.ToLower(g.Name), q) ||
			strings.Contains(strings.ToLower(g.InstallDir), q) {
			matched = append(matched, g)
		}
	}
	if len(matched) > 0 {
		title := "游戏库"
		if h.platform != "" {
			title = string(h.platform)
		}
		h.allRows = append(h.allRows, row{kind: rowHeader, header: fmt.Sprintf("%s · %d", title, len(matched))})
		for _, g := range matched {
			g := g
			h.allRows = append(h.allRows, row{kind: rowGame, game: &g})
		}
	}
	if q != "" && len(h.trainers) > 0 {
		h.allRows = append(h.allRows, row{kind: rowHeader, header: fmt.Sprintf("修改器 · %d", len(h.trainers))})
		for _, t := range h.trainers {
			t := t
			h.allRows = append(h.allRows, row{kind: rowTrainer, trainer: &t})
		}
	}
	if len(h.allRows) == 0 && !h.scanning {
		h.empty.Show()
	} else {
		h.empty.Hide()
	}
	h.applyPage()
	h.list.Refresh()
}

func (h *Home) updateRow(i widget.ListItemID, o fyne.CanvasObject) {
	r := h.rows[i]
	ro := o.(*rowObject)

	switch r.kind {
	case rowHeader:
		ro.cover.container.Hide()
		ro.sub.Hide()
		ro.action.Hide()
		ro.title.TextStyle = fyne.TextStyle{Bold: true}
		ro.title.SetText(r.header)
	case rowGame:
		ro.cover.container.Show()
		ro.sub.Show()
		ro.action.Show()
		ro.action.SetText("找修改器")
		ro.title.TextStyle = fyne.TextStyle{Bold: true}
		ro.title.SetText(r.game.Name)
		ro.sub.SetText(fmt.Sprintf("%s · %s", r.game.Platform, r.game.InstallDir))

		ro.cover.resetImage()
		ro.cover.setGame(r.game.Name)
		loadCover(h.db, *r.game, ro.cover, h.list.Refresh)

		g := r.game
		ro.action.OnTapped = func() {
			h.searchEntry.SetText(g.Name)
		}
	case rowTrainer:
		ro.cover.container.Show()
		ro.sub.Show()
		ro.action.Show()
		ro.action.SetText("打开页面")
		ro.title.TextStyle = fyne.TextStyle{Bold: false}
		ro.title.SetText(r.trainer.Title)
		ro.sub.SetText("来源: " + r.trainer.Provider)

		ro.cover.resetImage()
		ro.cover.setGame(r.trainer.Provider)

		u := r.trainer.PageURL
		ro.action.OnTapped = func() {
			if parsed, err := url.Parse(u); err == nil {
				_ = h.app.OpenURL(parsed)
			}
		}
	}
	ro.title.Refresh()
}

// rebuildNav 按当前游戏库重建侧栏导航项(只显示有游戏的平台)。
func (h *Home) rebuildNav() {
	counts := map[game.Platform]int{}
	for _, g := range h.games {
		counts[g.Platform]++
	}
	h.navItems = h.navItems[:0]
	h.navItems = append(h.navItems, navItem{platform: "", label: "全部", count: len(h.games)})
	for _, p := range game.PlatformOrder {
		if n := counts[p]; n > 0 {
			h.navItems = append(h.navItems, navItem{platform: p, label: string(p), count: n})
		}
	}
	// 平台消失时回退到"全部",否则保持原选中
	sel := 0
	found := false
	for i, it := range h.navItems {
		if it.platform == h.platform {
			sel = i
			found = true
			break
		}
	}
	if !found {
		h.platform = ""
	}
	h.navSync = true
	h.nav.Select(sel)
	h.navSync = false
	h.nav.Refresh()
}

func (h *Home) updateNavRow(i widget.ListItemID, o fyne.CanvasObject) {
	it := h.navItems[i]
	no := o.(*navRowObject)
	no.name.SetText(it.label)
	no.count.SetText(strconv.Itoa(it.count))
}

// ---- 设置 ----

var scaleLabels = []string{"自动(跟随系统)", "75%", "100%", "125%", "150%", "175%", "200%"}
var scaleValues = []float32{0, 0.75, 1.0, 1.25, 1.5, 1.75, 2.0}
var pageLabels = []string{"自动(按窗口高度)", "20", "50", "100"}
var pageValues = []int{0, 20, 50, 100}

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

	// 界面缩放选择(0 = 跟随系统 DPI)
	scaleSel := widget.NewSelect(scaleLabels, nil)
	scaleSel.SetSelectedIndex(indexOfFloat(scaleValues, st.UIScale))
	// 每页条数选择(0 = 按窗口高度自动)
	pageSel := widget.NewSelect(pageLabels, nil)
	pageSel.SetSelectedIndex(indexOfInt(pageValues, st.PageSize))

	body := container.NewBorder(
		container.NewVBox(
			container.NewBorder(nil, nil, widget.NewLabel("界面缩放"), nil, scaleSel),
			container.NewBorder(nil, nil, widget.NewLabel("每页条数"), nil, pageSel),
			widget.NewSeparator(),
			widget.NewLabel("自定义游戏目录(扫描其中的 .exe)"),
		), nil, nil, nil,
		container.NewBorder(nil,
			container.NewHBox(add, remove), nil, nil,
			container.NewGridWrap(fyne.NewSize(520, 240), list),
		),
	)

	dialog.ShowCustomConfirm("设置", "保存", "取消", body, func(ok bool) {
		if !ok {
			return
		}
		st.CustomDirs = dirs
		st.UIScale = scaleValues[safeIndex(scaleSel.SelectedIndex(), len(scaleValues))]
		st.PageSize = pageValues[safeIndex(pageSel.SelectedIndex(), len(pageValues))]
		_ = h.db.SaveSettings(st)
		applyScale(st.UIScale, h.win)
		h.refresh()
		h.rescan()
	}, h.win)
}

// applyScale 通过 FYNE_SCALE 环境变量调整 fyne 缩放因子,
// 并微调窗口尺寸触发重算;设置已持久化,重启后同样生效。
func applyScale(v float32, w fyne.Window) {
	if v <= 0 {
		_ = os.Setenv("FYNE_SCALE", "auto")
	} else {
		_ = os.Setenv("FYNE_SCALE", strconv.FormatFloat(float64(v), 'f', 2, 32))
	}
	size := w.Canvas().Size()
	w.Resize(fyne.NewSize(size.Width+1, size.Height))
}

func indexOfFloat(vals []float32, v float32) int {
	for i, x := range vals {
		if x == v {
			return i
		}
	}
	return 0
}

func indexOfInt(vals []int, v int) int {
	for i, x := range vals {
		if x == v {
			return i
		}
	}
	return 0
}

func safeIndex(i, n int) int {
	if i < 0 || i >= n {
		return 0
	}
	return i
}
