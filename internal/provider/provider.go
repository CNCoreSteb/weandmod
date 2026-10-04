// Package provider 定义修改器提供方的插件模型。
//
// 每个 Provider 适配一个修改器站点/社区。适配器在自己的子包中
// 通过 init() 调用 Register 完成注册，再在 main.go 中以
// blank import 的方式启用。
//
// 可选能力以额外接口表达，调用点做类型断言检测：
//
//	Searcher         – 按关键词搜索修改器
//	DownloadResolver – 把搜索结果解析为直链下载地址
//
// 适配器只需实现其站点支持的能力。
package provider

import "context"

// Provider 标识一个修改器来源适配器。
type Provider interface {
	// ID 是稳定标识符，如 "fling"。
	ID() string
	// Name 是展示名。
	Name() string
	// BaseURL 是站点根地址（用于署名/浏览器打开）。
	BaseURL() string
}

// Searcher 由支持关键词搜索的提供方实现。
type Searcher interface {
	Search(ctx context.Context, query string) ([]Result, error)
}

// DownloadResolver 由能把结果解析成直接下载产物的提供方实现
//（例如解析详情页拿到修改器文件直链）。
type DownloadResolver interface {
	Resolve(ctx context.Context, r Result) (Download, error)
}

// Result 是一条修改器搜索结果。ProviderID/Provider 由 SearchAll
// 统一回填，适配器只需设置 Title/PageURL。
type Result struct {
	ProviderID string `json:"provider_id"`
	Provider   string `json:"provider"`
	Title      string `json:"title"`
	PageURL    string `json:"page_url"`
}

// Download 是解析出的可下载产物。Kind 取值：
// "zip" / "rar" / "7z" / "exe" / "page"（兜底：只能打开网页）。
type Download struct {
	FileURL  string `json:"file_url"`
	FileName string `json:"file_name"`
	Kind     string `json:"kind"`
}
