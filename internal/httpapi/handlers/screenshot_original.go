package handlers

import (
	"mime"
	"net/http"

	"minfo/internal/httpapi/transport"
	screenshotdelivery "minfo/internal/screenshot/delivery"
)

// ScreenshotOriginalHandler 下载无损原图，或在入口消失后删除原图缓存。
func ScreenshotOriginalHandler(w http.ResponseWriter, r *http.Request) {
	if r.Method == http.MethodDelete {
		if err := screenshotdelivery.DeleteOriginalPNG(r.URL.Query().Get("token")); err != nil {
			transport.WriteError(w, http.StatusInternalServerError, "无损原图清理失败。")
			return
		}
		w.WriteHeader(http.StatusNoContent)
		return
	}
	if r.Method != http.MethodGet && r.Method != http.MethodHead {
		w.Header().Set("Allow", "GET, HEAD, DELETE")
		transport.WriteError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}
	path, filename, err := screenshotdelivery.GetOriginalPNG(r.URL.Query().Get("token"))
	if err != nil {
		transport.WriteError(w, http.StatusNotFound, "无损原图已过期或不存在，请重新生成截图。")
		return
	}
	w.Header().Set("Content-Type", "image/png")
	w.Header().Set("Content-Disposition", mime.FormatMediaType("attachment", map[string]string{"filename": filename}))
	w.Header().Set("Cache-Control", "no-store")
	http.ServeFile(w, r, path)
}
