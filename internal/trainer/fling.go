package trainer

import (
	"context"
	"fmt"
	"net/http"
	"net/url"
	"regexp"
	"strings"
	"time"
)

// Fling searches flingtrainer.com via its WordPress search endpoint.
// Best-effort scraping — the site layout may change.
type Fling struct{}

const flingBase = "https://flingtrainer.com"

// entry-title anchors carry the trainer post title and link.
var flingResultRe = regexp.MustCompile(`<a[^>]+href="(https://flingtrainer\.com/[^"]+)"[^>]*rel="bookmark"[^>]*>(.*?)</a>`)
var flingTagRe = regexp.MustCompile(`<[^>]+>`)

func (f *Fling) Name() string { return "FLiNG" }

func (f *Fling) Search(ctx context.Context, query string) ([]Trainer, error) {
	q := strings.TrimSpace(query)
	if q == "" {
		return nil, nil
	}
	reqURL := flingBase + "/?s=" + url.QueryEscape(q)
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, reqURL, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("User-Agent", "Mozilla/5.0 (Windows NT 10.0; Win64; x64) WeAndMod/0.1")
	req.Header.Set("Accept", "text/html")

	client := &http.Client{Timeout: 15 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("fling: HTTP %d", resp.StatusCode)
	}
	body := make([]byte, 0, 1<<20)
	buf := make([]byte, 32*1024)
	for len(body) < 2<<20 {
		n, err := resp.Body.Read(buf)
		body = append(body, buf[:n]...)
		if err != nil {
			break
		}
	}

	var out []Trainer
	seen := map[string]bool{}
	for _, m := range flingResultRe.FindAllSubmatch(body, -1) {
		link := string(m[1])
		title := strings.TrimSpace(flingTagRe.ReplaceAllString(string(m[2]), ""))
		if title == "" || seen[link] {
			continue
		}
		seen[link] = true
		out = append(out, Trainer{Title: title, URL: link, Source: f.Name()})
	}
	return out, nil
}
