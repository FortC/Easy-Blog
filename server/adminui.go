package main

// 管理页：embed 进二进制的单文件 SPA（/admin）

import (
	_ "embed"
	"net/http"
)

//go:embed admin/index.html
var adminHTML []byte

func serveAdmin(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("X-Frame-Options", "DENY")
	w.WriteHeader(http.StatusOK)
	w.Write(adminHTML)
}
