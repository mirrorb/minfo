package main

import (
    "archive/zip"
    "bytes"
    "context"
    "encoding/base64"
    "encoding/json"
    "errors"
    "fmt"
    "io"
    "log"
    "net/http"
    "os"
    "os/exec"
    "path/filepath"
    "strings"
    "time"
    "unicode/utf8"
)

const (
    defaultPort           = "8080"
    defaultRoot           = "/media"
    maxUploadBytes        = int64(8 << 30)
    maxMemoryBytes        = int64(32 << 20)
    maxSuggestions        = 200
    mountTimeout          = 30 * time.Second
    umountTimeout         = 30 * time.Second
    defaultRequestTimeout = 10 * time.Minute
)

var (
    requestTimeout = durationFromEnv("REQUEST_TIMEOUT", defaultRequestTimeout)
)

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

func noop() {}

func getenv(key, fallback string) string {
    value := strings.TrimSpace(os.Getenv(key))
    if value == "" {
        return fallback
    }
    return value
}

func durationFromEnv(key string, fallback time.Duration) time.Duration {
    value := strings.TrimSpace(os.Getenv(key))
    if value == "" {
        return fallback
    }

    duration, err := time.ParseDuration(value)
    if err != nil || duration <= 0 {
        log.Printf("invalid %s=%q; fallback to %s", key, value, fallback)
        return fallback
    }
    return duration
}

func mediaRoot() string {
    return getenv("MEDIA_ROOT", defaultRoot)
}

func logging(next http.Handler) http.Handler {
    return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
        start := time.Now()
        next.ServeHTTP(w, r)
        log.Printf("%s %s %s", r.Method, r.URL.Path, time.Since(start))
    })
}

func authenticate(next http.Handler) http.Handler {
    password := getenv("WEB_PASSWORD", "")
    if password == "" {
        return next
    }

    return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
        _, pass, ok := parseBasicAuth(r.Header.Get("Authorization"))
        if !ok || pass != password {
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

func runCommand(ctx context.Context, bin string, args ...string) (string, string, error) {
    cmd := exec.Command(bin, args...)
    setCommandProcessGroup(cmd)

    var stdout bytes.Buffer
    var stderr bytes.Buffer
    cmd.Stdout = &stdout
    cmd.Stderr = &stderr

    if err := cmd.Start(); err != nil {
        return stdout.String(), stderr.String(), err
    }

    waitCh := make(chan error, 1)
    go func() {
        waitCh <- cmd.Wait()
    }()

    select {
    case err := <-waitCh:
        return stdout.String(), stderr.String(), err
    case <-ctx.Done():
        killCommandProcessGroup(cmd)
        <-waitCh
        return stdout.String(), stderr.String(), ctx.Err()
    }
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

