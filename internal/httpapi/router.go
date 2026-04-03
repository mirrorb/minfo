package httpapi

import (
	"io/fs"
	"net/http"

	"minfo/internal/httpapi/handlers"
	"minfo/internal/httpapi/middleware"
)

func NewHandler(assets fs.FS) http.Handler {
	mux := http.NewServeMux()
	mux.Handle("/", http.FileServer(http.FS(assets)))
	mux.HandleFunc("/api/logs", handlers.LogsHandler)
	mux.HandleFunc("/api/mediainfo", handlers.MediaInfoHandler("MEDIAINFO_BIN", "mediainfo"))
	mux.HandleFunc("/api/bdinfo", handlers.BDInfoHandler("BDINFO_BIN", "bdinfo"))
	mux.HandleFunc("/api/screenshots", handlers.ScreenshotsHandler)
	mux.HandleFunc("/api/path", handlers.PathSuggestHandler)
	return middleware.Logging(middleware.Authenticate(mux))
}
