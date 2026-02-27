package main

import (
    "context"
    "errors"
    "fmt"
    "log"
    "math/rand"
    "net/http"
    "os"
    "path/filepath"
    "strconv"
    "strings"
    "time"
)

func infoHandler(envKey, fallback string) http.HandlerFunc {
    return func(w http.ResponseWriter, r *http.Request) {
        if !ensurePost(w, r) {
            return
        }
        if err := parseForm(w, r); err != nil {
            writeError(w, http.StatusBadRequest, err.Error())
            return
        }
        defer cleanupMultipart(r)

        path, cleanup, err := inputPath(r)
        if err != nil {
            writeError(w, http.StatusBadRequest, err.Error())
            return
        }
        defer cleanup()

        bin, err := resolveBin(envKey, fallback)
        if err != nil {
            writeError(w, http.StatusBadRequest, err.Error())
            return
        }

        ctx, cancel := context.WithTimeout(r.Context(), infoTimeout)
        defer cancel()

        stdout, stderr, err := runCommand(ctx, bin, path)
        if err != nil {
            writeError(w, http.StatusInternalServerError, bestErrorMessage(err, stderr, stdout))
            return
        }

        output := strings.TrimSpace(stdout)
        if strings.TrimSpace(stderr) != "" {
            if output != "" {
                output += "\n\n"
            }
            output += strings.TrimSpace(stderr)
        }
        if output == "" {
            writeError(w, http.StatusInternalServerError, "mediainfo returned empty output")
            return
        }

        writeJSON(w, http.StatusOK, infoResponse{OK: true, Output: output})
    }
}

func mediainfoHandler(envKey, fallback string) http.HandlerFunc {
    return func(w http.ResponseWriter, r *http.Request) {
        if !ensurePost(w, r) {
            return
        }
        if err := parseForm(w, r); err != nil {
            writeError(w, http.StatusBadRequest, err.Error())
            return
        }
        defer cleanupMultipart(r)

        path, cleanup, err := inputPath(r)
        if err != nil {
            writeError(w, http.StatusBadRequest, err.Error())
            return
        }
        defer cleanup()

        bin, err := resolveBin(envKey, fallback)
        if err != nil {
            writeError(w, http.StatusBadRequest, err.Error())
            return
        }

        ctx, cancel := context.WithTimeout(r.Context(), infoTimeout)
        defer cancel()

        candidates, sourceCleanup, err := resolveMediaInfoCandidates(ctx, path, mediaInfoCandidateLimit)
        if err != nil {
            writeError(w, http.StatusBadRequest, err.Error())
            return
        }
        defer sourceCleanup()

        var lastErr string
        for _, sourcePath := range candidates {
            stdout, stderr, err := runCommand(ctx, bin, sourcePath)
            if err != nil {
                lastErr = bestErrorMessage(err, stderr, stdout)
                continue
            }

            output := strings.TrimSpace(stdout)
            if strings.TrimSpace(stderr) != "" {
                if output != "" {
                    output += "\n\n"
                }
                output += strings.TrimSpace(stderr)
            }
            if output == "" {
                lastErr = fmt.Sprintf("mediainfo returned empty output for: %s", sourcePath)
                continue
            }

            writeJSON(w, http.StatusOK, infoResponse{OK: true, Output: output})
            return
        }

        if lastErr == "" {
            lastErr = "mediainfo returned empty output"
        }
        writeError(w, http.StatusInternalServerError, lastErr)
    }
}

func bdinfoHandler(envKey, fallback string) http.HandlerFunc {
    return func(w http.ResponseWriter, r *http.Request) {
        if !ensurePost(w, r) {
            return
        }
        if err := parseForm(w, r); err != nil {
            writeError(w, http.StatusBadRequest, err.Error())
            return
        }
        defer cleanupMultipart(r)

        path, cleanup, err := inputPath(r)
        if err != nil {
            writeError(w, http.StatusBadRequest, err.Error())
            return
        }
        defer cleanup()

        bin, err := resolveBin(envKey, fallback)
        if err != nil {
            writeError(w, http.StatusBadRequest, err.Error())
            return
        }

        ctx, cancel := context.WithTimeout(r.Context(), infoTimeout)
        defer cancel()

        bdPath, bdCleanup, err := resolveBDInfoSource(ctx, path)
        if err != nil {
            writeError(w, http.StatusBadRequest, err.Error())
            return
        }
        defer bdCleanup()

        stdout, stderr, err := runCommand(ctx, bin, bdPath)
        if err != nil {
            writeError(w, http.StatusInternalServerError, bestErrorMessage(err, stderr, stdout))
            return
        }

        output := strings.TrimSpace(stdout)
        if strings.TrimSpace(stderr) != "" {
            if output != "" {
                output += "\n\n"
            }
            output += strings.TrimSpace(stderr)
        }

        writeJSON(w, http.StatusOK, infoResponse{OK: true, Output: output})
    }
}

func screenshotsHandler(w http.ResponseWriter, r *http.Request) {
    if !ensurePost(w, r) {
        return
    }
    if err := parseForm(w, r); err != nil {
        writeError(w, http.StatusBadRequest, err.Error())
        return
    }
    defer cleanupMultipart(r)

    path, cleanup, err := inputPath(r)
    if err != nil {
        writeError(w, http.StatusBadRequest, err.Error())
        return
    }
    defer cleanup()

    ffprobe, err := resolveBin("FFPROBE_BIN", "ffprobe")
    if err != nil {
        writeError(w, http.StatusBadRequest, err.Error())
        return
    }
    ffmpeg, err := resolveBin("FFMPEG_BIN", "ffmpeg")
    if err != nil {
        writeError(w, http.StatusBadRequest, err.Error())
        return
    }

    ctx, cancel := context.WithTimeout(r.Context(), shotTimeout)
    defer cancel()

    sourcePath, sourceCleanup, err := resolveScreenshotSource(ctx, path)
    if err != nil {
        writeError(w, http.StatusBadRequest, err.Error())
        return
    }
    defer sourceCleanup()

    duration, err := probeDuration(ctx, ffprobe, sourcePath)
    if err != nil {
        writeError(w, http.StatusInternalServerError, err.Error())
        return
    }

    tempDir, err := os.MkdirTemp("", "minfo-shots-*")
    if err != nil {
        writeError(w, http.StatusInternalServerError, err.Error())
        return
    }
    defer os.RemoveAll(tempDir)

    stamps := calcTimestamps(duration)
    files := make([]string, 0, len(stamps))
    for i, ts := range stamps {
        outPath := filepath.Join(tempDir, fmt.Sprintf("shot_%02d.png", i+1))
        if err := captureShot(ctx, ffmpeg, sourcePath, ts, outPath); err != nil {
            writeError(w, http.StatusInternalServerError, err.Error())
            return
        }
        files = append(files, outPath)
    }

    zipBytes, err := zipFiles(files)
    if err != nil {
        writeError(w, http.StatusInternalServerError, err.Error())
        return
    }

    w.Header().Set("Content-Type", "application/zip")
    w.Header().Set("Content-Disposition", "attachment; filename=\"screenshots.zip\"")
    w.WriteHeader(http.StatusOK)
    if _, err := w.Write(zipBytes); err != nil {
        log.Printf("write response: %v", err)
    }
}

func pathSuggestHandler(w http.ResponseWriter, r *http.Request) {
    if r.Method != http.MethodGet {
        writePathError(w, http.StatusMethodNotAllowed, "method not allowed")
        return
    }

    root := mediaRoot()
    prefix := strings.TrimSpace(r.URL.Query().Get("prefix"))
    prefix = strings.Trim(prefix, "\"")

    items, err := suggestPaths(root, prefix, maxSuggestions)
    if err != nil {
        writePathError(w, http.StatusBadRequest, err.Error())
        return
    }

    writePathJSON(w, http.StatusOK, pathResponse{
        OK:    true,
        Root:  root,
        Items: items,
    })
}

func probeDuration(ctx context.Context, ffprobe, path string) (float64, error) {
    stdout, stderr, err := runCommand(ctx, ffprobe,
        "-v", "error",
        "-show_entries", "format=duration",
        "-of", "default=noprint_wrappers=1:nokey=1",
        path,
    )
    if err != nil {
        msg := strings.TrimSpace(stderr)
        if msg == "" {
            msg = err.Error()
        }
        return 0, fmt.Errorf("ffprobe failed: %s", msg)
    }

    value := strings.TrimSpace(stdout)
    if value == "" {
        return 0, errors.New("ffprobe returned empty duration")
    }
    duration, err := strconv.ParseFloat(value, 64)
    if err != nil {
        return 0, fmt.Errorf("invalid duration: %v", err)
    }
    if duration <= 0 {
        return 0, errors.New("duration must be positive")
    }
    return duration, nil
}

func captureShot(ctx context.Context, ffmpeg, path string, seconds float64, outPath string) error {
    ts := fmt.Sprintf("%.3f", seconds)
    stdout, stderr, err := runCommand(ctx, ffmpeg,
        "-hide_banner",
        "-loglevel", "error",
        "-y",
        "-ss", ts,
        "-i", path,
        "-frames:v", "1",
        "-q:v", "2",
        "-an",
        outPath,
    )
    if err != nil {
        msg := strings.TrimSpace(stderr)
        if msg == "" {
            msg = err.Error()
        }
        if strings.TrimSpace(stdout) != "" {
            msg += "\n" + strings.TrimSpace(stdout)
        }
        return fmt.Errorf("ffmpeg failed: %s", msg)
    }
    return nil
}

func calcTimestamps(duration float64) []float64 {
    const shots = 8
    if duration <= 0 {
        return nil
    }

    rng := rand.New(rand.NewSource(time.Now().UnixNano()))
    ts := make([]float64, 0, shots)
    used := make(map[int]bool, shots)

    step := duration / float64(shots+1)
    maxT := duration - 0.2
    if maxT < 0 {
        maxT = duration
    }

    for i := 0; i < shots; i++ {
        base := step * float64(i+1)
        if duration < 1 {
            base = duration * (float64(i+1) / float64(shots+1))
        }

        jitter := step * 0.25
        if jitter <= 0 {
            jitter = duration * 0.05
        }
        t := base + (rng.Float64()*2-1)*jitter
        if t > maxT {
            t = maxT
        }
        if t < 0 {
            t = 0
        }

        key := int(t * 1000)
        for tries := 0; tries < 10 && used[key]; tries++ {
            t += 0.137
            if t > maxT {
                t = maxT - 0.137
            }
            if t < 0 {
                t = 0
            }
            key = int(t * 1000)
        }
        used[key] = true
        ts = append(ts, t)
    }

    return ts
}

func suggestPaths(root, prefix string, limit int) ([]string, error) {
    root = filepath.Clean(root)
    rootAbs, err := filepath.Abs(root)
    if err != nil {
        return nil, err
    }

    if prefix == "" {
        return listDir(rootAbs, "", limit)
    }

    cleaned := filepath.Clean(prefix)
    var absPrefix string
    if filepath.IsAbs(cleaned) {
        absPrefix = cleaned
    } else {
        absPrefix = filepath.Join(rootAbs, cleaned)
    }

    sep := string(filepath.Separator)
    if strings.HasSuffix(prefix, sep) || strings.HasSuffix(prefix, "/") {
        if !isSubpath(rootAbs, absPrefix) {
            return nil, errors.New("path is outside MEDIA_ROOT")
        }
        return listDir(absPrefix, "", limit)
    }

    dir := filepath.Dir(absPrefix)
    base := filepath.Base(absPrefix)
    if !isSubpath(rootAbs, dir) {
        return nil, errors.New("path is outside MEDIA_ROOT")
    }
    return listDir(dir, base, limit)
}

func listDir(dir, base string, limit int) ([]string, error) {
    entries, err := os.ReadDir(dir)
    if err != nil {
        return nil, err
    }

    baseLower := strings.ToLower(base)
    items := make([]string, 0, len(entries))
    for _, entry := range entries {
        name := entry.Name()
        if baseLower != "" && !strings.HasPrefix(strings.ToLower(name), baseLower) {
            continue
        }
        full := filepath.Join(dir, name)
        if entry.IsDir() {
            full += string(filepath.Separator)
        }
        items = append(items, full)
        if limit > 0 && len(items) >= limit {
            break
        }
    }

    return items, nil
}

func isSubpath(root, path string) bool {
    rel, err := filepath.Rel(root, path)
    if err != nil {
        return false
    }
    return rel == "." || (!strings.HasPrefix(rel, ".."+string(filepath.Separator)) && rel != "..")
}
