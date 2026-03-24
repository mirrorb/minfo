package handlers

import (
	"context"
	"fmt"
	"net/http"

	"minfo/internal/config"
	"minfo/internal/httpapi/transport"
	"minfo/internal/media"
	"minfo/internal/system"
)

func MediaInfoHandler(envKey, fallback string) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if !transport.EnsurePost(w, r) {
			return
		}
		if err := transport.ParseForm(w, r); err != nil {
			transport.WriteError(w, http.StatusBadRequest, err.Error())
			return
		}
		defer transport.CleanupMultipart(r)

		path, cleanup, err := transport.InputPath(r)
		if err != nil {
			transport.WriteError(w, http.StatusBadRequest, err.Error())
			return
		}
		defer cleanup()

		bin, err := system.ResolveBin(envKey, fallback)
		if err != nil {
			transport.WriteError(w, http.StatusBadRequest, err.Error())
			return
		}

		ctx, cancel := context.WithTimeout(r.Context(), config.RequestTimeout)
		defer cancel()

		candidates, sourceCleanup, err := media.ResolveMediaInfoCandidates(ctx, path, media.MediaInfoCandidateLimit)
		if err != nil {
			transport.WriteError(w, http.StatusBadRequest, err.Error())
			return
		}
		defer sourceCleanup()

		var lastErr string
		for _, sourcePath := range candidates {
			stdout, stderr, err := system.RunCommand(ctx, bin, sourcePath)
			if err != nil {
				lastErr = system.BestErrorMessage(err, stderr, stdout)
				continue
			}

			output := system.CombineCommandOutput(stdout, stderr)
			if output == "" {
				lastErr = fmt.Sprintf("mediainfo returned empty output for: %s", sourcePath)
				continue
			}

			transport.WriteJSON(w, http.StatusOK, transport.InfoResponse{OK: true, Output: output})
			return
		}

		if lastErr == "" {
			lastErr = "mediainfo returned empty output"
		}
		transport.WriteError(w, http.StatusInternalServerError, lastErr)
	}
}

func BDInfoHandler(envKey, fallback string) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if !transport.EnsurePost(w, r) {
			return
		}
		if err := transport.ParseForm(w, r); err != nil {
			transport.WriteError(w, http.StatusBadRequest, err.Error())
			return
		}
		defer transport.CleanupMultipart(r)

		path, cleanup, err := transport.InputPath(r)
		if err != nil {
			transport.WriteError(w, http.StatusBadRequest, err.Error())
			return
		}
		defer cleanup()

		bin, err := system.ResolveBin(envKey, fallback)
		if err != nil {
			transport.WriteError(w, http.StatusBadRequest, err.Error())
			return
		}

		ctx, cancel := context.WithTimeout(r.Context(), config.RequestTimeout)
		defer cancel()

		bdPath, bdCleanup, err := media.ResolveBDInfoSource(ctx, path)
		if err != nil {
			transport.WriteError(w, http.StatusBadRequest, err.Error())
			return
		}
		defer bdCleanup()

		stdout, stderr, err := system.RunCommand(ctx, bin, bdPath)
		if err != nil {
			transport.WriteError(w, http.StatusInternalServerError, system.BestErrorMessage(err, stderr, stdout))
			return
		}

		transport.WriteJSON(w, http.StatusOK, transport.InfoResponse{OK: true, Output: system.CombineCommandOutput(stdout, stderr)})
	}
}
