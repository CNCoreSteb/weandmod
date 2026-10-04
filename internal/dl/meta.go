package dl

import (
	"context"
	"encoding/json"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"sync"

	"github.com/CNCoreSteb/weandmod/internal/provider"
)

// Meta 记录一次下载的来源信息,以「文件名.json」旁挂在修改器旁,
// 供启动时重新解析原文页做更新检测。
type Meta struct {
	Title      string `json:"title"`       // 文章标题(也是文件名)
	ProviderID string `json:"provider_id"` // 提供方,用于重新解析
	PageURL    string `json:"page_url"`    // 原文链接
	FileURL    string `json:"file_url"`    // 下载链接
	FileName   string `json:"file_name"`   // 下载链接的名称
}

// metaPath 文件对应的元数据路径:<basename>.json。
func metaPath(filePath string) string {
	ext := filepath.Ext(filePath)
	return strings.TrimSuffix(filePath, ext) + ".json"
}

// WriteMeta 在下载文件旁写同名 .json 元数据。
func WriteMeta(filePath string, m Meta) error {
	b, err := json.MarshalIndent(m, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(metaPath(filePath), b, 0o644)
}

// Entry 扫描到的一条已下载修改器。
type Entry struct {
	Game     string // 归属游戏(目录名)
	FilePath string // 修改器本体绝对路径
	MetaPath string // 元数据 .json 路径
	Meta     Meta
}

// ScanDownloaded 遍历下载根目录,读取所有 .json 元数据。
func ScanDownloaded() ([]Entry, error) {
	root, err := Root()
	if err != nil {
		return nil, err
	}
	var out []Entry
	err = filepath.WalkDir(root, func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			return nil // 目录不存在等问题跳过
		}
		if d.IsDir() || !strings.HasSuffix(d.Name(), ".json") {
			return nil
		}
		b, err := os.ReadFile(p)
		if err != nil {
			return nil
		}
		var m Meta
		if json.Unmarshal(b, &m) != nil || m.PageURL == "" {
			return nil // 非元数据文件或损坏
		}
		out = append(out, Entry{
			Game:     filepath.Base(filepath.Dir(p)),
			FilePath: filepath.Join(filepath.Dir(p), m.FileName),
			MetaPath: p,
			Meta:     m,
		})
		return nil
	})
	return out, err
}

// Update 一条检测到的新版本修改器。
type Update struct {
	Entry
	NewFileURL  string // 重新解析出的新下载链接
	NewFileName string // 新链接的名称
}

// CheckUpdates 并发重新解析每条已下载记录的原文页,
// 下载链接或其名称与本地记录不一致即视为有更新。网络错误跳过。
func CheckUpdates(ctx context.Context) []Update {
	ents, err := ScanDownloaded()
	if err != nil || len(ents) == 0 {
		return nil
	}
	var mu sync.Mutex
	var ups []Update
	var wg sync.WaitGroup
	for _, e := range ents {
		e := e
		wg.Add(1)
		go func() {
			defer wg.Done()
			d, err := provider.Resolve(ctx, provider.Result{
				ProviderID: e.Meta.ProviderID,
				PageURL:    e.Meta.PageURL,
			})
			if err != nil || d.FileURL == "" {
				return
			}
			if d.FileURL != e.Meta.FileURL || d.FileName != e.Meta.FileName {
				mu.Lock()
				ups = append(ups, Update{Entry: e, NewFileURL: d.FileURL, NewFileName: d.FileName})
				mu.Unlock()
			}
		}()
	}
	wg.Wait()
	return ups
}
