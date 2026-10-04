package provider

import (
	"context"
	"errors"
)

// SearchAll 并发查询所有实现了 Searcher 的提供方并合并结果，
// 每条结果自动回填 ProviderID/Provider 显示名。
// 仅当所有提供方都失败时才返回 error。
func SearchAll(ctx context.Context, query string) ([]Result, error) {
	providers := All()
	type outcome struct {
		res []Result
		err error
	}
	ch := make(chan outcome, len(providers))
	for _, p := range providers {
		p := p
		go func() {
			s, ok := p.(Searcher)
			if !ok {
				ch <- outcome{}
				return
			}
			res, err := s.Search(ctx, query)
			for i := range res {
				res[i].ProviderID = p.ID()
				res[i].Provider = p.Name()
			}
			ch <- outcome{res, err}
		}()
	}
	var merged []Result
	var errs []error
	for range providers {
		o := <-ch
		merged = append(merged, o.res...)
		if o.err != nil {
			errs = append(errs, o.err)
		}
	}
	if len(merged) == 0 && len(errs) > 0 {
		return nil, errors.Join(errs...)
	}
	return merged, nil
}

// Resolve 把一条结果解析为 Download。若提供方未实现
// DownloadResolver，则回退为 "page" 类型（只能打开网页）。
func Resolve(ctx context.Context, r Result) (Download, error) {
	p, ok := Get(r.ProviderID)
	if ok {
		if rs, ok := p.(DownloadResolver); ok {
			return rs.Resolve(ctx, r)
		}
	}
	return Download{FileURL: r.PageURL, Kind: "page"}, nil
}
