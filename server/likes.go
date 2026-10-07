package main

// 作品点赞：内存 + JSON 持久化（与评论同套路，零数据库）。
// 无上限：点一次 +1，可连点（仅做每 IP 每分钟限流防脚本刷）。
// GET  /api/likes        → {counts: {key: n}}
// POST /api/likes {page} → {count}

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"net/http"
	"os"
	"path/filepath"
	"regexp"
	"sync"
	"time"
)

type likeEntry struct {
	Count int      `json:"count"`
	IPs   []string `json:"ips,omitempty"` /* 兼容旧文件结构，不再写入 */
}

var (
	likeMu    sync.Mutex
	likeData  = map[string]likeEntry{}
	likePath  = filepath.Join(dataDir, "likes.json")
	likeKeyRe = regexp.MustCompile(`^works/[\w\-]+$`)
)

/* 每 IP 每分钟最多 30 次点赞请求（人手速点不满，脚本挡在门外） */
var (
	likeRateMu   sync.Mutex
	likeRateHits = map[string][]time.Time{}
)

func likeAllow(ip string) bool {
	likeRateMu.Lock()
	defer likeRateMu.Unlock()
	now := time.Now()
	hits := likeRateHits[ip]
	keep := hits[:0]
	for _, t := range hits {
		if now.Sub(t) < time.Minute {
			keep = append(keep, t)
		}
	}
	if len(keep) >= 30 {
		likeRateHits[ip] = keep
		return false
	}
	likeRateHits[ip] = append(keep, now)
	return true
}

func initLikes() {
	likeMu.Lock()
	defer likeMu.Unlock()
	if b, err := os.ReadFile(likePath); err == nil {
		json.Unmarshal(b, &likeData)
	}
}

func saveLikesLocked() {
	b, _ := json.Marshal(likeData)
	tmp := likePath + ".tmp"
	if os.WriteFile(tmp, b, 0o600) == nil {
		os.Rename(tmp, likePath)
	}
}

func ipHash(ip string) string {
	h := sha256.Sum256([]byte("eblog-like:" + ip))
	return hex.EncodeToString(h[:12])
}

func handleLikes(w http.ResponseWriter, r *http.Request) {
	switch r.Method {
	case http.MethodGet:
		likeMu.Lock()
		counts := map[string]int{}
		for k, v := range likeData {
			counts[k] = v.Count
		}
		likeMu.Unlock()
		writeJSON(w, http.StatusOK, map[string]any{"counts": counts})

	case http.MethodPost:
		var req struct {
			Page string `json:"page"`
		}
		if !decodeBody(w, r, &req) {
			return
		}
		if !likeKeyRe.MatchString(req.Page) {
			writeJSON(w, http.StatusBadRequest, errStr("这个作品点不了赞"))
			return
		}
		if !likeAllow(clientIP(r)) {
			writeJSON(w, http.StatusTooManyRequests, errStr("点太快啦，歇一秒"))
			return
		}
		likeMu.Lock()
		e := likeData[req.Page]
		e.Count++
		e.IPs = nil
		likeData[req.Page] = e
		saveLikesLocked()
		count := e.Count
		likeMu.Unlock()
		writeJSON(w, http.StatusOK, map[string]any{"count": count})

	default:
		writeJSON(w, http.StatusMethodNotAllowed, errStr("GET/POST only"))
	}
}
