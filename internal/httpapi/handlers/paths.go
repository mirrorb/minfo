package handlers

import (
	"net/http"
	"strings"

	"minfo/internal/httpapi/transport"
	"minfo/internal/media"
)

func PathSuggestHandler(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		transport.WritePathError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}

	roots, err := media.ResolveRoots(media.MediaRoots())
	if err != nil {
		transport.WritePathError(w, http.StatusBadRequest, err.Error())
		return
	}
	prefix := strings.TrimSpace(r.URL.Query().Get("prefix"))
	prefix = strings.Trim(prefix, "\"")

	items, root, err := media.SuggestPaths(roots, prefix, 0)
	if err != nil {
		transport.WritePathError(w, http.StatusBadRequest, err.Error())
		return
	}

	transport.WritePathJSON(w, http.StatusOK, transport.PathResponse{
		OK:    true,
		Root:  root,
		Roots: roots,
		Items: items,
	})
}
