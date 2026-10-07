package main

// AI 助手代理：openai 兼容（OpenAI/GLM/DeepSeek/Kimi/Qwen/Ollama…）与 anthropic 双协议，
// 统一输出 data: {"delta":"…"} 流。迁移自 ai-proxy，已实测验证。

import (
	"bufio"
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"
	"os"
	"strings"
	"sync"
	"time"
)

var (
	aiProvider = env("AI_PROVIDER", "openai")
	aiBase     = strings.TrimRight(env("AI_BASE_URL", "https://api.openai.com/v1"), "/")
	aiModel    = env("AI_MODEL", "gpt-4o-mini")
	aiKey      = os.Getenv("AI_API_KEY")
	aiAllowRef = os.Getenv("ALLOWED_ORIGIN")
	aiMaxTok   = env("AI_MAX_TOKENS", "1024")
)

func clientIP(r *http.Request) string {
	if xff := r.Header.Get("X-Forwarded-For"); xff != "" {
		return strings.TrimSpace(strings.Split(xff, ",")[0])
	}
	host, _, err := net.SplitHostPort(r.RemoteAddr)
	if err != nil {
		return r.RemoteAddr
	}
	return host
}

func errStr(s string) map[string]string { return map[string]string{"error": s} }

func decodeBody(w http.ResponseWriter, r *http.Request, v any) bool {
	if err := json.NewDecoder(io.LimitReader(r.Body, 1<<20)).Decode(v); err != nil {
		writeJSON(w, http.StatusBadRequest, errStr("请求体不合法"))
		return false
	}
	return true
}

/* ================= 以下为 AI 代理（自 ai-proxy 迁移） ================= */

type msg struct {
	Role    string `json:"role"`
	Content string `json:"content"`
}

type chatReq struct {
	Messages []msg `json:"messages"`
}

/* AI 接口限流：每 IP 每分钟 10 次 */
type bucket struct {
	window time.Time
	count  int
}

var (
	aiMu sync.Mutex
	aiRL = map[string]*bucket{}
)

func allow(ip string) bool {
	aiMu.Lock()
	defer aiMu.Unlock()
	now := time.Now()
	b, ok := aiRL[ip]
	if !ok || now.Sub(b.window) > time.Minute {
		aiRL[ip] = &bucket{window: now, count: 1}
		if len(aiRL) > 5000 {
			aiRL = map[string]*bucket{ip: {window: now, count: 1}}
		}
		return true
	}
	b.count++
	return b.count <= 10
}

// buildUpstream 把统一消息转换为上游请求（body、URL、headers）
func buildUpstream(messages []msg, c aiConf) (string, http.Header, []byte, error) {
	// 截断：保留最近 20 条，防止 token 失控
	if len(messages) > 20 {
		messages = messages[len(messages)-20:]
	}

	switch c.Provider {
	case "anthropic":
		system := ""
		rest := make([]msg, 0, len(messages))
		for _, m := range messages {
			if m.Role == "system" {
				system += m.Content + "\n"
				continue
			}
			role := m.Role
			if role != "user" && role != "assistant" {
				role = "user"
			}
			rest = append(rest, msg{Role: role, Content: m.Content})
		}
		payload := map[string]any{
			"model":      c.Model,
			"messages":   rest,
			"max_tokens": json.Number(c.MaxTokens),
			"stream":     true,
		}
		if system != "" {
			payload["system"] = system
		}
		body, err := json.Marshal(payload)
		if err != nil {
			return "", nil, nil, err
		}
		h := http.Header{}
		h.Set("Content-Type", "application/json")
		h.Set("x-api-key", c.APIKey)
		h.Set("anthropic-version", "2023-06-01")
		url := c.BaseURL + "/v1/messages"
		if strings.Contains(c.BaseURL, "anthropic.com") {
			url = c.BaseURL + "/messages"
		}
		return url, h, body, nil

	default: // openai 兼容
		payload := map[string]any{
			"model":      c.Model,
			"messages":   messages,
			"max_tokens": json.Number(c.MaxTokens),
			"stream":     true,
		}
		body, err := json.Marshal(payload)
		if err != nil {
			return "", nil, nil, err
		}
		h := http.Header{}
		h.Set("Content-Type", "application/json")
		h.Set("Authorization", "Bearer "+c.APIKey)
		return c.BaseURL + "/chat/completions", h, body, nil
	}
}

/* ---------------- SSE 解析与统一转发 ---------------- */

type sseEvent struct {
	Delta string `json:"delta,omitempty"`
	Done  bool   `json:"done,omitempty"`
	Error string `json:"error,omitempty"`
}

/* ---------------- handlers ---------------- */

func handleChat(w http.ResponseWriter, r *http.Request) {
	ip := clientIP(r)
	if !allow(ip) {
		writeJSON(w, http.StatusTooManyRequests, map[string]string{"error": "too many requests"})
		return
	}
	cfg := effectiveAI()
	if cfg.APIKey == "" {
		writeJSON(w, http.StatusServiceUnavailable, map[string]string{"error": "ai not configured"})
		return
	}
	if aiAllowRef != "" && !strings.HasPrefix(r.Header.Get("Referer"), aiAllowRef) {
		writeJSON(w, http.StatusForbidden, map[string]string{"error": "forbidden"})
		return
	}
	if r.Method != http.MethodPost {
		writeJSON(w, http.StatusMethodNotAllowed, map[string]string{"error": "POST only"})
		return
	}

	var req chatReq
	if err := json.NewDecoder(io.LimitReader(r.Body, 1<<20)).Decode(&req); err != nil || len(req.Messages) == 0 {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "bad request"})
		return
	}

	url, header, body, err := buildUpstream(req.Messages, cfg)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "build failed"})
		return
	}

	hreq, err := http.NewRequestWithContext(r.Context(), http.MethodPost, url, bytes.NewReader(body))
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "upstream build failed"})
		return
	}
	hreq.Header = header

	resp, err := http.DefaultClient.Do(hreq)
	if err != nil {
		writeJSON(w, http.StatusBadGateway, map[string]string{"error": "upstream unreachable"})
		return
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		b, _ := io.ReadAll(io.LimitReader(resp.Body, 2048))
		writeJSON(w, http.StatusBadGateway, map[string]string{"error": fmt.Sprintf("upstream %d: %s", resp.StatusCode, string(b))})
		return
	}

	/* 统一 SSE 流 */
	flusher, ok := w.(http.Flusher)
	if !ok {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "streaming unsupported"})
		return
	}
	w.Header().Set("Content-Type", "text/event-stream; charset=utf-8")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("X-Accel-Buffering", "no") // nginx 不缓冲
	w.WriteHeader(http.StatusOK)

	send := func(ev sseEvent) bool {
		b, _ := json.Marshal(ev)
		if _, err := fmt.Fprintf(w, "data: %s\n\n", b); err != nil {
			return false
		}
		flusher.Flush()
		return true
	}

	sc := bufio.NewScanner(resp.Body)
	sc.Buffer(make([]byte, 0, 64*1024), 1024*1024)
	for sc.Scan() {
		line := strings.TrimSpace(sc.Text())
		if !strings.HasPrefix(line, "data:") {
			continue
		}
		data := strings.TrimSpace(strings.TrimPrefix(line, "data:"))

		var delta string
		var done bool
		var errText string

		switch cfg.Provider {
		case "anthropic":
			var ev struct {
				Type  string `json:"type"`
				Delta struct {
					Type string `json:"type"`
					Text string `json:"text"`
				} `json:"delta"`
				Error struct {
					Message string `json:"message"`
				} `json:"error"`
			}
			if json.Unmarshal([]byte(data), &ev) == nil {
				switch ev.Type {
				case "content_block_delta":
					if ev.Delta.Type == "text_delta" {
						delta = ev.Delta.Text
					}
				case "message_stop":
					done = true
				case "error":
					errText = ev.Error.Message
				}
			}
		default: // openai 兼容
			if data == "[DONE]" {
				done = true
				break
			}
			var ev struct {
				Choices []struct {
					Delta struct {
						Content string `json:"content"`
					} `json:"delta"`
				} `json:"choices"`
				Error *struct {
					Message string `json:"message"`
				} `json:"error"`
			}
			if json.Unmarshal([]byte(data), &ev) == nil {
				if ev.Error != nil {
					errText = ev.Error.Message
				} else if len(ev.Choices) > 0 {
					delta = ev.Choices[0].Delta.Content
				}
			}
		}

		if delta != "" {
			if !send(sseEvent{Delta: delta}) {
				return
			}
		}
		if errText != "" {
			send(sseEvent{Error: errText})
			return
		}
		if done {
			send(sseEvent{Done: true})
			return
		}
	}
	/* 上游流自然结束（无显式 done）也收尾 */
	send(sseEvent{Done: true})
}