package handlers

import (
	"log"
	"net/http"

	"minfo/internal/httpapi/logstream"
	"minfo/internal/httpapi/transport"
)

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

	if err := logstream.ServeWS(w, r, sessionID); err != nil {
		log.Printf("logs websocket upgrade failed: %v", err)
	}
}
