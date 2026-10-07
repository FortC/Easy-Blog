package main

// 可选静态托管：STATIC_SERVE=1 时直接托管 public/ 下的构建产物，
// 不用 nginx 也能单容器跑完整站点（Docker 一键部署的默认形态）。
// 不启用时本文件全部逻辑只是空转，行为与 nginx + 反代形态完全一致。

import (
	"compress/gzip"
	"io"
	"net/http"
	"os"
	"path"
	"path/filepath"
	"strings"
	"sync"
)

var (
	staticServe = env("STATIC_SERVE", "") != "" && env("STATIC_SERVE", "") != "0"
	publicDir   = filepath.Join(siteRoot, "public")
)

func fileExists(p string) bool {
	st, err := os.Stat(p)
	return err == nil && !st.IsDir()
}

func handleStatic(w http.ResponseWriter, r *http.Request) {
	if !staticServe {
		http.NotFound(w, r)
		return
	}
	/* path.Clean("/") 前缀天然挡掉 ../ 穿越；再排除 Windows 反斜杠 */
	rel := strings.TrimPrefix(path.Clean("/"+r.URL.Path), "/")
	if rel == "" || strings.ContainsRune(rel, '\\') {
		rel = "index.html"
	}
	full := filepath.Join(publicDir, filepath.FromSlash(rel))
	if st, err := os.Stat(full); err == nil && st.IsDir() {
		full = filepath.Join(full, "index.html")
	}
	if !fileExists(full) {
		servePublicFile(w, r, filepath.Join(publicDir, "404.html"), true)
		return
	}
	servePublicFile(w, r, full, false)
}

func servePublicFile(w http.ResponseWriter, r *http.Request, full string, is404 bool) {
	f, err := os.Open(full)
	if err != nil {
		http.Error(w, "站点还没构建：登录 /admin 点「手动重建发布」，或重启容器", http.StatusServiceUnavailable)
		return
	}
	defer f.Close()
	st, err := f.Stat()
	if err != nil || st.IsDir() {
		http.Error(w, "internal error", http.StatusInternalServerError)
		return
	}

	/* 缓存策略与 deploy/nginx-easyblog.conf 保持一致 */
	switch {
	case strings.HasPrefix(r.URL.Path, "/css/"):
		w.Header().Set("Cache-Control", "public, max-age=31536000, immutable") // 文件名带 sha256 指纹
	case strings.HasSuffix(r.URL.Path, ".html") || r.URL.Path == "/":
		w.Header().Set("Cache-Control", "no-cache, must-revalidate")
	case hasSuffix(r.URL.Path, ".woff2", ".svg", ".png", ".txt", ".xml"):
		w.Header().Set("Cache-Control", "public, max-age=2592000")
	}

	sw := &staticWriter{ResponseWriter: w}
	if is404 {
		sw.code = http.StatusNotFound
	}
	if acceptsGzip(r) && isCompressible(r.URL.Path) && r.Header.Get("Range") == "" {
		sw.gzip = true
	}
	http.ServeContent(sw, r, full, st.ModTime(), f)
	sw.Close()
}

func hasSuffix(s string, exts ...string) bool {
	for _, e := range exts {
		if strings.HasSuffix(s, e) {
			return true
		}
	}
	return false
}

func acceptsGzip(r *http.Request) bool {
	return strings.Contains(r.Header.Get("Accept-Encoding"), "gzip")
}

func isCompressible(p string) bool {
	return p == "/" || hasSuffix(p, ".html", ".css", ".js", ".json", ".svg", ".txt", ".xml", ".map")
}

/* staticWriter：在 WriteHeader 落地的一瞬注入 404 状态码 / gzip 头（此时还能改 Header） */

type staticWriter struct {
	http.ResponseWriter
	code  int // 非 0 时强制用这个状态码
	gzip  bool
	z     *gzip.Writer
	mu    sync.Mutex
	err   error
}

func (s *staticWriter) WriteHeader(code int) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.code != 0 {
		code = s.code
	}
	if s.gzip {
		s.Header().Set("Content-Encoding", "gzip")
		s.Header().Del("Content-Length")
	}
	s.ResponseWriter.WriteHeader(code)
}

func (s *staticWriter) Write(p []byte) (int, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.err != nil {
		return 0, s.err
	}
	if s.gzip {
		if s.z == nil {
			s.z = gzip.NewWriter(s.ResponseWriter)
		}
		n, err := s.z.Write(p)
		if err != nil {
			s.err = err
		}
		return n, err
	}
	return s.ResponseWriter.Write(p)
}

func (s *staticWriter) Close() {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.z != nil {
		s.z.Close()
	}
}

var _ io.Writer = (*staticWriter)(nil)
