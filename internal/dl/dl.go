// Package dl 管理修改器下载:统一放在用户「文档」目录下的
// WeAndMod\<游戏名>\ 中,按目录内容判断游戏是否已有修改器。
// 支持 HTTP Range 多线程分块下载,并跟踪进行中任务的进度。
package dl

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
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

// probeResult 探测结果:文件总大小与 Range 支持情况。
func probe(ctx context.Context, fileURL string) (total int64, ranges bool, err error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, fileURL, nil)
	if err != nil {
		return 0, false, err
	}
	req.Header.Set("User-Agent", "Mozilla/5.0 WeAndMod/0.1")
	req.Header.Set("Range", "bytes=0-0")
	resp, err := client.Do(req)
	if err != nil {
		return 0, false, err
	}
	defer resp.Body.Close()
	if ct := resp.Header.Get("Content-Type"); strings.Contains(ct, "text/html") {
		return 0, false, ErrHTML
	}
	if resp.StatusCode == http.StatusPartialContent {
		// Content-Range: bytes 0-0/12345
		cr := resp.Header.Get("Content-Range")
		if i := strings.LastIndex(cr, "/"); i >= 0 {
			if n, e := strconv.ParseInt(cr[i+1:], 10, 64); e == nil {
				return n, true, nil
			}
		}
	}
	if resp.StatusCode == http.StatusOK {
		return resp.ContentLength, false, nil // 不支持 Range,只能单线程
	}
	return 0, false, fmt.Errorf("dl: HTTP %d", resp.StatusCode)
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
	total, ranges, err := probe(ctx, fileURL)
	if err != nil {
		return "", err
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
		u := fileURL
		if i := strings.IndexAny(u, "?#"); i >= 0 {
			u = u[:i]
		}
		name = filepath.Base(u)
	}
	name = SanitizeName(name)
	if filepath.Ext(name) == "" {
		// 标题命名时没有扩展名:从下载链接后缀补齐(更新检测依赖同名)
		u := fileURL
		if i := strings.IndexAny(u, "?#"); i >= 0 {
			u = u[:i]
		}
		if ext := filepath.Ext(filepath.Base(u)); ext != "" && len(ext) <= 6 {
			name += ext
		} else {
			name += ".bin"
		}
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
	return dst, os.Rename(tmp, dst)
}

// downloadSingle 单线程流式下载。
func downloadSingle(ctx context.Context, fileURL, tmp string, tsk *task) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, fileURL, nil)
	if err != nil {
		return err
	}
	req.Header.Set("User-Agent", "Mozilla/5.0 WeAndMod/0.1")
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
			req.Header.Set("User-Agent", "Mozilla/5.0 WeAndMod/0.1")
			req.Header.Set("Range", fmt.Sprintf("bytes=%d-%d", start, end))
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
