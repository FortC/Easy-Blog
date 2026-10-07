package main

// 全站访问计数：POST /api/visit → {count}。有人打开页面就 +1；
// 同一 IP 15 秒内的重复刷新不重复计（防刷新灌水），计数持久化到 serverdata/visits.json。

import (
	"encoding/json"
	"net/http"
	"os"
	"path/filepath"
	"sync"
	"time"
)

var (
	visitMu   sync.Mutex
	visitN    int64
	visitSeen = map[string]int64{} /* ipHash → 最近一次计数时间 */
	visitPath = filepath.Join(dataDir, "visits.json")
)

func initVisits() {
	visitMu.Lock()
	defer visitMu.Unlock()
	if b, err := os.ReadFile(visitPath); err == nil {
		json.Unmarshal(b, &visitN)
	}
}

func handleVisit(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost && r.Method != http.MethodGet {
		writeJSON(w, http.StatusMethodNotAllowed, errStr("GET/POST only"))
		return
	}
	visitMu.Lock()
	defer visitMu.Unlock()
	if r.Method == http.MethodPost {
		h := ipHash(clientIP(r))
		if time.Now().Unix()-visitSeen[h] >= 15 {
			visitN++
			visitSeen[h] = time.Now().Unix()
			if len(visitSeen) > 5000 { /* 防止内存慢慢涨 */
				visitSeen = map[string]int64{h: visitSeen[h]}
			}
			b, _ := json.Marshal(visitN)
			tmp := visitPath + ".tmp"
			if os.WriteFile(tmp, b, 0o600) == nil {
				os.Rename(tmp, visitPath)
			}
		}
	}
	writeJSON(w, http.StatusOK, map[string]any{"count": visitN})
}
