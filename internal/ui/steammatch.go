package ui

import (
	"context"
	"encoding/json"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"sync"
	"time"
)

// steammatch 用 Steam 商店搜索 API 把任意游戏名解析为 appid,
// 供非 Steam 平台游戏匹配封面等元数据。

var (
	steamResolveCache sync.Map // 归一化名字 -> appid("":未匹配)
	steamHTTP         = &http.Client{Timeout: 12 * time.Second}
)

type steamSearchResp struct {
	Items []struct {
		ID   int    `json:"id"`
		Name string `json:"name"`
	} `json:"items"`
}

// normalizeGameName 归一化游戏名用于匹配:小写、去标点和商标符。
func normalizeGameName(s string) string {
	s = strings.ToLower(s)
	var b strings.Builder
	for _, r := range s {
		switch {
		case r >= 'a' && r <= 'z', r >= '0' && r <= '9', r > 127: // 保留字母数字和CJK
			b.WriteRune(r)
		}
	}
	return b.String()
}

// resolveSteamAppID 按名字查 Steam 商店,返回匹配到的 appid。
// found=false 表示确认无匹配(可负缓存);err 表示网络失败(不缓存)。
func resolveSteamAppID(ctx context.Context, name string) (appid string, found bool, err error) {
	key := normalizeGameName(name)
	if key == "" {
		return "", false, nil
	}
	if v, ok := steamResolveCache.Load(key); ok {
		id := v.(string)
		return id, id != "", nil
	}

	u := "https://store.steampowered.com/api/storesearch/?term=" +
		url.QueryEscape(name) + "&cc=us&l=en&format=json"
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, u, nil)
	if err != nil {
		return "", false, err
	}
	req.Header.Set("User-Agent", "Mozilla/5.0 WeAndMod/0.1")
	resp, err := steamHTTP.Do(req)
	if err != nil {
		return "", false, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return "", false, nil
	}
	var sr steamSearchResp
	if json.NewDecoder(resp.Body).Decode(&sr) != nil {
		return "", false, nil
	}

	// 匹配策略:归一化完全相等优先,其次互为包含;都不满足视为未匹配
	for _, it := range sr.Items {
		if normalizeGameName(it.Name) == key {
			appid = strconv.Itoa(it.ID)
			break
		}
	}
	if appid == "" {
		for _, it := range sr.Items {
			n := normalizeGameName(it.Name)
			if n != "" && (strings.Contains(n, key) || strings.Contains(key, n)) {
				appid = strconv.Itoa(it.ID)
				break
			}
		}
	}
	steamResolveCache.Store(key, appid)
	return appid, appid != "", nil
}
