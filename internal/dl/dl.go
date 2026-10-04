// Package dl 管理修改器下载:统一放在用户「文档」目录下的
// WeAndMod\<游戏名>\ 中,按目录内容判断游戏是否已有修改器。
// 支持 HTTP Range 多线程分块下载,并跟踪进行中任务的进度。
package dl

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
)

// ErrHTML 目标返回的是 HTML 页面而非文件,调用方应回退为打开网页。
var ErrHTML = errors.New("dl: 返回的是网页而非文件")

var client = &http.Client{}

// ---- 进度跟踪 ----

// task 一次进行中的下载任务。
type task struct {
	total    int64
	done     atomic.Int64
	threads  int
	parallel bool
}

// Progress 是任务进度的快照。
type Progress struct {
	Total    int64 // 总大小,未知为 0
	Done     int64 // 已下载字节数
	Threads  int   // 实际线程数
	Parallel bool  // 是否多线程分块
}

var active sync.Map // 归属名(游戏名/标题) -> *task

// ProgressOf 查询某归属名下的进行中下载进度。
func ProgressOf(key string) (Progress, bool) {
	v, ok := active.Load(key)
	if !ok {
		return Progress{}, false
	}
	t := v.(*task)
	return Progress{Total: t.total, Done: t.done.Load(), Threads: t.threads, Parallel: t.parallel}, true
}

// HasActive 是否有任何下载在进行(驱动 UI 定时刷新)。
func HasActive() bool {
	found := false
	active.Range(func(_, _ any) bool {
		found = true
		return false
	})
	return found
}

// ---- 目录 ----

var rootOverride string // 测试覆写根目录

// SetRoot 覆盖下载根目录(仅测试用)。
func SetRoot(dir string) { rootOverride = dir }

// Root 下载根目录:文档\WeAndMod。
func Root() (string, error) {
	if rootOverride != "" {
		return rootOverride, nil
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(home, "Documents", "WeAndMod"), nil
}

var invalidChars = regexp.MustCompile(`[<>:"/\\|?*\x00-\x1f]`)

// SanitizeName 把游戏名清理成合法目录/文件名。
func SanitizeName(name string) string {
	s := invalidChars.ReplaceAllString(name, " ")
	s = strings.Join(strings.Fields(s), " ")
	s = strings.TrimRight(strings.TrimSpace(s), ".")
	if len(s) > 80 {
		s = s[:80]
	}
	if s == "" {
		s = "unknown"
	}
	return s
}

// GameDir 某游戏的修改器目录(不保证已创建)。
func GameDir(gameName string) (string, error) {
	root, err := Root()
	if err != nil {
		return "", err
	}
	return filepath.Join(root, SanitizeName(gameName)), nil
}

// Files 返回游戏目录下已下载的文件绝对路径。
func Files(gameName string) []string {
	dir, err := GameDir(gameName)
	if err != nil {
		return nil
	}
	ents, err := os.ReadDir(dir)
	if err != nil {
		return nil
	}
	var out []string
	for _, e := range ents {
		if e.IsDir() || strings.HasSuffix(e.Name(), ".part") {
			continue
		}
		out = append(out, filepath.Join(dir, e.Name()))
	}
	return out
}

// HasTrainer 该游戏目录下是否已有下载的修改器。
func HasTrainer(gameName string) bool { return len(Files(gameName)) > 0 }

// OpenTarget 游戏行「打开」的目标:仅一个文件时打开文件本身,
// 否则打开目录。
func OpenTarget(gameName string) (string, error) {
	fs := Files(gameName)
	if len(fs) == 1 {
		return fs[0], nil
	}
	dir, err := GameDir(gameName)
	if err != nil {
		return "", err
	}
	return dir, nil
}

// ---- 下载 ----

var cdNameRe = regexp.MustCompile(`filename\*?=(?:UTF-8''|")?([^";]+)`)

// cdFileName 从 Content-Disposition 头解析文件名(附件下载的标准做法)。
func cdFileName(h http.Header) string {
	cd := h.Get("Content-Disposition")
	if cd == "" {
		return ""
	}
	m := cdNameRe.FindStringSubmatch(cd)
	if m == nil {
		return ""
	}
	return strings.TrimSpace(strings.TrimSuffix(m[1], `"`))
}

// sameOriginReferer 部分站点(如 FLiNG)终点要求同源 Referer 才放行。
func sameOriginReferer(rawurl string) string {
	u, err := url.Parse(rawurl)
	if err != nil {
		return ""
	}
	return u.Scheme + "://" + u.Host + "/"
}

// setHeaders 统一 UA / Referer;range 非空时带 Range 头。
func setHeaders(req *http.Request, fileURL, rangeHeader string) {
	req.Header.Set("User-Agent", "Mozilla/5.0 WeAndMod/0.1")
	if ref := sameOriginReferer(fileURL); ref != "" {
		req.Header.Set("Referer", ref)
	}
	if rangeHeader != "" {
		req.Header.Set("Range", rangeHeader)
	}
}

// probe 探测:文件总大小、Range 支持、服务器文件名、重定向后的最终地址。
func probe(ctx context.Context, fileURL string) (total int64, ranges bool, cdName, finalURL string, err error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, fileURL, nil)
	if err != nil {
		return 0, false, "", "", err
	}
	setHeaders(req, fileURL, "bytes=0-0")
	resp, err := client.Do(req)
	if err != nil {
		return 0, false, "", "", err
	}
	defer resp.Body.Close()
	finalURL = resp.Request.URL.String() // 跟随重定向后的真实文件地址
	if ct := resp.Header.Get("Content-Type"); strings.Contains(ct, "text/html") {
		return 0, false, "", "", ErrHTML
	}
	if resp.StatusCode == http.StatusPartialContent {
		total, _ := strconv.ParseInt(contentRangeTotal(resp.Header.Get("Content-Range")), 10, 64)
		return total, true, cdFileName(resp.Header), finalURL, nil
	}
	if resp.StatusCode == http.StatusOK {
		return resp.ContentLength, false, cdFileName(resp.Header), finalURL, nil // 不支持 Range,只能单线程
	}
	return 0, false, "", "", fmt.Errorf("dl: HTTP %d", resp.StatusCode)
}

// contentRangeTotal 解析 Content-Range: bytes 0-0/12345 的总长部分。
func contentRangeTotal(cr string) string {
	if i := strings.LastIndex(cr, "/"); i >= 0 {
		return cr[i+1:]
	}
	return ""
}

// autoThreads 按文件大小自动选线程数:每 4MiB 一个线程,封顶 max。
func autoThreads(total int64, max int) int {
	n := int(total / (4 << 20))
	if n < 1 {
		n = 1
	}
	if n > max {
		n = max
	}
	if n > 8 {
		n = 8
	}
	return n
}

// countWriter 包装写器,把写入字节数计入任务进度。
type countWriter struct {
	w io.Writer
	t *task
}

func (c *countWriter) Write(p []byte) (int, error) {
	n, err := c.w.Write(p)
	c.t.done.Add(int64(n))
	return n, err
}

// Download 把 url 下载到 dir 下,key 用于进度查询(一般用游戏名)。
// threads>1 且服务器支持 Range 时自动多线程分块;否则单线程。
// suggestedName 为空时从 URL 推断;返回落盘后的完整路径。
func Download(ctx context.Context, key, fileURL, dir, suggestedName string, threads int) (string, error) {
	total, ranges, cdName, finalURL, err := probe(ctx, fileURL)
	if err != nil {
		return "", err
	}
	if finalURL == "" {
		finalURL = fileURL
	}
	if threads < 1 {
		threads = 1
	}
	parallel := ranges && total > 0 && threads > 1
	nThreads := 1
	if parallel {
		nThreads = autoThreads(total, threads)
	}
	tsk := &task{total: total, threads: nThreads, parallel: parallel}
	active.Store(key, tsk)
	defer active.Delete(key)

	if err := os.MkdirAll(dir, 0o755); err != nil {
		return "", err
	}
	name := suggestedName
	if name == "" {
		name = cdName // 服务器 Content-Disposition 给的文件名
	}
	if name == "" {
		name = urlBase(finalURL) // 重定向后的最终地址才有真实文件名
	}
	name = SanitizeName(name)
	if filepath.Ext(name) == "" {
		// 标题命名时没有扩展名:依次从最终地址后缀、CD 文件名补齐
		ext := ""
		if e := filepath.Ext(urlBase(finalURL)); e != "" && len(e) <= 6 {
			ext = e
		} else if e := filepath.Ext(cdName); e != "" && len(e) <= 6 {
			ext = e
		} else {
			ext = ".bin"
		}
		name += ext
	}
	dst := filepath.Join(dir, name)
	tmp := dst + ".part"

	var derr error
	if parallel {
		derr = downloadChunks(ctx, fileURL, tmp, total, nThreads, tsk)
		if derr != nil {
			// 分块失败(服务器中途拒绝 Range 等)回退单线程
			tsk.parallel = false
			tsk.threads = 1
			tsk.done.Store(0)
			derr = downloadSingle(ctx, fileURL, tmp, tsk)
		}
	} else {
		derr = downloadSingle(ctx, fileURL, tmp, tsk)
	}
	if derr != nil {
		_ = os.Remove(tmp)
		return "", derr
	}
	// 完整性校验:探测到总长时比对实际写入字节数
	if total > 0 && tsk.done.Load() != total {
		_ = os.Remove(tmp)
		return "", fmt.Errorf("dl: 文件不完整 %d/%d 字节", tsk.done.Load(), total)
	}
	return dst, os.Rename(tmp, dst)
}

// magics 已知文件头:PE 可执行 / zip / rar / 7z。
var magics = [][]byte{
	{'M', 'Z'},
	{'P', 'K', 0x03, 0x04}, {'P', 'K', 0x05, 0x06}, // zip(含空包 EOCD)
	{'R', 'a', 'r', '!'},
	{'7', 'z', 0xbc, 0xaf, 0x27, 0x1c},
	{'M', 'S', 'C', 'F'}, // cab
}

// Verify 校验下载产物:大小非空 + 文件头符合扩展名预期
// (服务器错误页/截断文件在此拦截)。
func Verify(path string) error {
	fi, err := os.Stat(path)
	if err != nil {
		return err
	}
	if fi.Size() == 0 {
		return fmt.Errorf("dl: 文件为空")
	}
	f, err := os.Open(path)
	if err != nil {
		return err
	}
	defer f.Close()
	head := make([]byte, 8)
	n, _ := f.Read(head)
	head = head[:n]

	ext := strings.ToLower(filepath.Ext(path))
	// 扩展名明确时按类型校验魔数
	want := map[string][]byte{
		".exe": {'M', 'Z'},
		".zip": {'P', 'K'},
		".rar": {'R', 'a', 'r', '!'},
		".7z":  {'7', 'z', 0xbc, 0xaf, 0x27, 0x1c},
	}
	if m, ok := want[ext]; ok {
		if len(head) < len(m) || !bytes.Equal(head[:len(m)], m) {
			return fmt.Errorf("dl: 文件头与 %s 类型不符(可能下载的是错误页)", ext)
		}
		return nil
	}
	// 无明确类型:只要不是 HTML/文本就放行
	if len(head) >= 5 && (bytes.EqualFold(head[:5], []byte("<html")) ||
		bytes.EqualFold(head[:5], []byte("<!doc")) ||
		bytes.EqualFold(head[:5], []byte("<?xml"))) {
		return fmt.Errorf("dl: 下载到的是网页而非文件")
	}
	for _, m := range magics {
		if len(head) >= len(m) && bytes.Equal(head[:len(m)], m) {
			return nil
		}
	}
	return nil // 未知类型放行
}

// urlBase 取 URL 路径末段(去掉 query/fragment)。
func urlBase(u string) string {
	if i := strings.IndexAny(u, "?#"); i >= 0 {
		u = u[:i]
	}
	return filepath.Base(u)
}

// downloadSingle 单线程流式下载。
func downloadSingle(ctx context.Context, fileURL, tmp string, tsk *task) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, fileURL, nil)
	if err != nil {
		return err
	}
	setHeaders(req, fileURL, "")
	resp, err := client.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("dl: HTTP %d", resp.StatusCode)
	}
	f, err := os.Create(tmp)
	if err != nil {
		return err
	}
	_, err = io.Copy(&countWriter{w: f, t: tsk}, io.LimitReader(resp.Body, 256<<20))
	cerr := f.Close()
	if err != nil {
		return err
	}
	return cerr
}

// downloadChunks Range 分块并行下载:预分配文件,各线程按偏移写。
func downloadChunks(ctx context.Context, fileURL, tmp string, total int64, nThreads int, tsk *task) error {
	f, err := os.Create(tmp)
	if err != nil {
		return err
	}
	if err := f.Truncate(total); err != nil {
		_ = f.Close()
		return err
	}
	defer f.Close()

	ctx, cancel := context.WithCancel(ctx)
	defer cancel()

	chunkSize := (total + int64(nThreads) - 1) / int64(nThreads)
	var wg sync.WaitGroup
	errCh := make(chan error, nThreads)
	for i := 0; i < nThreads; i++ {
		start := int64(i) * chunkSize
		end := start + chunkSize - 1
		if end >= total {
			end = total - 1
		}
		if start > end {
			continue
		}
		wg.Add(1)
		go func(start, end int64) {
			defer wg.Done()
			req, err := http.NewRequestWithContext(ctx, http.MethodGet, fileURL, nil)
			if err != nil {
				errCh <- err
				return
			}
			setHeaders(req, fileURL, fmt.Sprintf("bytes=%d-%d", start, end))
			resp, err := client.Do(req)
			if err != nil {
				select {
				case errCh <- err:
				case <-ctx.Done():
				}
				return
			}
			defer resp.Body.Close()
			if resp.StatusCode != http.StatusPartialContent {
				errCh <- fmt.Errorf("dl: 分块请求被拒 HTTP %d", resp.StatusCode)
				return
			}
			w := io.NewOffsetWriter(f, start)
			_, err = io.CopyN(&countWriter{w: w, t: tsk}, resp.Body, end-start+1)
			if err != nil {
				select {
				case errCh <- err:
				case <-ctx.Done():
				}
			}
		}(start, end)
	}
	wg.Wait()
	select {
	case err := <-errCh:
		return err
	default:
		return nil
	}
}
