// Package fling 是 flingtrainer.com 的提供方适配器。
// 通过抓取站点 WordPress 搜索页与详情页 HTML 实现，
// 属尽力而为的解析——该站没有官方 API。
package fling

import (
	"context"
	"errors"
	"net/url"
	"path"
	"regexp"
	"strings"

	"github.com/CNCoreSteb/weandmod/internal/provider"
)

const base = "https://flingtrainer.com"

// Provider 适配 flingtrainer.com。
type Provider struct{}

func init() { provider.Register(Provider{}) }

func (Provider) ID() string      { return "fling" }
func (Provider) Name() string    { return "FLiNG" }
func (Provider) BaseURL() string { return base }

// WordPress 主题的搜索结果标题都是 rel="bookmark" 锚点。
var (
	resultRe = regexp.MustCompile(`<a[^>]+href="(https://flingtrainer\.com/[^"]+)"[^>]*rel="bookmark"[^>]*>(.*?)</a>`)
	tagRe    = regexp.MustCompile(`<[^>]+>`)
)

// Search 实现 provider.Searcher，走站点 ?s= 搜索接口。
func (Provider) Search(ctx context.Context, query string) ([]provider.Result, error) {
	q := strings.TrimSpace(query)
	if q == "" {
		return nil, nil
	}
	body, err := provider.Fetch(ctx, base+"/?s="+url.QueryEscape(q), "text/html")
	if err != nil {
		return nil, err
	}
	var out []provider.Result
	seen := map[string]bool{}
	for _, m := range resultRe.FindAllSubmatch(body, -1) {
		link := string(m[1])
		title := strings.TrimSpace(tagRe.ReplaceAllString(string(m[2]), ""))
		if title == "" || seen[link] {
			continue
		}
		seen[link] = true
		out = append(out, provider.Result{Title: title, PageURL: link})
	}
	return out, nil
}

// linkRe 抓 <a> 标签的整段属性(1)与 href(2),再筛 class="attachment-link"。
var (
	linkRe  = regexp.MustCompile(`<a\b([^>]*?)href="([^"]+)"([^>]*)>`)
	titleRe = regexp.MustCompile(`title="([^"]*)"`)
	ampRe   = regexp.MustCompile(`&amp;`)
)

// Resolve 实现 provider.DownloadResolver：拉取修改器详情页,
// 在附件表中筛 class="attachment-link" 的锚点。
// 优先最新独立版本(/downloads/<token>,token 随版本变化,更新检测才有意义);
// 自动更新版(download.php?title_id=,链接恒定)兜底。
func (Provider) Resolve(ctx context.Context, r provider.Result) (provider.Download, error) {
	page := r.PageURL
	if page == "" {
		return provider.Download{}, errors.New("fling: 结果缺少详情页地址")
	}
	body, err := provider.Fetch(ctx, page, "text/html")
	if err != nil {
		return provider.Download{}, err
	}
	return parseDownload(body)
}

// parseDownload 从详情页 HTML 挑选下载链接(纯函数便于测试)。
// 优先最新独立版本(/downloads/<token>,token 随版本变化,更新检测才有意义);
// 自动更新版(download.php?title_id=,链接恒定)兜底。
func parseDownload(body []byte) (provider.Download, error) {
	type cand struct {
		href, title string
		standalone  bool
	}
	var cands []cand
	for _, m := range linkRe.FindAllSubmatch(body, -1) {
		attrs := string(m[1]) + string(m[3])
		href := ampRe.ReplaceAllString(string(m[2]), "&")
		if !strings.Contains(attrs, "attachment-link") && !isDownloadURL(href) {
			continue
		}
		title := ""
		if tm := titleRe.FindStringSubmatch(attrs); tm != nil {
			title = tm[1]
		}
		cands = append(cands, cand{href, title, strings.Contains(href, "/downloads/")})
	}
	if len(cands) == 0 {
		return provider.Download{}, errors.New("fling: 未在页面中找到下载链接")
	}

	// 优先第一个独立版本链接(列表按新到旧排,第一条即最新)
	best := -1
	for i, c := range cands {
		if c.standalone {
			best = i
			break
		}
	}
	if best < 0 {
		best = 0 // 只有自动更新版等兜底链接
	}
	c := cands[best]

	u := c.href
	if strings.HasPrefix(u, "/") {
		u = base + u
	}
	name := c.title
	if name == "" {
		name = path.Base(u)
	}
	return provider.Download{FileURL: u, FileName: name, Kind: kindOf(u)}, nil
}

// isDownloadURL URL 是否像修改器下载地址。
func isDownloadURL(u string) bool {
	l := strings.ToLower(u)
	return strings.Contains(l, "/downloads/") ||
		strings.Contains(l, "download.php") ||
		strings.Contains(l, "/attachment")
}

// kindOf 从 URL 推断产物类型。
func kindOf(u string) string {
	l := strings.ToLower(u)
	if i := strings.IndexAny(l, "?#"); i >= 0 {
		l = l[:i]
	}
	for _, ext := range []string{".zip", ".rar", ".7z", ".exe"} {
		if strings.HasSuffix(l, ext) {
			return ext[1:]
		}
	}
	return "file"
}
