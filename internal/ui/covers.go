package ui

import (
	"context"
	"errors"
	"fmt"
	"image/color"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"sync"
	"time"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/canvas"
	"fyne.io/fyne/v2/container"
	"fyne.io/fyne/v2/layout"

	"github.com/CNCoreSteb/weandmod/internal/game"
	"github.com/CNCoreSteb/weandmod/internal/store"
)

// coverW/coverH 行内封面尺寸(2.1:1,对应 Steam header.jpg 比例)。
const (
	coverW = 96
	coverH = 46
)

// coverURL 目前只有 Steam 有公开封面 CDN,其余平台返回空走占位块。
func coverURL(g game.Game) string {
	if g.Platform == game.PlatformSteam && strings.HasPrefix(g.ID, "steam:") {
		appid := strings.TrimPrefix(g.ID, "steam:")
		return "https://cdn.cloudflare.steamstatic.com/steam/apps/" + appid + "/header.jpg"
	}
	return ""
}

var coverFileRe = regexp.MustCompile(`[^a-zA-Z0-9_-]`)

func coverPath(dir string, g game.Game) string {
	name := coverFileRe.ReplaceAllString(g.ID, "_")
	return filepath.Join(dir, name+".jpg")
}

// ---- 封面容器(GridWrap{Stack{rect, Center{letter}, img}}) ----

// tilePalette 按名字 hash 取色的深色系色板。
var tilePalette = []color.RGBA{
	{0x5b, 0x21, 0xb6, 0xff}, {0x0f, 0x76, 0x6e, 0xff}, {0xb4, 0x53, 0x09, 0xff},
	{0x9d, 0x17, 0x4d, 0xff}, {0x1d, 0x4e, 0xd8, 0xff}, {0x4d, 0x7c, 0x0f, 0xff},
	{0xa2, 0x1c, 0xaf, 0xff}, {0x0e, 0x74, 0x90, 0xff},
}

// newCoverBox 创建封面容器。结构:
//   GridWrap(96x46) -> Stack[ rect底色 | Center(letter首字母) | img封面 ]
func newCoverBox() *fyne.Container {
	img := canvas.NewImageFromResource(nil)
	img.FillMode = canvas.ImageFillContain
	img.SetMinSize(fyne.NewSize(coverW, coverH))
	img.Hide()

	rect := canvas.NewRectangle(tilePalette[0])
	rect.CornerRadius = 6
	letter := canvas.NewText("?", color.White)
	letter.TextStyle = fyne.TextStyle{Bold: true}
	letter.TextSize = 18

	stack := container.NewStack(rect, container.NewCenter(letter), img)
	return container.NewGridWrap(fyne.NewSize(coverW, coverH), stack)
}

// coverParts 拆解封面容器的内部对象。
func coverParts(c *fyne.Container) (rect *canvas.Rectangle, letter *canvas.Text, img *canvas.Image) {
	stack := c.Objects[0].(*fyne.Container)
	return stack.Objects[0].(*canvas.Rectangle),
		stack.Objects[1].(*fyne.Container).Objects[0].(*canvas.Text),
		stack.Objects[2].(*canvas.Image)
}

// coverReset 行复用时清掉封面图。
func coverReset(c *fyne.Container) {
	_, _, img := coverParts(c)
	img.File = ""
	img.Resource = nil
	img.Hide()
}

// coverPlaceholder 设置占位块颜色与首字母。
func coverPlaceholder(c *fyne.Container, name string) {
	rect, letter, _ := coverParts(c)
	h := 0
	for _, r := range name {
		h = h*31 + int(r)
	}
	if h < 0 {
		h = -h
	}
	rect.FillColor = tilePalette[h%len(tilePalette)]

	initial := "?"
	for _, r := range name { // 取首个字符(含中文)
		initial = strings.ToUpper(string(r))
		break
	}
	letter.Text = initial
	rect.Refresh()
	letter.Refresh()
}

// ---- 异步封面加载 ----

var (
	coverInflight sync.Map // cachePath -> bool,防止重复下载
	coverAssigned sync.Map // *canvas.Image -> 游戏ID,防行复用贴错图
	coverClient   = &http.Client{Timeout: 12 * time.Second}
)

// errNotFound 资源不存在(404),写入负缓存避免反复请求。
var errNotFound = fmt.Errorf("not found")

// loadCover 异步加载游戏封面:先命中磁盘缓存,其次 Steam 直链,
// 非 Steam 平台在开启 Steam 匹配时先解析 appid 再拉封面。
// onReady 在下载完成后调用(让调用方刷新可见行,命中新缓存)。
func loadCover(db *store.Store, g game.Game, cover *fyne.Container, onReady func()) {
	cache := coverPath(db.CoversDir(), g)
	if _, err := os.Stat(cache); err == nil {
		_, _, img := coverParts(cover)
		img.File = cache
		img.Resource = nil
		img.Show()
		img.Refresh()
		return
	}
	miss := cache + ".miss"
	if _, err := os.Stat(miss); err == nil {
		return // 已确认无封面
	}

	u := coverURL(g)
	needResolve := false
	if u == "" {
		if !db.Settings().SteamMatch() {
			return // 未开启 Steam 匹配,保持占位块
		}
		needResolve = true
	}

	_, _, img := coverParts(cover)
	coverAssigned.Store(img, g.ID)
	if _, loaded := coverInflight.LoadOrStore(cache, true); loaded {
		return // 已在下载;完成后由下一轮列表刷新命中缓存
	}
	go func() {
		defer coverInflight.Delete(cache)
		uu := u
		if needResolve {
			// 用 Steam 商店搜索把游戏名解析成 appid
			ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
			appid, found, err := resolveSteamAppID(ctx, g.Name)
			cancel()
			if err != nil {
				return // 网络失败,下次再试
			}
			if !found {
				_ = os.WriteFile(miss, nil, 0o644) // 负缓存
				return
			}
			uu = "https://cdn.cloudflare.steamstatic.com/steam/apps/" + appid + "/header.jpg"
		}
		err := downloadCover(uu, cache)
		if err != nil {
			if errors.Is(err, errNotFound) {
				_ = os.WriteFile(miss, nil, 0o644)
			}
			return // 失败保持占位块
		}
		fyne.Do(func() {
			// 行对象可能已被复用给其他游戏,校验后再贴图
			if id, ok := coverAssigned.Load(img); ok && id == g.ID {
				img.File = cache
				img.Resource = nil
				img.Show()
				img.Refresh()
			}
			if onReady != nil {
				onReady() // 其他等待同一封面的行刷新后会命中缓存
			}
		})
	}()
}

func downloadCover(u, dest string) error {
	req, err := http.NewRequestWithContext(context.Background(), http.MethodGet, u, nil)
	if err != nil {
		return err
	}
	req.Header.Set("User-Agent", "Mozilla/5.0 WeAndMod/0.1")
	resp, err := coverClient.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode == http.StatusNotFound {
		return errNotFound
	}
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("cover: HTTP %d", resp.StatusCode)
	}
	data, err := io.ReadAll(io.LimitReader(resp.Body, 2<<20))
	if err != nil {
		return err
	}
	tmp := dest + ".tmp"
	if err := os.WriteFile(tmp, data, 0o644); err != nil {
		return err
	}
	return os.Rename(tmp, dest)
}

// notifyLayout 包装布局:布局时把容器尺寸回调出去
//(用于窗口尺寸变化时重算自动分页大小)。
type notifyLayout struct {
	base     fyne.Layout
	onLayout func(fyne.Size)
}

func (l notifyLayout) Layout(objects []fyne.CanvasObject, size fyne.Size) {
	l.base.Layout(objects, size)
	if l.onLayout != nil {
		l.onLayout(size)
	}
}

func (l notifyLayout) MinSize(objects []fyne.CanvasObject) fyne.Size {
	return l.base.MinSize(objects)
}

// newSizedStack 用 Stack 布局包装对象,并在每次布局时回调尺寸。
func newSizedStack(onLayout func(fyne.Size), objects ...fyne.CanvasObject) *fyne.Container {
	return container.New(notifyLayout{base: layout.NewStackLayout(), onLayout: onLayout}, objects...)
}
