package ui

import (
	"context"
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

// ---- 字母占位图块 ----

// tilePalette 按名字 hash 取色的深色系色板。
var tilePalette = []color.RGBA{
	{0x5b, 0x21, 0xb6, 0xff}, {0x0f, 0x76, 0x6e, 0xff}, {0xb4, 0x53, 0x09, 0xff},
	{0x9d, 0x17, 0x4d, 0xff}, {0x1d, 0x4e, 0xd8, 0xff}, {0x4d, 0x7c, 0x0f, 0xff},
	{0xa2, 0x1c, 0xaf, 0xff}, {0x0e, 0x74, 0x90, 0xff},
}

// coverBox 是一行左侧的封面区:底色块 + 首字母 + 封面图层。
// currentID 记录当前绑定的游戏 ID,防止行复用后异步封面贴错对象。
type coverBox struct {
	container *fyne.Container
	img       *canvas.Image
	rect      *canvas.Rectangle
	letter    *canvas.Text
	currentID string
}

func newCoverBox() *coverBox {
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
	return &coverBox{
		container: container.NewGridWrap(fyne.NewSize(coverW, coverH), stack),
		img:       img,
		rect:      rect,
		letter:    letter,
	}
}

// setGame 按游戏名刷新占位块(无封面时显示)。
func (b *coverBox) setGame(name string) {
	h := 0
	for _, r := range name {
		h = h*31 + int(r)
	}
	if h < 0 {
		h = -h
	}
	b.rect.FillColor = tilePalette[h%len(tilePalette)]

	initial := "?"
	for _, r := range name { // 取首个字符(含中文)
		initial = strings.ToUpper(string(r))
		break
	}
	b.letter.Text = initial
	b.rect.Refresh()
	b.letter.Refresh()
}

// resetImage 行复用时清掉封面图。
func (b *coverBox) resetImage() {
	b.img.File = ""
	b.img.Resource = nil
	b.img.Hide()
}

// ---- 异步封面加载 ----

var (
	coverInflight sync.Map // cachePath -> bool,防止重复下载
	coverClient   = &http.Client{Timeout: 12 * time.Second}
)

// loadCover 异步加载游戏封面:先命中磁盘缓存,没有再走网络下载。
// url 为空(非 Steam)时不动图片层,保留占位块。
// onReady 在下载完成后调用(让调用方刷新可见行,命中新缓存)。
func loadCover(db *store.Store, g game.Game, b *coverBox, onReady func()) {
	u := coverURL(g)
	if u == "" {
		return
	}
	b.currentID = g.ID
	cache := coverPath(db.CoversDir(), g)
	if _, err := os.Stat(cache); err == nil {
		b.img.File = cache
		b.img.Resource = nil
		b.img.Show()
		b.img.Refresh()
		return
	}
	if _, loaded := coverInflight.LoadOrStore(cache, true); loaded {
		return // 已在下载;完成后由下一轮列表刷新命中缓存
	}
	go func() {
		defer coverInflight.Delete(cache)
		if err := downloadCover(u, cache); err != nil {
			return // 失败保持占位块
		}
		fyne.Do(func() {
			// 行对象可能已被复用给其他游戏,校验后再贴图
			if b.currentID == g.ID {
				b.img.File = cache
				b.img.Resource = nil
				b.img.Show()
				b.img.Refresh()
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
