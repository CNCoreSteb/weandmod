// Package scan discovers installed games from store platforms and
// user-supplied directories.
package scan

import (
	"sort"
	"strings"
	"sync"

	"github.com/CNCoreSteb/weandmod/internal/game"
)

// Result is the outcome of a full library scan.
type Result struct {
	Games     []game.Game
	PerSource map[game.Platform]int
	Errors    []error
}

// platformScanners 各平台扫描入口(自定义目录单独处理)。
var platformScanners = []func() []game.Game{
	scanSteam,
	scanEpic,
	scanGOG,
	scanXbox,
	scanWeGame,
	scanRegInstalls,   // Ubisoft / Rockstar / EA 安装记录
	scanUninstallGames, // Battle.net 等按发行商兜底
}

// Scan runs every scanner concurrently and returns merged, deduplicated games.
// Platform results win over custom-directory hits with the same name.
func Scan(customDirs []string) Result {
	var (
		mu    sync.Mutex
		wg    sync.WaitGroup
		games []game.Game
	)
	run := func(fn func() []game.Game) {
		wg.Add(1)
		go func() {
			defer wg.Done()
			got := safe(fn)
			mu.Lock()
			games = append(games, got...)
			mu.Unlock()
		}()
	}
	for _, fn := range platformScanners {
		run(fn)
	}
	for _, dir := range customDirs {
		d := dir
		run(func() []game.Game { return scanCustom(d) })
	}
	wg.Wait()

	seen := map[string]bool{}
	var merged []game.Game
	// 先按平台优先级排序,平台结果优先于本地目录同名命中
	order := map[game.Platform]int{}
	for i, p := range game.PlatformOrder {
		order[p] = i
	}
	sort.SliceStable(games, func(i, j int) bool {
		return order[games[i].Platform] < order[games[j].Platform]
	})
	for _, g := range games {
		k := g.Key()
		if k == "" || seen[k] {
			continue
		}
		seen[k] = true
		merged = append(merged, g)
	}
	sort.Slice(merged, func(i, j int) bool {
		return strings.ToLower(merged[i].Name) < strings.ToLower(merged[j].Name)
	})

	per := map[game.Platform]int{}
	for _, g := range merged {
		per[g.Platform]++
	}
	return Result{Games: merged, PerSource: per}
}

func safe(fn func() []game.Game) (out []game.Game) {
	defer func() { _ = recover() }()
	return fn()
}
