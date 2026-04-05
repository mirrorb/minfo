// Package handlers 提供实时日志 WebSocket 接口。

package handlers

import (
	"log"
	"net/http"

	"minfo/internal/httpapi/logstream"
	"minfo/internal/httpapi/transport"
)

// LogsHandler 校验日志会话参数，并将请求升级为实时日志 WebSocket 连接。
func LogsHandler(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		transport.WriteError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}

	sessionID := logstream.NormalizeSessionID(r.URL.Query().Get("session"))
	if sessionID == "" {
		transport.WriteError(w, http.StatusBadRequest, "invalid log session")
		return
	}

	if err := logstream.ServeWebSocket(w, r, sessionID); err != nil {
		log.Printf("logs websocket upgrade failed: %v | host=%q origin=%q x_forwarded_host=%q x_forwarded_port=%q forwarded=%q",
			err,
			r.Host,
			r.Header.Get("Origin"),
			r.Header.Get("X-Forwarded-Host"),
			r.Header.Get("X-Forwarded-Port"),
			r.Header.Get("Forwarded"),
		)
	}
}
