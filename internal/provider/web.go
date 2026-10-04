package provider

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"time"
)

var httpClient = &http.Client{Timeout: 15 * time.Second}

const maxBodyBytes = 4 << 20 // 抓取页面限制 4 MiB

// Fetch 以浏览器 UA 拉取 URL 并返回正文（有上限）。
// 各适配器共用，避免重复造轮子。
func Fetch(ctx context.Context, rawurl, accept string) ([]byte, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, rawurl, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("User-Agent", "Mozilla/5.0 (Windows NT 10.0; Win64; x64) WeAndMod/0.1")
	if accept != "" {
		req.Header.Set("Accept", accept)
	}
	resp, err := httpClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("GET %s: HTTP %d", rawurl, resp.StatusCode)
	}
	return io.ReadAll(io.LimitReader(resp.Body, maxBodyBytes))
}
