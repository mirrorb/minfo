// Package httpapi 负责组装 API 路由和静态资源服务。

package httpapi

import (
	"io/fs"
	"net/http"

	"minfo/internal/httpapi/handlers"
	"minfo/internal/httpapi/middleware"
)

// NewHandler 组装 API 路由、静态文件服务和鉴权中间件，返回应用的统一入口 Handler。
func NewHandler(assets fs.FS) http.Handler {
	mux := http.NewServeMux()
	mux.Handle("/", http.FileServer(http.FS(assets)))
	mux.HandleFunc("/api/mediainfo", handlers.MediaInfoHandler())
	mux.HandleFunc("/api/bdinfo", handlers.BDInfoHandler())
	mux.HandleFunc("/api/info-jobs", handlers.InfoJobsHandler)
	mux.HandleFunc("/api/info-jobs/", handlers.InfoJobHandler)
	mux.HandleFunc("/api/screenshot-jobs", handlers.ScreenshotJobsHandler)
	mux.HandleFunc("/api/screenshot-jobs/", handlers.ScreenshotJobHandler)
	mux.HandleFunc("/api/screenshots", handlers.ScreenshotsHandler)
	mux.HandleFunc("/api/torrent-jobs", handlers.TorrentJobsHandler)
	mux.HandleFunc("/api/torrent-jobs/", handlers.TorrentJobHandler)
	mux.HandleFunc("/api/path", handlers.PathSuggestHandler)
	return middleware.Logging(middleware.Authenticate(mux))
}
