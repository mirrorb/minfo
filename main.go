package main

import (
    "archive/zip"
    "bytes"
    "context"
    "embed"
    "encoding/json"
    "errors"
    "fmt"
    "io"
    "io/fs"
    "log"
    "net/http"
    "os"
    "os/exec"
    "path/filepath"
    "strconv"
    "strings"
    "time"
)

const (
    defaultPort    = "8080"
    defaultRoot    = "/media"
    maxUploadBytes = int64(8 << 30)
    maxMemoryBytes = int64(32 << 20)
    maxSuggestions = 200
    infoTimeout    = 3 * time.Minute
    shotTimeout    = 10 * time.Minute
)

//go:embed static/*
var staticFS embed.FS

type infoResponse struct {
    OK     bool   `json:"ok"`
    Output string `json:"output,omitempty"`
    Error  string `json:"error,omitempty"`
}

type pathResponse struct {
    OK    bool     `json:"ok"`
    Root  string   `json:"root,omitempty"`
    Items []string `json:"items,omitempty"`
    Error string   `json:"error,omitempty"`
}

func main() {
    port := getenv("PORT", defaultPort)

    sub, err := fs.Sub(staticFS, "static")
    if err != nil {
        log.Fatalf("failed to load static assets: %v", err)
    }

    mux := http.NewServeMux()
    mux.Handle("/", http.FileServer(http.FS(sub)))
    mux.HandleFunc("/api/mediainfo", infoHandler("MEDIAINFO_BIN", "mediainfo"))
    mux.HandleFunc("/api/bdinfo", infoHandler("BDINFO_BIN", "bdinfo"))
    mux.HandleFunc("/api/screenshots", screenshotsHandler)
    mux.HandleFunc("/api/path", pathSuggestHandler)

    server := &http.Server{
        Addr:    ":" + port,
        Handler: logging(mux),
    }

    log.Printf("minfo listening on http://localhost:%s", port)
    log.Fatal(server.ListenAndServe())
}

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

    duration, err := probeDuration(ctx, ffprobe, path)
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
        if err := captureShot(ctx, ffmpeg, path, ts, outPath); err != nil {
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

func parseForm(w http.ResponseWriter, r *http.Request) error {
    r.Body = http.MaxBytesReader(w, r.Body, maxUploadBytes)
    if err := r.ParseMultipartForm(maxMemoryBytes); err != nil {
        return err
    }
    return nil
}

func cleanupMultipart(r *http.Request) {
    if r.MultipartForm != nil {
        _ = r.MultipartForm.RemoveAll()
    }
}

func inputPath(r *http.Request) (string, func(), error) {
    path := strings.TrimSpace(r.FormValue("path"))
    path = strings.Trim(path, "\"")
    if path != "" {
        path = filepath.Clean(path)
        if _, err := os.Stat(path); err != nil {
            return "", noop, fmt.Errorf("path not found: %v", err)
        }
        return path, noop, nil
    }

    file, header, err := r.FormFile("file")
    if err != nil {
        return "", noop, errors.New("missing file or path")
    }
    defer file.Close()

    ext := filepath.Ext(header.Filename)
    tempFile, err := os.CreateTemp("", "minfo-*"+ext)
    if err != nil {
        return "", noop, err
    }

    if _, err := io.Copy(tempFile, file); err != nil {
        tempFile.Close()
        _ = os.Remove(tempFile.Name())
        return "", noop, err
    }
    if err := tempFile.Close(); err != nil {
        _ = os.Remove(tempFile.Name())
        return "", noop, err
    }

    cleanup := func() {
        _ = os.Remove(tempFile.Name())
    }

    return tempFile.Name(), cleanup, nil
}

func resolveBin(envKey, fallback string) (string, error) {
    bin := strings.TrimSpace(os.Getenv(envKey))
    if bin == "" {
        bin = fallback
    }
    if _, err := exec.LookPath(bin); err != nil {
        return "", fmt.Errorf("%s not found; set %s or add to PATH", bin, envKey)
    }
    return bin, nil
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
    positions := []float64{0.1, 0.3, 0.5, 0.7}
    ts := make([]float64, 0, len(positions))
    for i, p := range positions {
        t := duration * p
        if duration < 1 {
            t = duration * (float64(i+1) / float64(len(positions)+1))
        }
        maxT := duration - 0.2
        if maxT < 0 {
            maxT = duration
        }
        if t > maxT {
            t = maxT
        }
        if t < 0 {
            t = 0
        }
        ts = append(ts, t)
    }
    return ts
}

func zipFiles(paths []string) ([]byte, error) {
    var buf bytes.Buffer
    zw := zip.NewWriter(&buf)

    for _, path := range paths {
        file, err := os.Open(path)
        if err != nil {
            _ = zw.Close()
            return nil, err
        }
        info, err := file.Stat()
        if err != nil {
            file.Close()
            _ = zw.Close()
            return nil, err
        }
        header, err := zip.FileInfoHeader(info)
        if err != nil {
            file.Close()
            _ = zw.Close()
            return nil, err
        }
        header.Name = filepath.Base(path)
        writer, err := zw.CreateHeader(header)
        if err != nil {
            file.Close()
            _ = zw.Close()
            return nil, err
        }
        if _, err := io.Copy(writer, file); err != nil {
            file.Close()
            _ = zw.Close()
            return nil, err
        }
        file.Close()
    }

    if err := zw.Close(); err != nil {
        return nil, err
    }
    return buf.Bytes(), nil
}

func runCommand(ctx context.Context, bin string, args ...string) (string, string, error) {
    cmd := exec.CommandContext(ctx, bin, args...)
    var stdout bytes.Buffer
    var stderr bytes.Buffer
    cmd.Stdout = &stdout
    cmd.Stderr = &stderr
    err := cmd.Run()
    return stdout.String(), stderr.String(), err
}

func ensurePost(w http.ResponseWriter, r *http.Request) bool {
    if r.Method != http.MethodPost {
        writeError(w, http.StatusMethodNotAllowed, "method not allowed")
        return false
    }
    return true
}

func writeJSON(w http.ResponseWriter, status int, payload infoResponse) {
    w.Header().Set("Content-Type", "application/json")
    w.WriteHeader(status)
    _ = json.NewEncoder(w).Encode(payload)
}

func writeError(w http.ResponseWriter, status int, msg string) {
    writeJSON(w, status, infoResponse{OK: false, Error: msg})
}

func writePathJSON(w http.ResponseWriter, status int, payload pathResponse) {
    w.Header().Set("Content-Type", "application/json")
    w.WriteHeader(status)
    _ = json.NewEncoder(w).Encode(payload)
}

func writePathError(w http.ResponseWriter, status int, msg string) {
    writePathJSON(w, status, pathResponse{OK: false, Error: msg})
}

func bestErrorMessage(err error, stderr, stdout string) string {
    msg := strings.TrimSpace(stderr)
    if msg == "" {
        msg = err.Error()
    }
    if strings.TrimSpace(stdout) != "" {
        msg += "\n\n" + strings.TrimSpace(stdout)
    }
    return msg
}

func logging(next http.Handler) http.Handler {
    return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
        start := time.Now()
        next.ServeHTTP(w, r)
        log.Printf("%s %s %s", r.Method, r.URL.Path, time.Since(start))
    })
}

func getenv(key, fallback string) string {
    value := strings.TrimSpace(os.Getenv(key))
    if value == "" {
        return fallback
    }
    return value
}

func mediaRoot() string {
    root := strings.TrimSpace(os.Getenv("MEDIA_ROOT"))
    if root == "" {
        return defaultRoot
    }
    return root
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

func noop() {}
