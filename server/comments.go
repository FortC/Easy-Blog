package main

// 评论系统：内存 + JSON 文件持久化（个人站量级足够，零数据库依赖）

import (
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"net/http"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"sync"
	"time"
)

type comment struct {
	ID   string `json:"id"`
	Page string `json:"page"` // 文章路径，如 /posts/xxx/
	Name string `json:"name"`
	Web  string `json:"web,omitempty"` // 个人网站（可选）
	Text string `json:"text"`
	TS   int64  `json:"ts"`
}

var (
	commentMu   sync.Mutex
	commentList []comment
	commentPath = filepath.Join(dataDir, "comments.json")
)

func initComments() {
	commentMu.Lock()
	defer commentMu.Unlock()
	b, err := os.ReadFile(commentPath)
	if err == nil {
		json.Unmarshal(b, &commentList)
	}
}

func saveCommentsLocked() {
	b, _ := json.MarshalIndent(commentList, "", " ")
	tmp := commentPath + ".tmp"
	if os.WriteFile(tmp, b, 0o600) == nil {
		os.Rename(tmp, commentPath)
	}
}

func randHex(n int) string {
	b := make([]byte, n)
	rand.Read(b)
	return hex.EncodeToString(b)
}

var (
	pageRe  = regexp.MustCompile(`^/[\w\-./%]*$`)
	webRe   = regexp.MustCompile(`^https?://[\w\-./%?=&#]+$`)
	nameMax = 30
	textMax = 1000
)

/* ---------- 公开接口 ---------- */

func handleCommentsPublic(w http.ResponseWriter, r *http.Request) {
	switch r.Method {
	case http.MethodGet:
		page := r.URL.Query().Get("page")
		if !pageRe.MatchString(page) {
			writeJSON(w, http.StatusBadRequest, errStr("bad page"))
			return
		}
		commentMu.Lock()
		defer commentMu.Unlock()
		out := make([]comment, 0, len(commentList))
		for _, c := range commentList {
			if c.Page == page {
				out = append(out, c)
			}
		}
		writeJSON(w, http.StatusOK, map[string]any{"comments": out})

	case http.MethodPost:
		ip := clientIP(r)
		if !commentRateOK(ip) {
			writeJSON(w, http.StatusTooManyRequests, errStr("留言太快啦，喝口水再发"))
			return
		}
		var req struct {
			Page    string `json:"page"`
			Name    string `json:"name"`
			Web     string `json:"web"`
			Text    string `json:"text"`
			Company string `json:"company"` // 蜜罐：机器人会填
		}
		if !decodeBody(w, r, &req) {
			return
		}
		// 蜜罐非空 = 机器人，假装成功
		if strings.TrimSpace(req.Company) != "" {
			writeJSON(w, http.StatusOK, map[string]bool{"ok": true})
			return
		}
		req.Name = strings.TrimSpace(req.Name)
		req.Text = strings.TrimSpace(req.Text)
		if req.Name == "" || req.Text == "" || len(req.Name) > nameMax || len(req.Text) > textMax {
			writeJSON(w, http.StatusBadRequest, errStr("昵称 1-30 字，内容 1-1000 字"))
			return
		}
		/* 防御：含无效 UTF-8 替换符（U+FFFD）的输入直接拒绝
		   ——正常浏览器 fetch 不会产生；拦截非标准客户端的坏编码 */
		if strings.ContainsRune(req.Name, 0xFFFD) || strings.ContainsRune(req.Text, 0xFFFD) {
			writeJSON(w, http.StatusBadRequest, errStr("内容编码异常，换个输入法/浏览器试试"))
			return
		}
		if !pageRe.MatchString(req.Page) {
			writeJSON(w, http.StatusBadRequest, errStr("bad page"))
			return
		}
		if req.Web != "" && !webRe.MatchString(req.Web) {
			req.Web = "" // 网址不合法就当没填
		}
		c := comment{ID: time.Now().Format("20060102") + randHex(4), Page: req.Page, Name: req.Name, Web: req.Web, Text: req.Text, TS: time.Now().Unix()}
		commentMu.Lock()
		commentList = append(commentList, c)
		saveCommentsLocked()
		commentMu.Unlock()
		writeJSON(w, http.StatusOK, map[string]any{"ok": true, "comment": c})

	default:
		writeJSON(w, http.StatusMethodNotAllowed, errStr("GET/POST only"))
	}
}

/* 留言限流：每 IP 60 秒 1 条 */
var commentHits = map[string]*attemptWindow{}

func commentRateOK(ip string) bool {
	authMu.Lock()
	defer authMu.Unlock()
	now := time.Now()
	a, ok := commentHits[ip]
	if !ok || now.Sub(a.window) > time.Minute {
		commentHits[ip] = &attemptWindow{window: now, count: 1}
		return true
	}
	a.count++
	return a.count <= 1
}

/* ---------- 管理接口 ---------- */

func handleCommentsAdmin(w http.ResponseWriter, r *http.Request) {
	switch r.Method {
	case http.MethodGet:
		commentMu.Lock()
		defer commentMu.Unlock()
		out := make([]comment, len(commentList))
		copy(out, commentList)
		// 新的在前
		for i, j := 0, len(out)-1; i < j; i, j = i+1, j-1 {
			out[i], out[j] = out[j], out[i]
		}
		writeJSON(w, http.StatusOK, map[string]any{"comments": out})

	case http.MethodDelete:
		id := r.URL.Query().Get("id")
		if id == "" {
			writeJSON(w, http.StatusBadRequest, errStr("missing id"))
			return
		}
		commentMu.Lock()
		defer commentMu.Unlock()
		for i, c := range commentList {
			if c.ID == id {
				commentList = append(commentList[:i], commentList[i+1:]...)
				saveCommentsLocked()
				writeJSON(w, http.StatusOK, map[string]bool{"ok": true})
				return
			}
		}
		writeJSON(w, http.StatusNotFound, errStr("not found"))

	default:
		writeJSON(w, http.StatusMethodNotAllowed, errStr("GET/DELETE only"))
	}
}
