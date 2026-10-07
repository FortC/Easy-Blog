package main

// AI 模型配置：后台「站点配置 → AI 助手」可视化编辑。
//   GET  /api/admin/aicfg        读当前配置（含环境变量兜底值）
//   PUT  /api/admin/aicfg        保存到 serverdata/ai.json（0600），立即生效无需重启
//   POST /api/admin/aicfg/test   用请求体里的配置实测连通（不落盘）
// 优先级：ai.json 里的非空字段 > 环境变量（AI_PROVIDER 等）> 默认值。

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"
)

type aiConf struct {
	Provider  string `json:"provider"`   // openai（兼容协议）| anthropic
	BaseURL   string `json:"base_url"`   // 如 https://open.bigmodel.cn/api/paas/v4
	Model     string `json:"model"`      // 如 glm-4-flash
	APIKey    string `json:"api_key"`    // 只存服务器，绝不进前台
	MaxTokens string `json:"max_tokens"` // 单次回复上限
}

var (
	aiCfgMu   sync.Mutex
	aiCfgPath = filepath.Join(dataDir, "ai.json")
)

func loadAICfgFile() aiConf {
	aiCfgMu.Lock()
	defer aiCfgMu.Unlock()
	var c aiConf
	b, err := os.ReadFile(aiCfgPath)
	if err == nil {
		json.Unmarshal(b, &c)
	}
	return c
}

func saveAICfgFile(c aiConf) error {
	aiCfgMu.Lock()
	defer aiCfgMu.Unlock()
	os.MkdirAll(filepath.Dir(aiCfgPath), 0o700)
	b, err := json.MarshalIndent(c, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(aiCfgPath, b, 0o600)
}

/* effectiveAI：环境变量打底，ai.json 的非空字段覆盖 */
func effectiveAI() aiConf {
	c := aiConf{
		Provider:  aiProvider,
		BaseURL:   aiBase,
		Model:     aiModel,
		APIKey:    aiKey,
		MaxTokens: aiMaxTok,
	}
	f := loadAICfgFile()
	if f.Provider != "" {
		c.Provider = f.Provider
	}
	if f.BaseURL != "" {
		c.BaseURL = strings.TrimRight(f.BaseURL, "/")
	}
	if f.Model != "" {
		c.Model = f.Model
	}
	if f.APIKey != "" {
		c.APIKey = f.APIKey
	}
	if f.MaxTokens != "" {
		c.MaxTokens = f.MaxTokens
	}
	if c.Provider == "" {
		c.Provider = "openai"
	}
	if c.MaxTokens == "" {
		c.MaxTokens = "1024"
	}
	return c
}

func handleAICfg(w http.ResponseWriter, r *http.Request) {
	switch r.Method {
	case http.MethodGet:
		c := effectiveAI()
		c.APIKey = maskKey(c.APIKey)
		writeJSON(w, http.StatusOK, c)

	case http.MethodPut:
		var c aiConf
		if !decodeBody(w, r, &c) {
			return
		}
		c.Provider = strings.ToLower(strings.TrimSpace(c.Provider))
		if c.Provider != "openai" && c.Provider != "anthropic" {
			writeJSON(w, http.StatusBadRequest, errStr("协议只能是 openai 或 anthropic"))
			return
		}
		c.BaseURL = strings.TrimRight(strings.TrimSpace(c.BaseURL), "/")
		c.Model = strings.TrimSpace(c.Model)
		c.MaxTokens = strings.TrimSpace(c.MaxTokens)
		if c.MaxTokens == "" {
			c.MaxTokens = "1024"
		}
		/* Key 留空 = 沿用已存的（界面上打码展示，避免每次保存都要重填） */
		if strings.TrimSpace(c.APIKey) == "" || strings.Contains(c.APIKey, "•") {
			c.APIKey = loadAICfgFile().APIKey
		}
		if c.APIKey == "" {
			c.APIKey = aiKey
		}
		if err := saveAICfgFile(c); err != nil {
			writeJSON(w, http.StatusInternalServerError, errStr("保存失败："+err.Error()))
			return
		}
		writeJSON(w, http.StatusOK, map[string]any{"ok": true, "ready": effectiveAI().APIKey != ""})

	default:
		writeJSON(w, http.StatusMethodNotAllowed, errStr("GET/PUT only"))
	}
}

/* 打码显示：只露尾 4 位，后台界面回显用 */
func maskKey(k string) string {
	if k == "" {
		return ""
	}
	if len(k) <= 8 {
		return "••••"
	}
	return "••••••" + k[len(k)-4:]
}

/* ================= 连通性测试：按表单值实发一次极小请求，不落盘 ================= */

func handleAITest(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writeJSON(w, http.StatusMethodNotAllowed, errStr("POST only"))
		return
	}
	var c aiConf
	if !decodeBody(w, r, &c) {
		return
	}
	base := effectiveAI()
	if c.Provider == "" {
		c.Provider = base.Provider
	}
	if c.BaseURL == "" {
		c.BaseURL = base.BaseURL
	}
	if c.Model == "" {
		c.Model = base.Model
	}
	if strings.TrimSpace(c.APIKey) == "" || strings.Contains(c.APIKey, "•") {
		c.APIKey = base.APIKey
	}
	if c.APIKey == "" {
		writeJSON(w, http.StatusBadRequest, errStr("还没填 API Key（也没在环境变量里配）"))
		return
	}
	c.BaseURL = strings.TrimRight(c.BaseURL, "/")

	t0 := time.Now()
	url, header, body := buildTestRequest(c)
	client := &http.Client{Timeout: 20 * time.Second}
	hreq, err := http.NewRequest(http.MethodPost, url, bytes.NewReader(body))
	if err != nil {
		writeJSON(w, http.StatusBadRequest, errStr("地址不合法："+err.Error()))
		return
	}
	hreq.Header = header
	resp, err := client.Do(hreq)
	if err != nil {
		writeJSON(w, http.StatusBadGateway, errStr("连不上："+err.Error()))
		return
	}
	defer resp.Body.Close()
	rb, _ := io.ReadAll(io.LimitReader(resp.Body, 4096))
	ms := time.Since(t0).Milliseconds()

	if resp.StatusCode != http.StatusOK {
		writeJSON(w, http.StatusOK, map[string]any{"ok": false, "ms": ms,
			"error": fmt.Sprintf("上游返回 %d：%s", resp.StatusCode, diagAI(resp.StatusCode, string(rb)))})
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"ok": true, "ms": ms, "reply": aiTestReply(c.Provider, rb)})
}

func buildTestRequest(c aiConf) (string, http.Header, []byte) {
	h := http.Header{}
	h.Set("Content-Type", "application/json")
	if c.Provider == "anthropic" {
		h.Set("x-api-key", c.APIKey)
		h.Set("anthropic-version", "2023-06-01")
		url := c.BaseURL + "/v1/messages"
		if strings.Contains(c.BaseURL, "anthropic.com") {
			url = c.BaseURL + "/messages"
		}
		body, _ := json.Marshal(map[string]any{
			"model":      c.Model,
			"max_tokens": 16,
			"messages":   []msg{{Role: "user", Content: "只回复两个字：通了"}},
		})
		return url, h, body
	}
	h.Set("Authorization", "Bearer "+c.APIKey)
	body, _ := json.Marshal(map[string]any{
		"model":      c.Model,
		"max_tokens": 16,
		"messages":   []msg{{Role: "user", Content: "只回复两个字：通了"}},
	})
	return c.BaseURL + "/chat/completions", h, body
}

func aiTestReply(provider string, body []byte) string {
	if provider == "anthropic" {
		var v struct {
			Content []struct {
				Text string `json:"text"`
			} `json:"content"`
		}
		if json.Unmarshal(body, &v) == nil && len(v.Content) > 0 {
			return v.Content[0].Text
		}
	} else {
		var v struct {
			Choices []struct {
				Message struct {
					Content string `json:"content"`
				} `json:"message"`
			} `json:"choices"`
		}
		if json.Unmarshal(body, &v) == nil && len(v.Choices) > 0 {
			return v.Choices[0].Message.Content
		}
	}
	return ""
}

/* 把上游报错翻译成人能看懂的话 */
func diagAI(status int, body string) string {
	s := strings.TrimSpace(body)
	if len(s) > 200 {
		s = s[:200]
	}
	switch status {
	case 401, 403:
		return "API Key 不对或没权限（" + s + "）"
	case 404:
		return "地址或模型名不存在，检查 Base URL / Model（" + s + "）"
	case 429:
		return "触发限流或余额不足（" + s + "）"
	default:
		return s
	}
}
