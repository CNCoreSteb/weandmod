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

// hrefRe 抓取所有锚点链接，Resolve 再筛出像下载地址的。
var hrefRe = regexp.MustCompile(`href="([^"]+)"`)

// Resolve 实现 provider.DownloadResolver：拉取修改器详情页，
// 选出第一个疑似修改器文件的链接。
func (Provider) Resolve(ctx context.Context, r provider.Result) (provider.Download, error) {
	page := r.PageURL
	if page == "" {
		return provider.Download{}, errors.New("fling: 结果缺少详情页地址")
	}
	body, err := provider.Fetch(ctx, page, "text/html")
	if err != nil {
		return provider.Download{}, err
	}
	for _, m := range hrefRe.FindAllSubmatch(body, -1) {
		u := strings.TrimSpace(string(m[1]))
		if k, ok := downloadKind(u); ok {
			return provider.Download{FileURL: u, FileName: path.Base(u), Kind: k}, nil
		}
	}
	return provider.Download{}, errors.New("fling: 未在页面中找到下载链接")
}

// downloadKind 判断 URL 是否像修改器产物，返回产物类型。
func downloadKind(u string) (string, bool) {
	l := strings.ToLower(u)
	// 去掉 query/fragment 再判断后缀
	if i := strings.IndexAny(l, "?#"); i >= 0 {
		l = l[:i]
	}
	for _, ext := range []string{".zip", ".rar", ".7z", ".exe"} {
		if strings.HasSuffix(l, ext) {
			return ext[1:], true
		}
	}
	if strings.Contains(l, "/download/") || strings.Contains(l, "/attachment") {
		return "page", true
	}
	return "", false
}
