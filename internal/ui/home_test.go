package ui

import (
	"strings"
	"testing"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/test"

	"github.com/CNCoreSteb/weandmod/internal/game"
	"github.com/CNCoreSteb/weandmod/internal/store"
)

func newTestHome(t *testing.T) *Home {
	t.Helper()
	autoScan = false // 测试环境关闭真实扫描
	a := test.NewApp()
	w := test.NewWindow(nil)
	w.Resize(fyne.NewSize(1180, 760))
	return NewHome(a, w, store.NewAt(t.TempDir()))
}

// TestSidebarPopulates 无头验证侧栏导航被数据填充。
func TestSidebarPopulates(t *testing.T) {
	h := newTestHome(t)

	if len(h.navItems) == 0 {
		t.Fatal("navItems 为空")
	}
	if h.navItems[0].label != "全部" {
		t.Fatalf("首项应为'全部',得到 %q", h.navItems[0].label)
	}

	h.games = []game.Game{
		{Name: "A", Platform: game.PlatformSteam},
		{Name: "B", Platform: game.PlatformSteam},
		{Name: "C", Platform: game.PlatformEpic},
	}
	fyne.DoAndWait(func() { h.refresh() })

	if len(h.navItems) != 3 { // 全部 + Steam + Epic
		t.Fatalf("navItems=%d, 期望 3: %+v", len(h.navItems), h.navItems)
	}
	if h.navItems[1].count != 2 || h.navItems[2].count != 1 {
		t.Fatalf("计数错误: %+v", h.navItems)
	}

	// 切平台过滤
	fyne.DoAndWait(func() {
		h.platform = game.PlatformEpic
		h.refresh()
	})
	var gameRows int
	for _, r := range h.allRows {
		if r.kind == rowGame {
			gameRows++
		}
	}
	if gameRows != 1 {
		t.Fatalf("Epic 过滤后应有 1 行,得到 %d", gameRows)
	}
}

// TestSidebarRenders 渲染侧栏到 markup,验证导航行真的被画出来。
func TestSidebarRenders(t *testing.T) {
	h := newTestHome(t)
	h.games = []game.Game{
		{Name: "A", Platform: game.PlatformSteam},
		{Name: "C", Platform: game.PlatformEpic},
	}
	fyne.DoAndWait(func() { h.refresh() })

	mk := test.RenderObjectToMarkup(h.nav)
	t.Log("nav markup:", mk)
	if !strings.Contains(mk, "全部") {
		t.Fatal("侧栏渲染中缺少'全部'项")
	}
	if !strings.Contains(mk, "Steam") {
		t.Fatal("侧栏渲染中缺少'Steam'项")
	}
}

// TestAutoPageSize 无头验证按高度自动分页。
func TestAutoPageSize(t *testing.T) {
	h := newTestHome(t)

	var gs []game.Game
	for i := 0; i < 100; i++ {
		gs = append(gs, game.Game{Name: "G", Platform: game.PlatformSteam})
	}
	fyne.DoAndWait(func() {
		h.games = gs
		h.listHeight = 500
		h.refresh()
	})
	ps := h.pageSize()
	if ps < 6 || ps > 500 {
		t.Fatalf("自动每页 %d 异常", ps)
	}
	if len(h.rows) != ps {
		t.Fatalf("首页行数 %d != 每页 %d", len(h.rows), ps)
	}
	tp := h.totalPages()
	if tp < 2 {
		t.Fatalf("100 项应至少 2 页,得到 %d", tp)
	}
	fyne.DoAndWait(func() { h.gotoPage(1) })
	if h.page != 1 {
		t.Fatal("翻页失败")
	}
}
