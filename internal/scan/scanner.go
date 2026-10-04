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
	Games      []game.Game
	PerSource  map[game.Platform]int
	Errors     []error
}

// Scan runs every scanner concurrently and returns merged, deduplicated games.
// Platform results win over custom-directory hits with the same name.
func Scan(customDirs []string) Result {
	var (
		mu     sync.Mutex
		wg     sync.WaitGroup
		games  []game.Game
		errs   []error
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
	run(scanSteam)
	run(scanEpic)
	run(scanGOG)
	for _, dir := range customDirs {
		d := dir
		run(func() []game.Game { return scanCustom(d) })
	}
	wg.Wait()

	seen := map[string]bool{}
	var merged []game.Game
	// Platforms first so custom hits lose the tiebreak.
	sort.SliceStable(games, func(i, j int) bool {
		return rank(games[i].Platform) < rank(games[j].Platform)
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
	return Result{Games: merged, PerSource: per, Errors: errs}
}

func rank(p game.Platform) int {
	switch p {
	case game.PlatformSteam:
		return 0
	case game.PlatformEpic:
		return 1
	case game.PlatformGOG:
		return 2
	}
	return 3
}

func safe(fn func() []game.Game) (out []game.Game) {
	defer func() { _ = recover() }()
	return fn()
}
