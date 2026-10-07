package main

// 内容管理：白名单文件读写（Markdown/YAML）+ Hugo 重建 + 原子发布

import (
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
)

/* ---------- 路径白名单 ----------
   允许编辑：
     hugo.yaml
     data/*.yaml
     content/*.md           （about/links/board 等单页）
     content/posts/*.md
     content/sayings/*.md
*/

var (
	reHugoYaml  = regexp.MustCompile(`^hugo\.yaml$`)
	reDataYaml  = regexp.MustCompile(`^data/[\w\-]+\.yaml$`)
	reRootMd    = regexp.MustCompile(`^content/[\w\-]+\.md$`)
	rePostsMd   = regexp.MustCompile(`^content/posts/[\w\-]+\.md$`)
	reSayMd     = regexp.MustCompile(`^content/sayings/[\w\-]+\.md$`)
	reTreMd     = regexp.MustCompile(`^content/treasure/[\w\-]+\.md$`)
	reWorksMd   = regexp.MustCompile(`^content/works/[\w\-]+\.md$`)
	reFilename  = regexp.MustCompile(`^[\w\-]+$`)
)

func allowedContentPath(rel string) bool {
	rel = filepath.ToSlash(rel)
	return reHugoYaml.MatchString(rel) || reDataYaml.MatchString(rel) ||
		reRootMd.MatchString(rel) || rePostsMd.MatchString(rel) || reSayMd.MatchString(rel) ||
		reTreMd.MatchString(rel) || reWorksMd.MatchString(rel)
}

/* ---------- 内容列表（解析 front matter 摘要） ---------- */

type postMeta struct {
	File    string `json:"file"` // 相对 content/ 的文件名
	Title   string `json:"title"`
	Date    string `json:"date"`
	Pin     bool   `json:"pin"`
	Cat     string `json:"cat,omitempty"`     // 百宝库分类
	URL     string `json:"url,omitempty"`     // 百宝库外链
	Desc    string `json:"desc,omitempty"`    // 百宝库一句话推荐
	Feature bool   `json:"feature,omitempty"` // 百宝库主推
}

func handlePosts(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		writeJSON(w, http.StatusMethodNotAllowed, errStr("GET only"))
		return
	}
	out := map[string][]postMeta{"posts": {}, "sayings": {}, "treasure": {}, "works": {}}
	for _, dir := range []string{"posts", "sayings", "treasure", "works"} {
		files, _ := filepath.Glob(filepath.Join(siteRoot, "content", dir, "*.md"))
		for _, f := range files {
			base := filepath.Base(f)
			if base == "_index.md" {
				continue
			}
			b, err := os.ReadFile(f)
			if err != nil {
				continue
			}
			fm := parseFrontMatter(string(b))
			out[dir] = append(out[dir], postMeta{
				File: dir + "/" + base, Title: fm["title"], Date: fm["date"], Pin: fm["pin"] == "true",
				Cat: fm["cat"], URL: fm["link"], Desc: fm["desc"], Feature: fm["feature"] == "true",
			})
		}
	}
	writeJSON(w, http.StatusOK, out)
}

/* 极简 front matter 解析：只取 title/date/pin 三项（够后台列表用） */
func parseFrontMatter(src string) map[string]string {
	out := map[string]string{}
	lines := strings.Split(src, "\n")
	if len(lines) == 0 || strings.TrimSpace(lines[0]) != "---" {
		return out
	}
	for _, l := range lines[1:] {
		if strings.TrimSpace(l) == "---" {
			break
		}
		if i := strings.Index(l, ":"); i > 0 {
			k := strings.TrimSpace(l[:i])
			v := strings.TrimSpace(l[i+1:])
			v = strings.Trim(v, `"'[]`)
			if k == "categories" || k == "tags" {
				continue
			}
			out[k] = v
		}
	}
	return out
}

/* ---------- 文件读写 ---------- */

func handleContent(w http.ResponseWriter, r *http.Request) {
	rel := strings.TrimPrefix(r.URL.Query().Get("path"), "/")

	var req struct {
		Path    string `json:"path"`
		Content string `json:"content"`
		Build   bool   `json:"build"`
	}
	if r.Method == http.MethodPut {
		if !decodeBody(w, r, &req) {
			return
		}
		if req.Path != "" {
			rel = strings.TrimPrefix(req.Path, "/")
		}
	}
	if !allowedContentPath(rel) {
		writeJSON(w, http.StatusForbidden, errStr("这个路径不让改（白名单：hugo.yaml、data/*.yaml、content/ 下的 md）"))
		return
	}
	full := filepath.Join(siteRoot, filepath.FromSlash(rel))

	switch r.Method {
	case http.MethodGet:
		b, err := os.ReadFile(full)
		if err != nil {
			writeJSON(w, http.StatusNotFound, errStr("文件不存在"))
			return
		}
		writeJSON(w, http.StatusOK, map[string]string{"path": rel, "content": string(b)})

	case http.MethodPut:
		if len(req.Content) > 512*1024 {
			writeJSON(w, http.StatusRequestEntityTooLarge, errStr("文件太大（>512KB）"))
			return
		}
		os.MkdirAll(filepath.Dir(full), 0o755)
		if err := os.WriteFile(full, []byte(req.Content), 0o644); err != nil {
			writeJSON(w, http.StatusInternalServerError, errStr("写不进去："+err.Error()))
			return
		}
		if req.Build {
			out, err := buildSite()
			if err != nil {
				writeJSON(w, http.StatusOK, map[string]any{"ok": true, "built": false, "buildLog": out})
				return
			}
			writeJSON(w, http.StatusOK, map[string]any{"ok": true, "built": true, "buildLog": out})
			return
		}
		writeJSON(w, http.StatusOK, map[string]any{"ok": true, "built": false})

	case http.MethodDelete:
		if rel == "hugo.yaml" {
			writeJSON(w, http.StatusForbidden, errStr("hugo.yaml 不能删"))
			return
		}
		if err := os.Remove(full); err != nil {
			writeJSON(w, http.StatusInternalServerError, errStr("删除失败"))
			return
		}
		writeJSON(w, http.StatusOK, map[string]bool{"ok": true})

	default:
		writeJSON(w, http.StatusMethodNotAllowed, errStr("GET/PUT/DELETE only"))
	}
}

/* ---------- Hugo 构建 + 原子发布 ---------- */

func buildSite() (string, error) {
	buildMu.Lock()
	defer buildMu.Unlock()

	tmpDir := filepath.Join(siteRoot, "public_tmp")
	oldDir := filepath.Join(siteRoot, "public_old")
	os.RemoveAll(tmpDir)

	cmd := exec.Command(hugoBin, "--minify", "--source", siteRoot, "-d", tmpDir)
	out, err := cmd.CombinedOutput()
	logStr := string(out)
	if err != nil {
		os.RemoveAll(tmpDir)
		return logStr, err
	}
	/* 成功：public → public_old，tmp → public，删 old */
	os.RemoveAll(oldDir)
	if _, statErr := os.Stat(filepath.Join(siteRoot, "public")); statErr == nil {
		if err := os.Rename(filepath.Join(siteRoot, "public"), oldDir); err != nil {
			return logStr, err
		}
	}
	if err := os.Rename(tmpDir, filepath.Join(siteRoot, "public")); err != nil {
		/* 回滚 */
		if _, statErr := os.Stat(oldDir); statErr == nil {
			os.Rename(oldDir, filepath.Join(siteRoot, "public"))
		}
		return logStr, err
	}
	os.RemoveAll(oldDir)
	return logStr + "\n构建成功并已发布。", nil
}

func handleBuild(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writeJSON(w, http.StatusMethodNotAllowed, errStr("POST only"))
		return
	}
	out, err := buildSite()
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "构建失败", "log": out + "\n" + err.Error()})
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"ok": "true", "log": out})
}

/* 可编辑配置文件清单 */
func handleConfigs(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		writeJSON(w, http.StatusMethodNotAllowed, errStr("GET only"))
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"configs": listEditableConfigs()})
}

/* 给 admin 页列出可编辑的配置文件 */
func listEditableConfigs() []string {
	out := []string{"hugo.yaml"}
	files, _ := filepath.Glob(filepath.Join(siteRoot, "data", "*.yaml"))
	sort.Strings(files)
	for _, f := range files {
		out = append(out, "data/"+filepath.Base(f))
	}
	return out
}
