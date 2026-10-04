package fling

import "testing"

// 页面附件表的真实片段(用户提供)。
const pageHTML = `<div class="download-attachments style-table">
<table class="da-attachments-table">
<tbody><tr class="alt"><td class="attachment-title" colspan="4">Auto-Updating Version:</td></tr>
<tr class="exe autoupdate"><td class="attachment-title"><a style="margin-left: 2px;" href="/download.php?title_id=47311&amp;source=website_attachment" rel="nofollow" class="attachment-link" title="Dyson.Sphere.Program.LatestVersion.Plus.17.Trainer-FLiNG">x</a></td></tr>
<tr class="alt"><td class="attachment-title" colspan="4">Standalone Versions:</td></tr>
<tr class="zip alt"><td class="attachment-title">
<a href="https://flingtrainer.com/downloads/vDkaSt9Yav0PwQkknnWtbQ,," title="Dyson.Sphere.Program.Early.Access.Plus.17.Trainer.Updated.2025.10.12-FLiNG" class="attachment-link" target="_self">x</a>
</td></tr>
<tr class="zip alt"><td class="attachment-title">
<a href="https://flingtrainer.com/downloads/qc5R2olKU2I-SXBrBaHoBw,," title="Dyson.Sphere.Program.Early.Access.Plus.17.Trainer.Updated.2024.12.08-FLiNG" class="attachment-link" target="_self">x</a>
</td></tr>
</tbody></table></div>`

func TestParseDownloadPrefersStandalone(t *testing.T) {
	d, err := parseDownload([]byte(pageHTML), "https://flingtrainer.com/x")
	if err != nil {
		t.Fatal(err)
	}
	// 应选第一条 /downloads/ 独立版(最新),而不是恒定的 download.php 自动更新版
	if d.FileURL != "https://flingtrainer.com/downloads/vDkaSt9Yav0PwQkknnWtbQ,," {
		t.Fatalf("file_url = %q", d.FileURL)
	}
	if d.FileName != "Dyson.Sphere.Program.Early.Access.Plus.17.Trainer.Updated.2025.10.12-FLiNG" {
		t.Fatalf("file_name = %q", d.FileName)
	}
}

func TestParseDownloadPageOnly(t *testing.T) {
	// 只有自动更新版(无独立版)时返回 page 类型,由 UI 只显示打开页面
	const onlyAuto = `<a href="/download.php?title_id=1&amp;source=x" rel="nofollow" class="attachment-link" title="G.Trainer-FLiNG">x</a>`
	d, err := parseDownload([]byte(onlyAuto), "https://flingtrainer.com/x")
	if err != nil {
		t.Fatal(err)
	}
	if d.Kind != "page" || d.FileURL != "https://flingtrainer.com/x" {
		t.Fatalf("expected page-only, got %+v", d)
	}
}
