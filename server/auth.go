package main

// 认证：单管理员 + HMAC 签名会话 Cookie + 登录限流

import (
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/hex"
	"net/http"
	"strconv"
	"strings"
	"sync"
	"time"
)

var (
	adminPassword = env("ADMIN_PASSWORD", "")
	adminSecret   = env("ADMIN_SECRET", adminPassword)
	sessTTL       = 7 * 24 * time.Hour
)

var authMu sync.Mutex

func initAuth() {
	if adminSecret == "" {
		// 没配密码时生成随机 secret（管理功能整体禁用，不会走到签名逻辑）
		b := make([]byte, 32)
		rand.Read(b)
		adminSecret = hex.EncodeToString(b)
	}
}

/* ---------- 会话 Cookie：eblog_sess=<expiry>.<nonce>.<hmac> ---------- */

func sign(payload string) string {
	m := hmac.New(sha256.New, []byte(adminSecret))
	m.Write([]byte(payload))
	return hex.EncodeToString(m.Sum(nil))
}

func newSessionToken() string {
	exp := time.Now().Add(sessTTL).Unix()
	nonce := randHex(8)
	payload := strconv.FormatInt(exp, 10) + "." + nonce
	return payload + "." + sign(payload)
}

func validSession(r *http.Request) bool {
	c, err := r.Cookie("eblog_sess")
	if err != nil || c.Value == "" {
		return false
	}
	parts := strings.Split(c.Value, ".")
	if len(parts) != 3 {
		return false
	}
	payload := parts[0] + "." + parts[1]
	want := sign(payload)
	if subtle.ConstantTimeCompare([]byte(want), []byte(parts[2])) != 1 {
		return false
	}
	exp, err := strconv.ParseInt(parts[0], 10, 64)
	return err == nil && time.Now().Unix() < exp
}

func setSessionCookie(w http.ResponseWriter, token string) {
	http.SetCookie(w, &http.Cookie{
		Name:     "eblog_sess",
		Value:    token,
		Path:     "/",
		MaxAge:   int(sessTTL.Seconds()),
		HttpOnly: true,
		SameSite: http.SameSiteLaxMode,
		Secure:   strings.HasPrefix(env("SITE_URL", ""), "https://"),
	})
}

/* ---------- 登录限流：每 IP 10 分钟 5 次 ---------- */

var loginAttempts = map[string]*attemptWindow{}

type attemptWindow struct {
	window time.Time
	count  int
}

func loginAllowed(ip string) bool {
	authMu.Lock()
	defer authMu.Unlock()
	now := time.Now()
	a, ok := loginAttempts[ip]
	if !ok || now.Sub(a.window) > 10*time.Minute {
		loginAttempts[ip] = &attemptWindow{window: now, count: 1}
		return true
	}
	a.count++
	return a.count <= 5
}

func loginSucceed(ip string) {
	authMu.Lock()
	defer authMu.Unlock()
	delete(loginAttempts, ip)
}

/* ---------- handlers ---------- */

func handleLogin(w http.ResponseWriter, r *http.Request) {
	ip := clientIP(r)
	if !loginAllowed(ip) {
		writeJSON(w, http.StatusTooManyRequests, map[string]string{"error": "试得太勤了，10 分钟后再来"})
		return
	}
	if r.Method != http.MethodPost {
		writeJSON(w, http.StatusMethodNotAllowed, errStr("POST only"))
		return
	}
	var req struct{ Password string `json:"password"` }
	if !decodeBody(w, r, &req) {
		return
	}
	if adminPassword == "" || subtle.ConstantTimeCompare([]byte(req.Password), []byte(adminPassword)) != 1 {
		writeJSON(w, http.StatusUnauthorized, map[string]string{"error": "密码不对"})
		return
	}
	loginSucceed(ip)
	setSessionCookie(w, newSessionToken())
	writeJSON(w, http.StatusOK, map[string]bool{"ok": true})
}

func handleLogout(w http.ResponseWriter, r *http.Request) {
	http.SetCookie(w, &http.Cookie{Name: "eblog_sess", Value: "", Path: "/", MaxAge: -1, HttpOnly: true, SameSite: http.SameSiteLaxMode})
	writeJSON(w, http.StatusOK, map[string]bool{"ok": true})
}

func handleSession(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, map[string]bool{"ok": true})
}

/* adminOnly 包装器：校验会话后才进入业务 handler */
func adminOnly(next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if adminPassword == "" {
			writeJSON(w, http.StatusServiceUnavailable, map[string]string{"error": "后台未启用（未设置 ADMIN_PASSWORD）"})
			return
		}
		if !validSession(r) {
			writeJSON(w, http.StatusUnauthorized, map[string]string{"error": "未登录"})
			return
		}
		next(w, r)
	}
}
