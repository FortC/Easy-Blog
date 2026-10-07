package main

// 站点配置的可视化编辑：
//   GET/PUT /api/admin/siteconfig —— hugo.yaml 高频字段的定向读写（保留全部注释）
//   GET/PUT /api/admin/treasure   —— data/treasure.yaml 整文件可视化读写
// 保存后自动构建，构建失败自动回滚文件。

import (
	"net/http"
	"os"
	"path/filepath"
	"regexp"
	"strings"
)

var hugoYamlPath = func() string { return filepath.Join(siteRoot, "hugo.yaml") }()

/* ---------- 读取：定向解析 params 下的高频字段 ---------- */

func readSiteConfig() map[string]any {
	out := map[string]any{
		"title": "", "author": "", "motto": "", "hero_mark": "", "intro": "", "email": "",
		"site_birth": "", "icp": "", "notice": "", "now_date": "",
		"seo_google": "", "seo_bing": "", "seo_baidu": "",
		"now_items": []string{},
		"ai_enabled": false, "ai_name": "", "ai_hello": "", "ai_placeholder": "",
	}
	b, err := os.ReadFile(hugoYamlPath)
	if err != nil {
		return out
	}
	if m := regexp.MustCompile(`(?m)^title:\s*(.*)$`).FindStringSubmatch(string(b)); m != nil {
		out["title"] = strings.Trim(strings.TrimSpace(m[1]), `"`)
	}
	lines := strings.Split(string(b), "\n")
	inNow, inAI := false, false
	var items []string
	reKV := regexp.MustCompile(`^\s{2}(\w+):\s*(.*)$`)
	for _, l := range lines {
		if m := reKV.FindStringSubmatch(l); m != nil {
			inNow = m[1] == "now"
			inAI = m[1] == "ai"
			if v, ok := out[m[1]]; ok {
				if _, isStr := v.(string); isStr && m[2] != "" {
					raw := m[2]
					if i := strings.Index(raw, " #"); i >= 0 { /* 去掉行尾注释（# 在引号外） */
						raw = raw[:i]
					}
					out[m[1]] = strings.Trim(strings.TrimSpace(raw), `"`)
				}
			}
			continue
		}
		if inNow {
			if m := regexp.MustCompile(`^\s{4}date:\s*(.*)$`).FindStringSubmatch(l); m != nil {
				out["now_date"] = strings.Trim(m[1], `"`)
			}
			if m := regexp.MustCompile(`^\s{6}-\s*(.*)$`).FindStringSubmatch(l); m != nil {
				items = append(items, strings.Trim(strings.TrimSpace(m[1]), `"'`))
			}
			if regexp.MustCompile(`^\S`).MatchString(l) && strings.TrimSpace(l) != "" {
				inNow = false
			}
		}
		if inAI {
			if m := regexp.MustCompile(`^\s{4}(enabled|name|placeholder):\s*(.*)$`).FindStringSubmatch(l); m != nil {
				v := strings.Trim(strings.TrimSpace(m[2]), `"`)
				if m[1] == "enabled" {
					out["ai_enabled"] = v == "true"
				} else {
					out["ai_"+m[1]] = v
				}
			}
			if m := regexp.MustCompile(`^\s{4}hello:\s*(.*)$`).FindStringSubmatch(l); m != nil {
				out["ai_hello"] = strings.Trim(strings.TrimSpace(m[1]), `"`)
			}
			if regexp.MustCompile(`^\S`).MatchString(l) && strings.TrimSpace(l) != "" {
				inAI = false
			}
		}
	}
	out["now_items"] = items
	out["about_body"] = readAboutBody()
	return out
}

/* ---------- 关于页正文：content/about.md 的 body（front matter 原样保留） ---------- */

func aboutPath() string { return filepath.Join(siteRoot, "content", "about.md") }

func readAboutBody() string {
	b, err := os.ReadFile(aboutPath())
	if err != nil {
		return ""
	}
	if parts := strings.SplitN(string(b), "---", 3); len(parts) >= 3 {
		return strings.TrimSpace(parts[2])
	}
	return strings.TrimSpace(string(b))
}

func writeAboutBody(body string, build bool) (string, bool, error) {
	var fm string
	if b, err := os.ReadFile(aboutPath()); err == nil {
		if parts := strings.SplitN(string(b), "---", 3); len(parts) >= 3 {
			fm = "---" + parts[1] + "---\n"
		}
	}
	if fm == "" {
		fm = "---\ntitle: 关于\nkicker: About\nshow_about_extras: true\ndescription: 关于这个站。\n---\n"
	}
	body = strings.ReplaceAll(strings.ReplaceAll(body, "\r\n", "\n"), "\r", "\n")
	return saveAndBuild(aboutPath(), []byte(fm+body+"\n"), build)
}

/* ---------- 写入：行级定向替换（注释全保留），失败回滚 ---------- */

func yamlQuote(v string) string {
	return `"` + strings.NewReplacer(`\`, `\\`, `"`, `\"`).Replace(v) + `"`
}

type aiCfg struct {
	Enabled     bool   `json:"enabled"`
	Name        string `json:"name"`
	Hello       string `json:"hello"`
	Placeholder string `json:"placeholder"`
}

func writeSiteConfig(fields map[string]string, items []string, nowDate string, ai *aiCfg, build bool) (string, bool, error) {
	b, err := os.ReadFile(hugoYamlPath)
	if err != nil {
		return "", false, err
	}
	src := string(b)

	simple := map[string]*regexp.Regexp{
		"author":     regexp.MustCompile(`(?m)^(\s{2}author:\s*).*$`),
		"motto":      regexp.MustCompile(`(?m)^(\s{2}motto:\s*).*$`),
		"intro":      regexp.MustCompile(`(?m)^(\s{2}intro:\s*).*$`),
		"email":      regexp.MustCompile(`(?m)^(\s{2}email:\s*).*$`),
		"site_birth": regexp.MustCompile(`(?m)^(\s{2}site_birth:\s*).*$`),
		"icp":        regexp.MustCompile(`(?m)^(\s{2}icp:\s*).*$`),
		"notice":     regexp.MustCompile(`(?m)^(\s{2}notice:\s*).*$`),
		"seo_google": regexp.MustCompile(`(?m)^(\s{2}seo_google:\s*).*$`),
		"seo_bing":   regexp.MustCompile(`(?m)^(\s{2}seo_bing:\s*).*$`),
		"seo_baidu":  regexp.MustCompile(`(?m)^(\s{2}seo_baidu:\s*).*$`),
	}
	/* 站名在顶层（0 缩进），单写一条 */
	if v := fields["title"]; v != "" {
		src = regexp.MustCompile(`(?m)^(title:\s*).*$`).ReplaceAllString(src, "$1"+yamlQuote(v))
	}
	/* 高亮字允许清空（清了就不高亮） */
	if v, ok := fields["hero_mark"]; ok {
		if re := regexp.MustCompile(`(?m)^(\s{2}hero_mark:\s*).*$`); re.MatchString(src) {
			src = re.ReplaceAllString(src, "$1"+yamlQuote(v))
		}
	}
	for k, re := range simple {
		if v, ok := fields[k]; ok && v != "" {
			if re.MatchString(src) {
				src = re.ReplaceAllString(src, "$1"+yamlQuote(v))
			}
		}
	}
	/* now 块整体重写（若提供了 items） */
	if items != nil {
		if m := regexp.MustCompile(`(?m)^\s{2}now:\n((?:[ \t]+.*\n?)*)`).FindString(src); m != "" {
			var nb strings.Builder
			nb.WriteString("  now:\n")
			nb.WriteString("    date: " + yamlQuote(nowDate) + "\n")
			nb.WriteString("    items:\n")
			for _, it := range items {
				if strings.TrimSpace(it) == "" {
					continue
				}
				nb.WriteString("      - " + yamlQuote(strings.TrimSpace(it)) + "\n")
			}
			src = regexp.MustCompile(`(?m)^\s{2}now:\n(?:[ \t]+.*\n?)*`).ReplaceAllString(src, nb.String())
		}
	}
	/* ai 块整体重写（若提供了 ai 配置）；块后的空行兜底，正则不会吞掉后面的键 */
	if ai != nil && regexp.MustCompile(`(?m)^  ai:\n`).MatchString(src) {
		ab := strings.Builder{}
		ab.WriteString("  ai:\n")
		if ai.Enabled {
			ab.WriteString("    enabled: true\n")
		} else {
			ab.WriteString("    enabled: false\n")
		}
		ab.WriteString("    name: " + yamlQuote(ai.Name) + "\n")
		ab.WriteString("    hello: " + yamlQuote(ai.Hello) + "\n")
		ab.WriteString("    placeholder: " + yamlQuote(ai.Placeholder) + "\n")
		src = regexp.MustCompile(`(?m)^  ai:\n(?:[ \t]+.*\n?)*`).ReplaceAllString(src, ab.String())
	}
	return saveAndBuild(hugoYamlPath, []byte(src), build)
}

/* ---------- 友链：data/links.yaml 整文件读写 ---------- */

type linkItem struct {
	Name string `json:"name"`
	URL  string `json:"url"`
	Desc string `json:"desc"`
}

func readLinks() []linkItem {
	out := []linkItem{}
	b, err := os.ReadFile(filepath.Join(siteRoot, "data", "links.yaml"))
	if err != nil {
		return out
	}
	lines := strings.Split(string(b), "\n")
	var cur *linkItem
	for _, l := range lines {
		if m := regexp.MustCompile(`^\s*- name:\s*(.*)$`).FindStringSubmatch(l); m != nil {
			if cur != nil {
				out = append(out, *cur)
			}
			cur = &linkItem{Name: strings.Trim(m[1], `"'`)}
			continue
		}
		if cur == nil {
			continue
		}
		if m := regexp.MustCompile(`^\s*url:\s*(\S+)`).FindStringSubmatch(l); m != nil {
			cur.URL = strings.Trim(m[1], `"'`)
		} else if m := regexp.MustCompile(`^\s*desc:\s*(.*)$`).FindStringSubmatch(l); m != nil {
			cur.Desc = strings.Trim(m[1], `"'`)
		}
	}
	if cur != nil {
		out = append(out, *cur)
	}
	return out
}

func writeLinks(items []linkItem, build bool) (string, bool, error) {
	var b strings.Builder
	b.WriteString("# 友链数据\n")
	b.WriteString("# 后台「站点配置 → 友链」可视化编辑，或直接改本文件；name/url/desc 三项\n\n")
	b.WriteString("items:\n")
	for _, it := range items {
		if strings.TrimSpace(it.Name) == "" {
			continue
		}
		b.WriteString("  - name: " + yamlQuote(it.Name) + "\n")
		b.WriteString("    url: " + yamlQuote(it.URL) + "\n")
		b.WriteString("    desc: " + yamlQuote(it.Desc) + "\n\n")
	}
	return saveAndBuild(filepath.Join(siteRoot, "data", "links.yaml"), []byte(b.String()), build)
}

/* ---------- 保存 + 构建（失败回滚） ---------- */

func saveAndBuild(path string, content []byte, build bool) (logStr string, built bool, err error) {
	old, readErr := os.ReadFile(path)
	if readErr != nil {
		old = nil
	}
	if wErr := os.WriteFile(path, content, 0o644); wErr != nil {
		return "", false, wErr
	}
	if !build {
		return "", false, nil
	}
	logStr, err = buildSite()
	if err != nil {
		if old != nil {
			os.WriteFile(path, old, 0o644) /* 构建失败回滚，保证站点源码不被写坏 */
		}
		return logStr, false, err
	}
	return logStr, true, nil
}

/* ---------- handlers ---------- */

func handleSiteConfig(w http.ResponseWriter, r *http.Request) {
	switch r.Method {
	case http.MethodGet:
		writeJSON(w, http.StatusOK, readSiteConfig())
	case http.MethodPut:
		var req struct {
			Fields  map[string]string `json:"fields"`
			NowDate string            `json:"nowDate"`
			Items   *[]string         `json:"items"`
			AI      *aiCfg            `json:"ai"`
			About   *string           `json:"about"`
			Build   bool              `json:"build"`
		}
		if !decodeBody(w, r, &req) {
			return
		}
		var items []string
		if req.Items != nil {
			items = *req.Items
		}
		for k, v := range req.Fields { /* 拦截坏编码，防止把 hugo.yaml 写花 */
			if strings.ContainsRune(v, 0xFFFD) {
				writeJSON(w, http.StatusBadRequest, errStr("字段 "+k+" 编码异常，换个输入法/浏览器试试"))
				return
			}
		}
		/* 关于页正文先落盘（不构建），随后 writeSiteConfig 统一构建一次 */
		if req.About != nil {
			if strings.ContainsRune(*req.About, 0xFFFD) {
				writeJSON(w, http.StatusBadRequest, errStr("关于页正文编码异常，换个输入法/浏览器试试"))
				return
			}
			if _, _, err := writeAboutBody(*req.About, false); err != nil {
				writeJSON(w, http.StatusInternalServerError, errStr("关于页保存失败：" + err.Error()))
				return
			}
		}
		logStr, built, err := writeSiteConfig(req.Fields, items, req.NowDate, req.AI, req.Build)
		if err != nil {
			writeJSON(w, http.StatusInternalServerError, map[string]any{"error": "保存或构建失败（已回滚）", "log": logStr + "\n" + err.Error()})
			return
		}
		writeJSON(w, http.StatusOK, map[string]any{"ok": true, "built": built, "log": logStr})
	default:
		writeJSON(w, http.StatusMethodNotAllowed, errStr("GET/PUT only"))
	}
}

func handleLinks(w http.ResponseWriter, r *http.Request) {
	switch r.Method {
	case http.MethodGet:
		writeJSON(w, http.StatusOK, map[string]any{"items": readLinks()})
	case http.MethodPut:
		var req struct {
			Items []linkItem `json:"items"`
			Build bool       `json:"build"`
		}
		if !decodeBody(w, r, &req) {
			return
		}
		logStr, built, err := writeLinks(req.Items, req.Build)
		if err != nil {
			writeJSON(w, http.StatusInternalServerError, map[string]any{"error": "保存或构建失败（已回滚）", "log": logStr + "\n" + err.Error()})
			return
		}
		writeJSON(w, http.StatusOK, map[string]any{"ok": true, "built": built, "log": logStr})
	default:
		writeJSON(w, http.StatusMethodNotAllowed, errStr("GET/PUT only"))
	}
}
