package ui

import (
	"context"
	"errors"
	"fmt"
	"image/color"
	"maps"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/canvas"
	"fyne.io/fyne/v2/container"
	"fyne.io/fyne/v2/dialog"
	"fyne.io/fyne/v2/layout"
	"fyne.io/fyne/v2/theme"
	"fyne.io/fyne/v2/widget"

	"github.com/CNCoreSteb/weandmod/internal/dl"
	"github.com/CNCoreSteb/weandmod/internal/game"
	"github.com/CNCoreSteb/weandmod/internal/proc"
	"github.com/CNCoreSteb/weandmod/internal/provider"
	"github.com/CNCoreSteb/weandmod/internal/scan"
	"github.com/CNCoreSteb/weandmod/internal/store"
)

// autoScan 启动时是否自动扫描(测试里关掉避免后台 goroutine)。
var autoScan = true

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

// newNavRowObject 侧栏行:Border{ center:Padded(name), right:count }。
// fyne 渲染树只认 *fyne.Container / fyne.Widget,自定义包装类型不会被遍历,
// 所以行对象一律用原生容器 + 固定索引访问。
func newNavRowObject() fyne.CanvasObject {
	name := widget.NewLabel("")
	count := widget.NewLabel("")
	return container.NewBorder(nil, nil, nil, count, container.NewPadded(name))
}

// newRowObject 主列表行:Border{ center:VBox(title,subRow,dlLine), left:cover, right:Center(action) }。
// Objects 顺序: [center, left, right]。subRow = HBox(sub, runMark),runMark 是绿色
// 「修改器运行中」标记(默认隐藏);dlLine 是下载进度行(进度条+文本),默认隐藏。
func newRowObject() fyne.CanvasObject {
	cover := newCoverBox()
	title := widget.NewLabelWithStyle("", fyne.TextAlignLeading, fyne.TextStyle{Bold: true})
	sub := widget.NewLabel("")
	sub.Truncation = fyne.TextTruncateEllipsis
	runMark := canvas.NewText("", color.NRGBA{R: 0x40, G: 0xc0, B: 0x60, A: 0xff})
	runMark.TextSize = 11
	runMark.Hide()
	subRow := container.NewBorder(nil, nil, nil, runMark, sub)
	dlBar := widget.NewProgressBar()
	dlText := canvas.NewText("", theme.Color(theme.ColorNameForeground))
	dlText.TextSize = 11
	dlLine := container.NewBorder(nil, nil, nil, dlText, dlBar)
	dlLine.Hide()
	action := widget.NewButton("", nil)
	action2 := widget.NewButton("", nil) // 修改器行的「打开页面」
	action2.Hide()
	return container.NewBorder(nil, nil,
		cover,
		container.NewCenter(container.NewVBox(action, action2)),
		container.NewVBox(title, subRow, dlLine),
	)
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

	searchGame string // 当前修改器搜索归属的游戏名(点「找修改器」时记下)
	searching  bool   // 修改器搜索进行中(进度动画 + 按钮禁用)

	resolved  sync.Map // PageURL -> provider.Download(Kind=="page" 表示仅网页可开)
	resolving sync.Map // PageURL -> bool,在途解析去重

	runningSet atomic.Value // map[string]bool:正在运行的修改器 exe 路径(小写)

	updates []dl.Update // 检测到可更新的已下载修改器

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
	bellDot     *canvas.Circle // 铃铛上的更新红点

	rowMinH    float32 // 单行最小高度(量一次)
	listHeight float32 // 列表可视高度(布局回调更新)
}

// NewHome builds the home page and kicks off the initial library scan.
func NewHome(a fyne.App, w fyne.Window, db *store.Store) *Home {
	h := &Home{app: a, win: w, db: db}
	h.games = db.CachedGames()
	h.build()
	if autoScan {
		h.rescan()
	}
	return h
}

func (h *Home) build() {
	h.searchEntry = widget.NewEntry()
	h.searchEntry.SetPlaceHolder("搜索游戏或修改器…")
	h.searchEntry.OnChanged = h.onQueryChanged

	refresh := widget.NewButtonWithIcon("", theme.ViewRefreshIcon(), h.rescan)
	settings := widget.NewButtonWithIcon("", theme.SettingsIcon(), h.showSettings)

	// 左上角铃铛:有修改器更新时显示红点,点击打开更新列表
	bell := widget.NewButton("🔔", h.showUpdates)
	h.bellDot = canvas.NewCircle(color.NRGBA{R: 0xe0, G: 0x40, B: 0x40, A: 0xff})
	h.bellDot.Hide()
	bellWrap := container.NewStack(bell,
		container.NewBorder(container.NewHBox(layout.NewSpacer(),
			container.NewGridWrap(fyne.NewSize(9, 9), h.bellDot)), nil, nil, nil))

	top := container.NewBorder(nil, nil,
		container.NewHBox(bellWrap,
			widget.NewLabelWithStyle("We&Mod", fyne.TextAlignLeading, fyne.TextStyle{Bold: true})),
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
	sized := newSizedStack(func(sz fyne.Size) {
		h.listHeight = sz.Height
		h.scheduleRelayout()
	}, h.list, container.NewCenter(h.empty))

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

	// 下载进度刷新:有活跃下载时 400ms 刷一次行渲染
	go func() {
		for range time.Tick(400 * time.Millisecond) {
			if dl.HasActive() {
				fyne.Do(h.list.Refresh)
			}
		}
	}()

	// 启动后后台检查已下载修改器的更新
	go h.checkUpdates()

	// 进程监控:每 2s 枚举运行中进程,命中已下载修改器时刷新行显示「运行中」
	go func() {
		h.runningSet.Store(map[string]bool{})
		prev := map[string]bool{}
		for range time.Tick(2 * time.Second) {
			exes := proc.RunningExes()
			run := map[string]bool{}
			for _, p := range dl.AllFiles() {
				k := strings.ToLower(filepath.Clean(p))
				if exes[k] {
					run[k] = true
				}
			}
			if !maps.Equal(run, prev) {
				prev = run
				h.runningSet.Store(run)
				fyne.Do(h.list.Refresh)
			}
		}
	}()
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
	// 搜索词被手动改走时,解除与游戏的下载归属
	if h.searchGame != "" && h.query != h.searchGame {
		h.searchGame = ""
	}
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
	fyne.Do(func() {
		if h.query != q {
			return
		}
		h.searching = true
		h.status.SetText("正在搜索修改器…")
		h.progress.Show()
		h.progress.Start()
		h.list.Refresh() // 对应游戏行按钮变「搜索中」
	})
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	found, err := provider.SearchAll(ctx, q)
	if err != nil {
		found = nil
	}
	fyne.Do(func() {
		h.searching = false
		h.progress.Stop()
		h.progress.Hide()
		if h.query != q {
			return // 过期结果,丢弃
		}
		h.trainers = found
		if len(found) > 0 {
			h.status.SetText(fmt.Sprintf("找到 %d 个修改器", len(found)))
		} else {
			h.status.SetText("未找到修改器")
		}
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

// rowParts 拆解主列表行 Border 的内部对象(顺序 [center, left, right])。
func rowParts(c *fyne.Container) (cover, texts, actionBox *fyne.Container) {
	return c.Objects[1].(*fyne.Container),
		c.Objects[0].(*fyne.Container),
		c.Objects[2].(*fyne.Container)
}

func (h *Home) updateRow(i widget.ListItemID, o fyne.CanvasObject) {
	r := h.rows[i]
	cover, texts, actionBox := rowParts(o.(*fyne.Container))
	title := texts.Objects[0].(*widget.Label)
	subRow := texts.Objects[1].(*fyne.Container)
	sub := subRow.Objects[0].(*widget.Label)
	runMark := subRow.Objects[1].(*canvas.Text)
	dlLine := texts.Objects[2].(*fyne.Container)
	dlBar := dlLine.Objects[0].(*widget.ProgressBar)
	dlText := dlLine.Objects[1].(*canvas.Text)
	btns := actionBox.Objects[0].(*fyne.Container)
	action := btns.Objects[0].(*widget.Button)
	action2 := btns.Objects[1].(*widget.Button)

	switch r.kind {
	case rowHeader:
		cover.Hide()
		subRow.Hide()
		dlLine.Hide()
		action.Hide()
		action2.Hide()
		title.TextStyle = fyne.TextStyle{Bold: true}
		title.SetText(r.header)
	case rowGame:
		cover.Show()
		subRow.Show()
		action.Show()
		action2.Hide()
		action.Enable() // 行复用:先复位禁用态,下载中/搜索中分支再禁用
		g := r.game
		pr, downloading := dl.ProgressOf(g.Name)
		if downloading {
			// 下载中:进度条 + 已下载/总量 + 线程信息
			dlLine.Show()
			dlBar.Show()
			var frac float64
			if pr.Total > 0 {
				frac = float64(pr.Done) / float64(pr.Total)
			}
			dlBar.SetValue(frac)
			dlText.Text = fmt.Sprintf("%s/%s · %s", humanBytes(pr.Done), humanBytes(pr.Total), threadDesc(pr))
			dlText.Refresh()
			action.SetText("下载中")
			action.Disable()
		} else {
			dlLine.Hide()
		}
		// 运行中标记放在副标题行内,明确归属本卡片
		if h.trainerRunning(g.Name) {
			runMark.Text = "● 修改器运行中"
			runMark.Show()
			runMark.Refresh()
		} else {
			runMark.Hide()
		}
		if !downloading {
			if dl.HasTrainer(g.Name) {
				// 已有下载的修改器:按钮变为「打开」
				action.SetText("打开")
				action.OnTapped = func() {
					if p, err := dl.OpenTarget(g.Name); err == nil {
						h.openPath(p)
					}
				}
			} else if h.searching && h.searchGame == g.Name {
				// 该游戏的修改器搜索进行中:禁用 + 提示
				action.SetText("搜索中")
				action.Disable()
			} else {
				action.Enable()
				action.SetText("找修改器")
				action.OnTapped = func() {
					h.searchGame = g.Name // 下载归到该游戏目录
					h.searchEntry.SetText(g.Name)
				}
			}
		}
		title.TextStyle = fyne.TextStyle{Bold: true}
		title.SetText(g.Name)
		sub.SetText(fmt.Sprintf("%s · %s", g.Platform, g.InstallDir))

		coverReset(cover)
		coverPlaceholder(cover, g.Name)
		loadCover(h.db, *g, cover, h.list.Refresh)
	case rowTrainer:
		cover.Show()
		subRow.Show()
		runMark.Hide()
		dlLine.Hide()
		action.Show()
		action.Enable()
		title.TextStyle = fyne.TextStyle{Bold: false}
		title.SetText(r.trainer.Title)
		sub.SetText("来源: " + r.trainer.Provider)

		coverReset(cover)
		coverPlaceholder(cover, r.trainer.Provider)

		t := r.trainer
		target := h.searchGame
		if target == "" {
			target = t.Title
		}
		d, resolved := h.trainerDL(t)
		p := h.trainerPath(t)
		_, inProgress := dl.ProgressOf(target)
		switch {
		case p != "":
			action.SetText("打开")
			action.OnTapped = func() { h.openPath(p) }
		case resolved && d.Kind == "page":
			action.Hide() // 无独立版可下载:只留「打开页面」
		case inProgress:
			action.SetText("下载中")
			action.Disable()
		default: // 未解析/解析失败/可下载:都显示「下载」,点击时走真实解析
			action.SetText("下载")
			action.OnTapped = func() { h.downloadTrainer(t) }
		}
		// 第二按钮:始终可打开原文页
		action2.Show()
		action2.SetText("打开页面")
		action2.OnTapped = func() { h.openURL(t.PageURL) }
	}
	title.Refresh()
}

// humanBytes 人类可读字节数。
func humanBytes(n int64) string {
	switch {
	case n >= 1<<30:
		return fmt.Sprintf("%.1f GiB", float64(n)/(1<<30))
	case n >= 1<<20:
		return fmt.Sprintf("%.1f MiB", float64(n)/(1<<20))
	case n >= 1<<10:
		return fmt.Sprintf("%.1f KiB", float64(n)/(1<<10))
	default:
		return fmt.Sprintf("%d B", n)
	}
}

// threadDesc 进度文本的线程描述:「多线程×4」或「单线程」。
func threadDesc(pr dl.Progress) string {
	if pr.Parallel {
		return fmt.Sprintf("多线程×%d", pr.Threads)
	}
	return "单线程"
}

// ---- 修改器下载 ----

// trainerDL 返回该结果的下载解析缓存;未解析时触发后台懒解析并返回 false。
func (h *Home) trainerDL(t *provider.Result) (provider.Download, bool) {
	if v, ok := h.resolved.Load(t.PageURL); ok {
		return v.(provider.Download), true
	}
	h.prefetchResolve(t)
	return provider.Download{}, false
}

// prefetchResolve 后台解析某结果的下载地址,只跑一次,完成后刷新行。
func (h *Home) prefetchResolve(t *provider.Result) {
	if _, loaded := h.resolving.LoadOrStore(t.PageURL, true); loaded {
		return
	}
	go func() {
		defer h.resolving.Delete(t.PageURL)
		ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
		defer cancel()
		d, err := provider.Resolve(ctx, *t)
		if err != nil {
			d = provider.Download{Kind: "error"} // 标记已尝试,行仍显示下载(点击重试)
		}
		h.resolved.Store(t.PageURL, d)
		fyne.Do(h.list.Refresh)
	}()
}

// trainerRunning 该游戏目录下的修改器进程是否在运行。
func (h *Home) trainerRunning(gameName string) bool {
	v := h.runningSet.Load()
	if v == nil {
		return false
	}
	set := v.(map[string]bool)
	for _, f := range dl.Files(gameName) {
		if set[strings.ToLower(filepath.Clean(f))] {
			return true
		}
	}
	return false
}

// trainerPath 返回该搜索结果已下载到本地的文件路径,未下载或文件已删返回 ""。
func (h *Home) trainerPath(t *provider.Result) string {
	p, ok := h.db.Settings().Downloads[t.PageURL]
	if !ok {
		return ""
	}
	if _, err := os.Stat(p); err != nil {
		return ""
	}
	return p
}

// downloadTrainer 下载一条搜索结果:归属目录优先用「找修改器」记下的游戏名。
func (h *Home) downloadTrainer(t *provider.Result) {
	target := h.searchGame
	if target == "" {
		target = t.Title
	}
	h.doDownload(*t, target, "")
}

// doDownload 解析直链并下载到 文档\WeAndMod\<target>,文件按文章标题命名,
// 旁挂 .json 元数据供更新检测。oldFile 非空且落盘名不同则删除旧文件(更新场景)。
// 解析不出直链或返回网页时回退浏览器打开详情页。
func (h *Home) doDownload(t provider.Result, target, oldFile string) {
	dir, err := dl.GameDir(target)
	if err != nil {
		h.status.SetText("下载失败: " + err.Error())
		return
	}
	h.status.SetText("正在解析下载地址…")
	h.progress.Show()
	h.progress.Start()

	done := func(msg string) {
		fyne.Do(func() {
			h.progress.Stop()
			h.progress.Hide()
			h.status.SetText(msg)
		})
	}
	go func() {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
		defer cancel()
		d, err := provider.Resolve(ctx, t)
		if err != nil || d.FileURL == "" || d.Kind == "page" {
			done("无可直链下载的独立版本,已打开详情页")
			fyne.Do(func() { h.openURL(t.PageURL) })
			return
		}
		fyne.Do(func() { h.status.SetText("正在下载 " + t.Title + " …") })
		// 设置关闭多线程时退化为单线程;开启时按文件大小自动分配
		threads := 1
		if h.db.Settings().MultiDL() {
			threads = 8
		}
		// 文件名用文章标题(更新检测依赖同名),扩展名从链接补齐
		path, err := dl.Download(ctx, target, d.FileURL, dir, t.Title, threads)
		if err != nil {
			if errors.Is(err, dl.ErrHTML) {
				done("资源是网页,已打开详情页")
				fyne.Do(func() { h.openURL(t.PageURL) })
			} else {
				done("下载失败: " + err.Error())
			}
			return
		}
		// 校验产物:文件头/完整性不符视为下载失败
		if verr := dl.Verify(path); verr != nil {
			_ = os.Remove(path)
			done("下载校验失败,文件已删除")
			fyne.Do(func() {
				dialog.ShowError(fmt.Errorf("下载的文件校验失败:%w\n可能抓到了错误页或被截断", verr), h.win)
			})
			return
		}
		// 旁挂元数据:下载链接、链接名称、原文链接
		_ = dl.WriteMeta(path, dl.Meta{
			Title:      t.Title,
			ProviderID: t.ProviderID,
			PageURL:    t.PageURL,
			FileURL:    d.FileURL,
			FileName:   d.FileName,
		})
		if oldFile != "" && oldFile != path {
			_ = os.Remove(oldFile) // 更新后清理旧文件
		}
		st := h.db.Settings()
		if st.Downloads == nil {
			st.Downloads = map[string]string{}
		}
		st.Downloads[t.PageURL] = path
		_ = h.db.SaveSettings(st)
		fyne.Do(func() {
			h.progress.Stop()
			h.progress.Hide()
			h.status.SetText("已下载: " + filepath.Base(path))
			// 下载完成的项从更新列表移除
			var kept []dl.Update
			for _, u := range h.updates {
				if u.Meta.PageURL != t.PageURL {
					kept = append(kept, u)
				}
			}
			h.updates = kept
			if len(kept) == 0 {
				h.bellDot.Hide()
			}
			h.refresh() // 游戏行翻转为「打开」
		})
	}()
}

// checkUpdates 后台扫描已下载修改器并检测更新,有则点亮铃铛红点。
func (h *Home) checkUpdates() {
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Minute)
	defer cancel()
	ups := dl.CheckUpdates(ctx)
	fyne.Do(func() {
		h.updates = ups
		if len(ups) > 0 {
			h.bellDot.Show()
		} else {
			h.bellDot.Hide()
		}
	})
}

// showUpdates 铃铛点击:弹出窗口列出所有可更新的修改器。
func (h *Home) showUpdates() {
	if len(h.updates) == 0 {
		dialog.ShowInformation("修改器更新", "已下载的修改器均为最新", h.win)
		return
	}
	var list *widget.List
	list = widget.NewList(
		func() int { return len(h.updates) },
		func() fyne.CanvasObject {
			l := widget.NewLabel("")
			l.Truncation = fyne.TextTruncateEllipsis
			return container.NewBorder(nil, nil, nil,
				widget.NewButton("更新", nil), l)
		},
		func(i widget.ListItemID, o fyne.CanvasObject) {
			u := h.updates[i]
			c := o.(*fyne.Container)
			c.Objects[0].(*widget.Label).SetText(u.Game + " — " + u.Meta.Title)
			btn := c.Objects[1].(*widget.Button)
			btn.OnTapped = func() {
				// 重新解析下载到同一游戏目录,旧文件在成功后清理
				go h.doDownload(provider.Result{
					ProviderID: u.Meta.ProviderID,
					PageURL:    u.Meta.PageURL,
					Title:      u.Meta.Title,
				}, u.Game, u.FilePath)
			}
		},
	)
	dialog.ShowCustom("修改器更新", "关闭",
		container.NewGridWrap(fyne.NewSize(480, 300), list), h.win)
}

// openURL 浏览器打开链接。
func (h *Home) openURL(u string) {
	if parsed, err := url.Parse(u); err == nil {
		_ = h.app.OpenURL(parsed)
	}
}

// openPath 打开本地文件/目录:目录进资源管理器,exe/文件用
// start 拉起(exe 即运行,其他类型走默认关联)。
// file:// URI 经 OpenURL 在 Windows 上常被路由到浏览器而非运行,故不走它。
func (h *Home) openPath(p string) {
	abs, err := filepath.Abs(p)
	if err != nil {
		return
	}
	var cmd *exec.Cmd
	if fi, _ := os.Stat(abs); fi != nil && fi.IsDir() {
		cmd = exec.Command("explorer", abs)
	} else {
		cmd = exec.Command("cmd", "/c", "start", "", abs)
	}
	cmd.Dir = filepath.Dir(abs)
	go func() {
		if err := cmd.Start(); err != nil {
			fyne.Do(func() { h.status.SetText("打开失败: " + err.Error()) })
		}
	}()
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
	c := o.(*fyne.Container)
	name := c.Objects[0].(*fyne.Container).Objects[0].(*widget.Label) // Padded 包的 name
	count := c.Objects[1].(*widget.Label)
	name.SetText(it.label)
	count.SetText(strconv.Itoa(it.count))
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
	// Steam 匹配:非 Steam 平台游戏用 Steam 商店解析 appid 取封面,默认开
	steamMatchCheck := widget.NewCheck("非 Steam 游戏用 Steam 匹配封面", nil)
	steamMatchCheck.Checked = st.SteamMatch()
	// 多线程下载:默认开,线程数按文件大小自动;关闭后单线程
	multiDLCheck := widget.NewCheck("多线程下载(线程数自动)", nil)
	multiDLCheck.Checked = st.MultiDL()

	body := container.NewBorder(
		container.NewVBox(
			container.NewBorder(nil, nil, widget.NewLabel("界面缩放"), nil, scaleSel),
			container.NewBorder(nil, nil, widget.NewLabel("每页条数"), nil, pageSel),
			steamMatchCheck,
			multiDLCheck,
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
		sm := steamMatchCheck.Checked
		st.SteamMatchOthers = &sm
		md := multiDLCheck.Checked
		st.MultiDownload = &md
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
