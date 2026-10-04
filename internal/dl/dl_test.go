package dl

import (
	"context"
	"crypto/rand"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"testing"
	"time"
)

// makeRangeServer 返回一个支持 Range 的测试服务器和数据本体。
func makeRangeServer(t *testing.T, size int) (*httptest.Server, []byte) {
	t.Helper()
	data := make([]byte, size)
	if _, err := rand.Read(data); err != nil {
		t.Fatal(err)
	}
	rangeRe := regexp.MustCompile(`bytes=(\d+)-(\d+)`)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if m := rangeRe.FindStringSubmatch(r.Header.Get("Range")); m != nil {
			start, _ := strconv.ParseInt(m[1], 10, 64)
			end, _ := strconv.ParseInt(m[2], 10, 64)
			if end >= int64(len(data)) {
				end = int64(len(data)) - 1
			}
			w.Header().Set("Content-Range",
				fmt.Sprintf("bytes %d-%d/%d", start, end, len(data)))
			w.WriteHeader(http.StatusPartialContent)
			_, _ = w.Write(data[start : end+1])
			return
		}
		w.Header().Set("Content-Type", "application/octet-stream")
		w.Header().Set("Content-Length", strconv.Itoa(len(data)))
		_, _ = w.Write(data)
	}))
	return srv, data
}

func TestDownloadParallel(t *testing.T) {
	srv, data := makeRangeServer(t, 9<<20) // 9MiB → autoThreads=2..8
	defer srv.Close()

	dir := t.TempDir()
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	dst, err := Download(ctx, "test-game", srv.URL+"/trainer.zip", dir, "", 4)
	if err != nil {
		t.Fatal(err)
	}
	got, err := os.ReadFile(dst)
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != len(data) {
		t.Fatalf("size mismatch: %d != %d", len(got), len(data))
	}
	for i := range got {
		if got[i] != data[i] {
			t.Fatalf("byte %d mismatch", i)
		}
	}
	if filepath.Base(dst) != "trainer.zip" {
		t.Fatalf("bad name %q", dst)
	}
}

func TestDownloadSingleNoRange(t *testing.T) {
	// 不支持 Range 的服务器:忽略 Range 头,直接 200 全量
	data := []byte("single-thread-payload-0123456789")
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/octet-stream")
		_, _ = w.Write(data)
	}))
	defer srv.Close()

	dir := t.TempDir()
	dst, err := Download(context.Background(), "g", srv.URL+"/f.bin", dir, "", 8)
	if err != nil {
		t.Fatal(err)
	}
	got, _ := os.ReadFile(dst)
	if string(got) != string(data) {
		t.Fatal("content mismatch")
	}
}

func TestVerify(t *testing.T) {
	dir := t.TempDir()
	write := func(name string, data []byte) string {
		p := filepath.Join(dir, name)
		if err := os.WriteFile(p, data, 0o644); err != nil {
			t.Fatal(err)
		}
		return p
	}

	// 合法 exe / zip 通过
	if err := Verify(write("a.exe", []byte{'M', 'Z', 1, 2, 3})); err != nil {
		t.Fatal(err)
	}
	if err := Verify(write("a.zip", []byte{'P', 'K', 3, 4})); err != nil {
		t.Fatal(err)
	}
	// 伪装成 exe 的 HTML 应被拦截
	if err := Verify(write("b.exe", []byte("<html>oops"))); err == nil {
		t.Fatal("html-as-exe should fail")
	}
	// 空文件拦截
	if err := Verify(write("c.zip", nil)); err == nil {
		t.Fatal("empty should fail")
	}
	// 未知类型但非 HTML 放行
	if err := Verify(write("d.dat", []byte{1, 2, 3, 4})); err != nil {
		t.Fatal(err)
	}
}

func TestProgressTracking(t *testing.T) {
	if _, ok := ProgressOf("nonexistent"); ok {
		t.Fatal("phantom progress")
	}
	if HasActive() {
		t.Fatal("should have no active downloads")
	}
}
