package main

// 头像 / 海报上传：解码 → 盒采样缩小 → JPEG 重编码（自动压缩），存 static/img/ 后重建。
// POST   /api/admin/upload  (multipart: file, kind=avatar|poster)
// DELETE /api/admin/upload?kind=avatar|poster

import (
	"fmt"
	"image"
	"image/gif"
	"image/jpeg"
	"image/png"
	"net/http"
	"os"
	"path/filepath"
	"strings"
)

type uploadSpec struct {
	rel    string // 相对 SITE_ROOT
	maxDim int    // 长边上限（px），只缩不放
}

var uploadKinds = map[string]uploadSpec{
	"avatar": {"static/img/avatar.jpg", 720},
	"poster": {"static/img/poster.jpg", 1600},
}

func handleUpload(w http.ResponseWriter, r *http.Request) {
	spec, ok := uploadKinds[r.FormValue("kind")]
	if !ok && r.Method == http.MethodPost {
		writeJSON(w, http.StatusBadRequest, errStr("kind 只能是 avatar 或 poster"))
		return
	}
	switch r.Method {
	case http.MethodPost:
		r.Body = http.MaxBytesReader(w, r.Body, 15<<20)
		file, _, err := r.FormFile("file")
		if err != nil {
			writeJSON(w, http.StatusBadRequest, errStr("没收到文件"))
			return
		}
		defer file.Close()

		head := make([]byte, 512)
		n, _ := file.Read(head)
		if _, err := file.Seek(0, 0); err != nil {
			writeJSON(w, http.StatusInternalServerError, errStr("读不了文件"))
			return
		}
		var img image.Image
		switch http.DetectContentType(head[:n]) {
		case "image/jpeg":
			img, err = jpeg.Decode(file)
		case "image/png":
			img, err = png.Decode(file)
		case "image/gif":
			img, err = gif.Decode(file) /* 只取第一帧 */
		default:
			writeJSON(w, http.StatusBadRequest, errStr("只支持 JPG / PNG / GIF（WebP 请先转成这三种）"))
			return
		}
		if err != nil {
			writeJSON(w, http.StatusBadRequest, errStr("图片解析失败，换一张试试"))
			return
		}

		out := shrink(img, spec.maxDim)
		full := filepath.Join(siteRoot, filepath.FromSlash(spec.rel))
		if err := os.MkdirAll(filepath.Dir(full), 0o755); err != nil {
			writeJSON(w, http.StatusInternalServerError, errStr("建目录失败"))
			return
		}
		f, err := os.Create(full)
		if err != nil {
			writeJSON(w, http.StatusInternalServerError, errStr("写不进去："+err.Error()))
			return
		}
		if err := jpeg.Encode(f, out, &jpeg.Options{Quality: 82}); err != nil {
			f.Close()
			writeJSON(w, http.StatusInternalServerError, errStr("压缩编码失败"))
			return
		}
		f.Close()

		logStr, buildErr := buildSite()
		if buildErr != nil {
			writeJSON(w, http.StatusInternalServerError, map[string]any{"error": "图片已存但构建失败", "log": logStr})
			return
		}
		st, _ := os.Stat(full)
		pubURL := "/" + strings.TrimPrefix(filepath.ToSlash(spec.rel), "static/")
		writeJSON(w, http.StatusOK, map[string]any{
			"ok": true, "built": true, "url": pubURL + "?v=" + stamp(st),
			"sizeKB": st.Size() / 1024,
		})

	case http.MethodDelete:
		kind := r.URL.Query().Get("kind")
		spec, ok := uploadKinds[kind]
		if !ok {
			writeJSON(w, http.StatusBadRequest, errStr("kind 只能是 avatar 或 poster"))
			return
		}
		full := filepath.Join(siteRoot, filepath.FromSlash(spec.rel))
		if _, err := os.Stat(full); err == nil {
			if err := os.Remove(full); err != nil {
				writeJSON(w, http.StatusInternalServerError, errStr("删除失败"))
				return
			}
			if _, buildErr := buildSite(); buildErr != nil {
				writeJSON(w, http.StatusInternalServerError, errStr("已删但重建失败"))
				return
			}
		}
		writeJSON(w, http.StatusOK, map[string]bool{"ok": true})

	default:
		writeJSON(w, http.StatusMethodNotAllowed, errStr("POST/DELETE only"))
	}
}

func stamp(st os.FileInfo) string {
	if st == nil {
		return "0"
	}
	return fmt.Sprintf("%d", st.ModTime().Unix())
}

/* 盒采样缩小：把源像素块平均成一个目标像素，透明色垫白（JPEG 无 alpha）。
   只缩不放；无需缩放时也走一遍重编码（等效压缩转档）。 */
func shrink(src image.Image, maxDim int) image.Image {
	b := src.Bounds()
	w, h := b.Dx(), b.Dy()
	if w < 1 || h < 1 {
		return src
	}
	scale := 1.0
	if w >= h && w > maxDim {
		scale = float64(maxDim) / float64(w)
	} else if h > w && h > maxDim {
		scale = float64(maxDim) / float64(h)
	}
	nw, nh := w, h
	if scale < 1.0 {
		nw = max(1, int(float64(w)*scale))
		nh = max(1, int(float64(h)*scale))
	}
	dst := image.NewRGBA(image.Rect(0, 0, nw, nh))
	for y := 0; y < nh; y++ {
		sy0 := b.Min.Y + y*h/nh
		sy1 := b.Min.Y + (y+1)*h/nh
		if sy1 <= sy0 {
			sy1 = sy0 + 1
		}
		for x := 0; x < nw; x++ {
			sx0 := b.Min.X + x*w/nw
			sx1 := b.Min.X + (x+1)*w/nw
			if sx1 <= sx0 {
				sx1 = sx0 + 1
			}
			var rsum, gsum, bsum, cnt uint64
			for sy := sy0; sy < sy1 && sy < b.Max.Y; sy++ {
				for sx := sx0; sx < sx1 && sx < b.Max.X; sx++ {
					pr, pg, pb, _ := src.At(sx, sy).RGBA()
					rsum += uint64(pr >> 8)
					gsum += uint64(pg >> 8)
					bsum += uint64(pb >> 8)
					cnt++
				}
			}
			if cnt == 0 {
				cnt = 1
			}
			off := dst.PixOffset(x, y)
			dst.Pix[off] = uint8(rsum / cnt)
			dst.Pix[off+1] = uint8(gsum / cnt)
			dst.Pix[off+2] = uint8(bsum / cnt)
			dst.Pix[off+3] = 255
		}
	}
	return dst
}
