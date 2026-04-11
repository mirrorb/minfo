// Package handlers 提供媒体路径联想接口。

package handlers

import (
	"net/http"
	"strings"

	"minfo/internal/httpapi/transport"
	"minfo/internal/media"
)

// PathSuggestHandler 根据前端输入前缀返回路径候选项和对应的根目录信息。
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

	responseItems := make([]transport.PathItem, 0, len(items))
	for _, item := range items {
		responseItems = append(responseItems, transport.PathItem{
			Path:     item.Path,
			IsDir:    item.IsDir,
			Size:     item.Size,
			Duration: item.Duration,
		})
	}

	transport.WritePathJSON(w, http.StatusOK, transport.PathResponse{
		OK:    true,
		Root:  root,
		Roots: roots,
		Items: responseItems,
	})
}
