// easyblog-server — 「Easy Blog」一体化后端（单二进制，纯标准库）
//
// 职责：
//   1. 站长后台 /admin：登录、文章/说说/配置在线编辑、保存后自动 Hugo 重建发布
//   2. 内置评论系统：访客填昵称即可留言（JSON 文件存储，零数据库）
//   3. AI 助手代理（openai 兼容 / anthropic 双协议，SSE 流式）
//   4. 可选静态托管：STATIC_SERVE=1 时直接服务 public/（Docker 单容器形态）
//
// 环境变量：
//   LISTEN          监听地址            默认 127.0.0.1:8788
//   SITE_ROOT       站点根目录（含 content/ layouts/ hugo.yaml） 默认工作目录
//   ADMIN_PASSWORD  站长密码（必配，否则 /admin 与管理 API 禁用）
//   ADMIN_SECRET    会话签名密钥        默认取 ADMIN_PASSWORD
//   HUGO_BIN        hugo 可执行文件路径  默认 hugo（PATH 里找）
//   DATA_DIR        评论数据目录        默认 SITE_ROOT/serverdata
//   STATIC_SERVE    置 1 托管 public/   默认关（静态交给 nginx）
//   ALLOWED_ORIGIN  AI 接口 Referer 白名单（空 = 不校验）
//   以及 AI_PROVIDER / AI_BASE_URL / AI_MODEL / AI_API_KEY / AI_MAX_TOKENS
package main

import (
	"encoding/json"
	"log"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"
)

var (
	listen   = env("LISTEN", "127.0.0.1:8788")
	siteRoot = absPath(env("SITE_ROOT", "."))
	dataDir  = env("DATA_DIR", filepath.Join(siteRoot, "serverdata"))
	hugoBin  = env("HUGO_BIN", "hugo")
)

func env(k, def string) string {
	if v := os.Getenv(k); v != "" {
		return v
	}
	return def
}

func absPath(p string) string {
	a, err := filepath.Abs(p)
	if err != nil {
		return p
	}
	return a
}

func main() {
	log.SetFlags(log.LstdFlags)
	os.MkdirAll(dataDir, 0o700)
	initComments()
	initLikes()
	initVisits()
	initAuth()

	mux := http.NewServeMux()

	/* ---- 公开接口 ---- */
	mux.HandleFunc("/api/health", handleHealth)
	mux.HandleFunc("/api/chat", handleChat)
	mux.HandleFunc("/api/comments", handleCommentsPublic)
	mux.HandleFunc("/api/likes", handleLikes)
	mux.HandleFunc("/api/visit", handleVisit)

	/* ---- 管理接口（需登录） ---- */
	mux.HandleFunc("/api/admin/login", handleLogin)
	mux.HandleFunc("/api/admin/logout", handleLogout)
	mux.HandleFunc("/api/admin/session", adminOnly(handleSession))
	mux.HandleFunc("/api/admin/posts", adminOnly(handlePosts))
	mux.HandleFunc("/api/admin/content", adminOnly(handleContent))
	mux.HandleFunc("/api/admin/configs", adminOnly(handleConfigs))
	mux.HandleFunc("/api/admin/siteconfig", adminOnly(handleSiteConfig))
	mux.HandleFunc("/api/admin/links", adminOnly(handleLinks))
	mux.HandleFunc("/api/admin/upload", adminOnly(handleUpload))
	mux.HandleFunc("/api/admin/aicfg", adminOnly(handleAICfg))
	mux.HandleFunc("/api/admin/aicfg/test", adminOnly(handleAITest))
	mux.HandleFunc("/api/admin/build", adminOnly(handleBuild))
	mux.HandleFunc("/api/admin/comments", adminOnly(handleCommentsAdmin))

	/* ---- 管理页（embed 的单文件 SPA） ---- */
	mux.HandleFunc("/admin", serveAdmin)
	mux.HandleFunc("/admin/", serveAdmin)
	mux.HandleFunc("/admin/index.html", serveAdmin)

	/* ---- 静态托管（STATIC_SERVE=1 时兜底接管其余路径，优先级最低） ---- */
	mux.HandleFunc("/", handleStatic)

	srv := &http.Server{
		Addr:              listen,
		Handler:           logReq(mux),
		ReadHeaderTimeout: 10 * time.Second,
		ReadTimeout:       30 * time.Second,
		WriteTimeout:      120 * time.Second,
		IdleTimeout:       120 * time.Second,
	}
	log.Printf("easyblog-server on %s (site=%s admin=%v ai=%v static=%v)",
		listen, siteRoot, adminPassword != "", effectiveAI().APIKey != "", staticServe)
	log.Fatal(srv.ListenAndServe())
}

func logReq(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.HasPrefix(r.URL.Path, "/api/") {
			log.Printf("%s %s", r.Method, r.URL.Path)
		}
		next.ServeHTTP(w, r)
	})
}

/* ---------- 公共小工具 ---------- */

var jsonHeaders = map[string]string{"Content-Type": "application/json; charset=utf-8"}

func writeJSON(w http.ResponseWriter, code int, v any) {
	for k, val := range jsonHeaders {
		w.Header().Set(k, val)
	}
	w.WriteHeader(code)
	if err := json.NewEncoder(w).Encode(v); err != nil {
		log.Printf("writeJSON: %v", err)
	}
}

func handleHealth(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, map[string]any{
		"ok":    true,
		"admin": adminPassword != "",
		"ai":    effectiveAI().APIKey != "",
	})
}

/* ---------- 构建锁 ---------- */

var buildMu sync.Mutex
