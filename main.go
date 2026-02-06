package main

import (
    "archive/zip"
    "bytes"
    "context"
	"embed"
	"encoding/base64"
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
	"unicode/utf8"
	"time"
)

const (
	defaultPort    = "8080"
	defaultRoot    = "/media"
	maxUploadBytes = int64(8 << 30)
	maxMemoryBytes = int64(32 << 20)
	maxSuggestions = 200
	mountTimeout   = 30 * time.Second
	umountTimeout  = 30 * time.Second
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
	mux.HandleFunc("/api/bdinfo", bdinfoHandler("BDINFO_BIN", "bdinfo"))
	mux.HandleFunc("/api/screenshots", screenshotsHandler)
	mux.HandleFunc("/api/path", pathSuggestHandler)

    server := &http.Server{
        Addr:    ":" + port,
        Handler: logging(authenticate(mux)),
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

func resolveScreenshotSource(ctx context.Context, input string) (string, func(), error) {
    info, err := os.Stat(input)
    if err != nil {
        return "", noop, err
    }

	if !info.IsDir() {
		if isISOFile(input) {
			return resolveM2TSFromMountedISO(ctx, input)
		}
		return input, noop, nil
	}

	if bdmvRoot, ok := resolveBDMVRoot(input); ok {
		m2ts, err := findLargestM2TS(bdmvRoot)
		if err != nil {
			return "", noop, err
		}
		return m2ts, noop, nil
	}

	isoPath, err := findISOInDir(input)
	if err == nil {
		return resolveM2TSFromMountedISO(ctx, isoPath)
	}
    if !errors.Is(err, errNoISO) {
        return "", noop, err
    }

	return "", noop, errors.New("path does not contain BDMV or BDISO content")
}

func resolveBDInfoSource(ctx context.Context, input string) (string, func(), error) {
	info, err := os.Stat(input)
	if err != nil {
		return "", noop, err
	}

	if !info.IsDir() {
		if isISOFile(input) {
			return resolveBDInfoFromMountedISO(ctx, input)
		}
		return input, noop, nil
	}

	if bdmvRoot, ok := resolveBDInfoRoot(input); ok {
		return bdmvRoot, noop, nil
	}

	isoPath, err := findISOInDir(input)
	if err == nil {
		return resolveBDInfoFromMountedISO(ctx, isoPath)
	}
	if !errors.Is(err, errNoISO) {
		return "", noop, err
	}

	return "", noop, errors.New("path does not contain BDMV or BDISO content")
}

func resolveBDInfoRoot(path string) (string, bool) {
	base := filepath.Base(path)
	if strings.EqualFold(base, "BDMV") {
		return path, true
	}
	if strings.EqualFold(base, "STREAM") {
		return filepath.Dir(path), true
	}
	bdmv := filepath.Join(path, "BDMV")
	if info, err := os.Stat(bdmv); err == nil && info.IsDir() {
		return bdmv, true
	}
	return "", false
}

func resolveBDMVRoot(path string) (string, bool) {
    base := filepath.Base(path)
    if strings.EqualFold(base, "BDMV") {
        return path, true
    }
    if strings.EqualFold(base, "STREAM") {
        return path, true
    }
    bdmv := filepath.Join(path, "BDMV")
    if info, err := os.Stat(bdmv); err == nil && info.IsDir() {
        return bdmv, true
    }
    return "", false
}

func isISOFile(path string) bool {
    return strings.EqualFold(filepath.Ext(path), ".iso")
}

var errNoISO = errors.New("no iso found")
var errISOFound = errors.New("iso found")

func findISOInDir(root string) (string, error) {
    var isoPath string
    err := filepath.WalkDir(root, func(path string, d fs.DirEntry, err error) error {
        if err != nil {
            return err
        }
        if d.IsDir() {
            return nil
        }
        if isISOFile(path) {
            isoPath = path
            return errISOFound
        }
        return nil
    })
    if err != nil && !errors.Is(err, errISOFound) {
        return "", err
    }
    if isoPath == "" {
        return "", errNoISO
    }
    return isoPath, nil
}

func findLargestM2TS(root string) (string, error) {
    searchRoot := root
    stream := filepath.Join(root, "STREAM")
    if info, err := os.Stat(stream); err == nil && info.IsDir() {
        searchRoot = stream
    }

    var largestPath string
    var largestSize int64

    err := filepath.WalkDir(searchRoot, func(path string, d fs.DirEntry, err error) error {
        if err != nil {
            return err
        }
        if d.IsDir() {
            return nil
        }
        if !strings.EqualFold(filepath.Ext(path), ".m2ts") {
            return nil
        }
        info, err := d.Info()
        if err != nil {
            return err
        }
        if info.Size() > largestSize {
            largestSize = info.Size()
            largestPath = path
        }
        return nil
    })
    if err != nil {
        return "", err
    }
    if largestPath == "" {
        return "", errors.New("no m2ts files found under BDMV")
    }
    return largestPath, nil
}

func resolveM2TSFromMountedISO(ctx context.Context, isoPath string) (string, func(), error) {
    mountDir, cleanup, err := mountISO(ctx, isoPath)
    if err != nil {
        return "", noop, err
    }

    bdmvRoot, ok := resolveBDMVRoot(mountDir)
    if !ok {
        cleanup()
        return "", noop, errors.New("BDMV folder not found in ISO")
    }

    m2ts, err := findLargestM2TS(bdmvRoot)
    if err != nil {
        cleanup()
        return "", noop, err
    }

    return m2ts, cleanup, nil
}

func resolveBDInfoFromMountedISO(ctx context.Context, isoPath string) (string, func(), error) {
    mountDir, cleanup, err := mountISO(ctx, isoPath)
    if err != nil {
        return "", noop, err
    }

    if _, ok := resolveBDInfoRoot(mountDir); !ok {
        cleanup()
        return "", noop, errors.New("BDMV folder not found in ISO")
    }

    return mountDir, cleanup, nil
}

func mountISO(ctx context.Context, isoPath string) (string, func(), error) {
    mountBin, err := resolveBin("MOUNT_BIN", "mount")
    if err != nil {
        return "", noop, err
    }
    umountBin, err := resolveBin("UMOUNT_BIN", "umount")
    if err != nil {
        return "", noop, err
    }

    mountDir, err := os.MkdirTemp("", "minfo-iso-mount-*")
    if err != nil {
        return "", noop, err
    }

    mountCtx, cancel := context.WithTimeout(ctx, mountTimeout)
    defer cancel()

    _, stderr, err := runCommand(mountCtx, mountBin, "-o", "loop,ro", isoPath, mountDir)
    if err != nil {
        _ = os.RemoveAll(mountDir)
        msg := strings.TrimSpace(stderr)
        if msg == "" {
            msg = err.Error()
        }
        return "", noop, fmt.Errorf("mount iso failed: %s", msg)
    }

    cleanup := func() {
        umountCtx, cancel := context.WithTimeout(context.Background(), umountTimeout)
        defer cancel()
        if _, _, err := runCommand(umountCtx, umountBin, mountDir); err != nil {
            _, _, _ = runCommand(umountCtx, umountBin, "-l", mountDir)
        }
        _ = os.RemoveAll(mountDir)
    }

    return mountDir, cleanup, nil
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

func authenticate(next http.Handler) http.Handler {
    password := strings.TrimSpace(os.Getenv("WEB_PASSWORD"))
    if password == "" {
        return next
    }

    return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
        user, pass, ok := parseBasicAuth(r.Header.Get("Authorization"))
        if !ok || pass != password {
            _ = user
            w.Header().Set("WWW-Authenticate", "Basic realm=\"minfo\"")
            writeError(w, http.StatusUnauthorized, "unauthorized")
            return
        }
        next.ServeHTTP(w, r)
    })
}

func parseBasicAuth(header string) (string, string, bool) {
    const prefix = "Basic "
    if !strings.HasPrefix(header, prefix) {
        return "", "", false
    }
    encoded := strings.TrimSpace(header[len(prefix):])
    if encoded == "" {
        return "", "", false
    }
    decoded, err := decodeBase64(encoded)
    if err != nil {
        return "", "", false
    }
    parts := strings.SplitN(decoded, ":", 2)
    if len(parts) != 2 {
        return "", "", false
    }
    return parts[0], parts[1], true
}

func decodeBase64(value string) (string, error) {
    data, err := base64Decode(value)
    if err != nil {
        return "", err
    }
    if !utf8.Valid(data) {
        return "", errors.New("invalid encoding")
    }
    return string(data), nil
}

func base64Decode(value string) ([]byte, error) {
    buf := make([]byte, base64.StdEncoding.DecodedLen(len(value)))
    n, err := base64.StdEncoding.Decode(buf, []byte(value))
    if err != nil {
        return nil, err
    }
    return buf[:n], nil
}
