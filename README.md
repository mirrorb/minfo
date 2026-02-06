# minfo

Local web UI to generate MediaInfo or BDInfo, plus a 4-shot screenshot download.

## Docker Compose (one-click)
```powershell
docker compose up --build
```
Then open http://localhost:8080

If you want to use the "server path" input, mount a host folder:
```yaml
services:
  minfo:
    volumes:
      - /path/to/media:/media:ro
```
Then use `/media/...` in the UI.

The container image downloads and includes:
- MediaInfo CLI
- FFmpeg + FFprobe
- BDInfo v0.8.0.1b (Linux prebuilt)

Optional env overrides:
- BDINFO_ARGS (defaults to `-w`)
- MEDIA_ROOT (root path used by server-path autocomplete, default: `/media`)
- WEB_PASSWORD (enable Basic Auth for the web UI)

## Requirements (local run)
- MediaInfo CLI
- BDInfo CLI
- FFmpeg and FFprobe

If the binaries are not on PATH, set these environment variables:
- MEDIAINFO_BIN
- BDINFO_BIN
- FFMPEG_BIN
- FFPROBE_BIN

## Run
```powershell
go run .
```
Then open http://localhost:8080

## Build (Linux x64 / arm64)
```bash
# Linux x64
GOOS=linux GOARCH=amd64 go build -o bin/minfo

# Linux arm64
GOOS=linux GOARCH=arm64 go build -o bin/minfo-arm64
```

## Notes
- Uploads are saved to a temporary file and removed after each request.
- For very large files or disc folders, use the server path input.
