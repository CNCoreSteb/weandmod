package dl

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/CNCoreSteb/weandmod/internal/provider"
)

// stubProvider 测试用提供方:Resolve 返回固定下载地址。
type stubProvider struct{ fileURL, fileName string }

func (p stubProvider) ID() string      { return "stubtest" }
func (p stubProvider) Name() string    { return "Stub" }
func (p stubProvider) BaseURL() string { return "https://stub.test" }
func (p stubProvider) Resolve(ctx context.Context, r provider.Result) (provider.Download, error) {
	return provider.Download{FileURL: p.fileURL, FileName: p.fileName, Kind: "zip"}, nil
}

func TestMetaRoundTripAndUpdateCheck(t *testing.T) {
	root := t.TempDir()
	SetRoot(root)

	gameDir := filepath.Join(root, "Some Game")
	if err := os.MkdirAll(gameDir, 0o755); err != nil {
		t.Fatal(err)
	}
	// 写修改器本体 + 元数据
	file := filepath.Join(gameDir, "Some Game Trainer.zip")
	if err := os.WriteFile(file, []byte("zip"), 0o644); err != nil {
		t.Fatal(err)
	}
	meta := Meta{
		Title:      "Some Game Trainer",
		ProviderID: "stubtest",
		PageURL:    "https://stub.test/a",
		FileURL:    "http://stub.test/old.zip",
		FileName:   "old.zip",
	}
	if err := WriteMeta(file, meta); err != nil {
		t.Fatal(err)
	}

	ents, err := ScanDownloaded()
	if err != nil || len(ents) != 1 {
		t.Fatalf("scan: %v %d", err, len(ents))
	}
	if ents[0].Game != "Some Game" ||
		ents[0].FilePath != filepath.Join(gameDir, "old.zip") {
		t.Fatalf("entry: %+v", ents[0])
	}
	if !HasTrainer("Some Game") {
		t.Fatal("HasTrainer should be true")
	}

	// 服务器端链接变了 → 检出更新
	provider.Register(stubProvider{"http://stub.test/new.zip", "new.zip"})
	ups := CheckUpdates(context.Background())
	if len(ups) != 1 || ups[0].NewFileName != "new.zip" {
		t.Fatalf("updates: %+v", ups)
	}

	// 与本地记录一致 → 无更新。需要换 provider:注册表不支持重复注册,
	// 直接改 meta 文件模拟已是最新
	meta.FileURL, meta.FileName = "http://stub.test/new.zip", "new.zip"
	if err := WriteMeta(file, meta); err != nil {
		t.Fatal(err)
	}
	if ups := CheckUpdates(context.Background()); len(ups) != 0 {
		t.Fatalf("unexpected updates: %+v", ups)
	}
}
